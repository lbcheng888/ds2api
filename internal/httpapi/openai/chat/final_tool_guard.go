package chat

import (
	"net/http"

	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

func prepareOpenAIFinalToolCalls(finalPrompt string, calls []toolcall.ParsedToolCall, toolsRaw any, toolNames []string) ([]toolcall.ParsedToolCall, int, string, string, bool) {
	if len(calls) == 0 {
		return nil, 0, "", "", false
	}
	normalized, report := toolcall.NormalizeParsedToolCallsForSchemasWithReport(calls, toolsRaw)
	claudecodeharness.RecordDeduplication("openai", "tool_calls", report.ToolCallsDropped)
	claudecodeharness.RecordDeduplication("openai", "todo_items", report.TodoItemsDropped)
	if recovered, ok := claudecodeharness.RecoverEditRetryAfterFailure(finalPrompt, normalized, toolNames); ok {
		normalized = recovered
	}
	if serialized, report := claudecodeharness.SerializeParallelShellToolCalls(normalized); report.ToolCallsDropped > 0 {
		normalized = serialized
		claudecodeharness.RecordDeduplication("openai", "tool_calls", report.ToolCallsDropped)
	}
	if guard := claudecodeharness.DetectInvalidPlanModeTransition(claudecodeharness.PlanModeGuardInput{
		FinalPrompt:         finalPrompt,
		Calls:               normalized,
		ToolNames:           toolNames,
		AllowMetaAgentTools: true,
	}); guard.Blocked {
		return nil,
			http.StatusBadGateway,
			claudecodeharness.PlanModeGuardMissingToolMessage(),
			claudecodeharness.MissingToolCallCode,
			true
	}
	if guard := claudecodeharness.DetectRepeatedExploration(claudecodeharness.ExplorationGuardInput{
		FinalPrompt: finalPrompt,
		Calls:       normalized,
	}); guard.Blocked {
		return nil,
			http.StatusBadGateway,
			"Upstream model repeated the same exploration tool call instead of advancing.",
			claudecodeharness.RepeatedExplorationCode,
			true
	}
	return normalized, 0, "", "", false
}
