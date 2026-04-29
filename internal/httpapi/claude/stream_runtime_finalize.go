package claude

import (
	"encoding/json"
	"fmt"
	"time"

	claudecodeharness "ds2api/internal/harness/claudecode"
	streamengine "ds2api/internal/stream"
	"ds2api/internal/util"
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

func (s *claudeStreamRuntime) finalize(stopReason string) {
	if s.ended {
		return
	}
	s.ended = true

	s.closeThinkingBlock()
	s.closeTextBlock()

	finalThinking := s.thinking.String()
	finalText := cleanVisibleOutput(s.text.String(), s.stripReferenceMarkers)
	visibleText := finalText

	if s.bufferToolContent {
		evaluated := claudecodeharness.EvaluateFinalOutput(claudecodeharness.FinalEvaluationInput{
			FinalPrompt:         s.finalPrompt,
			Text:                finalText,
			Thinking:            finalThinking,
			ToolNames:           s.toolNames,
			ToolSchemas:         s.toolSchemas,
			AllowMetaAgentTools: s.allowMetaAgentTools,
		})
		visibleText = evaluated.Text

		// DroppedTaskOutputIDs: silently filter, keep valid calls. Do NOT error out.
		// The filtering is already done inside EvaluateFinalOutput — evaluated.Calls
		// already has invalid calls removed. Just proceed normally.

		if evaluated.MissingToolDecision.Blocked {
			// Smart recovery: emit visible text + correction prompt, end turn normally.
			s.recoveryNeeded = true
			s.recoveryContext = "The model described planned work but didn't emit tool calls. Retrying with clearer instructions..."
			if visibleText == "" {
				visibleText = "Let me try again with the correct tool calls."
			}
			s.emitBufferedText(visibleText)
			s.emitBufferedText("[System-Reminder: " + s.recoveryContext + "]")
		} else {
			detected := evaluated.Calls
			if len(detected) > 0 {
				stopReason = "tool_use"
				s.emitBufferedText(visibleText)
				for i, tc := range detected {
					idx := s.nextBlockIndex + i
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
				s.nextBlockIndex += len(detected)
			} else if visibleText != "" {
				s.emitBufferedText(visibleText)
			}
		}
	} else {
		// Lightweight harness evaluation for non-buffered mode.
		// Text has already been emitted via streaming; we can only append warnings.
		parsed, _ := claudecodeharness.DetectFinalToolCalls(claudecodeharness.FinalToolCallInput{
			Text:      finalText,
			Thinking:  finalThinking,
			ToolNames: s.toolNames,
		})
		if len(parsed.Calls) == 0 {
			missingDecision := claudecodeharness.DetectMissingToolCall(claudecodeharness.MissingToolCallInput{
				Text:                finalText,
				FinalPrompt:         s.finalPrompt,
				ToolNames:           s.toolNames,
				ToolSchemas:         s.toolSchemas,
				AllowMetaAgentTools: s.allowMetaAgentTools,
			})
			if missingDecision.Blocked {
				// Emit a warning-level text block appended after streaming content.
				// This is NOT an error -- text has already been sent to the client.
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
						"text": "\n\n[System Reminder: " + missingDecision.Message + "]",
					},
				})
				s.send("content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": idx,
				})
			}
		}
	}

	outputTokens := util.EstimateTokens(finalThinking) + util.EstimateTokens(visibleText)
	s.send("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	})
	s.send("message_stop", map[string]any{"type": "message_stop"})
}

func (s *claudeStreamRuntime) emitBufferedText(text string) {
	if text == "" {
		return
	}
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
			"text": text,
		},
	})
	s.send("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})
}

func (s *claudeStreamRuntime) onFinalize(reason streamengine.StopReason, scannerErr error) {
	if string(reason) == "upstream_error" {
		s.sendError(s.upstreamErr)
		return
	}
	if scannerErr != nil {
		s.sendError(scannerErr.Error())
		return
	}
	s.finalize("end_turn")
}

func (s *claudeStreamRuntime) IsRecoveryNeeded() bool {
	return s.recoveryNeeded
}
