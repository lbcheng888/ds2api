package openai

import (
	"ds2api/internal/toolcall"
	"net/http"
	"strings"

	"ds2api/internal/config"
	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/util"
)

type responsesStreamRuntime struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	canFlush bool

	sessionID           string
	responseID          string
	model               string
	finalPrompt         string
	toolNames           []string
	traceID             string
	toolChoice          util.ToolChoicePolicy
	allowMetaAgentTools bool

	thinkingEnabled       bool
	searchEnabled         bool
	stripReferenceMarkers bool

	bufferToolContent    bool
	emitEarlyToolDeltas  bool
	bufferedToolMaxBytes int
	toolCallsEmitted     bool
	toolCallsDoneEmitted bool

	sieve             toolStreamSieveState
	thinking          strings.Builder
	text              strings.Builder
	visibleText       strings.Builder
	streamToolCallIDs map[int]string
	functionItemIDs   map[int]string
	functionOutputIDs map[int]int
	functionArgs      map[int]string
	functionDone      map[int]bool
	functionAdded     map[int]bool
	functionNames     map[int]string
	messageItemID     string
	messageOutputID   int
	nextOutputID      int
	messageAdded      bool
	messagePartAdded  bool
	sequence          int
	failed            bool

	persistResponse func(obj map[string]any)
}

func newResponsesStreamRuntime(
	w http.ResponseWriter,
	rc *http.ResponseController,
	canFlush bool,
	sessionID string,
	responseID string,
	model string,
	finalPrompt string,
	thinkingEnabled bool,
	searchEnabled bool,
	stripReferenceMarkers bool,
	toolNames []string,
	bufferToolContent bool,
	emitEarlyToolDeltas bool,
	bufferedToolMaxBytes int,
	toolChoice util.ToolChoicePolicy,
	allowMetaAgentTools bool,
	traceID string,
	persistResponse func(obj map[string]any),
) *responsesStreamRuntime {
	return &responsesStreamRuntime{
		w:                     w,
		rc:                    rc,
		canFlush:              canFlush,
		sessionID:             sessionID,
		responseID:            responseID,
		model:                 model,
		finalPrompt:           finalPrompt,
		thinkingEnabled:       thinkingEnabled,
		searchEnabled:         searchEnabled,
		stripReferenceMarkers: stripReferenceMarkers,
		toolNames:             toolNames,
		bufferToolContent:     bufferToolContent,
		emitEarlyToolDeltas:   emitEarlyToolDeltas,
		bufferedToolMaxBytes:  bufferedToolMaxBytes,
		streamToolCallIDs:     map[int]string{},
		functionItemIDs:       map[int]string{},
		functionOutputIDs:     map[int]int{},
		functionArgs:          map[int]string{},
		functionDone:          map[int]bool{},
		functionAdded:         map[int]bool{},
		functionNames:         map[int]string{},
		messageOutputID:       -1,
		toolChoice:            toolChoice,
		allowMetaAgentTools:   allowMetaAgentTools,
		traceID:               traceID,
		persistResponse:       persistResponse,
	}
}

func (s *responsesStreamRuntime) failResponse(message, code string) {
	s.failed = true
	capture := annotateFailureCaptureHeaders(s.w, s.sessionID)
	message = withFailureCaptureMessage(message, capture)
	failedResp := map[string]any{
		"id":          s.responseID,
		"type":        "response",
		"object":      "response",
		"model":       s.model,
		"status":      "failed",
		"output":      []any{},
		"output_text": "",
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
			"param":   nil,
		},
	}
	if s.persistResponse != nil {
		s.persistResponse(failedResp)
	}
	s.sendEvent("response.failed", openaifmt.BuildResponsesFailedPayload(s.responseID, s.model, message, code))
	s.sendDone()
}

