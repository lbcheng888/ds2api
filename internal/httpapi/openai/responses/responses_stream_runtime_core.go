package responses

import (
	"ds2api/internal/toolcall"
	"net/http"
	"strings"

	"ds2api/internal/config"
	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/promptcompat"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/toolstream"
)

type responsesStreamRuntime struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	canFlush bool

	responseID  string
	model       string
	finalPrompt string
	toolNames   []string
	toolSchemas toolcall.ParameterSchemas
	traceID     string
	toolChoice  promptcompat.ToolChoicePolicy

	thinkingEnabled       bool
	searchEnabled         bool
	stripReferenceMarkers bool
	allowMetaAgentTools   bool

	bufferToolContent    bool
	emitEarlyToolDeltas  bool
	toolCallsEmitted     bool
	toolCallsDoneEmitted bool

	sieve                 toolstream.State
	thinking              strings.Builder
	toolDetectionThinking strings.Builder
	text                  strings.Builder
	visibleText           strings.Builder
	responseMessageID     int
	streamToolCallIDs     map[int]string
	functionItemIDs       map[int]string
	functionOutputIDs     map[int]int
	functionArgs          map[int]string
	functionDone          map[int]bool
	functionAdded         map[int]bool
	functionNames         map[int]string
	messageItemID         string
	messageOutputID       int
	reasoningItemID       string
	reasoningOutputID     int
	nextOutputID          int
	messageAdded          bool
	messagePartAdded      bool
	reasoningAdded        bool
	sequence              int
	failed                bool
	finalErrorStatus      int
	finalErrorMessage     string
	finalErrorCode        string
	terminalSent          bool

	persistResponse func(obj map[string]any)
}

func newResponsesStreamRuntime(
	w http.ResponseWriter,
	rc *http.ResponseController,
	canFlush bool,
	responseID string,
	model string,
	finalPrompt string,
	thinkingEnabled bool,
	searchEnabled bool,
	stripReferenceMarkers bool,
	toolNames []string,
	toolSchemas toolcall.ParameterSchemas,
	allowMetaAgentTools bool,
	bufferToolContent bool,
	emitEarlyToolDeltas bool,
	toolChoice promptcompat.ToolChoicePolicy,
	traceID string,
	persistResponse func(obj map[string]any),
) *responsesStreamRuntime {
	return &responsesStreamRuntime{
		w:                     w,
		rc:                    rc,
		canFlush:              canFlush,
		responseID:            responseID,
		model:                 model,
		finalPrompt:           finalPrompt,
		thinkingEnabled:       thinkingEnabled,
		searchEnabled:         searchEnabled,
		stripReferenceMarkers: stripReferenceMarkers,
		toolNames:             toolNames,
		toolSchemas:           toolSchemas,
		allowMetaAgentTools:   allowMetaAgentTools,
		bufferToolContent:     bufferToolContent,
		emitEarlyToolDeltas:   emitEarlyToolDeltas,
		streamToolCallIDs:     map[int]string{},
		functionItemIDs:       map[int]string{},
		functionOutputIDs:     map[int]int{},
		functionArgs:          map[int]string{},
		functionDone:          map[int]bool{},
		functionAdded:         map[int]bool{},
		functionNames:         map[int]string{},
		messageOutputID:       -1,
		reasoningOutputID:     -1,
		toolChoice:            toolChoice,
		traceID:               traceID,
		persistResponse:       persistResponse,
	}
}

func (s *responsesStreamRuntime) failResponse(status int, message, code string) {
	if s.terminalSent {
		return
	}
	s.failed = true
	s.terminalSent = true
	s.finalErrorStatus = status
	s.finalErrorMessage = message
	s.finalErrorCode = code
	failedResp := map[string]any{
		"id":          s.responseID,
		"type":        "response",
		"object":      "response",
		"model":       s.model,
		"status":      "failed",
		"status_code": status,
		"output":      []any{},
		"output_text": "",
		"error": map[string]any{
			"message": message,
			"type":    openAIErrorType(status),
			"code":    code,
			"param":   nil,
		},
	}
	if s.persistResponse != nil {
		s.persistResponse(failedResp)
	}
	s.sendEvent("response.failed", openaifmt.BuildResponsesFailedPayload(s.responseID, s.model, status, message, code))
	s.sendDone()
}

