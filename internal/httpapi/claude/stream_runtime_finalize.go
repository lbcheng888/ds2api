package claude

import (
	"ds2api/internal/assistantturn"
	"ds2api/internal/responsehistory"
	"ds2api/internal/sse"
	"ds2api/internal/toolcall"
	"ds2api/internal/toolstream"
	"encoding/json"
	"fmt"
	"time"

	streamengine "ds2api/internal/stream"
)

func (s *claudeStreamRuntime) closeThinkingBlock() {
	if !s.thinkingBlockOpen {
		return
	}
	s.send("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": s.thinkingBlockIndex,
	})
	s.thinkingBlockOpen = false
	s.thinkingBlockIndex = -1
}

func (s *claudeStreamRuntime) closeTextBlock() {
	if !s.textBlockOpen {
		return
	}
	s.send("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": s.textBlockIndex,
	})
	s.textBlockOpen = false
	s.textBlockIndex = -1
}

func (s *claudeStreamRuntime) sendToolUseBlock(idx int, tc toolcall.ParsedToolCall) {
	s.send("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": idx,
		"content_block": map[string]any{
			"type":  "tool_use",
			"id":    fmt.Sprintf("toolu_%d_%d", time.Now().Unix(), idx),
			"name":  tc.Name,
			"input": map[string]any{},
		},
	})
	inputBytes, _ := json.Marshal(tc.Input)
	s.send("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": idx,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": string(inputBytes),
		},
	})
	s.send("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})
}

func (s *claudeStreamRuntime) finalize(stopReason string) {
	_ = s.finalizeWithRetry(stopReason, false)
}

