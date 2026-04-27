package protocol

type StreamState struct {
	ErrorCode         string
	VisibleContent    bool
	ToolCallsStarted  bool
	ToolCallsFinished bool
}

func (s StreamState) RetryableFailure() bool {
	if s.VisibleContent || s.ToolCallsStarted || s.ToolCallsFinished {
		return false
	}
	switch s.ErrorCode {
	case "upstream_empty_output",
		"upstream_no_action_timeout",
		"upstream_missing_tool_call",
		"upstream_invalid_tool_call",
		"upstream_invalid_ref_file_id",
		"tool_choice_violation":
		return true
	default:
		return false
	}
}
