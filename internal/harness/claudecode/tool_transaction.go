package claudecode

import (
	"strings"

	"ds2api/internal/toolcall"
)

type FinalToolTransactionInput struct {
	FinalPrompt         string
	Text                string
	Thinking            string
	ToolNames           []string
	ToolSchemas         toolcall.ParameterSchemas
	AllowMetaAgentTools bool
	ContentFilter       bool
	Profile             string
}

type FinalToolTransactionResult struct {
	VisibleText          string
	PreservedThinking    string
	Parsed               toolcall.ToolCallParseResult
	Calls                []toolcall.ParsedToolCall
	Repair               FinalOutputResult
	DroppedTaskOutputIDs []string
	DedupeReport         toolcall.DedupeReport
}

func RunFinalToolTransaction(in FinalToolTransactionInput) FinalToolTransactionResult {
	repair := RepairFinalOutput(FinalOutputInput(in))
	parsed, visibleText := DetectFinalToolCalls(FinalToolCallInput{
		Text:      repair.Text,
		Thinking:  in.Thinking,
		ToolNames: in.ToolNames,
	})
	parsed.Calls = CompleteToolCallsWithSchemaDefaults(parsed.Calls, in.ToolSchemas)
	calls, droppedTaskOutputIDs := FilterInvalidTaskOutputCallsWithReport(parsed.Calls, in.FinalPrompt)
	parsed.Calls = calls
	if len(droppedTaskOutputIDs) > 0 && len(parsed.Calls) == 0 && strings.TrimSpace(visibleText) == "" {
		visibleText = InvalidTaskOutputNotice(droppedTaskOutputIDs)
	}
	calls, dedupeReport := toolcall.NormalizeCallsForSchemasWithMetaReport(parsed.Calls, in.ToolSchemas, in.AllowMetaAgentTools)
	if recovered, ok := RecoverEditRetryAfterFailure(in.FinalPrompt, calls, in.ToolNames); ok {
		calls = recovered
	}
	if serialized, report := SerializeParallelShellToolCalls(calls); report.ToolCallsDropped > 0 {
		calls = serialized
		dedupeReport.ToolCallsDropped += report.ToolCallsDropped
	}
	return FinalToolTransactionResult{
		VisibleText:          visibleText,
		PreservedThinking:    repair.PreservedThinking,
		Parsed:               parsed,
		Calls:                calls,
		Repair:               repair,
		DroppedTaskOutputIDs: droppedTaskOutputIDs,
		DedupeReport:         dedupeReport,
	}
}
