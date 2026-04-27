package openai

import (
	"encoding/json"

	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/toolcall"
)

func (s *responsesStreamRuntime) nextSequence() int {
	s.sequence++
	return s.sequence
}

func (s *responsesStreamRuntime) sendEvent(event string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["sequence_number"]; !ok {
		payload["sequence_number"] = s.nextSequence()
	}
	b, _ := json.Marshal(payload)
	_, _ = s.w.Write([]byte("event: " + event + "\n"))
	_, _ = s.w.Write([]byte("data: "))
	_, _ = s.w.Write(b)
	_, _ = s.w.Write([]byte("\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *responsesStreamRuntime) sendCreated() {
	s.sendEvent("response.created", openaifmt.BuildResponsesCreatedPayload(s.responseID, s.model))
}

func (s *responsesStreamRuntime) sendDone() {
	_, _ = s.w.Write([]byte("data: [DONE]\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *responsesStreamRuntime) processToolStreamEvents(events []toolStreamEvent, emitContent bool, resetAfterToolCalls bool) {
	for _, evt := range events {
		if s.failed {
			return
		}
		if emitContent && evt.Content != "" {
			s.emitTextDelta(evt.Content)
		}
		if len(evt.ToolCallDeltas) > 0 {
			if !s.emitEarlyToolDeltas {
				continue
			}
			filtered := filterIncrementalToolCallDeltasByAllowed(evt.ToolCallDeltas, s.functionNames, s.allowMetaAgentTools)
			if len(filtered) == 0 {
				continue
			}
			s.emitFunctionCallDeltaEvents(filtered)
		}
		if len(evt.ToolCalls) > 0 {
			if _, message, code, ok := invalidTaskOutputCallDetail(evt.ToolCalls, s.finalPrompt); ok {
				s.failResponse(message, code)
				return
			}
			normalized := toolcall.NormalizeCallsForSchemasWithMeta(evt.ToolCalls, s.toolSchemas, s.allowMetaAgentTools)
			if len(normalized) == 0 {
				continue
			}
			if normalizedToolCallsExceedInputBytes(normalized, s.toolSchemas, s.allowMetaAgentTools, s.bufferedToolMaxBytes) {
				_, message, code := toolCallTooLargeError()
				s.failResponse(message, code)
				return
			}
			s.emitFunctionCallDoneEvents(normalized)
			if resetAfterToolCalls {
				s.resetStreamToolCallState()
			}
		}
	}
}
