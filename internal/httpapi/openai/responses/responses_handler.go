package responses

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	dsprotocol "ds2api/internal/deepseek/protocol"
	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/promptcompat"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/toolcall"
)

func (h *Handler) GetResponseByID(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.DetermineCaller(r)
	if err != nil {
		writeOpenAIError(w, http.StatusUnauthorized, err.Error())
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "response_id"))
	if id == "" {
		writeOpenAIError(w, http.StatusBadRequest, "response_id is required.")
		return
	}
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	st := h.getResponseStore()
	item, ok := st.get(owner, id)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "Response not found.")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
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
	defer h.Auth.Release(a)
	r = r.WithContext(auth.WithAuth(r.Context(), a))
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.preprocessInlineFileInputs(r.Context(), a, req); err != nil {
		writeOpenAIInlineFileError(w, err)
		return
	}
	traceID := requestTraceID(r)
	stdReq, err := promptcompat.NormalizeOpenAIResponsesRequest(h.Store, req, traceID)
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

	attempt, err := shared.CallCompletionWithManagedFailover(r.Context(), h.Auth, h.DS, h.Store, a, stdReq)
	if err != nil {
		status, message, code := shared.CompletionAttemptErrorDetail(a, attempt.Stage)
		writeOpenAIErrorWithCode(w, status, message, code)
		return
	}

	responseID := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if stdReq.Stream {
		h.handleResponsesStreamWithRetry(w, r, a, attempt.Response, attempt.Payload, attempt.Pow, owner, responseID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames, stdReq.ToolSchemas, stdReq.ToolChoice, stdReq.AllowMetaAgentTools, traceID)
		return
	}
	h.handleResponsesNonStreamWithRetry(w, r.Context(), a, attempt.Response, attempt.Payload, attempt.Pow, owner, responseID, stdReq.ResponseModel, stdReq.FinalPrompt, stdReq.Thinking, stdReq.Search, stdReq.ToolNames, stdReq.ToolSchemas, stdReq.ToolChoice, stdReq.AllowMetaAgentTools, traceID)
}

func (h *Handler) handleResponsesNonStream(w http.ResponseWriter, resp *http.Response, owner, responseID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string, toolChoice promptcompat.ToolChoicePolicy, traceID string) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	result := sse.CollectStream(resp, thinkingEnabled, true)
	stripReferenceMarkers := h.compatStripReferenceMarkers()
	sanitizedThinking := cleanVisibleOutput(result.Thinking, stripReferenceMarkers)
	toolDetectionThinking := cleanVisibleOutput(result.ToolDetectionThinking, stripReferenceMarkers)
	sanitizedText := cleanVisibleOutput(result.Text, stripReferenceMarkers)
	if result.ErrorMessage != "" {
		status, message, code := upstreamStreamErrorDetail(result.ErrorCode, result.ErrorMessage)
		writeOpenAIErrorWithCode(w, status, message, code)
		return
	}
	if searchEnabled {
		sanitizedText = replaceCitationMarkersWithLinks(sanitizedText, result.CitationLinks)
	}
	textParsed := detectAssistantToolCalls(sanitizedText, sanitizedThinking, toolDetectionThinking, toolNames)
	if status, message, code, ok := invalidTaskOutputCallDetail(textParsed.Calls, finalPrompt); ok {
		writeOpenAIErrorWithCode(w, status, message, code)
		return
	}
	if len(textParsed.Calls) == 0 {
		if status, message, code, ok := missingToolCallDetail(sanitizedText, finalPrompt, toolNames, nil, false); ok {
			writeOpenAIErrorWithCode(w, status, message, code)
			return
		}
	}
	if len(textParsed.Calls) == 0 && writeUpstreamEmptyOutputError(w, sanitizedText, sanitizedThinking, result.ContentFilter) {
		return
	}
	logResponsesToolPolicyRejection(traceID, toolChoice, textParsed, "text")

	callCount := len(textParsed.Calls)
	if toolChoice.IsRequired() && callCount == 0 {
		writeOpenAIErrorWithCode(w, http.StatusUnprocessableEntity, "tool_choice requires at least one valid tool call.", "tool_choice_violation")
		return
	}

	responseObj := openaifmt.BuildResponseObjectWithToolCalls(responseID, model, finalPrompt, sanitizedThinking, sanitizedText, textParsed.Calls)
	h.getResponseStore().put(owner, responseID, responseObj)
	writeJSON(w, http.StatusOK, responseObj)
}

func (h *Handler) handleResponsesStream(w http.ResponseWriter, r *http.Request, resp *http.Response, owner, responseID, model, finalPrompt string, thinkingEnabled, searchEnabled bool, toolNames []string, toolChoice promptcompat.ToolChoicePolicy, traceID string) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)

	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	bufferToolContent := len(toolNames) > 0
	emitEarlyToolDeltas := h.toolcallFeatureMatchEnabled() && h.toolcallEarlyEmitHighConfidence()
	stripReferenceMarkers := h.compatStripReferenceMarkers()

	streamRuntime := newResponsesStreamRuntime(
		w,
		rc,
		canFlush,
		responseID,
		model,
		finalPrompt,
		thinkingEnabled,
		searchEnabled,
		stripReferenceMarkers,
		toolNames,
		nil,
		false,
		bufferToolContent,
		emitEarlyToolDeltas,
		toolChoice,
		traceID,
		func(obj map[string]any) {
			h.getResponseStore().put(owner, responseID, obj)
		},
	)
	streamRuntime.sendCreated()

	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         initialType,
		KeepAliveInterval:   time.Duration(dsprotocol.KeepAliveTimeout) * time.Second,
		IdleTimeout:         time.Duration(dsprotocol.StreamIdleTimeout) * time.Second,
		MaxKeepAliveNoInput: dsprotocol.MaxKeepaliveCount,
	}, streamengine.ConsumeHooks{
		OnParsed: streamRuntime.onParsed,
		OnFinalize: func(reason streamengine.StopReason, _ error) {
			if string(reason) == "content_filter" {
				streamRuntime.finalize("content_filter", false)
				return
			}
			streamRuntime.finalize("stop", false)
		},
	})
}

func logResponsesToolPolicyRejection(traceID string, policy promptcompat.ToolChoicePolicy, parsed toolcall.ToolCallParseResult, channel string) {
	rejected := filteredRejectedToolNamesForLog(parsed.RejectedToolNames)
	if !parsed.RejectedByPolicy || len(rejected) == 0 {
		return
	}
	config.Logger.Warn(
		"[responses] rejected tool calls by policy",
		"trace_id", strings.TrimSpace(traceID),
		"channel", channel,
		"tool_choice_mode", policy.Mode,
		"rejected_tool_names", strings.Join(rejected, ","),
	)
}

func filteredRejectedToolNamesForLog(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		switch strings.ToLower(trimmed) {
		case "", "tool_name":
			continue
		default:
			out = append(out, trimmed)
		}
	}
	return out
}
