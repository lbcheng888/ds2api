package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/promptcompat"
	"ds2api/internal/sse"
)

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if isVercelStreamReleaseRequest(r) {
		h.handleVercelStreamRelease(w, r)
		return
	}
	if isVercelStreamPowRequest(r) {
		h.handleVercelStreamPow(w, r)
		return
	}
	if isVercelStreamPrepareRequest(r) {
		h.handleVercelStreamPrepare(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, openAIGeneralMaxSize)
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			writeOpenAIError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	a, err := shared.DetermineWithAffinity(h.Auth, r, req)
	if err != nil {
		status := http.StatusUnauthorized
		detail := err.Error()
		if err == auth.ErrNoAccount {
			status = http.StatusTooManyRequests
		}
		writeOpenAIError(w, status, detail)
		return
	}
	var sessionID string
	defer func() {
		h.autoDeleteRemoteSession(r.Context(), a, sessionID)
		h.Auth.Release(a)
	}()

	r = r.WithContext(auth.WithAuth(r.Context(), a))

	if err := h.preprocessInlineFileInputs(r.Context(), a, req); err != nil {
		writeOpenAIInlineFileError(w, err)
		return
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, requestTraceID(r))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	stdReq, err = h.applyCurrentInputFile(r.Context(), a, stdReq)
	if err != nil {
		status, message := mapCurrentInputFileError(err)
		writeOpenAIError(w, status, message)
		return
	}
	historySession := startChatHistory(h.ChatHistory, r, a, stdReq)

	attempt, err := shared.CallCompletionWithManagedFailover(r.Context(), h.Auth, h.DS, h.Store, a, stdReq)
	if err != nil {
		status, message, code := shared.CompletionAttemptErrorDetail(a, attempt.Stage)
		if historySession != nil {
			historySession.error(status, message, code, "", "")
		}
		writeOpenAIErrorWithCode(w, status, message, code)
		return
	}
	sessionID = attempt.SessionID
	if stdReq.Stream {
		h.handleStreamWithRetry(w, r, a, attempt.Response, attempt.Payload, attempt.Pow, sessionID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames, stdReq.ToolSchemas, stdReq.AllowMetaAgentTools, historySession)
		return
	}
	h.handleNonStreamWithRetry(w, r.Context(), a, attempt.Response, attempt.Payload, attempt.Pow, sessionID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames, stdReq.ToolSchemas, stdReq.AllowMetaAgentTools, historySession)
}

func (h *Handler) autoDeleteRemoteSession(ctx context.Context, a *auth.RequestAuth, sessionID string) {
	mode := h.Store.AutoDeleteMode()
	if mode == "none" || a.DeepSeekToken == "" {
		return
	}

	deleteBaseCtx := context.WithoutCancel(ctx)
	deleteCtx, cancel := context.WithTimeout(deleteBaseCtx, 10*time.Second)
	defer cancel()

	switch mode {
	case "single":
		if sessionID == "" {
			config.Logger.Warn("[auto_delete_sessions] skipped single-session delete because session_id is empty", "account", a.AccountID)
			return
		}
		_, err := h.DS.DeleteSessionForToken(deleteCtx, a.DeepSeekToken, sessionID)
		if err != nil {
			config.Logger.Warn("[auto_delete_sessions] failed", "account", a.AccountID, "mode", mode, "session_id", sessionID, "error", err)
			return
		}
		config.Logger.Debug("[auto_delete_sessions] success", "account", a.AccountID, "mode", mode, "session_id", sessionID)
	case "all":
		if err := h.DS.DeleteAllSessionsForToken(deleteCtx, a.DeepSeekToken); err != nil {
			config.Logger.Warn("[auto_delete_sessions] failed", "account", a.AccountID, "mode", mode, "error", err)
			return
		}
		config.Logger.Debug("[auto_delete_sessions] success", "account", a.AccountID, "mode", mode)
	default:
		config.Logger.Warn("[auto_delete_sessions] unknown mode", "account", a.AccountID, "mode", mode)
	}
}

func (h *Handler) handleNonStream(w http.ResponseWriter, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string, historySession *chatHistorySession) {
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if historySession != nil {
			historySession.error(resp.StatusCode, string(body), "error", "", "")
		}
		writeOpenAIError(w, resp.StatusCode, string(body))
		return
	}
	result := sse.CollectStream(resp, thinkingEnabled, true)

	stripReferenceMarkers := h.compatStripReferenceMarkers()
	finalThinking := cleanVisibleOutput(result.Thinking, stripReferenceMarkers)
	finalToolDetectionThinking := cleanVisibleOutput(result.ToolDetectionThinking, stripReferenceMarkers)
	finalText := cleanVisibleOutput(result.Text, stripReferenceMarkers)
	if result.ErrorMessage != "" {
		status, message, code := upstreamStreamErrorDetail(result.ErrorCode, result.ErrorMessage)
		if historySession != nil {
			historySession.error(status, message, code, finalThinking, finalText)
		}
		writeOpenAIErrorWithCode(w, status, message, code)
		return
	}
	if searchEnabled {
		finalText = replaceCitationMarkersWithLinks(finalText, result.CitationLinks)
	}
	detected := detectAssistantToolCalls(finalText, finalThinking, finalToolDetectionThinking, toolNames)
	if status, message, code, ok := invalidTaskOutputCallDetail(detected.Calls, finalPrompt); ok {
		if historySession != nil {
			historySession.error(status, message, code, finalThinking, finalText)
		}
		writeOpenAIErrorWithCode(w, status, message, code)
		return
	}
	if len(detected.Calls) == 0 {
		if status, message, code, ok := missingToolCallDetail(finalText, finalPrompt, toolNames, nil, false); ok {
			if historySession != nil {
				historySession.error(status, message, code, finalThinking, finalText)
			}
			writeOpenAIErrorWithCode(w, status, message, code)
			return
		}
	}
	if shouldWriteUpstreamEmptyOutputError(finalText) && len(detected.Calls) == 0 {
		status, message, code := upstreamEmptyOutputDetail(result.ContentFilter, finalText, finalThinking)
		if historySession != nil {
			historySession.error(status, message, code, finalThinking, finalText)
		}
		writeUpstreamEmptyOutputError(w, finalText, finalThinking, result.ContentFilter)
		return
	}
	respBody := openaifmt.BuildChatCompletionWithToolCalls(completionID, model, finalPrompt, finalThinking, finalText, detected.Calls)
	finishReason := "stop"
	if choices, ok := respBody["choices"].([]map[string]any); ok && len(choices) > 0 {
		if fr, _ := choices[0]["finish_reason"].(string); strings.TrimSpace(fr) != "" {
			finishReason = fr
		}
	}
	if historySession != nil {
		historySession.success(http.StatusOK, finalThinking, finalText, finishReason, openaifmt.BuildChatUsage(finalPrompt, finalThinking, finalText))
	}
	writeJSON(w, http.StatusOK, respBody)
}
