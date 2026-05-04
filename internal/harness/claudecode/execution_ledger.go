package claudecode

import (
	"regexp"
	"strings"
	"unicode"

	"ds2api/internal/toolcall"
)

type ExecutionLedgerInput struct {
	FinalPrompt string
	ToolCalls   []toolcall.ParsedToolCall
}

type ExecutionLedger struct {
	CurrentTaskWindow     string
	Evidence              []ExecutionLedgerToolEvidence
	RecentToolNames       []string
	HasReadEvidence       bool
	HasSearchEvidence     bool
	HasWriteEvidence      bool
	HasRunEvidence        bool
	HasAgentEvidence      bool
	HasTaskOutputEvidence bool
}

type ExecutionLedgerToolEvidence struct {
	Name   string
	Kind   ExecutionLedgerEvidenceKind
	Source ExecutionLedgerEvidenceSource
}

type ExecutionLedgerEvidenceKind string

const (
	ExecutionLedgerEvidenceRead       ExecutionLedgerEvidenceKind = "read"
	ExecutionLedgerEvidenceSearch     ExecutionLedgerEvidenceKind = "search"
	ExecutionLedgerEvidenceWrite      ExecutionLedgerEvidenceKind = "write"
	ExecutionLedgerEvidenceRun        ExecutionLedgerEvidenceKind = "run"
	ExecutionLedgerEvidenceAgent      ExecutionLedgerEvidenceKind = "agent"
	ExecutionLedgerEvidenceTaskOutput ExecutionLedgerEvidenceKind = "task_output"
)

type ExecutionLedgerEvidenceSource string

const (
	ExecutionLedgerEvidenceFromPrompt  ExecutionLedgerEvidenceSource = "prompt"
	ExecutionLedgerEvidenceFromCurrent ExecutionLedgerEvidenceSource = "current"
)

var (
	executionLedgerUserTagPattern        = regexp.MustCompile(`(?is)<\s*user\b[^>]*>`)
	executionLedgerHistoryUserPattern    = regexp.MustCompile(`(?im)^===\s*\d+\.\s*USER\s*===\s*$`)
	executionLedgerTaskOutputLinePattern = regexp.MustCompile(`(?im)\bTask\s+Output(?:\s*\([^)]*\))?\s+[a-z0-9_-]{4,}\b`)
	executionLedgerAgentResultPattern    = regexp.MustCompile(`(?im)\b(?:Async\s+agent\s+launched\s+successfully|background\s+agents?\s+launched|the\s+agent\s+is\s+working\s+in\s+the\s+background)\b`)
)

func BuildExecutionLedger(in ExecutionLedgerInput) ExecutionLedger {
	window := executionLedgerCurrentTaskWindow(in.FinalPrompt)
	ledger := ExecutionLedger{CurrentTaskWindow: window}
	ledger.addCalls(toolcall.ParseStandaloneToolCalls(window, nil), ExecutionLedgerEvidenceFromPrompt)
	ledger.addPromptResultEvidence(window)
	ledger.addCalls(in.ToolCalls, ExecutionLedgerEvidenceFromCurrent)
	return ledger
}

func executionLedgerCurrentTaskWindow(finalPrompt string) string {
	if finalPrompt == "" {
		return ""
	}
	start := -1
	lower := strings.ToLower(finalPrompt)
	for _, marker := range []string{"<｜user｜>", "<|user|>"} {
		if idx := strings.LastIndex(lower, marker); idx > start {
			start = idx
		}
	}
	for _, match := range executionLedgerUserTagPattern.FindAllStringIndex(finalPrompt, -1) {
		if match[0] > start {
			start = match[0]
		}
	}
	for _, match := range executionLedgerHistoryUserPattern.FindAllStringIndex(finalPrompt, -1) {
		if match[0] > start {
			start = match[0]
		}
	}
	if start < 0 {
		return finalPrompt
	}
	return finalPrompt[start:]
}

func (l *ExecutionLedger) addCalls(calls []toolcall.ParsedToolCall, source ExecutionLedgerEvidenceSource) {
	for _, call := range calls {
		l.addCall(call, source)
	}
}

func (l *ExecutionLedger) addCall(call toolcall.ParsedToolCall, source ExecutionLedgerEvidenceSource) {
	name := strings.TrimSpace(call.Name)
	if name == "" || toolcall.IsTaskTrackingToolName(name) {
		return
	}
	kind, ok := executionLedgerEvidenceKindForTool(name)
	if !ok {
		return
	}
	l.Evidence = append(l.Evidence, ExecutionLedgerToolEvidence{
		Name:   name,
		Kind:   kind,
		Source: source,
	})
	l.RecentToolNames = append(l.RecentToolNames, name)
	l.mergeFlags(name, call.Input)
}

func (l *ExecutionLedger) addPromptResultEvidence(window string) {
	if !l.HasTaskOutputEvidence {
		for range executionLedgerTaskOutputLinePattern.FindAllStringIndex(window, -1) {
			l.addSyntheticEvidence("TaskOutput", ExecutionLedgerEvidenceTaskOutput, ExecutionLedgerEvidenceFromPrompt)
		}
	}
	if !l.HasAgentEvidence && executionLedgerAgentResultPattern.MatchString(window) {
		l.addSyntheticEvidence("Agent", ExecutionLedgerEvidenceAgent, ExecutionLedgerEvidenceFromPrompt)
	}
}

