package claudecode

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"ds2api/internal/toolcall"
)

const AgentSchedulerMaxBackgroundAgents = 4

type AgentSchedulerInput struct {
	FinalPrompt         string
	Text                string
	FinalText           string
	PromiseText         string
	ToolNames           []string
	AllowMetaAgentTools bool
}

type AgentSchedulerDecision struct {
	Calls      []toolcall.ParsedToolCall
	Reason     string
	Suppressed bool
}

func ScheduleAgentToolCallsFromLaunchPromise(in AgentSchedulerInput) []toolcall.ParsedToolCall {
	return ScheduleAgentLaunch(in).Calls
}

func ScheduleAgentToolCallTextFromLaunchPromise(in AgentSchedulerInput) string {
	calls := ScheduleAgentToolCallsFromLaunchPromise(in)
	if len(calls) == 0 {
		return ""
	}
	return FormatParsedToolCallsAsPromptXML(calls)
}

func ScheduleAgentLaunch(in AgentSchedulerInput) AgentSchedulerDecision {
	if !in.AllowMetaAgentTools {
		return AgentSchedulerDecision{Reason: "meta_agent_tools_disabled"}
	}
	toolName, ok := FindBackgroundAgentToolName(in.ToolNames)
	if !ok {
		return AgentSchedulerDecision{Reason: "agent_tool_unavailable"}
	}
	if agentSchedulerHasCurrentTurnAgentEvidence(in.FinalPrompt) && !LatestUserRequestsAdditionalAgentLaunch(in.FinalPrompt) {
		return AgentSchedulerDecision{Reason: "current_turn_agent_exists", Suppressed: true}
	}
	text := agentSchedulerPromiseText(in)
	if !agentSchedulerLooksLikeLaunchPromise(text, in.FinalPrompt) {
		return AgentSchedulerDecision{Reason: "no_launch_promise"}
	}
	if looksLikeInvalidLegacyToolCallSyntax(text) || toolcall.LooksLikeToolCallSyntax(text) {
		return AgentSchedulerDecision{Reason: "already_tool_syntax"}
	}
	request := agentSchedulerRequestText(in.FinalPrompt, text)
	if request == "" {
		return AgentSchedulerDecision{Reason: "empty_request"}
	}
	count := agentSchedulerLaunchCount(in.FinalPrompt, text)
	tasks := agentSchedulerPromisedTasks(text, count)
	calls := agentSchedulerBuildCalls(toolName, request, text, tasks, count)
	if len(calls) == 0 {
		return AgentSchedulerDecision{Reason: "empty_schedule"}
	}
	return AgentSchedulerDecision{Calls: calls, Reason: "scheduled"}
}