func (s *responsesStreamRuntime) finalize(finishReason string, deferEmptyOutput bool) bool {
	if s.terminalSent {
		return true
	}
	s.failed = false
	s.finalErrorStatus = 0
	s.finalErrorMessage = ""
	s.finalErrorCode = ""
	finalThinking := s.thinking.String()
	finalToolDetectionThinking := s.toolDetectionThinking.String()
	finalText := cleanVisibleOutput(s.text.String(), s.stripReferenceMarkers)

	if s.bufferToolContent {
		s.processToolStreamEvents(toolstream.Flush(&s.sieve, s.toolNames), true, true)
		if s.failed {
			return true
		}
	}

	textParsed := detectAssistantToolCalls(finalText, finalThinking, finalToolDetectionThinking, s.toolNames)
	textParsed.Calls = toolcall.NormalizeCallsForSchemasWithMeta(textParsed.Calls, s.toolSchemas, s.allowMetaAgentTools)
	detected := textParsed.Calls
	if status, message, code, ok := invalidTaskOutputCallDetail(detected, s.finalPrompt); ok {
		s.failResponse(status, message, code)
		return true
	}
	if len(detected) == 0 {
		if status, message, code, ok := missingToolCallDetail(finalText, s.finalPrompt, s.toolNames, s.toolSchemas, s.allowMetaAgentTools); ok {
			if !s.bufferToolContent {
				// Non-buffered mode: text has already been emitted to the client.
				// We cannot fail the response. Log the detection and complete normally.
				config.Logger.Warn("[responses] missing tool call in non-buffered mode",
					"trace_id", strings.TrimSpace(s.traceID),
					"message", message,
				)
			} else if deferEmptyOutput && !s.messageAdded {
				s.finalErrorStatus = status
				s.finalErrorMessage = message
				s.finalErrorCode = code
				return false
			} else {
				s.failResponse(status, message, code)
				return true
			}
		}
	}

	if len(detected) > 0 {
		s.toolCallsEmitted = true
		if !s.toolCallsDoneEmitted {
			s.emitFunctionCallDoneEvents(detected)
		}
	}

	if len(detected) == 0 && strings.TrimSpace(finalText) != "" && !s.messageAdded {
		s.emitTextDelta(finalText)
	}
	s.closeMessageItem()

	if s.toolChoice.IsRequired() && len(detected) == 0 {
		s.failResponse(http.StatusUnprocessableEntity, "tool_choice requires at least one valid tool call.", "tool_choice_violation")
		return true
	}
	if len(detected) == 0 && strings.TrimSpace(finalText) == "" {
		status, message, code := upstreamEmptyOutputDetail(finishReason == "content_filter", finalText, finalThinking)
		if deferEmptyOutput {
			s.finalErrorStatus = status
			s.finalErrorMessage = message
			s.finalErrorCode = code
			return false
		}
		s.failResponse(status, message, code)
		return true
	}
	s.closeIncompleteFunctionItems()

	obj := s.buildCompletedResponseObject(finalThinking, finalText, detected)
	if s.persistResponse != nil {
		s.persistResponse(obj)
	}
	s.terminalSent = true
	s.sendEvent("response.completed", openaifmt.BuildResponsesCompletedPayload(obj))
	s.sendDone()
	return true
}


func (s *responsesStreamRuntime) onParsed(parsed sse.LineResult) streamengine.ParsedDecision {
	if !parsed.Parsed {
		return streamengine.ParsedDecision{}
	}
	if parsed.ResponseMessageID > 0 {
		s.responseMessageID = parsed.ResponseMessageID
	}
	if parsed.ContentFilter {
		return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReason("content_filter")}
	}
	if parsed.ErrorMessage != "" {
		status, message, code := upstreamStreamErrorDetail(parsed.ErrorCode, parsed.ErrorMessage)
		s.failResponse(status, message, code)
		return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReasonHandlerRequested}
	}
	if parsed.Stop {
		return streamengine.ParsedDecision{Stop: true}
	}

	contentSeen := false
	for _, p := range parsed.ToolDetectionThinkingParts {
		trimmed := sse.TrimContinuationOverlap(s.toolDetectionThinking.String(), p.Text)
		if trimmed != "" {
			s.toolDetectionThinking.WriteString(trimmed)
		}
	}
	for _, p := range parsed.Parts {
		cleanedText := cleanVisibleOutput(p.Text, s.stripReferenceMarkers)
		if cleanedText == "" {
			continue
		}
		if p.Type != "thinking" && s.searchEnabled && sse.IsCitation(cleanedText) {
			continue
		}
		contentSeen = true
		if p.Type == "thinking" {
			if !s.thinkingEnabled {
				continue
			}
			trimmed := sse.TrimContinuationOverlap(s.thinking.String(), cleanedText)
			if trimmed == "" {
				continue
			}
			s.thinking.WriteString(trimmed)
			s.reasoningAdded = true
			s.sendEvent("response.reasoning.delta", openaifmt.BuildResponsesReasoningDeltaPayload(s.responseID, trimmed))
			continue
		}

		trimmed := sse.TrimContinuationOverlap(s.text.String(), cleanedText)
		if trimmed == "" {
			continue
		}
		s.text.WriteString(trimmed)
		if !s.bufferToolContent {
			s.emitTextDelta(trimmed)
			continue
		}
		s.processToolStreamEvents(toolstream.ProcessChunk(&s.sieve, trimmed, s.toolNames), false, true)
		if s.failed {
			return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReasonHandlerRequested, ContentSeen: true}
		}
	}

	return streamengine.ParsedDecision{ContentSeen: contentSeen}
}
