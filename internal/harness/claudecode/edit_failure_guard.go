package claudecode

import (
	"fmt"
	"path"
	"strings"

	"ds2api/internal/toolcall"
)

const EditFailureRecoveryReason = "edit_failure_requires_read"

func RecoverEditRetryAfterFailure(finalPrompt string, calls []toolcall.ParsedToolCall, toolNames []string) ([]toolcall.ParsedToolCall, bool) {
	if len(calls) == 0 {
		return calls, false
	}
	readName := editFailurePreferredReadTool(toolNames)
	if readName == "" {
		return calls, false
	}
	out := make([]toolcall.ParsedToolCall, 0, len(calls))
	changed := false
	for _, call := range calls {
		filePath := editFailureCallPath(call)
		if filePath == "" || !editFailureRequiresFreshRead(finalPrompt, filePath) {
			out = append(out, call)
			continue
		}
		out = append(out, toolcall.ParsedToolCall{
			Name: readName,
			Input: map[string]any{
				"file_path": filePath,
				"limit":     int64(240),
			},
		})
		changed = true
	}
	if !changed {
		return calls, false
	}
	return toolcall.DedupeParsedToolCalls(out), true
}

func editFailureRequiresFreshRead(finalPrompt, filePath string) bool {
	window := executionLedgerCurrentTaskWindow(finalPrompt)
	if window == "" {
		return false
	}
	lower := strings.ToLower(window)
	failIdx := strings.LastIndex(lower, "error editing file")
	if failIdx < 0 {
		return false
	}
	beforeFailure := window[:failIdx]
	if !editFailureSegmentHasEditForFile(beforeFailure, filePath) {
		return false
	}
	afterFailure := window[failIdx+len("error editing file"):]
	return !editFailureSegmentHasReadForFile(afterFailure, filePath)
}

func editFailureSegmentHasEditForFile(segment, filePath string) bool {
	for _, call := range toolcall.ParseStandaloneToolCalls(segment, nil) {
		if editFailureCallPath(call) == filePath {
			return true
		}
	}
	return false
}

func editFailureSegmentHasReadForFile(segment, filePath string) bool {
	for _, call := range toolcall.ParseStandaloneToolCalls(segment, nil) {
		if !editFailureIsReadTool(call.Name) {
			continue
		}
		if editFailureInputPath(call.Input) == filePath {
			return true
		}
	}
	return false
}

func editFailureCallPath(call toolcall.ParsedToolCall) string {
	if !editFailureIsEditTool(call.Name) {
		return ""
	}
	return editFailureInputPath(call.Input)
}

func editFailureInputPath(input map[string]any) string {
	for _, key := range []string{"file_path", "filePath", "filepath", "path"} {
		value, ok := input[key]
		if !ok {
			continue
		}
		if s := editFailureCanonicalPath(asExecutionLedgerString(value)); s != "" {
			return s
		}
	}
	return ""
}

func editFailureCanonicalPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "\\", "/")
	cleaned := path.Clean(raw)
	if cleaned == "." {
		return raw
	}
	if strings.HasPrefix(raw, "./") && !strings.HasPrefix(cleaned, "./") {
		return strings.TrimPrefix(cleaned, "./")
	}
	return cleaned
}

func editFailurePreferredReadTool(toolNames []string) string {
	for _, want := range []string{"Read", "read_file", "read"} {
		for _, name := range toolNames {
			if strings.EqualFold(strings.TrimSpace(name), want) {
				return strings.TrimSpace(name)
			}
		}
	}
	return ""
}

func editFailureIsReadTool(name string) bool {
	switch executionLedgerCanonicalToolName(name) {
	case "read", "readfile", "view", "openfile":
		return true
	default:
		return false
	}
}

func editFailureIsEditTool(name string) bool {
	switch executionLedgerCanonicalToolName(name) {
	case "edit", "update", "multiedit", "strreplaceeditor", "strreplace":
		return true
	default:
		return false
	}
}

func asExecutionLedgerString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
