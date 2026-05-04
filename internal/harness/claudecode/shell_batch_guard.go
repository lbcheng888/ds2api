package claudecode

import "ds2api/internal/toolcall"

func SerializeParallelShellToolCalls(calls []toolcall.ParsedToolCall) ([]toolcall.ParsedToolCall, toolcall.DedupeReport) {
	if len(calls) < 2 {
		return calls, toolcall.DedupeReport{}
	}
	seenShell := false
	out := make([]toolcall.ParsedToolCall, 0, len(calls))
	report := toolcall.DedupeReport{}
	for _, call := range calls {
		if IsShellExecutionToolName(call.Name) {
			if seenShell {
				report.ToolCallsDropped++
				continue
			}
			seenShell = true
		}
		out = append(out, call)
	}
	if report.ToolCallsDropped == 0 {
		return calls, report
	}
	return out, report
}

func IsShellExecutionToolName(name string) bool {
	switch CanonicalTaskOutputToolName(name) {
	case "bash", "shell", "sh", "terminal", "execcommand", "executecommand":
		return true
	default:
		return false
	}
}
