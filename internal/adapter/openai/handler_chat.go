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
	"ds2api/internal/deepseek"
	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/toolcall"
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
	stdReq, err := normalizeOpenAIChatRequest(h.Store, req, requestTraceID(r))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	stdReq, err = h.applyHistorySplit(r.Context(), a, stdReq)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	historySession := startChatHistory(h.ChatHistory, r, a, stdReq)

	var resp *http.Response
	var stage string
	sessionID, resp, stage, err = h.callCompletionWithFailover(r.Context(), a, stdReq)
	if err != nil {
		h.writeChatCompletionAttemptError(w, a, stage, historySession)
		return
	}
	if stdReq.Stream {
		h.handleStream(w, r, resp, sessionID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames, stdReq.ToolSchemas, stdReq.AllowMetaAgentTools, stdReq.StreamIncludeUsage, historySession)
		return
	}
	h.handleNonStream(w, resp, sessionID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames, stdReq.ToolSchemas, stdReq.AllowMetaAgentTools, historySession)
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

func (h *Handler) handleNonStream(w http.ResponseWriter, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string, toolSchemas toolcall.ParameterSchemas, allowMetaAgentTools bool, historySession *chatHistorySession) {
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

	stripReferenceMarkers := h.compatStripReferenceMarkers()
	finalThinking := cleanVisibleOutput(result.Thinking, stripReferenceMarkers)
	finalText := cleanVisibleOutput(result.Text, stripReferenceMarkers)
	if searchEnabled {
		finalText = replaceCitationMarkersWithLinks(finalText, result.CitationLinks)
	}
	if repaired := synthesizeTaskOutputToolCallTextFromAgentWaiting(finalPrompt, finalText, toolNames, allowMetaAgentTools); repaired != "" {
		finalText = repaired
	}
	if !result.ContentFilter && strings.TrimSpace(finalText) == "" {
		if repaired := synthesizeTaskOutputToolCallTextFromTaskNotification(finalPrompt, toolNames, allowMetaAgentTools); repaired != "" {
			finalText = repaired
		} else if promoted := executableToolCallTextFromThinking(finalThinking, toolNames, toolSchemas, allowMetaAgentTools); promoted != "" {
			finalText = promoted
		}
	}
	if shouldWriteUpstreamEmptyOutputError(finalText) {
		status, message, code := upstreamEmptyOutputDetail(result.ContentFilter, finalText, finalThinking)
		if historySession != nil {
			historySession.error(status, message, code, finalThinking, finalText)
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, status, message, code, completionID)
		return
	}
	if status, message, code, ok := futureActionMissingToolCallDetail(finalText, toolNames, toolSchemas, allowMetaAgentTools); ok {
		if historySession != nil {
			historySession.error(status, message, code, finalThinking, finalText)
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, status, message, code, completionID)
		return
	}
	detectedToolCalls := toolcall.ParseStandaloneToolCallsDetailed(finalText, toolNames)
	if normalizedToolCallsExceedInputBytes(detectedToolCalls.Calls, toolSchemas, allowMetaAgentTools, runtimeBufferedToolContentMaxBytes(h.Store)) {
		status, message, code := toolCallTooLargeError()
		if historySession != nil {
			historySession.error(status, message, code, finalThinking, finalText)
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, status, message, code, completionID)
		return
	}
	respBody := openaifmt.BuildChatCompletion(completionID, model, finalPrompt, finalThinking, finalText, toolNames, toolSchemas, allowMetaAgentTools)
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

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string, toolSchemas toolcall.ParameterSchemas, allowMetaAgentTools bool, streamIncludeUsage bool, historySession *chatHistorySession) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if historySession != nil {
			historySession.error(resp.StatusCode, string(body), "error", "", "")
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, resp.StatusCode, string(body), "", completionID)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)
	if !canFlush {
		config.Logger.Warn("[stream] response writer does not support flush; streaming may be buffered")
	}

	created := time.Now().Unix()
	bufferToolContent := len(toolNames) > 0
	emitEarlyToolDeltas := h.toolcallFeatureMatchEnabled() && h.toolcallEarlyEmitHighConfidence()
	stripReferenceMarkers := h.compatStripReferenceMarkers()
	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}

	streamRuntime := newChatStreamRuntime(
		w,
		rc,
		canFlush,
		completionID,
		created,
		model,
		finalPrompt,
		thinkingEnabled,
		searchEnabled,
		stripReferenceMarkers,
		toolNames,
		toolSchemas,
		allowMetaAgentTools,
		streamIncludeUsage,
		bufferToolContent,
		emitEarlyToolDeltas,
		runtimeBufferedToolContentMaxBytes(h.Store),
	)

	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         initialType,
		KeepAliveInterval:   time.Duration(deepseek.KeepAliveTimeout) * time.Second,
		IdleTimeout:         time.Duration(deepseek.StreamIdleTimeout) * time.Second,
		MaxDuration:         time.Duration(runtimeStreamMaxDurationSeconds(h.Store)) * time.Second,
		MaxKeepAliveNoInput: deepseek.MaxKeepaliveCount,
	}, streamengine.ConsumeHooks{
		OnKeepAlive: func() {
			streamRuntime.sendKeepAlive()
		},
		OnParsed: func(parsed sse.LineResult) streamengine.ParsedDecision {
			decision := streamRuntime.onParsed(parsed)
			if historySession != nil {
				historySession.progress(streamRuntime.thinking.String(), streamRuntime.text.String())
			}
			return decision
		},
		OnFinalize: func(reason streamengine.StopReason, _ error) {
			if reason == streamengine.StopReasonMaxDuration {
				status, message, code := http.StatusBadGateway, "Upstream stream exceeded max duration before completing.", "upstream_stream_timeout"
				streamRuntime.sendFailedChunk(status, message, code)
			} else if string(reason) == "content_filter" {
				streamRuntime.finalize("content_filter")
			} else {
				streamRuntime.finalize("stop")
			}
			if historySession == nil {
				return
			}
			if streamRuntime.finalErrorMessage != "" {
				historySession.error(streamRuntime.finalErrorStatus, streamRuntime.finalErrorMessage, streamRuntime.finalErrorCode, streamRuntime.thinking.String(), streamRuntime.text.String())
				return
			}
			historySession.success(http.StatusOK, streamRuntime.finalThinking, streamRuntime.finalText, streamRuntime.finalFinishReason, streamRuntime.finalUsage)
		},
		OnContextDone: func() {
			if historySession != nil {
				historySession.stopped(streamRuntime.thinking.String(), streamRuntime.text.String(), string(streamengine.StopReasonContextCancelled))
			}
		},
	})
}
