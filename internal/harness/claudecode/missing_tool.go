package claudecode

import (
	"regexp"
	"strings"

	"ds2api/internal/toolcall"
)

const MissingToolCallCode = "upstream_missing_tool_call"
const InvalidToolCallCode = "upstream_invalid_tool_call"

type MissingToolCallInput struct {
	Text                string
	FinalPrompt         string
	ToolNames           []string
	ToolSchemas         toolcall.ParameterSchemas
	AllowMetaAgentTools bool
	Profile             string
}

type MissingToolCallDecision struct {
	Blocked bool
	Message string
	Code    string
}

func DetectMissingToolCall(in MissingToolCallInput) MissingToolCallDecision {
	finalText := strings.TrimSpace(in.Text)
	if stripped, changed := StripEmptyToolCallContainerNoise(finalText); changed {
		finalText = strings.TrimSpace(stripped)
	}
	if finalText == "" {
		return MissingToolCallDecision{}
	}
	if IsBackgroundAgentAcknowledgement(in.FinalPrompt, finalText, in.AllowMetaAgentTools) {
		return MissingToolCallDecision{}
	}
	parsed := toolcall.ParseStandaloneToolCallsDetailed(finalText, in.ToolNames)
	if len(toolcall.NormalizeCallsForSchemasWithMeta(parsed.Calls, in.ToolSchemas, in.AllowMetaAgentTools)) > 0 {
		return MissingToolCallDecision{}
	}
	if len(parsed.Calls) > 0 && toolcall.AllCallsAreTaskTrackingTools(parsed.Calls) {
		return missingToolDecision(in.Profile)
	}
	if looksLikeFencedJSONToolCall(finalText, in.ToolNames) {
		return missingToolDecision(in.Profile)
	}
	if parsed.SawToolCallSyntax {
		return invalidToolSyntaxDecision(in.Profile)
	}
	if looksLikeInvalidLegacyToolCallSyntax(finalText) {
		return invalidToolSyntaxDecision(in.Profile)
	}
	if LooksLikeUnexecutedAgentLaunch(finalText, in.FinalPrompt, in.AllowMetaAgentTools) {
		return missingToolDecision(in.Profile)
	}
	if !HasCallableTools(in.ToolNames) {
		return MissingToolCallDecision{}
	}
	if looksLikeExplicitUnexecutedFileToolPlan(finalText) {
		return missingToolDecision(in.Profile)
	}
	if looksLikeUnexecutedCodingAction(finalText, in.FinalPrompt) {
		return missingToolDecision(in.Profile)
	}
	if looksLikeUnsupportedCompletionClaim(finalText, in.FinalPrompt) {
		return missingToolDecision(in.Profile)
	}
	if !looksLikeFutureToolAction(finalText) {
		return MissingToolCallDecision{}
	}
	return missingToolDecision(in.Profile)
}

func looksLikeInvalidLegacyToolCallSyntax(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "<toolcall") || strings.Contains(lower, "</toolcall>")
}

func HasCallableTools(toolNames []string) bool {
	for _, name := range toolNames {
		if strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}

var fencedCodeBlockPattern = regexp.MustCompile("(?is)```\\s*([a-zA-Z0-9_-]*)\\s*\\n(.*?)```")

func looksLikeFencedJSONToolCall(text string, toolNames []string) bool {
	for _, match := range fencedCodeBlockPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 3 {
			continue
		}
		lang := strings.ToLower(strings.TrimSpace(match[1]))
		body := strings.TrimSpace(match[2])
		if body == "" {
			continue
		}
		if lang != "" && lang != "json" && lang != "jsonc" && lang != "javascript" && lang != "js" {
			continue
		}
		if !strings.Contains(strings.ToLower(body), `"tool"`) && !strings.Contains(strings.ToLower(body), `"function"`) {
			continue
		}
		_, calls, _, ok := toolcall.ExtractVisibleJSONToolCalls(body, toolNames)
		if ok && len(calls) > 0 {
			return true
		}
	}
	return false
}

func IsBackgroundAgentAcknowledgement(finalPrompt, finalText string, allowMetaAgentTools bool) bool {
	if !allowMetaAgentTools || !RecentPromptHasBackgroundAgentLaunch(finalPrompt) {
		return false
	}
	trimmed := strings.TrimSpace(finalText)
	if trimmed == "" || len([]rune(trimmed)) > 1200 {
		return false
	}
	lower := strings.ToLower(trimmed)
	if !strings.Contains(lower, "agent") && !strings.Contains(trimmed, "代理") {
		return false
	}
	for _, phrase := range []string{
		"wait for",
		"waiting for",
		"after the agent",
		"after agents",
		"after all agents",
		"agent returns",
		"agent completes",
		"background agent",
		"agent 返回",
		"agent 完成",
		"等待",
		"返回后",
		"完成后",
		"汇总",
		"仍在运行",
	} {
		if strings.Contains(lower, phrase) || strings.Contains(trimmed, phrase) {
			return true
		}
	}
	return false
}

