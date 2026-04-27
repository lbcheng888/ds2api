package rawsample

import (
	"encoding/json"
	"regexp"
	"strings"
)

var toolSyntaxRe = regexp.MustCompile(`(?is)<\s*(?:tool_call|tool_calls|tool_use|invoke|function_call)\b`)

type Analysis struct {
	Category        string   `json:"category"`
	Detail          string   `json:"detail,omitempty"`
	EventCount      int      `json:"event_count,omitempty"`
	ParsedJSONCount int      `json:"parsed_json_count,omitempty"`
	SawFinish       bool     `json:"saw_finish,omitempty"`
	VisibleChars    int      `json:"visible_chars,omitempty"`
	ReasoningChars  int      `json:"reasoning_chars,omitempty"`
	ToolSyntaxCount int      `json:"tool_syntax_count,omitempty"`
	ErrorMessages   []string `json:"error_messages,omitempty"`
}

func AnalyzeUpstreamBody(raw []byte) *Analysis {
	a := &Analysis{}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		a.Category = "empty_stream"
		a.Detail = "upstream stream body is empty"
		return a
	}
	for _, block := range splitSSEBlocks(text) {
		eventName, payload := parseSSEBlock(block)
		if payload == "" {
			continue
		}
		a.EventCount++
		if strings.EqualFold(eventName, "finish") || payload == "[DONE]" {
			a.SawFinish = true
		}
		if !strings.HasPrefix(strings.TrimSpace(payload), "{") {
			collectPlainTextSignals(a, payload)
			continue
		}
		var decoded any
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			continue
		}
		a.ParsedJSONCount++
		inspectJSONValue(a, decoded, "")
	}
	a.Category, a.Detail = classifyAnalysis(a)
	return a
}

func splitSSEBlocks(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Split(normalized, "\n\n")
}

func parseSSEBlock(block string) (string, string) {
	eventName := "message"
	data := []string{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return eventName, strings.TrimSpace(strings.Join(data, "\n"))
}

func collectPlainTextSignals(a *Analysis, text string) {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "socket connection was closed") || strings.Contains(lower, "unable to connect") {
		appendAnalysisError(a, text)
	}
	a.ToolSyntaxCount += len(toolSyntaxRe.FindAllStringIndex(text, -1))
}

func inspectJSONValue(a *Analysis, v any, keyPath string) {
	switch x := v.(type) {
	case nil:
		return
	case string:
		inspectStringValue(a, keyPath, x)
	case []any:
		for _, item := range x {
			inspectJSONValue(a, item, keyPath)
		}
	case map[string]any:
		inspectJSONMap(a, x, keyPath)
	}
}

func inspectJSONMap(a *Analysis, m map[string]any, keyPath string) {
	hasPathValue := false
	if p, ok := m["p"].(string); ok {
		hasPathValue = true
		inspectPathValue(a, p, m["v"])
	}
	for _, key := range []string{"error", "biz_msg"} {
		if value, ok := m[key]; ok {
			msg := strings.TrimSpace(extractText(value))
			if msg != "" {
				appendAnalysisError(a, msg)
			}
		}
	}
	for key, value := range m {
		if hasPathValue && key == "v" {
			continue
		}
		nextPath := key
		if keyPath != "" {
			nextPath = keyPath + "." + key
		}
		inspectJSONValue(a, value, nextPath)
	}
}

func inspectPathValue(a *Analysis, path string, value any) {
	text := extractText(value)
	if text == "" {
		return
	}
	lowerPath := strings.ToLower(path)
	upperText := strings.ToUpper(text)
	if strings.Contains(lowerPath, "status") && strings.Contains(upperText, "FINISHED") {
		a.SawFinish = true
	}
	if strings.Contains(lowerPath, "thinking") || strings.Contains(lowerPath, "reasoning") {
		a.ReasoningChars += len([]rune(text))
	} else if strings.Contains(lowerPath, "content") || strings.Contains(lowerPath, "output_text") || strings.Contains(lowerPath, "text") {
		a.VisibleChars += len([]rune(text))
	}
	a.ToolSyntaxCount += len(toolSyntaxRe.FindAllStringIndex(text, -1))
}

func inspectStringValue(a *Analysis, keyPath, text string) {
	if text == "" {
		return
	}
	lowerPath := strings.ToLower(keyPath)
	switch {
	case strings.Contains(lowerPath, "reasoning"):
		a.ReasoningChars += len([]rune(text))
	case strings.Contains(lowerPath, "thinking"):
		a.ReasoningChars += len([]rune(text))
	case strings.Contains(lowerPath, "content"), strings.Contains(lowerPath, "output_text"), strings.Contains(lowerPath, "text"):
		a.VisibleChars += len([]rune(text))
	}
	a.ToolSyntaxCount += len(toolSyntaxRe.FindAllStringIndex(text, -1))
}

func extractText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		var out strings.Builder
		for _, item := range x {
			out.WriteString(extractText(item))
		}
		return out.String()
	case map[string]any:
		var out strings.Builder
		for _, item := range x {
			out.WriteString(extractText(item))
		}
		return out.String()
	default:
		return ""
	}
}

func appendAnalysisError(a *Analysis, msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	if len([]rune(msg)) > 240 {
		msg = string([]rune(msg)[:240])
	}
	for _, existing := range a.ErrorMessages {
		if existing == msg {
			return
		}
	}
	a.ErrorMessages = append(a.ErrorMessages, msg)
}

func classifyAnalysis(a *Analysis) (string, string) {
	switch {
	case len(a.ErrorMessages) > 0:
		return "upstream_error", "upstream returned an explicit error payload"
	case a.EventCount == 0:
		return "empty_stream", "no SSE data events were parsed"
	case !a.SawFinish:
		return "missing_finish", "stream ended without a finish signal"
	case a.ToolSyntaxCount > 0:
		return "tool_syntax_candidate", "stream contains XML-style tool call syntax"
	case a.ReasoningChars > 0 && a.VisibleChars == 0:
		return "reasoning_without_visible_output", "stream has reasoning text but no visible text"
	case a.VisibleChars == 0:
		return "empty_visible_output", "stream finished without visible text"
	default:
		return "ok", "stream contains visible output and finish signal"
	}
}
