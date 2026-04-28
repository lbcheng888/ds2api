package claudecode

import (
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
	if parsed.SawToolCallSyntax {
		return invalidToolSyntaxDecision()
	}
	if looksLikeInvalidLegacyToolCallSyntax(finalText) {
		return invalidToolSyntaxDecision()
	}
	if LooksLikeUnexecutedAgentLaunch(finalText, in.FinalPrompt, in.AllowMetaAgentTools) {
		return missingToolDecision()
	}
	if !HasCallableTools(in.ToolNames) {
		return MissingToolCallDecision{}
	}
	if looksLikeExplicitUnexecutedFileToolPlan(finalText) {
		return missingToolDecision()
	}
	if looksLikeUnexecutedCodingAction(finalText, in.FinalPrompt) {
		return missingToolDecision()
	}
	if !looksLikeFutureToolAction(finalText) {
		return MissingToolCallDecision{}
	}
	return missingToolDecision()
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

func missingToolDecision() MissingToolCallDecision {
	recordFailureDecision(MissingToolCallCode)
	return MissingToolCallDecision{
		Blocked: true,
		Message: "Upstream model promised tool work but emitted no tool call.",
		Code:    MissingToolCallCode,
	}
}

func invalidToolSyntaxDecision() MissingToolCallDecision {
	recordFailureDecision(InvalidToolCallCode)
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
		"先并行读取",
		"并行读取需修改",
		"先读",
		"先读取",
		"继续读取",
		"reading the rest",
		"now reading",
		"start examining",
		"start reading",
		"continue reading",
		"批量写入",
		"i'll launch parallel",
		"i will launch parallel",
		"let me launch parallel",
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
		"让我",
		"先",
		"接下来",
		"继续",
		"现在开始",
		"开始",
		"马上",
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
	}
}
