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
var taskIDJSONPattern = regexp.MustCompile(`(?is)["']task[-_]?id["']\s*:\s*["']([^"']+)["']`)
var taskOutputLineIDPattern = regexp.MustCompile(`(?is)\bTask\s+Output(?:\s*\([^)]*\))?\s+([a-z0-9_-]{8,})\b`)
var prefixedTaskIDPattern = regexp.MustCompile(`(?is)\b(task[_-][a-z0-9_-]{4,})\b`)

func synthesizeTaskOutputToolCallTextFromTaskNotification(finalPrompt string, toolNames []string, allowMetaAgentTools bool) string {
	calls := synthesizeTaskOutputToolCallsFromTaskNotification(finalPrompt, toolNames, allowMetaAgentTools)
	if len(calls) == 0 {
		return ""
	}
	return formatParsedToolCallsAsPromptXML(calls)
}

func synthesizeTaskOutputToolCallTextFromAgentWaiting(finalPrompt, finalText string, toolNames []string, allowMetaAgentTools bool) string {
	calls := synthesizeTaskOutputToolCallsFromAgentWaiting(finalPrompt, finalText, toolNames, allowMetaAgentTools)
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

func synthesizeTaskOutputToolCallsFromAgentWaiting(finalPrompt, finalText string, toolNames []string, allowMetaAgentTools bool) []toolcall.ParsedToolCall {
	if !allowMetaAgentTools || !looksLikeAgentWaitingText(finalText) {
		return nil
	}
	if toolcall.LooksLikeToolCallSyntax(finalText) {
		return nil
	}
	toolName, ok := findTaskOutputToolName(toolNames)
	if !ok {
		return nil
	}
	ids := extractRunningTaskOutputIDs(finalPrompt)
	if len(ids) == 0 {
		ids = extractAllTaskIDs(recentTaskPromptWindow(finalPrompt))
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > 4 {
		ids = ids[len(ids)-4:]
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

func looksLikeAgentWaitingText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len([]rune(trimmed)) > 800 {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "agent") {
		for _, phrase := range []string{
			"still running",
			"still in progress",
			"wait for",
			"waiting for",
			"after they complete",
			"after all agents",
			"background agents",
			"local agents",
		} {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
	}
	if strings.Contains(trimmed, "代理") {
		return strings.Contains(trimmed, "等待") ||
			strings.Contains(trimmed, "完成后") ||
			strings.Contains(trimmed, "汇总") ||
			strings.Contains(trimmed, "仍在运行")
	}
	return false
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

func extractAllTaskIDs(text string) []string {
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
	for _, id := range extractTaskNotificationIDs(text) {
		addID(id)
	}
	for _, pattern := range []*regexp.Regexp{taskIDAttrPattern, taskIDTagPattern, taskIDJSONPattern, taskOutputLineIDPattern, prefixedTaskIDPattern} {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			if len(match) >= 2 {
				addID(match[1])
			}
		}
	}
	return out
}

func extractRunningTaskOutputIDs(text string) []string {
	text = recentTaskPromptWindow(text)
	seen := map[string]struct{}{}
	out := []string{}
	matches := taskOutputLineIDPattern.FindAllStringSubmatchIndex(text, -1)
	for i, match := range matches {
		if len(match) < 4 {
			continue
		}
		windowStart := match[1]
		windowEnd := len(text)
		if i+1 < len(matches) {
			windowEnd = matches[i+1][0]
		}
		if windowEnd-windowStart > 800 {
			windowEnd = windowStart + 800
		}
		if !taskOutputWindowStillRunning(text[windowStart:windowEnd]) {
			continue
		}
		id := cleanSyntheticTaskID(text[match[2]:match[3]])
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func taskOutputWindowStillRunning(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range []string{
		"still running",
		"still in progress",
		"task is running",
		"not done",
		"not completed",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return strings.Contains(text, "仍在运行") ||
		strings.Contains(text, "正在运行") ||
		strings.Contains(text, "尚未完成") ||
		strings.Contains(text, "未完成")
}

func recentTaskPromptWindow(text string) string {
	const max = 20000
	if len(text) <= max {
		return text
	}
	return text[len(text)-max:]
}

func cleanSyntheticTaskID(raw string) string {
	id := strings.TrimSpace(html.UnescapeString(raw))
	if id == "" || strings.Contains(id, "<") || strings.Contains(id, ">") {
		return ""
	}
	return id
}