func (l *ExecutionLedger) addSyntheticEvidence(name string, kind ExecutionLedgerEvidenceKind, source ExecutionLedgerEvidenceSource) {
	l.Evidence = append(l.Evidence, ExecutionLedgerToolEvidence{
		Name:   name,
		Kind:   kind,
		Source: source,
	})
	l.RecentToolNames = append(l.RecentToolNames, name)
	l.mergeKind(kind)
}

func (l *ExecutionLedger) mergeFlags(name string, input map[string]any) {
	key := executionLedgerCanonicalToolName(name)
	l.mergeKind(mustExecutionLedgerEvidenceKind(name))
	if executionLedgerShellCommandCanWrite(key, input) {
		l.HasWriteEvidence = true
	}
}

func (l *ExecutionLedger) mergeKind(kind ExecutionLedgerEvidenceKind) {
	switch kind {
	case ExecutionLedgerEvidenceRead:
		l.HasReadEvidence = true
	case ExecutionLedgerEvidenceSearch:
		l.HasSearchEvidence = true
	case ExecutionLedgerEvidenceWrite:
		l.HasWriteEvidence = true
	case ExecutionLedgerEvidenceRun:
		l.HasRunEvidence = true
	case ExecutionLedgerEvidenceAgent:
		l.HasAgentEvidence = true
	case ExecutionLedgerEvidenceTaskOutput:
		l.HasTaskOutputEvidence = true
	}
}

func mustExecutionLedgerEvidenceKind(name string) ExecutionLedgerEvidenceKind {
	kind, _ := executionLedgerEvidenceKindForTool(name)
	return kind
}

func executionLedgerEvidenceKindForTool(name string) (ExecutionLedgerEvidenceKind, bool) {
	key := executionLedgerCanonicalToolName(name)
	switch {
	case key == "taskoutput":
		return ExecutionLedgerEvidenceTaskOutput, true
	case toolcall.IsBackgroundAgentToolName(name):
		return ExecutionLedgerEvidenceAgent, true
	case executionLedgerIsReadTool(key):
		return ExecutionLedgerEvidenceRead, true
	case executionLedgerIsSearchTool(key):
		return ExecutionLedgerEvidenceSearch, true
	case executionLedgerIsWriteTool(key):
		return ExecutionLedgerEvidenceWrite, true
	case executionLedgerIsRunTool(key):
		return ExecutionLedgerEvidenceRun, true
	default:
		return "", false
	}
}

func executionLedgerCanonicalToolName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch r {
		case '_', '-', ' ', '.':
			continue
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func executionLedgerIsReadTool(key string) bool {
	switch key {
	case "read", "readfile", "view", "openfile":
		return true
	default:
		return false
	}
}

func executionLedgerIsSearchTool(key string) bool {
	switch key {
	case "search", "grep", "glob", "rg", "ripgrep", "find", "ls":
		return true
	default:
		return strings.Contains(key, "search") || strings.Contains(key, "grep") || strings.Contains(key, "glob")
	}
}

func executionLedgerIsWriteTool(key string) bool {
	switch key {
	case "edit", "multiedit", "multiwrite", "write", "applypatch", "applydiff", "patch", "update", "strreplaceeditor", "strreplace", "notebookedit", "filewrite", "createfile":
		return true
	default:
		return false
	}
}

func executionLedgerIsRunTool(key string) bool {
	switch key {
	case "bash", "shell", "sh", "terminal", "execcommand", "executecommand", "test", "gotest", "pytest", "unittest", "build", "compile", "testsim", "buildsim", "buildrunsim":
		return true
	default:
		return strings.Contains(key, "test") || strings.Contains(key, "build") || strings.Contains(key, "compile")
	}
}

func executionLedgerShellCommandCanWrite(key string, input map[string]any) bool {
	if key != "bash" && key != "shell" && key != "sh" && key != "terminal" && key != "execcommand" && key != "executecommand" {
		return false
	}
	command := executionLedgerCommandInput(input)
	lower := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if lower == "" {
		return false
	}
	for _, phrase := range []string{
		"apply_patch",
		"applypatch",
		"gofmt -w",
		"sed -i",
		"perl -pi",
		"cat >",
		"cat <<",
		"tee ",
		"| tee",
		">>",
		"> ",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	for _, word := range []string{"mv", "cp", "rm", "mkdir", "touch", "chmod"} {
		if executionLedgerContainsShellWord(lower, word) {
			return true
		}
	}
	return false
}

func executionLedgerCommandInput(input map[string]any) string {
	for _, key := range []string{"command", "cmd", "script"} {
		if value, ok := input[key].(string); ok {
			return value
		}
	}
	for _, key := range []string{"args", "argv"} {
		if value, ok := input[key]; ok {
			return executionLedgerJoinArgs(value)
		}
	}
	return ""
}

func executionLedgerJoinArgs(value any) string {
	switch v := value.(type) {
	case []string:
		return strings.Join(v, " ")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func executionLedgerContainsShellWord(command, word string) bool {
	for _, field := range strings.FieldsFunc(command, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ';' || r == '&' || r == '|'
	}) {
		if strings.TrimLeft(field, "(") == word {
			return true
		}
	}
	return false
}
