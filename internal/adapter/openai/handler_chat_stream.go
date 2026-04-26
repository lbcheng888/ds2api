package openai

import (
	"io"
	"net/http"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/deepseek"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/toolcall"
	"ds2api/internal/util"
)

func (h *Handler) handleStreamWithRetry(w http.ResponseWriter, r *http.Request, a *auth.RequestAuth, resp *http.Response, completionID string, stdReq util.StandardRequest, historySession *chatHistorySession) string {
	currentCompletionID := completionID
	currentResp := resp
	allowProtocolRetry := h.shouldRetryStreamProtocolFailure(a)
	for attempt := 1; ; attempt++ {
		runtime := h.handleStreamAttempt(
			w,
			r,
			currentResp,
			currentCompletionID,
			stdReq.ResponseModel,
			stdReq.FinalPrompt,
			stdReq.Thinking,
			stdReq.Search,
			stdReq.ToolNames,
			stdReq.ToolSchemas,
			stdReq.ToolChoice,
			stdReq.AllowMetaAgentTools,
			stdReq.StreamIncludeUsage,
			allowProtocolRetry,
			historySession,
		)
		if runtime == nil || !allowProtocolRetry || !runtime.retryableProtocolFailure() {
			return currentCompletionID
		}
		h.autoDeleteRemoteSession(r.Context(), a, currentCompletionID)
		h.markManagedAccountFailure(a)
		config.Logger.Warn("[openai_stream] upstream protocol failure; retrying on another managed account", "attempt", attempt, "account", accountIDForLog(a), "session_id", currentCompletionID, "code", runtime.finalErrorCode)
		if attempt >= maxManagedCompletionAccountAttempts || !h.switchManagedAccount(r.Context(), a) {
			runtime.sendFailedChunk(runtime.finalErrorStatus, runtime.finalErrorMessage, runtime.finalErrorCode)
			if historySession != nil {
				historySession.error(runtime.finalErrorStatus, runtime.finalErrorMessage, runtime.finalErrorCode, runtime.thinking.String(), runtime.text.String())
			}
			return currentCompletionID
		}
		nextCompletionID, nextResp, stage, err := h.callCompletionWithFailover(r.Context(), a, stdReq)
		if err != nil {
			status, message, code := completionAttemptErrorDetail(a, stage)
			runtime.sendFailedChunk(status, message, code)
			if historySession != nil {
				historySession.error(status, message, code, runtime.thinking.String(), runtime.text.String())
			}
			return currentCompletionID
		}
		currentCompletionID = nextCompletionID
		currentResp = nextResp
	}
}

func (h *Handler) shouldRetryStreamProtocolFailure(a *auth.RequestAuth) bool {
	return a != nil && a.UseConfigToken
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string, toolSchemas toolcall.ParameterSchemas, toolChoice util.ToolChoicePolicy, allowMetaAgentTools bool, streamIncludeUsage bool, historySession *chatHistorySession) {
	h.handleStreamAttempt(w, r, resp, completionID, model, finalPrompt, thinkingEnabled, searchEnabled, toolNames, toolSchemas, toolChoice, allowMetaAgentTools, streamIncludeUsage, false, historySession)
}

func (h *Handler) handleStreamAttempt(w http.ResponseWriter, r *http.Request, resp *http.Response, completionID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string, toolSchemas toolcall.ParameterSchemas, toolChoice util.ToolChoicePolicy, allowMetaAgentTools bool, streamIncludeUsage bool, deferRetryableProtocolFailure bool, historySession *chatHistorySession) *chatStreamRuntime {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if historySession != nil {
			historySession.error(resp.StatusCode, string(body), "error", "", "")
		}
		writeOpenAIErrorWithCodeAndFailureCapture(w, resp.StatusCode, string(body), "", completionID)
		return nil
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
	streamRuntime.toolChoice = toolChoice
	streamRuntime.deferRetryableProtocolFailure = deferRetryableProtocolFailure

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
			if streamRuntime.deferRetryableProtocolFailure && streamRuntime.retryableProtocolFailure() {
				return
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
	return streamRuntime
}
