package openai

import (
	"html"
	"regexp"
	"strings"

	"ds2api/internal/toolcall"
)

var taskNotificationBlockPattern = regexp.MustCompile(`(?is)<task-notification\b[^>]*>(.*?)</task-notification>`)
var taskIDTagPattern = regexp.MustCompile(`(?is)<task[-_]?id\b[^>]*>(.*?)</task[-_]?id>`)
var taskIDAttrPattern = regexp.MustCompile(`(?is)\btask[-_]?id\s*=\s*["']([^"']+)["']`)

func synthesizeTaskOutputToolCallTextFromTaskNotification(finalPrompt string, toolNames []string, allowMetaAgentTools bool) string {
	calls := synthesizeTaskOutputToolCallsFromTaskNotification(finalPrompt, toolNames, allowMetaAgentTools)
	if len(calls) == 0 {
		return ""
	}
	return formatParsedToolCallsAsPromptXML(calls)
}

func synthesizeTaskOutputToolCallsFromTaskNotification(finalPrompt string, toolNames []string, allowMetaAgentTools bool) []toolcall.ParsedToolCall {
	if !allowMetaAgentTools {
		return nil
	}
	toolName, ok := findTaskOutputToolName(toolNames)
	if !ok {
		return nil
	}
	latestUser := html.UnescapeString(latestUserPromptBlock(finalPrompt))
	if !strings.Contains(strings.ToLower(latestUser), "<task-notification") {
		return nil
	}
	ids := extractTaskNotificationIDs(latestUser)
	if len(ids) == 0 {
		return nil
	}
	calls := make([]toolcall.ParsedToolCall, 0, len(ids))
	for _, id := range ids {
		calls = append(calls, toolcall.ParsedToolCall{
			Name: toolName,
			Input: map[string]any{
				"task_id": id,
				"block":   false,
				"timeout": 5000,
			},
		})
	}
	return calls
}

func findTaskOutputToolName(toolNames []string) (string, bool) {
	for _, name := range toolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || trimmed == "__any_tool__" {
			continue
		}
		if canonicalTaskOutputToolName(trimmed) == "taskoutput" {
			return trimmed, true
		}
	}
	return "", false
}

func canonicalTaskOutputToolName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func latestUserPromptBlock(finalPrompt string) string {
	const userMarker = "<｜User｜>"
	const assistantMarker = "<｜Assistant｜>"
	start := strings.LastIndex(finalPrompt, userMarker)
	if start < 0 {
		return finalPrompt
	}
	tail := finalPrompt[start+len(userMarker):]
	if end := strings.Index(tail, assistantMarker); end >= 0 {
		tail = tail[:end]
	}
	return tail
}

func extractTaskNotificationIDs(text string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	addID := func(raw string) {
		id := cleanSyntheticTaskID(raw)
		if id == "" {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, blockMatch := range taskNotificationBlockPattern.FindAllStringSubmatch(text, -1) {
		if len(blockMatch) < 2 {
			continue
		}
		block := blockMatch[0]
		for _, attrMatch := range taskIDAttrPattern.FindAllStringSubmatch(block, -1) {
			if len(attrMatch) >= 2 {
				addID(attrMatch[1])
			}
		}
		for _, tagMatch := range taskIDTagPattern.FindAllStringSubmatch(block, -1) {
			if len(tagMatch) >= 2 {
				addID(tagMatch[1])
			}
		}
	}
	return out
}

func cleanSyntheticTaskID(raw string) string {
	id := strings.TrimSpace(html.UnescapeString(raw))
	if id == "" || strings.Contains(id, "<") || strings.Contains(id, ">") {
		return ""
	}
	return id
}
