package toolstream

import (
	"strings"

	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

func ProcessChunk(state *State, chunk string, toolNames []string) []Event {
	if state == nil {
		return nil
	}
	if chunk != "" {
		state.pending.WriteString(chunk)
	}
	events := make([]Event, 0, 2)
	if len(state.pendingToolCalls) > 0 {
		events = append(events, Event{ToolCalls: state.pendingToolCalls})
		state.pendingToolRaw = ""
		state.pendingToolCalls = nil
	}

	for {
		if state.capturing {
			if state.pending.Len() > 0 {
				state.capture.WriteString(state.pending.String())
				state.pending.Reset()
			}
			prefix, calls, suffix, ready := consumeToolCapture(state, toolNames)
			if !ready {
				break
			}
			captured := state.capture.String()
			state.capture.Reset()
			state.capturing = false
			state.resetIncrementalToolState()
			if len(calls) > 0 {
				if prefix != "" {
					state.noteText(prefix)
					events = append(events, Event{Content: prefix})
				}
				if suffix != "" {
					state.pending.WriteString(suffix)
				}
				_ = captured
				state.pendingToolCalls = calls
				continue
			}
			if prefix != "" {
				state.noteText(prefix)
				events = append(events, Event{Content: prefix})
			}
			if suffix != "" {
				state.pending.WriteString(suffix)
			}
			continue
		}

		pending := state.pending.String()
		if pending == "" {
			break
		}
		start := findToolSegmentStart(state, pending)
		if start >= 0 {
			prefix := pending[:start]
			if prefix != "" {
				state.noteText(prefix)
				events = append(events, Event{Content: prefix})
			}
			state.pending.Reset()
			state.capture.WriteString(pending[start:])
			state.capturing = true
			state.resetIncrementalToolState()
			continue
		}

		safe, hold := splitSafeContentForToolDetection(state, pending)
		if safe == "" {
			break
		}
		state.pending.Reset()
		state.pending.WriteString(hold)
		state.noteText(safe)
		events = append(events, Event{Content: safe})
	}

	return events
}

func Flush(state *State, toolNames []string) []Event {
	if state == nil {
		return nil
	}
	events := ProcessChunk(state, "", toolNames)
	if len(state.pendingToolCalls) > 0 {
		events = append(events, Event{ToolCalls: state.pendingToolCalls})
		state.pendingToolRaw = ""
		state.pendingToolCalls = nil
	}
	if state.capturing {
		consumedPrefix, consumedCalls, consumedSuffix, ready := consumeToolCapture(state, toolNames)
		if ready {
			if consumedPrefix != "" {
				state.noteText(consumedPrefix)
				events = append(events, Event{Content: consumedPrefix})
			}
			if len(consumedCalls) > 0 {
				events = append(events, Event{ToolCalls: consumedCalls})
			}
			if consumedSuffix != "" {
				state.noteText(consumedSuffix)
				events = append(events, Event{Content: consumedSuffix})
			}
		} else {
			content := state.capture.String()
			if content != "" {
				recovered := toolcall.SanitizeLooseCDATA(content)
				if recovered != content {
					if prefix, calls, suffix, recoveredReady := consumeXMLToolCapture(recovered, toolNames); recoveredReady && len(calls) > 0 {
						if prefix != "" {
							state.noteText(prefix)
							events = append(events, Event{Content: prefix})
						}
						events = append(events, Event{ToolCalls: calls})
						if suffix != "" {
							state.noteText(suffix)
							events = append(events, Event{Content: suffix})
						}
					} else if code, message, ok := incompleteToolTransactionError(content); ok {
						events = append(events, Event{ErrorCode: code, ErrorMessage: message})
					} else {
						state.noteText(content)
						events = append(events, Event{Content: content})
					}
				} else if code, message, ok := incompleteToolTransactionError(content); ok {
					events = append(events, Event{ErrorCode: code, ErrorMessage: message})
				} else {
					state.noteText(content)
					events = append(events, Event{Content: content})
				}
			}
		}
		state.capture.Reset()
		state.capturing = false
		state.resetIncrementalToolState()
	}
	if state.pending.Len() > 0 {
		content := state.pending.String()
		// If pending never resolved into a real tool call, release it as text.
		state.noteText(content)
		events = append(events, Event{Content: content})
		state.pending.Reset()
	}
	return events
}

func splitSafeContentForToolDetection(state *State, s string) (safe, hold string) {
	if s == "" {
		return "", ""
	}
	if xmlIdx := findPartialXMLToolTagStart(s); xmlIdx >= 0 {
		if insideCodeFenceWithState(state, s[:xmlIdx]) {
			return s, ""
		}
		if xmlIdx > 0 {
			return s[:xmlIdx], s[xmlIdx:]
		}
		return "", s
	}
	return s, ""
}

func findToolSegmentStart(state *State, s string) int {
	if s == "" {
		return -1
	}
	lower := strings.ToLower(s)
	offset := 0
	for {
		bestKeyIdx := -1
		matchedTag := ""
		for _, tag := range xmlToolTagsToDetect {
			idx := strings.Index(lower[offset:], tag)
			if idx >= 0 {
				idx += offset
				if bestKeyIdx < 0 || idx < bestKeyIdx {
					bestKeyIdx = idx
					matchedTag = tag
				}
			}
		}
		if bestKeyIdx < 0 {
			return -1
		}
		if !insideCodeFenceWithState(state, s[:bestKeyIdx]) {
			return bestKeyIdx
		}
		offset = bestKeyIdx + len(matchedTag)
	}
}

func consumeToolCapture(state *State, toolNames []string) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	captured := state.capture.String()
	if captured == "" {
		return "", nil, "", false
	}

	// XML tool call extraction only.
	if xmlPrefix, xmlCalls, xmlSuffix, xmlReady := consumeXMLToolCapture(captured, toolNames); xmlReady {
		return xmlPrefix, xmlCalls, xmlSuffix, true
	}
	// If XML tags are present but block is incomplete, keep buffering.
	if hasOpenXMLToolTag(captured) {
		return "", nil, "", false
	}
	if shouldKeepBareInvokeCapture(captured) {
		return "", nil, "", false
	}
	return captured, nil, "", true
}