func (s *responsesStreamRuntime) finalize() {
	if s.failed {
		return
	}
	finalThinking := s.thinking.String()
	finalText := cleanVisibleOutput(s.text.String(), s.stripReferenceMarkers)
	if repaired := synthesizeTaskOutputToolCallTextFromAgentWaiting(s.finalPrompt, finalText, s.toolNames, s.allowMetaAgentTools); repaired != "" {
		finalText = repaired
	}
	if strings.TrimSpace(finalText) == "" {
		if repaired := synthesizeTaskOutputToolCallTextFromTaskNotification(s.finalPrompt, s.toolNames, s.allowMetaAgentTools); repaired != "" {
			finalText = repaired
		} else if promoted := executableToolCallTextFromThinking(finalThinking, s.toolNames, nil, s.allowMetaAgentTools); promoted != "" {
			finalText = promoted
		}
	}

	if s.bufferToolContent {
		s.processToolStreamEvents(flushToolSieveWithMeta(&s.sieve, s.toolNames, s.allowMetaAgentTools), false, true)
	}

	textParsed := toolcall.ParseStandaloneToolCallsDetailed(finalText, s.toolNames)
	if normalizedToolCallsExceedInputBytes(textParsed.Calls, nil, s.allowMetaAgentTools, s.bufferedToolMaxBytes) {
		_, message, code := toolCallTooLargeError()
		s.failResponse(message, code)
		return
	}
	detected := toolcall.NormalizeCallsForSchemasWithMeta(textParsed.Calls, nil, s.allowMetaAgentTools)
	s.logToolPolicyRejections(textParsed)

	if len(detected) > 0 {
		s.toolCallsEmitted = true
		if !s.toolCallsDoneEmitted {
			s.emitFunctionCallDoneEvents(detected)
		}
	}

	if s.toolChoice.IsRequired() && len(detected) == 0 {
		s.failResponse("tool_choice requires at least one valid tool call.", "tool_choice_violation")
		return
	}
	if len(detected) == 0 && !s.toolCallsEmitted {
		if _, message, code, ok := futureActionMissingToolCallDetail(finalText, s.toolNames, nil, s.allowMetaAgentTools); ok {
			s.failResponse(message, code)
			return
		}
		if strings.TrimSpace(s.visibleText.String()) == "" && strings.TrimSpace(finalText) != "" {
			s.emitTextDelta(finalText)
		}
	}
	s.closeMessageItem()

	if len(detected) == 0 && strings.TrimSpace(finalText) == "" {
		code := "upstream_empty_output"
		message := "Upstream model returned empty output."
		if finalThinking != "" {
			message = "Upstream model returned reasoning without visible output."
		}
		s.failResponse(message, code)
		return
	}
	s.closeIncompleteFunctionItems()

	obj := s.buildCompletedResponseObject(finalThinking, finalText, detected)
	if s.persistResponse != nil {
		s.persistResponse(obj)
	}
	s.sendEvent("response.completed", openaifmt.BuildResponsesCompletedPayload(obj))
	s.sendDone()
}

func (s *responsesStreamRuntime) logToolPolicyRejections(textParsed toolcall.ToolCallParseResult) {
	logRejected := func(parsed toolcall.ToolCallParseResult, channel string) {
		rejected := filteredRejectedToolNamesForLog(parsed.RejectedToolNames)
		if !parsed.RejectedByPolicy || len(rejected) == 0 {
			return
		}
		config.Logger.Warn(
			"[responses] rejected tool calls by policy",
			"trace_id", strings.TrimSpace(s.traceID),
			"channel", channel,
			"tool_choice_mode", s.toolChoice.Mode,
			"rejected_tool_names", strings.Join(rejected, ","),
		)
	}
	logRejected(textParsed, "text")
}

func (s *responsesStreamRuntime) onParsed(parsed sse.LineResult) streamengine.ParsedDecision {
	if !parsed.Parsed {
		return streamengine.ParsedDecision{}
	}
	if parsed.ContentFilter || parsed.ErrorMessage != "" || parsed.Stop {
		return streamengine.ParsedDecision{Stop: true}
	}

	contentSeen := false
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
		s.processToolStreamEvents(processToolSieveChunkWithMeta(&s.sieve, trimmed, s.toolNames, s.allowMetaAgentTools), false, true)
		if s.failed {
			return streamengine.ParsedDecision{Stop: true}
		}
		if s.bufferedToolMaxBytes > 0 && s.text.Len() > s.bufferedToolMaxBytes && !s.toolCallsEmitted {
			_, message, code := toolCallTooLargeError()
			s.failResponse(message, code)
			return streamengine.ParsedDecision{Stop: true}
		}
	}

	return streamengine.ParsedDecision{ContentSeen: contentSeen}
}
