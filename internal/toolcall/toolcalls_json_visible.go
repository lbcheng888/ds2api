package toolcall

import (
	"encoding/json"
	"strings"
)

func parseVisibleJSONToolCalls(text string) []ParsedToolCall {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if calls := parseVisibleJSONToolCallsStrict(trimmed); len(calls) > 0 {
		return calls
	}
	repaired := repairVisibleJSONLooseCommandStrings(trimmed)
	if repaired != trimmed {
		return parseVisibleJSONToolCallsStrict(repaired)
	}
	return nil
}

func parseVisibleJSONToolCallsStrict(trimmed string) []ParsedToolCall {
	if strings.HasPrefix(trimmed, "{") {
		return parseVisibleJSONToolObjectSequence(trimmed)
	}
	if !strings.HasPrefix(trimmed, "[") {
		return nil
	}
	var items []any
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&items); err != nil {
		return nil
	}
	if strings.TrimSpace(trimmed[dec.InputOffset():]) != "" {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil
		}
		call, ok := parseVisibleJSONToolCallObject(obj)
		if !ok {
			return nil
		}
		out = append(out, call)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ExtractVisibleJSONToolCalls(text string, availableToolNames []string) (prefix string, calls []ParsedToolCall, suffix string, ok bool) {
	for start := findVisibleJSONToolStart(text, 0); start >= 0; start = findVisibleJSONToolStart(text, start+1) {
		if isInsideMarkdownFence(text[:start]) {
			continue
		}
		end := visibleJSONToolBlockEnd(text[start:])
		if end <= 0 {
			continue
		}
		block := text[start : start+end]
		parsed := ParseToolCalls(block, availableToolNames)
		if len(parsed) == 0 {
			continue
		}
		return text[:start], parsed, text[start+end:], true
	}
	return "", nil, "", false
}

func findVisibleJSONToolStart(text string, from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(text); i++ {
		if text[i] != '{' && text[i] != '[' {
			continue
		}
		lineStart := strings.LastIndex(text[:i], "\n") + 1
		if strings.TrimSpace(text[lineStart:i]) != "" {
			continue
		}
		return i
	}
	return -1
}

func visibleJSONToolBlockEnd(text string) int {
	trimmedLeft := strings.TrimLeft(text, " \t\r\n")
	leading := len(text) - len(trimmedLeft)
	if trimmedLeft == "" {
		return -1
	}
	if strings.HasPrefix(trimmedLeft, "[") {
		var items []any
		dec := json.NewDecoder(strings.NewReader(trimmedLeft))
		dec.UseNumber()
		if err := dec.Decode(&items); err != nil {
			if end := visibleJSONLooseBlockEnd(trimmedLeft); end > 0 && len(parseVisibleJSONToolCalls(trimmedLeft[:end])) > 0 {
				return leading + end
			}
			return -1
		}
		return leading + int(dec.InputOffset())
	}
	if !strings.HasPrefix(trimmedLeft, "{") {
		return -1
	}
	dec := json.NewDecoder(strings.NewReader(trimmedLeft))
	dec.UseNumber()
	lastEnd := int64(0)
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if lastEnd == 0 {
				if end := visibleJSONLooseBlockEnd(trimmedLeft); end > 0 && len(parseVisibleJSONToolCalls(trimmedLeft[:end])) > 0 {
					return leading + end
				}
				return -1
			}
			if end := visibleJSONLooseBlockEnd(trimmedLeft); end > int(lastEnd) && len(parseVisibleJSONToolCalls(trimmedLeft[:end])) > 0 {
				return leading + end
			}
			return leading + int(lastEnd)
		}
		lastEnd = dec.InputOffset()
		rest := strings.TrimLeft(trimmedLeft[lastEnd:], " \t\r\n")
		if rest == "" || !strings.HasPrefix(rest, "{") {
			return leading + int(lastEnd)
		}
	}
}

func isInsideMarkdownFence(prefix string) bool {
	return strings.Count(prefix, "```")%2 == 1 || strings.Count(prefix, "~~~")%2 == 1
}

func parseVisibleJSONToolObjectSequence(text string) []ParsedToolCall {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	out := make([]ParsedToolCall, 0, 1)
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			return nil
		}
		call, ok := parseVisibleJSONToolCallObject(obj)
		if !ok {
			return nil
		}
		out = append(out, call)
		rest := strings.TrimSpace(text[dec.InputOffset():])
		if rest == "" {
			break
		}
		if !strings.HasPrefix(rest, "{") {
			return nil
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseVisibleJSONToolCallObject(obj map[string]any) (ParsedToolCall, bool) {
	name := strings.TrimSpace(asString(obj["tool"]))
	if name == "" {
		name = strings.TrimSpace(asString(obj["name"]))
	}
	if name == "" {
		name = strings.TrimSpace(asString(obj["tool_name"]))
	}
	input := map[string]any{}
	if fn, ok := obj["function"].(map[string]any); ok {
		if name == "" {
			name = strings.TrimSpace(asString(fn["name"]))
		}
		input = firstVisibleJSONInput(fn)
	}
	if len(input) == 0 {
		input = firstVisibleJSONInput(obj)
	}
	if len(input) == 0 && name == "" {
		input = rootVisibleJSONInput(obj)
	}
	if name == "" && len(input) == 0 {
		return ParsedToolCall{}, false
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func firstVisibleJSONInput(obj map[string]any) map[string]any {
	for _, key := range []string{"arguments", "input", "params", "parameters"} {
		if value, ok := obj[key]; ok {
			return parseToolCallInput(value)
		}
	}
	return map[string]any{}
}

func rootVisibleJSONInput(obj map[string]any) map[string]any {
	if !hasRootToolParameter(obj) {
		return map[string]any{}
	}
	out := map[string]any{}
	for key, value := range obj {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "tool", "name", "tool_name", "function":
			continue
		default:
			out[key] = value
		}
	}
	return out
}

func hasRootToolParameter(obj map[string]any) bool {
	for key := range obj {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "command", "cmd", "file_path", "filepath", "file-path", "task_id", "taskid":
			return true
		}
	}
	return false
}

func looksLikeVisibleJSONToolCallSyntax(text string) bool {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		return false
	}
	lower := strings.ToLower(trimmed)
	hasName := strings.Contains(lower, `"tool"`) ||
		strings.Contains(lower, `"name"`) ||
		strings.Contains(lower, `"tool_name"`) ||
		strings.Contains(lower, `"function"`)
	hasArgs := strings.Contains(lower, `"arguments"`) ||
		strings.Contains(lower, `"input"`) ||
		strings.Contains(lower, `"params"`) ||
		strings.Contains(lower, `"parameters"`)
	return hasName && hasArgs && len(parseVisibleJSONToolCalls(trimmed)) > 0
}