func incompleteToolTransactionError(captured string) (string, string, bool) {
	trimmed := strings.TrimSpace(captured)
	if trimmed == "" {
		return "", "", false
	}
	if isCompleteBareInvokeText(trimmed) {
		return "", "", false
	}
	lower := strings.ToLower(trimmed)
	if hasOpenXMLToolTag(trimmed) || containsAnyToolSyntax(lower, []string{
		"<tool_call",
		"<tool_calls",
		"<invoke",
		"<function_call",
		"<function_calls",
		"<tool_use",
		"<attempt_completion",
		"<ask_followup_question",
		"<new_task",
		"<|dsml",
		"<｜dsml",
		"<dsml",
	}) {
		return claudecodeharness.InvalidToolCallCode, "Upstream model emitted invalid tool call syntax.", true
	}
	if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && hasVisibleJSONToolHints(lower) {
		return claudecodeharness.InvalidToolCallCode, "Upstream model emitted invalid tool call syntax.", true
	}
	return "", "", false
}

func isCompleteBareInvokeText(trimmed string) bool {
	lower := strings.ToLower(strings.TrimSpace(trimmed))
	if !strings.HasPrefix(lower, "<invoke") {
		return false
	}
	return strings.Contains(lower, "</invoke>") &&
		!strings.Contains(lower, "<tool_calls") &&
		!strings.Contains(lower, "<|dsml|tool_calls")
}

func hasVisibleJSONToolHints(lower string) bool {
	hasName := strings.Contains(lower, `"tool"`) ||
		strings.Contains(lower, `"name"`) ||
		strings.Contains(lower, `"tool_name"`) ||
		strings.Contains(lower, `"function"`)
	hasArgs := strings.Contains(lower, `"arguments"`) ||
		strings.Contains(lower, `"input"`) ||
		strings.Contains(lower, `"params"`) ||
		strings.Contains(lower, `"parameters"`)
	return hasName && hasArgs
}

func containsAnyToolSyntax(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