func agentSchedulerPromiseText(in AgentSchedulerInput) string {
	for _, text := range []string{in.PromiseText, in.FinalText, in.Text} {
		if strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func agentSchedulerRequestText(finalPrompt, fallback string) string {
	latestUser := html.UnescapeString(LatestUserPromptBlock(finalPrompt))
	latestUser = systemReminderBlockPattern.ReplaceAllString(latestUser, " ")
	if request := compactPromptText(latestUser); request != "" {
		return request
	}
	return compactPromptText(fallback)
}

func agentSchedulerHasCurrentTurnAgentEvidence(finalPrompt string) bool {
	state := ExtractTeamAgentState(latestConversationTurnBlock(finalPrompt))
	return state.HasRecentBackgroundLaunch || len(state.Running) > 0 || len(state.Completed) > 0
}

func agentSchedulerLooksLikeLaunchPromise(text, finalPrompt string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if LooksLikeUnexecutedAgentLaunch(trimmed, finalPrompt, true) {
		return true
	}
	evidence := agentLaunchEvidenceText(trimmed)
	if evidence != "" {
		trimmed = evidence
	}
	if len([]rune(trimmed)) > 1000 || strings.Count(trimmed, "\n") > 12 {
		return false
	}
	lower := strings.ToLower(trimmed)
	if agentSchedulerExtractExplicitCount(trimmed) > 0 {
		return strings.Contains(lower, "agent") ||
			strings.Contains(trimmed, "代理") ||
			strings.Contains(trimmed, "子代理") ||
			agentSchedulerRouteSplitPattern.MatchString(trimmed)
	}
	return containsAny(lower, []string{"team agents", "launch agents", "start agents"}) ||
		containsAny(trimmed, []string{"启动多个子代理", "准备多个子代理", "子代理并行", "多个子代理", "分多路"})
}

func agentSchedulerLaunchCount(finalPrompt, text string) int {
	if count := agentSchedulerExtractExplicitCount(text); count > 0 {
		return agentSchedulerClampCount(count)
	}
	latestUser := html.UnescapeString(LatestUserPromptBlock(finalPrompt))
	if count := agentSchedulerExtractExplicitCount(latestUser); count > 0 {
		return agentSchedulerClampCount(count)
	}
	if tasks := agentSchedulerPromisedTasks(text, AgentSchedulerMaxBackgroundAgents); len(tasks) > 0 {
		return agentSchedulerClampCount(len(tasks))
	}
	if agentSchedulerLooksPluralLaunch(text) || agentSchedulerLooksPluralLaunch(latestUser) {
		return AgentSchedulerMaxBackgroundAgents
	}
	return 1
}

func agentSchedulerClampCount(count int) int {
	if count < 1 {
		return 1
	}
	if count > AgentSchedulerMaxBackgroundAgents {
		return AgentSchedulerMaxBackgroundAgents
	}
	return count
}

var (
	agentSchedulerEnglishCountPattern  = regexp.MustCompile(`(?i)(?:launch|start|create|run|spawn|prepare)\s+(\d+|one|two|three|four)\s*(?:agent|sub-agent|subagent)s?`)
	agentSchedulerChineseCountPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?:启动|创建|运行|生成|发起|调用|准备|派|开)\s*(` + chineseAgentCountTokenPattern + `)\s*个?\s*(?:同时|并行)?\s*(?:实现)?(?:子代理|代理|agent|agents)`),
		regexp.MustCompile(`(` + chineseAgentCountTokenPattern + `)\s*个?\s*(?:实现)?(?:子代理|代理)\s*(?:同时|并行)?(?:推进|实现|处理|启动|发起|调用|开|派)?`),
		regexp.MustCompile(`(` + chineseAgentCountTokenPattern + `)\s*个?\s*(?:同时|并行)\s*(?:启动|发起|调用|开|派)`),
		regexp.MustCompile(`(?:同时|并行)\s*(?:启动|发起|调用|开|派)\s*(` + chineseAgentCountTokenPattern + `)\s*个?\s*(?:实现)?(?:子代理|代理)?`),
		regexp.MustCompile(`分\s*(` + chineseAgentCountTokenPattern + `)\s*路`),
	}
	agentSchedulerRouteSplitPattern = regexp.MustCompile(`分\s*` + chineseAgentCountTokenPattern + `\s*路`)
	agentSchedulerPluralPattern     = regexp.MustCompile(`(?i)(?:multiple|several)\s+(?:agent|sub-agent|subagent)s?`)
)

func agentSchedulerExtractExplicitCount(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	if match := agentSchedulerEnglishCountPattern.FindStringSubmatch(text); len(match) >= 2 {
		if count, ok := agentSchedulerParseCountToken(match[1]); ok {
			return count
		}
	}
	for _, pattern := range agentSchedulerChineseCountPatterns {
		if match := pattern.FindStringSubmatch(text); len(match) >= 2 {
			if count, ok := agentSchedulerParseCountToken(match[1]); ok {
				return count
			}
		}
	}
	return 0
}

func agentSchedulerParseCountToken(raw string) (int, bool) {
	token := strings.ToLower(strings.TrimSpace(raw))
	switch token {
	case "one":
		return 1, true
	case "two":
		return 2, true
	case "three":
		return 3, true
	case "four":
		return 4, true
	}
	if count, err := strconv.Atoi(token); err == nil {
		return count, count >= 1
	}
	token = strings.ReplaceAll(token, "两", "二")
	token = strings.ReplaceAll(token, "俩", "二")
	if digit, ok := agentSchedulerChineseDigit(token); ok {
		return digit, true
	}
	parts := strings.Split(token, "十")
	if len(parts) != 2 {
		return 0, false
	}
	tens := 1
	if parts[0] != "" {
		digit, ok := agentSchedulerChineseDigit(parts[0])
		if !ok {
			return 0, false
		}
		tens = digit
	}
	ones := 0
	if parts[1] != "" {
		digit, ok := agentSchedulerChineseDigit(parts[1])
		if !ok {
			return 0, false
		}
		ones = digit
	}
	return tens*10 + ones, true
}

func agentSchedulerChineseDigit(raw string) (int, bool) {
	switch raw {
	case "一":
		return 1, true
	case "二":
		return 2, true
	case "三":
		return 3, true
	case "四":
		return 4, true
	case "五":
		return 5, true
	case "六":
		return 6, true
	case "七":
		return 7, true
	case "八":
		return 8, true
	case "九":
		return 9, true
	default:
		return 0, false
	}
}

func agentSchedulerLooksPluralLaunch(text string) bool {
	lower := strings.ToLower(text)
	return agentSchedulerPluralPattern.MatchString(text) ||
		containsAny(lower, []string{"team agents", "launch agents", "start agents", "agents in parallel"}) ||
		containsAny(text, []string{"多个子代理", "多 个子代理", "若干子代理", "几个子代理", "子代理并行", "并行代理", "分多路"})
}

var (
	agentSchedulerNumberedTaskPattern = regexp.MustCompile(`(?m)^\s*(?:[-*]\s*)?(?:\d+[.)、]|[一二两俩三四五六七八九十]+[.)、])\s*(.+?)\s*$`)
	agentSchedulerChineseTaskPattern  = regexp.MustCompile(`(?:^|[：:，,、；;\n。])\s*(?:一个|一位|第[一二两俩三四五六七八九十\d]+个|[一二两俩三四五六七八九十\d]+[.、])\s*([^：:，,、；;\n。]+)`)
)

func agentSchedulerPromisedTasks(text string, max int) []string {
	if max <= 0 {
		return nil
	}
	seen := map[string]struct{}{}
	tasks := make([]string, 0, max)
	add := func(raw string) {
		if len(tasks) >= max {
			return
		}
		task := agentSchedulerCleanTask(raw)
		if task == "" {
			return
		}
		if _, exists := seen[task]; exists {
			return
		}
		seen[task] = struct{}{}
		tasks = append(tasks, task)
	}
	for _, match := range agentSchedulerNumberedTaskPattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			add(match[1])
		}
	}
	for _, match := range agentSchedulerChineseTaskPattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			add(match[1])
		}
	}
	return tasks
}

func agentSchedulerCleanTask(raw string) string {
	task := strings.TrimSpace(raw)
	task = strings.Trim(task, "`\"' ")
	task = strings.TrimRight(task, "。；;，,、")
	task = compactPromptText(task)
	if task == "" || len([]rune(task)) < 3 {
		return ""
	}
	lower := strings.ToLower(task)
	if containsAny(lower, []string{"start agents", "launch agents", "agents completed"}) ||
		containsAny(task, []string{"同时启动", "汇总结果", "然后汇总", "等待", "再汇总"}) {
		return ""
	}
	return task
}

