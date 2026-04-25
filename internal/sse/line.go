package sse

import (
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
	Parts         []ContentPart
	NextType      string
	LateToolTitle bool
}

// ParseDeepSeekContentLine centralizes one-line DeepSeek SSE parsing for both
// streaming and non-streaming handlers.
func ParseDeepSeekContentLine(raw []byte, thinkingEnabled bool, currentType string) LineResult {
	chunk, done, parsed := ParseDeepSeekSSELine(raw)
	if !parsed {
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
