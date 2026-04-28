package openai

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
	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/protocol"
	"ds2api/internal/sse"
	"ds2api/internal/toolcall"
	"ds2api/internal/util"
)

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if isVercelStreamReleaseRequest(r) {
		h.handleVercelStreamRelease(w, r)
		return
	}
	if isVercelStreamPrepareRequest(r) {
		h.handleVercelStreamPrepare(w, r)
		return
	}

	a, err := h.Auth.Determine(r)
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
	if err := h.preprocessInlineFileInputs(r.Context(), a, req); err != nil {
		writeOpenAIInlineFileError(w, err)
		return
	}
	profile := protocol.DetectClientProfile(r, req)
	stdReq, err := normalizeOpenAIChatRequestWithProfile(h.Store, req, requestTraceID(r), profile)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if stdReq.ClientProfile != "" {
		w.Header().Set("X-Ds2-Client-Profile", stdReq.ClientProfile)
	}
	stdReq, err = h.applyHistorySplit(r.Context(), a, stdReq)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	setPromptCacheHeaders(w, stdReq.FinalPrompt)
	historySession := startChatHistory(h.ChatHistory, r, a, stdReq)

	var resp *http.Response
	var stage string
	sessionID, resp, stage, err = h.callCompletionWithFailover(r.Context(), a, stdReq)
	if err != nil {
		h.writeChatCompletionAttemptError(w, a, stage, historySession)
		return
	}
	if stdReq.Stream {
		sessionID = h.handleStreamWithRetry(w, r, a, resp, sessionID, stdReq, historySession)
		return
	}
	h.handleNonStream(w, resp, sessionID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames, stdReq.ToolSchemas, stdReq.ToolChoice, stdReq.AllowMetaAgentTools, a, historySession)
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

func (h *Handler) handleNonStream(w http.ResponseWriter, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string, toolSchemas toolcall.ParameterSchemas, toolChoice util.ToolChoicePolicy, allowMetaAgentTools bool, a *auth.RequestAuth, historySession *chatHistorySession) {
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if historySession != nil {
			historySession.error(resp.StatusCode, string(body), "error", "", "")
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, resp.StatusCode, string(body), "", completionID)
		return
	}
	result := sse.CollectStream(resp, thinkingEnabled, true)
	if result.ErrorMessage != "" {
		h.markManagedAccountFailure(a)
		code := strings.TrimSpace(result.ErrorCode)
		if code == "" {
			code = "upstream_error"
		}
		if historySession != nil {
			historySession.error(http.StatusBadGateway, result.ErrorMessage, code, "", "")
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, http.StatusBadGateway, result.ErrorMessage, code, completionID)
		return
	}

	stripReferenceMarkers := h.compatStripReferenceMarkers()
	finalThinking := cleanVisibleOutput(result.Thinking, stripReferenceMarkers)
	finalText := cleanVisibleOutput(result.Text, stripReferenceMarkers)
	if searchEnabled {
		finalText = replaceCitationMarkersWithLinks(finalText, result.CitationLinks)
	}
	evaluated := claudecodeharness.EvaluateFinalOutput(claudecodeharness.FinalEvaluationInput{
		FinalPrompt:         finalPrompt,
		Text:                finalText,
		Thinking:            finalThinking,
		ToolNames:           toolNames,
		ToolSchemas:         toolSchemas,
		AllowMetaAgentTools: allowMetaAgentTools,
		ContentFilter:       result.ContentFilter,
	})
	finalText = evaluated.Text
	if shouldWriteUpstreamEmptyOutputError(finalText) && len(evaluated.Calls) == 0 {
		h.markManagedAccountFailure(a)
		status, message, code := upstreamEmptyOutputDetail(result.ContentFilter, finalText, finalThinking)
		if historySession != nil {
			historySession.error(status, message, code, finalThinking, finalText)
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, status, message, code, completionID)
		return
	}
	if evaluated.MissingToolDecision.Blocked {
		h.markManagedAccountFailure(a)
		if historySession != nil {
			historySession.error(http.StatusBadGateway, evaluated.MissingToolDecision.Message, evaluated.MissingToolDecision.Code, finalThinking, finalText)
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, http.StatusBadGateway, evaluated.MissingToolDecision.Message, evaluated.MissingToolDecision.Code, completionID)
		return
	}
	if normalizedToolCallsExceedInputBytes(evaluated.Parsed.Calls, toolSchemas, allowMetaAgentTools, runtimeBufferedToolContentMaxBytes(h.Store)) {
		status, message, code := toolCallTooLargeError()
		if historySession != nil {
			historySession.error(status, message, code, finalThinking, finalText)
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, status, message, code, completionID)
		return
	}
	if toolChoice.IsRequired() && len(evaluated.Calls) == 0 {
		h.markManagedAccountFailure(a)
		if historySession != nil {
			historySession.error(http.StatusUnprocessableEntity, "tool_choice requires at least one valid tool call.", "tool_choice_violation", finalThinking, finalText)
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, http.StatusUnprocessableEntity, "tool_choice requires at least one valid tool call.", "tool_choice_violation", completionID)
		return
	}
	respBody := openaifmt.BuildChatCompletionFromToolCalls(completionID, model, finalPrompt, finalThinking, finalText, evaluated.Calls)
	finishReason := "stop"
	if choices, ok := respBody["choices"].([]map[string]any); ok && len(choices) > 0 {
		if fr, _ := choices[0]["finish_reason"].(string); strings.TrimSpace(fr) != "" {
			finishReason = fr
		}
	}
	if historySession != nil {
		usage, _ := respBody["usage"].(map[string]any)
		historySession.success(http.StatusOK, finalThinking, finalText, finishReason, usage)
	}
	writeJSON(w, http.StatusOK, respBody)
}