type agentSchedulerRole struct {
	Description  string
	SubagentType string
	Instruction  string
}

var agentSchedulerReadOnlyRoles = []agentSchedulerRole{
	{
		Description:  "Map implementation route",
		SubagentType: "Explore",
		Instruction:  "Map the implementation route, current blockers, key files, and the smallest sequence of executable steps. Read-only analysis; do not edit files or commit.",
	},
	{
		Description:  "Review code risks",
		SubagentType: "code-reviewer",
		Instruction:  "Review likely correctness, compatibility, and protocol risks for this request. Read-only analysis; do not edit files or commit.",
	},
	{
		Description:  "Design end-state",
		SubagentType: "design",
		Instruction:  "Design the target end-state and rollout strategy. Read-only analysis; do not edit files or commit.",
	},
	{
		Description:  "Plan verification",
		SubagentType: "Explore",
		Instruction:  "Find the local verification commands, tests, and observability checks needed to prove the work. Read-only analysis; do not edit files or commit.",
	},
}

var agentSchedulerExecutionRoles = []agentSchedulerRole{
	{
		Description:  "Implement primary slice",
		SubagentType: "Explore",
		Instruction:  "Implement a focused, high-impact slice of the user's request. Edit files directly, keep the change scoped, and report changed files and verification results.",
	},
	{
		Description:  "Implement adjacent slice",
		SubagentType: "Explore",
		Instruction:  "Implement another independent slice of the user's request. Edit files directly, avoid overlapping work, and report changed files and verification results.",
	},
	{
		Description:  "Repair integration risks",
		SubagentType: "code-reviewer",
		Instruction:  "Find correctness or integration risks in the requested change and fix them directly when clear. Edit files directly and report changed files and verification results.",
	},
	{
		Description:  "Verify and finish",
		SubagentType: "Explore",
		Instruction:  "Run targeted verification for the requested change, fix directly exposed issues when practical, and report changed files and verification results.",
	},
}

