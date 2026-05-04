package claudecode

import "ds2api/internal/toolcall"

type FinalEvaluationInput struct {
	FinalPrompt         string
	Text                string
	Thinking            string
	ToolNames           []string
	ToolSchemas         toolcall.ParameterSchemas
	AllowMetaAgentTools bool
	ContentFilter       bool
	Profile             string
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
	transaction := RunFinalToolTransaction(FinalToolTransactionInput(in))
	recordDedupeReport(in.Profile, transaction.DedupeReport)
	result := FinalEvaluationResult{
		Text:                 transaction.VisibleText,
		Thinking:             in.Thinking,
		PreservedThinking:    transaction.PreservedThinking,
		Parsed:               transaction.Parsed,
		Calls:                transaction.Calls,
		Repair:               transaction.Repair,
		DroppedTaskOutputIDs: transaction.DroppedTaskOutputIDs,
	}
	if len(result.Calls) > 0 {
		if guard := DetectInvalidPlanModeTransition(PlanModeGuardInput{
			FinalPrompt:         in.FinalPrompt,
			Calls:               result.Calls,
			ToolNames:           in.ToolNames,
			AllowMetaAgentTools: in.AllowMetaAgentTools,
		}); guard.Blocked {
			result.Calls = nil
			result.MissingToolDecision = planModeGuardDecision(in.Profile)
		}
	}
	if len(result.Calls) > 0 {
		if guard := DetectRepeatedExploration(ExplorationGuardInput{
			FinalPrompt: in.FinalPrompt,
			Calls:       result.Calls,
		}); guard.Blocked {
			result.Calls = nil
			result.MissingToolDecision = repeatedExplorationDecision(in.Profile)
		}
	}
	if len(result.Calls) == 0 && len(result.Parsed.Calls) > 0 &&
		toolcall.AllCallsAreTaskTrackingTools(result.Parsed.Calls) {
		result.MissingToolDecision = missingToolDecision(in.Profile)
	}
	if len(result.Calls) == 0 && !result.MissingToolDecision.Blocked {
		result.MissingToolDecision = DetectMissingToolCall(MissingToolCallInput{
			Text:                transaction.VisibleText,
			FinalPrompt:         in.FinalPrompt,
			ToolNames:           in.ToolNames,
			ToolSchemas:         in.ToolSchemas,
			AllowMetaAgentTools: in.AllowMetaAgentTools,
		})
	}
	return result
}