func missingToolDecision(profile string) MissingToolCallDecision {
	recordFailureDecision(profile, MissingToolCallCode)
	return MissingToolCallDecision{
		Blocked: true,
		Message: "Upstream model promised tool work but emitted no tool call.",
		Code:    MissingToolCallCode,
	}
}

func invalidToolSyntaxDecision(profile string) MissingToolCallDecision {
	recordFailureDecision(profile, InvalidToolCallCode)
	return MissingToolCallDecision{
		Blocked: true,
		Message: "Upstream model emitted invalid tool call syntax.",
		Code:    InvalidToolCallCode,
	}
}

func looksLikeUnexecutedCodingAction(finalText, finalPrompt string) bool {
	if !latestUserRequestedExecution(finalPrompt) {
		return false
	}
	trimmed := strings.TrimSpace(finalText)
	if trimmed == "" || len([]rune(trimmed)) > 800 || strings.Count(trimmed, "\n") > 8 {
		return false
	}
	if !containsAny(trimmed, []string{
		"只需要补",
		"只要补",
		"需要补",
		"还需要补",
		"需要新增",
		"需要添加",
		"需要改",
		"还要改",
		"需要实现",
		"补上",
		"补一个",
		"加一个",
		"新增一个",
		"实现一个",
		"需要创建",
		"需要修改",
		"需要修复",
		"需要编写",
		"需要更新",
		"要改",
		"要加",
		"要补",
		"要新增",
		"要修复",
		"要修改",
		"要创建",
		"要实现",
		"要写",
	}) {
		return false
	}
	return containsAny(strings.ToLower(trimmed), []string{
		".cheng",
		".go",
		".js",
		".jsx",
		".ts",
		".tsx",
		".py",
		"文件",
		"模块",
		"函数",
		"测试",
		"tool",
		"edit",
		"read",
	})
}

func looksLikeUnsupportedCompletionClaim(finalText, finalPrompt string) bool {
	if !latestUserRequestedExecution(finalPrompt) {
		return false
	}
	trimmed := strings.TrimSpace(finalText)
	if trimmed == "" || len([]rune(trimmed)) > 1200 || strings.Count(trimmed, "\n") > 16 {
		return false
	}
	lower := strings.ToLower(trimmed)
	if !mentionsCodeArtifact(lower) || !containsAny(lower, completionClaimPhrases()) {
		return false
	}
	return !recentPromptHasExecutionToolEvidence(finalPrompt)
}

func mentionsCodeArtifact(lower string) bool {
	return containsAny(lower, []string{
		".go",
		".js",
		".jsx",
		".ts",
		".tsx",
		".py",
		".json",
		".md",
		".yaml",
		".yml",
		".toml",
		"handler_",
		"文件",
		"测试",
		"代码",
		"路径",
	})
}

func completionClaimPhrases() []string {
	return []string{
		"已集成",
		"已接入",
		"已修复",
		"已更新",
		"已完成",
		"已实现",
		"已添加",
		"已新增",
		"已经集成",
		"已经接入",
		"已经修复",
		"已经更新",
		"已经完成",
		"已经实现",
		"修复完成",
		"改动完成",
		"测试通过",
		"implemented",
		"integrated",
		"wired",
		"fixed",
		"updated",
		"completed",
		"added",
		"tests pass",
	}
}

func recentPromptHasExecutionToolEvidence(finalPrompt string) bool {
	turn := strings.ToLower(latestConversationTurnBlock(finalPrompt))
	if strings.TrimSpace(turn) == "" {
		return false
	}
	for _, marker := range []string{
		`invoke name="edit"`,
		`invoke name="multiedit"`,
		`invoke name="multi_edit"`,
		`invoke name="write"`,
		`invoke name="bash"`,
		`invoke name="shell"`,
		`invoke name="update"`,
		`invoke name="applypatch"`,
		`invoke name="apply_patch"`,
		`"name":"edit"`,
		`"name":"multiedit"`,
		`"name":"write"`,
		`"name":"bash"`,
		`"name":"update"`,
		`"name":"applypatch"`,
		`"name":"apply_patch"`,
		"<tool>edit",
		"<tool>multiedit",
		"<tool>write",
		"<tool>bash",
		"<tool>update",
		"<tool>applypatch",
	} {
		if strings.Contains(turn, marker) {
			return true
		}
	}
	return false
}