func agentSchedulerBuildCalls(toolName, request, promise string, tasks []string, count int) []toolcall.ParsedToolCall {
	count = agentSchedulerClampCount(count)
	calls := make([]toolcall.ParsedToolCall, 0, count)
	for i := 0; i < len(tasks) && i < count; i++ {
		task := tasks[i]
		execute := agentSchedulerTaskExecutionRequested(request, promise, task)
		calls = append(calls, agentSchedulerNewCall(
			toolName,
			agentSchedulerTaskDescription(task, i),
			agentSchedulerRoleForIndex(i, execute).SubagentType,
			request,
			agentSchedulerConcreteInstruction(task, execute),
		))
	}
	genericExecute := agentSchedulerExecutionRequested(request + " " + promise)
	for i := len(calls); i < count; i++ {
		role := agentSchedulerRoleForIndex(i, genericExecute)
		calls = append(calls, agentSchedulerNewCall(toolName, role.Description, role.SubagentType, request, role.Instruction))
	}
	return calls
}

func agentSchedulerRoleForIndex(index int, execute bool) agentSchedulerRole {
	roles := agentSchedulerReadOnlyRoles
	if execute {
		roles = agentSchedulerExecutionRoles
	}
	if index < 0 {
		index = 0
	}
	if index >= len(roles) {
		index = len(roles) - 1
	}
	return roles[index]
}

func agentSchedulerTaskExecutionRequested(request, promise, task string) bool {
	if task != "" {
		if agentSchedulerExecutionRequested(task) {
			return true
		}
		if agentSchedulerReadOnlyRequested(task) {
			return false
		}
	}
	return agentSchedulerExecutionRequested(request + " " + promise)
}

var (
	agentSchedulerEnglishExecutionPattern = regexp.MustCompile(`(?i)\b(?:implement|fix|modify|edit|patch|refactor|complete|proceed|continue|apply|land|write|build)\b`)
	agentSchedulerChineseEditPattern      = regexp.MustCompile(`(?:^|[：:，,、；;\s])改(?:$|[\sA-Za-z0-9_./-]|[^\x00-\x7F])`)
	agentSchedulerReadOnlyPattern         = regexp.MustCompile(`(?i)\b(?:analy[sz]e|review|assess|evaluate|inspect|research|audit)\b`)
)

func agentSchedulerExecutionRequested(text string) bool {
	lower := strings.ToLower(text)
	return agentSchedulerEnglishExecutionPattern.MatchString(lower) ||
		agentSchedulerChineseEditPattern.MatchString(text) ||
		containsAny(text, []string{
			"一口气实现",
			"并行实现",
			"实现这些",
			"实现功能",
			"实现方案",
			"直接动手",
			"写到位",
			"现在实现",
			"现在修改",
			"现在写",
			"修复",
			"修改",
			"改造",
			"落地",
			"写入",
			"补齐",
			"重构",
			"应用",
			"推进",
			"完成",
		})
}

func agentSchedulerReadOnlyRequested(text string) bool {
	return agentSchedulerReadOnlyPattern.MatchString(text) ||
		containsAny(text, []string{"评估", "分析", "审查", "审阅", "研究", "检查", "梳理"})
}

func agentSchedulerConcreteInstruction(task string, execute bool) string {
	if execute {
		return "Execute this concrete subtask: " + task + ". Edit files directly when code changes are needed, keep edits scoped to this subtask, and report changed files and verification results."
	}
	return "Analyze this concrete subtask: " + task + ". Read-only analysis; do not edit files or commit. Report concrete file/path references where possible."
}

func agentSchedulerTaskDescription(task string, index int) string {
	task = compactPromptText(task)
	runes := []rune(task)
	if len(runes) > 72 {
		task = string(runes[:72])
	}
	if task == "" {
		return "Agent subtask " + strconv.Itoa(index+1)
	}
	return task
}

func agentSchedulerNewCall(toolName, description, subagentType, request, instruction string) toolcall.ParsedToolCall {
	return toolcall.ParsedToolCall{
		Name: toolName,
		Input: map[string]any{
			"description":       description,
			"prompt":            "User request:\n" + request + "\n\n" + instruction + "\nKeep the report concise and actionable.",
			"subagent_type":     subagentType,
			"run_in_background": true,
		},
	}
}
