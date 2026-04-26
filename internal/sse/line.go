package sse

import (
	"encoding/json"
	"fmt"
	"strings"
)

// LineResult is the normalized parse result for one DeepSeek SSE line.
type LineResult struct {
	Parsed        bool
	Stop          bool
	Finished      bool
	Done          bool
	ContentFilter bool
	ErrorMessage  string
	ErrorCode     string
	Parts         []ContentPart
	NextType      string
	LateToolTitle bool
}

// ParseDeepSeekContentLine centralizes one-line DeepSeek SSE parsing for both
// streaming and non-streaming handlers.
func ParseDeepSeekContentLine(raw []byte, thinkingEnabled bool, currentType string) LineResult {
	chunk, done, parsed := ParseDeepSeekSSELine(raw)
	if !parsed {
		if message, code, ok := parseDeepSeekBusinessErrorLine(raw); ok {
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
	if code, _ := chunk["code"].(string); code == "content_filter" {
		return LineResult{
			Parsed:        true,
			Stop:          true,
			ContentFilter: true,
			NextType:      currentType,
		}
	}
	if message, code, ok := parseDeepSeekBusinessErrorChunk(chunk); ok {
		return LineResult{
			Parsed:       true,
			Stop:         true,
			ErrorMessage: message,
			ErrorCode:    code,
			NextType:     currentType,
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
	parts, finished, nextType := ParseSSEChunkForContent(chunk, thinkingEnabled, currentType)
	parts = filterLeakedContentFilterParts(parts)
	return LineResult{
		Parsed:   true,
		Stop:     finished,
		Finished: finished,
		Parts:    parts,
		NextType: nextType,
	}
}

func parseDeepSeekBusinessErrorLine(raw []byte) (string, string, bool) {
	line := strings.TrimSpace(string(raw))
	if line == "" || !strings.HasPrefix(line, "{") {
		return "", "", false
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		return "", "", false
	}
	return parseDeepSeekBusinessErrorChunk(chunk)
}

func parseDeepSeekBusinessErrorChunk(chunk map[string]any) (string, string, bool) {
	data, _ := chunk["data"].(map[string]any)
	if data == nil {
		return "", "", false
	}
	bizCode, ok := intLike(data["biz_code"])
	if !ok || bizCode == 0 {
		return "", "", false
	}
	message := strings.TrimSpace(fmt.Sprint(data["biz_msg"]))
	if message == "" || message == "<nil>" {
		message = fmt.Sprintf("DeepSeek upstream business error %d", bizCode)
	}
	return message, deepSeekBusinessErrorCode(message, bizCode), true
}

func deepSeekBusinessErrorCode(message string, bizCode int) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(lower, "invalid ref file id") {
		return "upstream_invalid_ref_file_id"
	}
	return "upstream_business_error"
}

func intLike(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		i, err := x.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func ParseDeepSeekContentLineWithEvent(raw []byte, eventName string, thinkingEnabled bool, currentType string) LineResult {
	result := ParseDeepSeekContentLine(raw, thinkingEnabled, currentType)
	if !strings.EqualFold(strings.TrimSpace(eventName), "title") {
		return result
	}
	content, ok := parseLateToolTitleContent(raw)
	if !ok {
		return result
	}
	result.Parsed = true
	result.Parts = append(result.Parts, ContentPart{Text: content, Type: "text"})
	result.LateToolTitle = true
	return result
}

func parseLateToolTitleContent(raw []byte) (string, bool) {
	chunk, done, parsed := ParseDeepSeekSSELine(raw)
	if !parsed || done {
		return "", false
	}
	content, _ := chunk["content"].(string)
	content = strings.TrimSpace(content)
	if content == "" || !looksLikeLateToolTitleContent(content) {
		return "", false
	}
	return content, true
}

func looksLikeLateToolTitleContent(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "<tool ") ||
		strings.Contains(lower, "<invoke") ||
		strings.Contains(lower, "<function_call") ||
		strings.Contains(lower, "<tool_use")
}