func (s *claudeStreamRuntime) finalizeWithRetry(stopReason string, deferMissingTool bool) bool {
	if s.ended {
		return true
	}
	s.finalErrorStatus = 0
	s.finalErrorMessage = ""
	s.finalErrorCode = ""

	s.closeThinkingBlock()

	if s.bufferToolContent {
		for _, evt := range toolstream.Flush(&s.sieve, s.toolNames) {
			if len(evt.ToolCalls) > 0 {
				s.closeTextBlock()
				s.toolCallsDetected = true
				normalized := toolcall.NormalizeParsedToolCallsForSchemas(evt.ToolCalls, s.toolsRaw)
				for _, tc := range normalized {
					idx := s.nextBlockIndex
					s.nextBlockIndex++
					s.sendToolUseBlock(idx, tc)
				}
				continue
			}
			if evt.Content != "" {
				cleaned := cleanVisibleOutput(evt.Content, s.stripReferenceMarkers)
				if cleaned == "" || (s.searchEnabled && sse.IsCitation(cleaned)) {
					continue
				}
				if s.shouldSuppressLeakedToolSyntax(cleaned) {
					continue
				}
				if s.shouldBufferToolModeText() {
					continue
				}
				if !s.textBlockOpen {
					s.textBlockIndex = s.nextBlockIndex
					s.nextBlockIndex++
					s.send("content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": s.textBlockIndex,
						"content_block": map[string]any{
							"type": "text",
							"text": "",
						},
					})
					s.textBlockOpen = true
				}
				s.send("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": s.textBlockIndex,
					"delta": map[string]any{
						"type": "text_delta",
						"text": cleaned,
					},
				})
				s.textEmitted = true
			}
		}
	}

	s.closeTextBlock()

	turn := assistantturn.BuildTurnFromStreamSnapshot(assistantturn.StreamSnapshot{
		RawText:               s.rawText.String(),
		VisibleText:           s.text.String(),
		RawThinking:           s.rawThinking.String(),
		VisibleThinking:       s.thinking.String(),
		DetectionThinking:     s.toolDetectionThinking.String(),
		AlreadyEmittedCalls:   s.toolCallsDetected,
		AlreadyEmittedToolRaw: s.toolCallsDetected,
	}, assistantturn.BuildOptions{
		Model:                 s.model,
		Prompt:                s.promptTokenText,
		SearchEnabled:         s.searchEnabled,
		StripReferenceMarkers: s.stripReferenceMarkers,
		ToolNames:             s.toolNames,
		ToolsRaw:              s.toolsRaw,
		Profile:               "claude",
	})
	finalText := turn.Text
	outcome := assistantturn.FinalizeTurn(turn, assistantturn.FinalizeOptions{
		AlreadyEmittedToolCalls: s.toolCallsDetected,
	})
	if outcome.ShouldFail {
		if deferMissingTool && !s.textEmitted && !s.toolCallsDetected && assistantturn.IsMissingToolError(outcome.Error) {
			s.finalErrorStatus = outcome.Error.Status
			s.finalErrorMessage = outcome.Error.Message
			s.finalErrorCode = outcome.Error.Code
			s.resetAfterDeferredMissingToolRetry()
			return false
		}
		s.ended = true
		if s.history != nil {
			s.history.Error(outcome.Error.Status, outcome.Error.Message, outcome.Error.Code, responsehistory.ThinkingForArchive(turn.RawThinking, turn.DetectionThinking, turn.Thinking), responsehistory.TextForArchive(turn.RawText, turn.Text))
		}
		s.sendErrorWithCode(outcome.Error.Message, outcome.Error.Code)
		return true
	}

	if s.bufferToolContent && !s.toolCallsDetected {
		if len(turn.ToolCalls) > 0 {
			stopReason = "tool_use"
			for _, tc := range turn.ToolCalls {
				idx := s.nextBlockIndex
				s.nextBlockIndex++
				s.sendToolUseBlock(idx, tc)
			}
		} else if finalText != "" && !s.textEmitted {
			idx := s.nextBlockIndex
			s.nextBlockIndex++
			s.send("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": idx,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			})
			s.send("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{
					"type": "text_delta",
					"text": finalText,
				},
			})
			s.textEmitted = true
			s.send("content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": idx,
			})
		}
	}

	if outcome.HasToolCalls {
		stopReason = "tool_use"
	}
	s.ended = true
	if s.history != nil {
		s.history.Success(
			200,
			responsehistory.ThinkingForArchive(turn.RawThinking, turn.DetectionThinking, turn.Thinking),
			responsehistory.TextForArchive(turn.RawText, turn.Text),
			stopReason,
			responsehistory.GenericUsage(turn),
		)
	}

	s.send("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outcome.Usage.OutputTokens,
		},
	})
	s.send("message_stop", map[string]any{"type": "message_stop"})
	return true
}

func (s *claudeStreamRuntime) resetAfterDeferredMissingToolRetry() {
	s.rawText.Reset()
	s.rawThinking.Reset()
	s.toolDetectionThinking.Reset()
	s.text.Reset()
	s.thinking.Reset()
	s.sieve = toolstream.State{}
}

func (s *claudeStreamRuntime) onFinalize(reason streamengine.StopReason, scannerErr error) {
	_ = s.onFinalizeWithRetry(reason, scannerErr, false)
}

func (s *claudeStreamRuntime) onFinalizeWithRetry(reason streamengine.StopReason, scannerErr error, deferMissingTool bool) bool {
	if string(reason) == "upstream_error" {
		s.ended = true
		if s.history != nil {
			s.history.Error(500, s.upstreamErr, "upstream_error", responsehistory.ThinkingForArchive(s.rawThinking.String(), s.toolDetectionThinking.String(), s.thinking.String()), responsehistory.TextForArchive(s.rawText.String(), s.text.String()))
		}
		s.sendError(s.upstreamErr)
		return true
	}
	if scannerErr != nil {
		s.ended = true
		if s.history != nil {
			s.history.Error(500, scannerErr.Error(), "error", responsehistory.ThinkingForArchive(s.rawThinking.String(), s.toolDetectionThinking.String(), s.thinking.String()), responsehistory.TextForArchive(s.rawText.String(), s.text.String()))
		}
		s.sendError(scannerErr.Error())
		return true
	}
	return s.finalizeWithRetry("end_turn", deferMissingTool)
}
