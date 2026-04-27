package openai

import (
	"html"
	"net/http"
	"regexp"
	"strings"

	"ds2api/internal/protocol"
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
	states := protocol.ExtractTaskStates(finalPrompt)
	ids := protocol.TaskIDsWithStatus(states, protocol.TaskStatusRunning)
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

func invalidTaskOutputCallDetail(calls []toolcall.ParsedToolCall, finalPrompt string) (int, string, string, bool) {
	invalid := invalidTaskOutputIDs(calls, finalPrompt)
	if len(invalid) == 0 {
		return 0, "", "", false
	}
	return http.StatusBadGateway,
		"Upstream model requested TaskOutput for an unknown or inactive task_id.",
		upstreamInvalidToolCallCode,
		true
}

func invalidTaskOutputIDs(calls []toolcall.ParsedToolCall, finalPrompt string) []string {
	if len(calls) == 0 {
		return nil
	}
	allowed := allowedTaskOutputIDSet(finalPrompt)
	invalid := []string{}
	for _, call := range calls {
		if canonicalTaskOutputToolName(call.Name) != "taskoutput" {
			continue
		}
		id := protocol.CleanTaskID(taskOutputInputString(call.Input["task_id"]))
		if id == "" {
			id = protocol.CleanTaskID(taskOutputInputString(call.Input["taskId"]))
		}
		if id == "" {
			continue
		}
		if _, ok := allowed[id]; !ok {
			invalid = append(invalid, id)
		}
	}
	return invalid
}

func allowedTaskOutputIDSet(finalPrompt string) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, state := range protocol.ExtractTaskStates(finalPrompt) {
		if state.Status != protocol.TaskStatusRunning && state.Status != protocol.TaskStatusCompleted {
			continue
		}
		id := protocol.CleanTaskID(state.TaskID)
		if id != "" {
			allowed[id] = struct{}{}
		}
	}
	latestUser := html.UnescapeString(latestUserPromptBlock(finalPrompt))
	if strings.Contains(strings.ToLower(latestUser), "<task-notification") {
		for _, id := range extractTaskNotificationIDs(latestUser) {
			if id != "" {
				allowed[id] = struct{}{}
			}
		}
	}
	return allowed
}

func taskOutputInputString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
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

func cleanSyntheticTaskID(raw string) string {
	return protocol.CleanTaskID(raw)
}
