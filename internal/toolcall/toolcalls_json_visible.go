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
	if name == "" {
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
