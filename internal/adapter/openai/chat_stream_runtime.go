package openai

import (
	"ds2api/internal/toolcall"
	"ds2api/internal/util"
	"encoding/json"
	"net/http"
	"strings"

	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
)

type chatStreamRuntime struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	canFlush bool

	completionID        string
	created             int64
	model               string
	finalPrompt         string
	toolNames           []string
	toolSchemas         toolcall.ParameterSchemas
	toolChoice          util.ToolChoicePolicy
	allowMetaAgentTools bool

	thinkingEnabled       bool
	searchEnabled         bool
	stripReferenceMarkers bool

	firstChunkSent       bool
	visibleContentSent   bool
	streamIncludeUsage   bool
	bufferToolContent    bool
	emitEarlyToolDeltas  bool
	bufferedToolMaxBytes int
	toolCallsEmitted     bool
	toolCallsDoneEmitted bool

	toolSieve         toolStreamSieveState
	streamToolCallIDs map[int]string
	streamToolNames   map[int]string
	thinking          strings.Builder
	text              strings.Builder

	finalThinking     string
	finalText         string
	finalFinishReason string
	finalUsage        map[string]any
	finalErrorStatus  int
	finalErrorMessage string
	finalErrorCode    string

	deferEmptyOutputFailure bool
}

func newChatStreamRuntime(
	w http.ResponseWriter,
	rc *http.ResponseController,
	canFlush bool,
	completionID string,
	created int64,
	model string,
	finalPrompt string,
	thinkingEnabled bool,
	searchEnabled bool,
	stripReferenceMarkers bool,
	toolNames []string,
	toolSchemas toolcall.ParameterSchemas,
	allowMetaAgentTools bool,
	streamIncludeUsage bool,
	bufferToolContent bool,
	emitEarlyToolDeltas bool,
	bufferedToolMaxBytes int,
) *chatStreamRuntime {
	return &chatStreamRuntime{
		w:                     w,
		rc:                    rc,
		canFlush:              canFlush,
		completionID:          completionID,
		created:               created,
		model:                 model,
		finalPrompt:           finalPrompt,
		toolNames:             toolNames,
		toolSchemas:           toolSchemas,
		allowMetaAgentTools:   allowMetaAgentTools,
		thinkingEnabled:       thinkingEnabled,
		searchEnabled:         searchEnabled,
		stripReferenceMarkers: stripReferenceMarkers,
		streamIncludeUsage:    streamIncludeUsage,
		bufferToolContent:     bufferToolContent,
		emitEarlyToolDeltas:   emitEarlyToolDeltas,
		bufferedToolMaxBytes:  bufferedToolMaxBytes,
		streamToolCallIDs:     map[int]string{},
		streamToolNames:       map[int]string{},
	}
}

func (s *chatStreamRuntime) sendKeepAlive() {
	if !s.canFlush {
		return
	}
	_, _ = s.w.Write([]byte(": keep-alive\n\n"))
	_ = s.rc.Flush()
}

