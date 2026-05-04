package responses

import (
	"ds2api/internal/toolcall"
	"net/http"
	"strings"

	"ds2api/internal/config"
	openaifmt "ds2api/internal/format/openai"
	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/promptcompat"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/textclean"
	"ds2api/internal/toolstream"
)

type responsesStreamRuntime struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	canFlush bool

	responseID    string
	model         string
	finalPrompt   string
	refFileTokens int
	toolNames     []string
	toolsRaw      any
	traceID       string
	toolChoice    promptcompat.ToolChoicePolicy

	thinkingEnabled       bool
	searchEnabled         bool
	stripReferenceMarkers bool

	bufferToolContent    bool
	holdBufferedToolText bool
	emitEarlyToolDeltas  bool
	toolCallsEmitted     bool
	toolCallsDoneEmitted bool

	sieve                 toolstream.State
	rawThinking           strings.Builder
	thinking              strings.Builder
	toolDetectionThinking strings.Builder
	rawText               strings.Builder
	text                  strings.Builder
	visibleText           strings.Builder
	deferredToolText      strings.Builder
	outputSanitizer       textclean.StreamSanitizer
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
	nextOutputID          int
	messageAdded          bool
	messagePartAdded      bool
	sequence              int
	failed                bool
	finalErrorStatus      int
	finalErrorMessage     string
	finalErrorCode        string

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
	toolsRaw any,
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
		toolsRaw:              toolsRaw,
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
		toolChoice:            toolChoice,
		traceID:               traceID,
		persistResponse:       persistResponse,
	}
}

func (s *responsesStreamRuntime) failResponse(status int, message, code string) {
	s.failed = true
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

func (s *responsesStreamRuntime) markContextCancelled() {
	s.failed = true
	s.finalErrorStatus = 499
	s.finalErrorMessage = "request context cancelled"
	s.finalErrorCode = string(streamengine.StopReasonContextCancelled)
}

func (s *responsesStreamRuntime) finalize(finishReason string, deferEmptyOutput bool) bool {
	if s.failed && s.finalErrorMessage != "" {
		return true
	}
	s.failed = false
	s.finalErrorStatus = 0
	s.finalErrorMessage = ""
	s.finalErrorCode = ""
	if s.bufferToolContent {
		s.processToolStreamEvents(toolstream.Flush(&s.sieve, s.toolNames), true, true)
	}

	finalThinking := s.thinking.String()
	finalText := cleanVisibleOutput(s.text.String(), s.stripReferenceMarkers)
	evaluationThinking := s.rawThinking.String() + "\n" + s.toolDetectionThinking.String()
	evaluated := evaluateResponsesFinalOutput(s.finalPrompt, finalText, s.rawText.String(), evaluationThinking, false, s.toolNames, s.toolsRaw)
	textParsed := evaluated.Parsed
	detected := evaluated.Calls
	s.logToolPolicyRejections(textParsed)

	if evaluated.MissingToolDecision.Blocked {
		s.failResponse(http.StatusBadGateway, evaluated.MissingToolDecision.Message, evaluated.MissingToolDecision.Code)
		return true
	}
	if len(detected) > 0 {
		s.toolCallsEmitted = true
		if !s.toolCallsDoneEmitted {
			s.emitFunctionCallDoneEvents(detected)
		}
	}
	if s.toolChoice.IsRequired() && len(detected) == 0 {
		s.failResponse(http.StatusUnprocessableEntity, "tool_choice requires at least one valid tool call.", "tool_choice_violation")
		return true
	}
	if len(detected) == 0 && strings.TrimSpace(evaluated.Text) == "" {
		status, message, code := upstreamEmptyOutputDetail(finishReason == "content_filter", evaluated.Text, finalThinking)
		if deferEmptyOutput {
			s.finalErrorStatus = status
			s.finalErrorMessage = message
			s.finalErrorCode = code
			return false
		}
		s.failResponse(status, message, code)
		return true
	}
	if len(detected) == 0 && !s.toolCallsEmitted {
		s.flushDeferredToolText()
	}

	s.closeMessageItem()
	s.closeIncompleteFunctionItems()

	obj := s.buildCompletedResponseObject(finalThinking, evaluated.Text, detected)
	if s.persistResponse != nil {
		s.persistResponse(obj)
	}
	s.sendEvent("response.completed", openaifmt.BuildResponsesCompletedPayload(obj))
	s.sendDone()
	return true
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
	if parsed.ResponseMessageID > 0 {
		s.responseMessageID = parsed.ResponseMessageID
	}
	if parsed.ContentFilter || parsed.ErrorMessage != "" {
		return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReason("content_filter")}
	}
	if parsed.Stop {
		return streamengine.ParsedDecision{Stop: true}
	}

	contentSeen := false
	batch := responsesDeltaBatch{runtime: s}
	for _, p := range parsed.ToolDetectionThinkingParts {
		trimmed := sse.TrimContinuationOverlap(s.toolDetectionThinking.String(), p.Text)
		if trimmed != "" {
			s.toolDetectionThinking.WriteString(trimmed)
		}
	}
	for _, p := range parsed.Parts {
		if p.Type == "thinking" {
			rawTrimmed := sse.TrimContinuationOverlap(s.rawThinking.String(), p.Text)
			if rawTrimmed != "" {
				s.rawThinking.WriteString(rawTrimmed)
				contentSeen = true
			}
			if !s.thinkingEnabled {
				continue
			}
			cleanedText := cleanVisibleOutput(rawTrimmed, s.stripReferenceMarkers)
			if cleanedText == "" {
				continue
			}
			trimmed := sse.TrimContinuationOverlap(s.thinking.String(), cleanedText)
			if trimmed == "" {
				continue
			}
			s.thinking.WriteString(trimmed)
			batch.append("reasoning", trimmed)
			continue
		}

		rawTrimmed := sse.TrimContinuationOverlap(s.rawText.String(), p.Text)
		if rawTrimmed == "" {
			continue
		}
		s.rawText.WriteString(rawTrimmed)
		contentSeen = true
		cleanedText := cleanVisibleOutput(s.outputSanitizer.Sanitize(rawTrimmed), s.stripReferenceMarkers)
		if s.searchEnabled && sse.IsCitation(cleanedText) {
			continue
		}
		trimmed := sse.TrimContinuationOverlap(s.text.String(), cleanedText)
		if trimmed != "" {
			s.text.WriteString(trimmed)
		}
		if !s.bufferToolContent {
			if trimmed == "" {
				continue
			}
			batch.append("text", trimmed)
			continue
		}
		batch.flush()
		s.processToolStreamEvents(toolstream.ProcessChunk(&s.sieve, rawTrimmed, s.toolNames), true, true)
	}

	batch.flush()
	return streamengine.ParsedDecision{ContentSeen: contentSeen}
}

