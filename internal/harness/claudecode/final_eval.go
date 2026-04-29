package claudecode

import (
	"strings"

	"ds2api/internal/toolcall"
)

type FinalEvaluationInput struct {
	FinalPrompt         string
	Text                string
	Thinking            string
	ToolNames           []string
	ToolSchemas         toolcall.ParameterSchemas
	AllowMetaAgentTools bool
	ContentFilter       bool
}

type FinalEvaluationResult struct {
	Text                 string
	Thinking             string
	PreservedThinking    string
	Parsed               toolcall.ToolCallParseResult
	Calls                []toolcall.ParsedToolCall
	Repair               FinalOutputResult
	MissingToolDecision  MissingToolCallDecision
	DroppedTaskOutputIDs []string
}

func EvaluateFinalOutput(in FinalEvaluationInput) FinalEvaluationResult {
	repair := RepairFinalOutput(FinalOutputInput(in))
	parsed, visibleText := DetectFinalToolCalls(FinalToolCallInput{
		Text:      repair.Text,
		Thinking:  in.Thinking,
		ToolNames: in.ToolNames,
	})
	// Fill in schema defaults for missing optional parameters (limit, offset)
	parsed.Calls = CompleteToolCallsWithSchemaDefaults(parsed.Calls, in.ToolSchemas)
	var dropped []string
	parsed.Calls, dropped = FilterInvalidTaskOutputCallsWithReport(parsed.Calls, in.FinalPrompt)
	if len(dropped) > 0 && len(parsed.Calls) == 0 && strings.TrimSpace(visibleText) == "" {
		visibleText = InvalidTaskOutputNotice(dropped)
	}
	calls := toolcall.NormalizeCallsForSchemasWithMeta(parsed.Calls, in.ToolSchemas, in.AllowMetaAgentTools)
	result := FinalEvaluationResult{
		Text:                 visibleText,
		Thinking:             in.Thinking,
		PreservedThinking:    repair.PreservedThinking,
		Parsed:               parsed,
		Calls:                calls,
		Repair:               repair,
		DroppedTaskOutputIDs: dropped,
	}
	if len(calls) == 0 {
		result.MissingToolDecision = DetectMissingToolCall(MissingToolCallInput{
			Text:                visibleText,
			FinalPrompt:         in.FinalPrompt,
			ToolNames:           in.ToolNames,
			ToolSchemas:         in.ToolSchemas,
			AllowMetaAgentTools: in.AllowMetaAgentTools,
		})
	}
	return result
}
