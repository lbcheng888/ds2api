package sse

import (
	"encoding/json"
	"fmt"
	"strings"
)

// LineResult is the normalized parse result for one DeepSeek SSE line.
type LineResult struct {
	Parsed                     bool
	Stop                       bool
	Done                       bool
	Finished                   bool
	ContentFilter              bool
	ErrorMessage               string
	ErrorCode                  string
	Parts                      []ContentPart
	ToolDetectionThinkingParts []ContentPart
	NextType                   string
	ResponseMessageID          int
	LateToolTitle              bool
}

// ParseDeepSeekContentLine centralizes one-line DeepSeek SSE parsing for both
// streaming and non-streaming handlers.
func ParseDeepSeekContentLine(raw []byte, thinkingEnabled bool, currentType string) LineResult {
	chunk, done, parsed := ParseDeepSeekSSELine(raw)
	if !parsed {
		if message, code, ok := parseRawBusinessError(raw); ok {
			return LineResult{
				Parsed:       true,
				Stop:         true,
				ErrorMessage: message,
				ErrorCode:    code,
				NextType:     currentType,
			}
		}
		return LineResult{NextType: currentType}
	}
	if done {
		return LineResult{Parsed: true, Stop: true, Done: true, NextType: currentType}
	}
	if errObj, hasErr := chunk["error"]; hasErr {
		return LineResult{
			Parsed:       true,
			Stop:         true,
			ErrorMessage: fmt.Sprintf("%v", errObj),
			ErrorCode:    "upstream_error",
			NextType:     currentType,
		}
	}
	if message, code, ok := parseUpstreamHintError(chunk); ok {
		return LineResult{
			Parsed:       true,
			Stop:         true,
			ErrorMessage: message,
			ErrorCode:    code,
			NextType:     currentType,
		}
	}
	if code, _ := chunk["code"].(string); code == "content_filter" {
		return LineResult{
			Parsed:        true,
			Stop:          true,
			ContentFilter: true,
			NextType:      currentType,
		}
	}
	if hasContentFilterStatus(chunk) {
		return LineResult{
			Parsed:        true,
			Stop:          true,
			ContentFilter: true,
			NextType:      currentType,
		}
	}
	parts, detectionThinkingParts, finished, nextType := ParseSSEChunkForContentDetailed(chunk, thinkingEnabled, currentType)
	parts = filterLeakedContentFilterParts(parts)
	detectionThinkingParts = filterLeakedContentFilterParts(detectionThinkingParts)
	var respMsgID int
	observeResponseMessageID(chunk, &respMsgID)
	return LineResult{
		Parsed:                     true,
		Stop:                       finished,
		Finished:                   finished,
		Parts:                      parts,
		ToolDetectionThinkingParts: detectionThinkingParts,
		NextType:                   nextType,
		ResponseMessageID:          respMsgID,
	}
}

func ParseDeepSeekContentLineWithEvent(raw []byte, eventName string, thinkingEnabled bool, currentType string) LineResult {
	if strings.EqualFold(strings.TrimSpace(eventName), "title") {
		if title := lateToolTitleContent(raw); title != "" {
			return LineResult{
				Parsed:        true,
				Parts:         []ContentPart{{Text: title, Type: "text"}},
				NextType:      currentType,
				LateToolTitle: true,
			}
		}
		return LineResult{Parsed: true, NextType: currentType}
	}
	return ParseDeepSeekContentLine(raw, thinkingEnabled, currentType)
}

func parseUpstreamHintError(chunk map[string]any) (string, string, bool) {
	if chunk == nil {
		return "", "", false
	}
	typeName := strings.ToLower(strings.TrimSpace(asStringField(chunk, "type")))
	finishReason := strings.ToLower(strings.TrimSpace(asStringField(chunk, "finish_reason")))
	message := strings.TrimSpace(firstStringField(chunk, "content", "message", "msg"))
	if finishReason == "input_exceeds_limit" || strings.Contains(message, "内容超长") || strings.Contains(strings.ToLower(message), "input exceeds") {
		if message == "" {
			message = "Upstream input exceeds the model context limit."
		}
		return message, "input_exceeds_limit", true
	}
	if typeName == "error" {
		if message == "" {
			message = fmt.Sprintf("%v", chunk)
		}
		return message, "upstream_error", true
	}
	return "", "", false
}

func firstStringField(chunk map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := asStringField(chunk, key); value != "" {
			return value
		}
	}
	return ""
}

func asStringField(chunk map[string]any, key string) string {
	if chunk == nil {
		return ""
	}
	value, _ := chunk[key].(string)
	return value
}

func parseRawBusinessError(raw []byte) (string, string, bool) {
	line := strings.TrimSpace(string(raw))
	if line == "" || !strings.HasPrefix(line, "{") {
		return "", "", false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return "", "", false
	}
	data, _ := obj["data"].(map[string]any)
	msg, _ := data["biz_msg"].(string)
	if strings.TrimSpace(msg) == "" {
		msg, _ = obj["msg"].(string)
	}
	if strings.Contains(strings.ToLower(msg), "invalid ref file id") {
		return msg, "upstream_invalid_ref_file_id", true
	}
	return "", "", false
}

func lateToolTitleContent(raw []byte) string {
	chunk, done, parsed := ParseDeepSeekSSELine(raw)
	if done || !parsed {
		return ""
	}
	content := titleContentString(chunk)
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return extractExecutableToolTitle(content)
}

func titleContentString(chunk map[string]any) string {
	for _, key := range []string{"content", "title", "v"} {
		if s, ok := chunk[key].(string); ok {
			return s
		}
	}
	return ""
}

func extractExecutableToolTitle(content string) string {
	text := normalizeTitleToolCallsPrefix(content)
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "tool_call") && !strings.Contains(lower, "<invoke") {
		return ""
	}
	if complete := completeToolCallPrefix(text); complete != "" {
		return complete
	}
	if !strings.Contains(lower, "</parameter>") {
		return ""
	}
	if strings.Contains(lower, "<tool_call") && (strings.Contains(lower, "<tool_name>") || strings.Contains(lower, `name="`) || strings.Contains(lower, `name='`)) {
		return closeTitleToolBlock(text)
	}
	return ""
}

func normalizeTitleToolCallsPrefix(content string) string {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	leading := content[:len(content)-len(trimmed)]
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "tool_calls\n") || strings.HasPrefix(lower, "tool_calls\r\n") {
		lineEnd := strings.IndexAny(trimmed, "\r\n")
		return leading + "<tool_calls>" + trimmed[lineEnd:]
	}
	if strings.HasPrefix(lower, "tool_calls>") {
		return leading + "<" + trimmed
	}
	return content
}

func completeToolCallPrefix(text string) string {
	lower := strings.ToLower(text)
	start := strings.Index(lower, "<tool_call")
	if start < 0 {
		return ""
	}
	closeIdx := strings.Index(lower[start:], "</tool_call>")
	if closeIdx < 0 {
		return ""
	}
	end := start + closeIdx + len("</tool_call>")
	return closeTitleToolBlock(text[:end])
}

func closeTitleToolBlock(text string) string {
	out := strings.TrimSpace(text)
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "<tool_calls") {
		out = "<tool_calls>" + out
		lower = strings.ToLower(out)
	}
	if strings.Contains(lower, "<tool_call") && !strings.Contains(lower, "</tool_call>") {
		out += "</tool_call>"
		lower = strings.ToLower(out)
	}
	if strings.Contains(lower, "<tool_calls") && !strings.Contains(lower, "</tool_calls>") {
		out += "</tool_calls>"
	}
	return out
}