func (s *responsesStreamRuntime) shouldHoldBufferedToolContent() bool {
	if !s.bufferToolContent || s.toolCallsEmitted {
		return false
	}
	if s.holdBufferedToolText {
		return true
	}
	if claudecodeharness.IsToolRequiredTurn(claudecodeharness.ToolRequiredTurnInput{
		FinalPrompt:         s.finalPrompt,
		ToolNames:           s.toolNames,
		AllowMetaAgentTools: true,
	}) {
		s.holdBufferedToolText = true
		return true
	}
	detectionText := s.text.String()
	if rawText := strings.TrimSpace(s.rawText.String()); rawText != "" {
		detectionText = rawText
	}
	if !claudecodeharness.LooksLikeBufferedToolHoldCandidate(detectionText) {
		return false
	}
	decision := claudecodeharness.DetectMissingToolCallNoRecord(claudecodeharness.MissingToolCallInput{
		Text:                detectionText,
		FinalPrompt:         s.finalPrompt,
		ToolNames:           s.toolNames,
		ToolSchemas:         toolcall.ExtractParameterSchemas(s.toolsRaw),
		AllowMetaAgentTools: true,
		Profile:             "openai",
	})
	if decision.Blocked {
		s.holdBufferedToolText = true
		return true
	}
	return false
}

func (s *responsesStreamRuntime) shouldDeferBufferedToolText() bool {
	return s.bufferToolContent && !s.toolCallsEmitted && strings.Contains(s.finalPrompt, promptcompat.CurrentInputContextFilename)
}

func (s *responsesStreamRuntime) flushDeferredToolText() {
	if s.deferredToolText.Len() == 0 {
		return
	}
	s.emitTextDelta(s.deferredToolText.String())
	s.deferredToolText.Reset()
}