func (s *chatStreamRuntime) sendChunk(v any) {
	b, _ := json.Marshal(v)
	_, _ = s.w.Write([]byte("data: "))
	_, _ = s.w.Write(b)
	_, _ = s.w.Write([]byte("\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *chatStreamRuntime) sendDone() {
	_, _ = s.w.Write([]byte("data: [DONE]\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *chatStreamRuntime) sendFailedChunk(status int, message, code string) {
	capture := annotateFailureCaptureHeaders(s.w, s.completionID)
	message = withFailureCaptureMessage(message, capture)
	s.finalErrorStatus = status
	s.finalErrorMessage = message
	s.finalErrorCode = code
	s.sendChunk(map[string]any{
		"status_code": status,
		"error": map[string]any{
			"message": message,
			"type":    openAIErrorType(status),
			"code":    code,
			"param":   nil,
		},
	})
	s.sendDone()
}

func (s *chatStreamRuntime) resetStreamToolCallState() {
	s.streamToolCallIDs = map[int]string{}
	s.streamToolNames = map[int]string{}
}

func (s *chatStreamRuntime) finalize(finishReason string) {
	if s.finalErrorMessage != "" {
		return
	}
	finalThinking := s.thinking.String()
	finalText := cleanVisibleOutput(s.text.String(), s.stripReferenceMarkers)
	if repaired := synthesizeTaskOutputToolCallTextFromAgentWaiting(s.finalPrompt, finalText, s.toolNames, s.allowMetaAgentTools); repaired != "" {
		finalText = repaired
	}
	if finishReason != "content_filter" && strings.TrimSpace(finalText) == "" {
		if repaired := synthesizeTaskOutputToolCallTextFromTaskNotification(s.finalPrompt, s.toolNames, s.allowMetaAgentTools); repaired != "" {
			finalText = repaired
		} else if promoted := executableToolCallTextFromThinking(finalThinking, s.toolNames, s.toolSchemas, s.allowMetaAgentTools); promoted != "" {
			finalText = promoted
		}
	}
	s.finalThinking = finalThinking
	s.finalText = finalText
	detected := toolcall.ParseStandaloneToolCallsDetailed(finalText, s.toolNames)
	if normalizedToolCallsExceedInputBytes(detected.Calls, s.toolSchemas, s.allowMetaAgentTools, s.bufferedToolMaxBytes) {
		status, message, code := toolCallTooLargeError()
		s.sendFailedChunk(status, message, code)
		return
	}
	formattedDetected := formatFinalStreamToolCallsWithStableIDs(detected.Calls, s.streamToolCallIDs, s.toolSchemas, s.allowMetaAgentTools)
	if s.toolChoice.IsRequired() && len(formattedDetected) == 0 && !s.toolCallsEmitted {
		s.sendFailedChunk(http.StatusUnprocessableEntity, "tool_choice requires at least one valid tool call.", "tool_choice_violation")
		return
	}
	if len(formattedDetected) > 0 && !s.toolCallsDoneEmitted {
		finishReason = "tool_calls"
		delta := map[string]any{
			"tool_calls": formattedDetected,
		}
		if !s.firstChunkSent {
			delta["role"] = "assistant"
			s.firstChunkSent = true
		}
		s.sendChunk(openaifmt.BuildChatStreamChunk(
			s.completionID,
			s.created,
			s.model,
			[]map[string]any{openaifmt.BuildChatStreamDeltaChoice(0, delta)},
			nil,
		))
		s.toolCallsEmitted = true
		s.toolCallsDoneEmitted = true
	} else if s.bufferToolContent {
		for _, evt := range flushToolSieveWithMeta(&s.toolSieve, s.toolNames, s.allowMetaAgentTools) {
			if normalizedToolCallsExceedInputBytes(evt.ToolCalls, s.toolSchemas, s.allowMetaAgentTools, s.bufferedToolMaxBytes) {
				status, message, code := toolCallTooLargeError()
				s.sendFailedChunk(status, message, code)
				return
			}
			formattedToolCalls := formatFinalStreamToolCallsWithStableIDs(evt.ToolCalls, s.streamToolCallIDs, s.toolSchemas, s.allowMetaAgentTools)
			if len(formattedToolCalls) > 0 {
				finishReason = "tool_calls"
				s.toolCallsEmitted = true
				s.toolCallsDoneEmitted = true
				tcDelta := map[string]any{
					"tool_calls": formattedToolCalls,
				}
				if !s.firstChunkSent {
					tcDelta["role"] = "assistant"
					s.firstChunkSent = true
				}
				s.sendChunk(openaifmt.BuildChatStreamChunk(
					s.completionID,
					s.created,
					s.model,
					[]map[string]any{openaifmt.BuildChatStreamDeltaChoice(0, tcDelta)},
					nil,
				))
				s.resetStreamToolCallState()
			}
			if evt.Content != "" {
				continue
			}
		}
	}

	if len(formattedDetected) > 0 || s.toolCallsEmitted {
		finishReason = "tool_calls"
	}
	if len(formattedDetected) == 0 && !s.toolCallsEmitted {
		if status, message, code, ok := futureActionMissingToolCallDetail(finalText, s.toolNames, s.toolSchemas, s.allowMetaAgentTools); ok {
			s.sendFailedChunk(status, message, code)
			return
		}
		if !s.visibleContentSent && strings.TrimSpace(finalText) != "" {
			delta := map[string]any{
				"content": finalText,
			}
			if !s.firstChunkSent {
				delta["role"] = "assistant"
				s.firstChunkSent = true
			}
			s.sendChunk(openaifmt.BuildChatStreamChunk(
				s.completionID,
				s.created,
				s.model,
				[]map[string]any{openaifmt.BuildChatStreamDeltaChoice(0, delta)},
				nil,
			))
			s.visibleContentSent = true
		}
	}
	if len(formattedDetected) == 0 && !s.toolCallsEmitted && strings.TrimSpace(finalText) == "" {
		status := http.StatusTooManyRequests
		message := "Upstream model returned empty output."
		code := "upstream_empty_output"
		if strings.TrimSpace(finalThinking) != "" {
			message = "Upstream model returned reasoning without visible output."
		}
		if finishReason == "content_filter" {
			status = http.StatusBadRequest
			message = "Upstream content filtered the response and returned no output."
			code = "content_filter"
		}
		s.finalErrorStatus = status
		s.finalErrorMessage = message
		s.finalErrorCode = code
		if s.deferEmptyOutputFailure && s.retryableEmptyOutputFailure() {
			return
		}
		s.sendFailedChunk(status, message, code)
		return
	}
	usage := openaifmt.BuildChatUsage(s.finalPrompt, finalThinking, finalText)
	s.finalFinishReason = finishReason
	s.finalUsage = usage
	s.sendChunk(openaifmt.BuildChatStreamChunk(
		s.completionID,
		s.created,
		s.model,
		[]map[string]any{openaifmt.BuildChatStreamFinishChoice(0, finishReason)},
		nil,
	))
	if s.streamIncludeUsage {
		s.sendChunk(openaifmt.BuildChatStreamUsageChunk(s.completionID, s.created, s.model, usage))
	}
	s.sendDone()
}

func (s *chatStreamRuntime) retryableEmptyOutputFailure() bool {
	if s == nil {
		return false
	}
	return s.finalErrorCode == "upstream_empty_output" &&
		!s.firstChunkSent &&
		!s.visibleContentSent &&
		!s.toolCallsEmitted &&
		!s.toolCallsDoneEmitted
}

func (s *chatStreamRuntime) onParsed(parsed sse.LineResult) streamengine.ParsedDecision {
	if !parsed.Parsed {
		return streamengine.ParsedDecision{}
	}
	if parsed.ContentFilter {
		if strings.TrimSpace(s.text.String()) == "" {
			return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReason("content_filter")}
		}
		return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReasonHandlerRequested}
	}
	if parsed.ErrorMessage != "" {
		return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReason("content_filter")}
	}
	if parsed.Stop {
		if parsed.Finished && s.bufferToolContent && hasCallableTools(s.toolNames) && !s.toolCallsEmitted {
			return streamengine.ParsedDecision{}
		}
		return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReasonHandlerRequested}
	}

	newChoices := make([]map[string]any, 0, len(parsed.Parts))
	contentSeen := false
	for _, p := range parsed.Parts {
		cleanedText := cleanVisibleOutput(p.Text, s.stripReferenceMarkers)
		if s.searchEnabled && sse.IsCitation(cleanedText) {
			continue
		}
		if cleanedText == "" {
			continue
		}
		contentSeen = true
		delta := map[string]any{}
		if !s.firstChunkSent {
			delta["role"] = "assistant"
			s.firstChunkSent = true
		}
		if p.Type == "thinking" {
			if s.thinkingEnabled {
				trimmed := sse.TrimContinuationOverlap(s.thinking.String(), cleanedText)
				if trimmed == "" {
					continue
				}
				s.thinking.WriteString(trimmed)
				delta["reasoning_content"] = trimmed
			}
		} else {
			trimmed := sse.TrimContinuationOverlap(s.text.String(), cleanedText)
			if trimmed == "" {
				continue
			}
			s.text.WriteString(trimmed)
			if !s.bufferToolContent {
				delta["content"] = trimmed
				s.visibleContentSent = true
			} else {
				events := processToolSieveChunkWithMeta(&s.toolSieve, trimmed, s.toolNames, s.allowMetaAgentTools)
				for _, evt := range events {
					if len(evt.ToolCallDeltas) > 0 {
						if !s.emitEarlyToolDeltas {
							continue
						}
						filtered := filterIncrementalToolCallDeltasByAllowed(evt.ToolCallDeltas, s.streamToolNames, s.allowMetaAgentTools)
						if len(filtered) == 0 {
							continue
						}
						formatted := formatIncrementalStreamToolCallDeltas(filtered, s.streamToolCallIDs)
						if len(formatted) == 0 {
							continue
						}
						tcDelta := map[string]any{
							"tool_calls": formatted,
						}
						s.toolCallsEmitted = true
						if !s.firstChunkSent {
							tcDelta["role"] = "assistant"
							s.firstChunkSent = true
						}
						newChoices = append(newChoices, openaifmt.BuildChatStreamDeltaChoice(0, tcDelta))
						continue
					}
					if normalizedToolCallsExceedInputBytes(evt.ToolCalls, s.toolSchemas, s.allowMetaAgentTools, s.bufferedToolMaxBytes) {
						status, message, code := toolCallTooLargeError()
						s.sendFailedChunk(status, message, code)
						return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReasonHandlerRequested}
					}
					formattedToolCalls := formatFinalStreamToolCallsWithStableIDs(evt.ToolCalls, s.streamToolCallIDs, s.toolSchemas, s.allowMetaAgentTools)
					if len(formattedToolCalls) > 0 {
						s.toolCallsEmitted = true
						s.toolCallsDoneEmitted = true
						tcDelta := map[string]any{
							"tool_calls": formattedToolCalls,
						}
						if !s.firstChunkSent {
							tcDelta["role"] = "assistant"
							s.firstChunkSent = true
						}
						newChoices = append(newChoices, openaifmt.BuildChatStreamDeltaChoice(0, tcDelta))
						s.resetStreamToolCallState()
						continue
					}
					if evt.Content != "" {
						continue
					}
				}
				if s.bufferedToolMaxBytes > 0 && s.text.Len() > s.bufferedToolMaxBytes && !s.toolCallsEmitted {
					status, message, code := toolCallTooLargeError()
					s.sendFailedChunk(status, message, code)
					return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReasonHandlerRequested}
				}
			}
		}
		if len(delta) > 0 {
			newChoices = append(newChoices, openaifmt.BuildChatStreamDeltaChoice(0, delta))
		}
	}

	if len(newChoices) > 0 {
		s.sendChunk(openaifmt.BuildChatStreamChunk(s.completionID, s.created, s.model, newChoices, nil))
	}
	return streamengine.ParsedDecision{ContentSeen: contentSeen}
}