func looksLikeExplicitUnexecutedFileToolPlan(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len([]rune(trimmed)) > 2400 || strings.Count(trimmed, "\n") > 40 {
		return false
	}
	lower := strings.ToLower(trimmed)
	if !containsAny(lower, []string{
		"/users/",
		"/tmp/",
		".cheng",
		".go",
		".js",
		".ts",
		".tsx",
		".py",
		".json",
		".md",
	}) {
		return false
	}
	return containsAny(lower, []string{
		"现在创建",
		"现在写",
		"现在生成",
		"创建它",
		"创建该",
		"创建这个",
		"写入 /",
		"写入/",
		"写入 `",
		"写出完整",
		"完整写出",
		"如果已存在则覆盖",
		"先读取",
		"然后读取",
		"批量写入",
		"using write",
		"use write",
		"write the file",
		"create the file",
		"overwrite",
		"建一个文件",
		"写一个文件",
		"创建文件",
		"写文件",
		"写入文件",
		"保存到",
		"write to ",
	})
}

func latestUserRequestedExecution(finalPrompt string) bool {
	latest := strings.ToLower(LatestUserPromptBlock(finalPrompt))
	if strings.TrimSpace(latest) == "" {
		return false
	}
	return containsAny(latest, []string{
		"请继续",
		"继续",
		"请优化",
		"优化",
		"完善",
		"按建议",
		"推进",
		"一口气",
		"直接改",
		"修复",
		"完成",
		"实现",
		"补",
		"continue",
		"proceed",
		"fix",
		"implement",
		"finish",
	})
}

func looksLikeFutureToolAction(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if len([]rune(trimmed)) > 420 || strings.Count(trimmed, "\n") > 5 {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, phrase := range []string{
		"继续推进剩余",
		"正在并行处理",
		"并行处理",
		"先并行读取",
		"并行读取需修改",
		"先读",
		"先读取",
		"逐个分析",
		"然后处理",
		"继续读取",
		"现在运行",
		"现在执行",
		"运行测试",
		"测试验证",
		"跑测试",
		"先检查",
		"先查询",
		"先确认",
		"先看",
		"先分析",
		"先搜索",
		"先组织",
		"先规划",
		"需要读取",
		"需要理解",
		"需要检查",
		"需要评估",
		"需要了解",
		"reading the rest",
		"now reading",
		"now running",
		"now run",
		"now executing",
		"run tests",
		"running tests",
		"test verification",
		"start examining",
		"start reading",
		"continue reading",
		"批量写入",
		"i'll launch parallel",
		"i will launch parallel",
		"let me launch parallel",
		"currently working on",
		"i'm working on",
		"i am working on",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return containsAny(lower, futureActionPrefixes()) && containsAny(lower, futureActionVerbs())
}

func futureActionPrefixes() []string {
	return []string{
		"i'll ",
		"i’ll ",
		"i will ",
		"i'm going to ",
		"i am going to ",
		"let me ",
		"next i'll ",
		"next i will ",
		"now i'll ",
		"now i will ",
		"我将",
		"我会",
		"我正",
		"我正在",
		"我先",
		"让我",
		"正在",
		"先",
		"接下来",
		"继续",
		"现在开始",
		"开始",
		"马上",
		"下一步",
		"我打算",
		"我准备",
		"我要",
		"i need to ",
		"i should ",
		"i want to ",
		"i'm about to ",
		"i am about to ",
	}
}

func futureActionVerbs() []string {
	return []string{
		" read",
		" reading",
		" inspect",
		" inspecting",
		" explore",
		" exploring",
		" check",
		" checking",
		" search",
		" searching",
		" grep",
		" glob",
		" run",
		" execute",
		" edit",
		" modify",
		" implement",
		" fix",
		" verify",
		" test",
		" launch",
		" start",
		" continue",
		" create",
		" update",
		" apply",
		" analyze",
		" review",
		" trace",
		" fill",
		" complete",
		"读取",
		"查看",
		"检查",
		"搜索",
		"定位",
		"查找",
		"运行",
		"执行",
		"修改",
		"编辑",
		"实现",
		"修复",
		"验证",
		"测试",
		"启动",
		"创建",
		"更新",
		"推进",
		"处理",
		"分析",
		"审查",
		"补齐",
		"补全",
		"完成",
		"阅读",
		"理解",
		"评估",
		"了解",
		"查询",
		"确认",
		"提交",
		" wait",
		" plan",
		" gather",
		" organize",
		" assess",
		" evaluate",
		" understand",
		" determine",
		" identify",
		" commit",
		" bookkeep",
	}
}
