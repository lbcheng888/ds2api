package responses

import (
	"net/http"

	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

func normalizeResponsesFinalToolCalls(finalPrompt string, calls []toolcall.ParsedToolCall, toolsRaw any, toolNames []string) ([]toolcall.ParsedToolCall, int, string, string, bool) {
	normalized, report := toolcall.NormalizeParsedToolCallsForSchemasWithReport(calls, toolsRaw)
	claudecodeharness.RecordDeduplication("openai", "tool_calls", report.ToolCallsDropped)
	claudecodeharness.RecordDeduplication("openai", "todo_items", report.TodoItemsDropped)
	normalized = filterResponsesExecutableToolCalls(normalized)
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

func filterResponsesExecutableToolCalls(calls []toolcall.ParsedToolCall) []toolcall.ParsedToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]toolcall.ParsedToolCall, 0, len(calls))
	backgroundAgents := 0
	for _, call := range calls {
		if toolcall.IsTaskTrackingToolName(call.Name) {
			continue
		}
		if toolcall.IsBackgroundAgentToolName(call.Name) {
			backgroundAgents++
			if backgroundAgents > 4 {
				continue
			}
		}
		out = append(out, call)
	}
	return out
}
