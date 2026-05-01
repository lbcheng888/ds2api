package claudecode

import (
	"html"
	"regexp"
	"strings"

	"ds2api/internal/protocol"
	"ds2api/internal/toolcall"
)

var taskNotificationBlockPattern = regexp.MustCompile(`(?is)<task-notification\b[^>]*>(.*?)</task-notification>`)
var taskIDTagPattern = regexp.MustCompile(`(?is)<task[-_]?id\b[^>]*>(.*?)</task[-_]?id>`)
var taskIDAttrPattern = regexp.MustCompile(`(?is)\btask[-_]?id\s*=\s*["']([^"']+)["']`)

func FindBackgroundAgentToolName(toolNames []string) (string, bool) {
	for _, name := range toolNames {
		trimmed := strings.TrimSpace(name)
		if strings.EqualFold(trimmed, "Agent") {
			return trimmed, true
		}
	}
	for _, name := range toolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || trimmed == "__any_tool__" {
			continue
		}
		if toolcall.IsBackgroundAgentToolName(trimmed) {
			return trimmed, true
		}
	}
	return "", false
}

func FindTaskOutputToolName(toolNames []string) (string, bool) {
	for _, name := range toolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || trimmed == "__any_tool__" {
			continue
		}
		if CanonicalTaskOutputToolName(trimmed) == "taskoutput" {
			return trimmed, true
		}
	}
	return "", false
}

func CanonicalTaskOutputToolName(name string) string {
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

func LatestUserPromptBlock(finalPrompt string) string {
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

func ExtractTaskNotificationIDs(text string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	addID := func(raw string) {
		id := protocol.CleanTaskID(raw)
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

func LooksLikeAgentWaitingText(text string) bool {
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

func RecentPromptHasBackgroundAgentLaunch(finalPrompt string) bool {
	const maxTailBytes = 12000
	tail := latestConversationTurnBlock(finalPrompt)
	if len(tail) > maxTailBytes {
		tail = tail[len(tail)-maxTailBytes:]
	}
	lower := strings.ToLower(tail)
	for _, phrase := range []string{
		"async agent launched successfully",
		"the agent is working in the background",
		"backgrounded agent",
		"background agent launched",
		"background agents launched",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func LatestUserRequestsAdditionalAgentLaunch(finalPrompt string) bool {
	latest := strings.ToLower(html.UnescapeString(LatestUserPromptBlock(finalPrompt)))
	if !containsAny(latest, []string{"agent", "代理", "子代理", "team agents"}) {
		return false
	}
	return containsAny(latest, []string{
		"再启动",
		"再开",
		"再用",
		"追加",
		"重新启动",
		"另启",
		"另外启动",
		"more agents",
		"additional agents",
		"start more",
		"launch more",
		"restart agents",
	})
}

func latestConversationTurnBlock(finalPrompt string) string {
	const userMarker = "<｜User｜>"
	start := strings.LastIndex(finalPrompt, userMarker)
	if start < 0 {
		return finalPrompt
	}
	return finalPrompt[start:]
}

func LooksLikeUnexecutedAgentLaunch(finalText, finalPrompt string, allowMetaAgentTools bool) bool {
	if !allowMetaAgentTools {
		return false
	}
	if stripped, changed := StripEmptyToolCallContainerNoise(finalText); changed {
		finalText = stripped
	}
	trimmed := strings.TrimSpace(finalText)
	if trimmed == "" {
		return false
	}
	trimmed = agentLaunchEvidenceText(trimmed)
	if trimmed == "" || len([]rune(trimmed)) > 800 || strings.Count(trimmed, "\n") > 8 {
		return false
	}
	lower := strings.ToLower(trimmed)
	latest := strings.ToLower(LatestUserPromptBlock(finalPrompt))
	if !containsAny(lower, []string{"agent", "代理"}) && !containsAny(latest, []string{"team agents", "agent", "代理"}) {
		return false
	}
	if containAnyAgentLaunchPatterns(lower) {
		return true
	}
	return false
}

var agentLaunchNumPattern = regexp.MustCompile(`(?i)(?:launch|start|create|run|spawn)\s+\d+\s*(?:agent|sub-agent|subagent)s?`)

func containAnyAgentLaunchPatterns(lower string) bool {
	if agentLaunchNumPattern.MatchString(lower) {
		return true
	}
	return containsAny(lower, []string{
		"launch agent",
		"launch agents",
		"launch implementation agents",
		"launch parallel implementation agents",
		"start agent",
		"start agents",
		"parallel agent",
		"parallel agents",
		"background agent",
		"start team agents",
		"launch team agents",
		"启动代理",
		"启动 agent",
		"启动agent",
		"启动 team agents",
		"启动team agents",
		"启动 4 个并行",
		"启动4个并行",
		"启动四个并行",
		"启动 4 个实现子代理",
		"启动4个实现子代理",
		"启动四个实现子代理",
		"启动多个实现子代理",
		"实现子代理",
		"启动 4 个实现代理",
		"启动4个实现代理",
		"启动四个实现代理",
		"启动多个实现代理",
		"实现代理并行启动",
		"实现代理并行启动中",
		"实现代理",
		"启动子代理",
		"启动多个子代理",
		"启动多 个子代理",
		"启动并行子代理",
		"启动 子代理",
		"子代理并行",
		"多个子代理",
		"多 个子代理",
		"并行代理",
		"个并行代理",
		"提交后启动",
		"提交，再启动",
		"提交，然后启动",
		"先提交，再启动",
		"提交当前修复，然后启动",
		"启动 4 个",
		"启动4个",
	})
}

func agentLaunchEvidenceText(text string) string {
	if len([]rune(text)) <= 800 && strings.Count(text, "\n") <= 8 {
		return text
	}
	compact := strings.Join(strings.Fields(text), " ")
	lower := strings.ToLower(compact)
	needles := []string{
		"launch parallel implementation agents",
		"launch implementation agents",
		"launch parallel agents",
		"launch agents",
		"launch ",
		"start agents",
		"start ",
		"parallel agents",
		"background agents",
		"启动 4 个实现子代理",
		"启动4个实现子代理",
		"启动四个实现子代理",
		"启动多个实现子代理",
		"实现子代理",
		"启动 4 个实现代理",
		"启动4个实现代理",
		"启动四个实现代理",
		"启动多个实现代理",
		"实现代理并行启动",
		"实现代理",
		"启动子代理",
		"启动多个子代理",
		"启动并行子代理",
		"子代理并行",
		"多个子代理",
		"启动 4 个并行",
		"启动4个并行",
		"并行代理",
	}
	best := -1
	for _, needle := range needles {
		if idx := strings.LastIndex(lower, needle); idx >= 0 && idx > best {
			best = idx
		}
	}
	if best < 0 {
		return ""
	}
	return runeWindowAroundByte(compact, best, 800)
}

func runeWindowAroundByte(text string, byteIdx int, width int) string {
	if width <= 0 {
		return ""
	}
	if byteIdx < 0 {
		byteIdx = 0
	}
	if byteIdx > len(text) {
		byteIdx = len(text)
	}
	runes := []rune(text)
	center := len([]rune(text[:byteIdx]))
	start := center - width/2
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(runes) {
		end = len(runes)
		start = end - width
		if start < 0 {
			start = 0
		}
	}
	return string(runes[start:end])
}

func allowedTaskOutputIDSet(finalPrompt string) map[string]struct{} {
	return ExtractTeamAgentState(finalPrompt).AllowedTaskOutputIDSet()
}

func inputString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func compactPromptText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	const maxRunes = 900
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
