package openai

import (
	"net/http"
	"strings"

	"ds2api/internal/toolcall"
)

const upstreamMissingToolCallCode = "upstream_missing_tool_call"
const upstreamInvalidToolCallCode = "upstream_invalid_tool_call"

func futureActionMissingToolCallDetail(finalText string, toolNames []string, toolSchemas toolcall.ParameterSchemas, allowMetaAgentTools bool) (int, string, string, bool) {
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
