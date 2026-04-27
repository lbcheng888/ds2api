package openai

import (
	"net/http"
	"strings"

	"ds2api/internal/toolcall"
)

const upstreamMissingToolCallCode = "upstream_missing_tool_call"
const upstreamInvalidToolCallCode = "upstream_invalid_tool_call"

func futureActionMissingToolCallDetail(finalText, finalPrompt string, toolNames []string, toolSchemas toolcall.ParameterSchemas, allowMetaAgentTools bool) (int, string, string, bool) {
	if !hasCallableTools(toolNames) {
		return 0, "", "", false
	}
	if strings.TrimSpace(finalText) == "" {
		return 0, "", "", false
	}
	parsed := toolcall.ParseStandaloneToolCallsDetailed(finalText, toolNames)
	if len(toolcall.NormalizeCallsForSchemasWithMeta(parsed.Calls, toolSchemas, allowMetaAgentTools)) > 0 {
		return 0, "", "", false
	}
	if parsed.SawToolCallSyntax {
		return http.StatusBadGateway,
			"Upstream model emitted invalid tool call syntax.",
			upstreamInvalidToolCallCode,
			true
	}
	if isBackgroundAgentAcknowledgement(finalPrompt, finalText, allowMetaAgentTools) {
		return 0, "", "", false
	}
	if looksLikeExplicitUnexecutedFileToolPlan(finalText) {
		return http.StatusBadGateway,
			"Upstream model promised tool work but emitted no tool call.",
			upstreamMissingToolCallCode,
			true
	}
	if looksLikeUnexecutedCodingAction(finalText, finalPrompt) {
		return http.StatusBadGateway,
			"Upstream model promised tool work but emitted no tool call.",
			upstreamMissingToolCallCode,
			true
	}
	if !looksLikeFutureToolAction(finalText) {
		return 0, "", "", false
	}
	return http.StatusBadGateway,
		"Upstream model promised tool work but emitted no tool call.",
		upstreamMissingToolCallCode,
		true
}

func hasCallableTools(toolNames []string) bool {
	for _, name := range toolNames {
		if strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}

func isBackgroundAgentAcknowledgement(finalPrompt, finalText string, allowMetaAgentTools bool) bool {
	if !allowMetaAgentTools || !recentPromptHasBackgroundAgentLaunch(finalPrompt) {
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

func recentPromptHasBackgroundAgentLaunch(finalPrompt string) bool {
	const maxTailBytes = 12000
	tail := finalPrompt
	if len(tail) > maxTailBytes {
		tail = tail[len(tail)-maxTailBytes:]
	}
	lower := strings.ToLower(tail)
	for _, phrase := range []string{
		"async agent launched successfully",
		"the agent is working in the background",
		"backgrounded agent",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
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
	latest := strings.ToLower(latestUserPromptBlock(finalPrompt))
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
		" inspect",
		" explore",
		" check",
		" search",
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
		"读取",
		"查看",
		"检查",
		"搜索",
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
	}
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
