package claudecode

import (
	"reflect"
	"testing"

	"ds2api/internal/toolcall"
)

func TestBuildExecutionLedgerSameTurnEditBashEvidence(t *testing.T) {
	got := BuildExecutionLedger(ExecutionLedgerInput{
		FinalPrompt: `<｜User｜>请修改并测试<｜Assistant｜><tool_calls>
<invoke name="Edit"><parameter name="file_path">a.go</parameter></invoke>
<invoke name="Bash"><parameter name="command">go test ./...</parameter></invoke>
</tool_calls><｜Tool｜>edited and tested<｜end▁of▁toolresults｜><｜Assistant｜>`,
	})

	if !got.HasWriteEvidence || !got.HasRunEvidence {
		t.Fatalf("expected write and run evidence, got %#v", got)
	}
	assertExecutionLedgerToolNames(t, got, []string{"Edit", "Bash"})
}

func TestBuildExecutionLedgerPreviousUserTurnDoesNotPolluteCurrent(t *testing.T) {
	got := BuildExecutionLedger(ExecutionLedgerInput{
		FinalPrompt: `<｜User｜>旧任务<｜Assistant｜><tool_calls>
<invoke name="Edit"><parameter name="file_path">old.go</parameter></invoke>
</tool_calls><｜Tool｜>edited<｜end▁of▁toolresults｜><｜Assistant｜>旧任务完成<｜end▁of▁sentence｜><｜User｜>新任务只读取<｜Assistant｜><tool_calls>
<invoke name="Read"><parameter name="file_path">new.go</parameter></invoke>
</tool_calls><｜Tool｜>read<｜end▁of▁toolresults｜><｜Assistant｜>`,
	})

	if got.HasWriteEvidence {
		t.Fatalf("previous turn write evidence polluted current turn: %#v", got)
	}
	if !got.HasReadEvidence {
		t.Fatalf("expected current read evidence, got %#v", got)
	}
	assertExecutionLedgerToolNames(t, got, []string{"Read"})
}

func TestBuildExecutionLedgerTodoWriteIsNotExecutionEvidence(t *testing.T) {
	got := BuildExecutionLedger(ExecutionLedgerInput{
		FinalPrompt: `<｜User｜>继续<｜Assistant｜><tool_calls>
<invoke name="TodoWrite"><parameter name="todos">[]</parameter></invoke>
</tool_calls><｜Tool｜>todos updated<｜end▁of▁toolresults｜><｜Assistant｜>`,
	})

	if len(got.Evidence) != 0 || len(got.RecentToolNames) != 0 {
		t.Fatalf("TodoWrite should not be execution evidence, got %#v", got)
	}
	if got.HasWriteEvidence || got.HasRunEvidence || got.HasAgentEvidence {
		t.Fatalf("TodoWrite should not set execution flags, got %#v", got)
	}
}

func TestBuildExecutionLedgerCurrentParsedAgentCallIsAgentEvidence(t *testing.T) {
	got := BuildExecutionLedger(ExecutionLedgerInput{
		FinalPrompt: "<｜User｜>启动代理<｜Assistant｜>",
		ToolCalls: []toolcall.ParsedToolCall{{
			Name:  "Agent",
			Input: map[string]any{"prompt": "inspect"},
		}},
	})

	if !got.HasAgentEvidence {
		t.Fatalf("expected current Agent call to count as agent evidence, got %#v", got)
	}
	assertExecutionLedgerToolNames(t, got, []string{"Agent"})
	if len(got.Evidence) != 1 || got.Evidence[0].Source != ExecutionLedgerEvidenceFromCurrent {
		t.Fatalf("expected current-source Agent evidence, got %#v", got.Evidence)
	}
}

func TestBuildExecutionLedgerParsesTaskOutputResultLine(t *testing.T) {
	got := BuildExecutionLedger(ExecutionLedgerInput{
		FinalPrompt: `<｜User｜>等代理结果<｜Assistant｜>
Task Output(non-blocking) runabcdef
Task is still running.
<｜Assistant｜>`,
	})

	if !got.HasTaskOutputEvidence {
		t.Fatalf("expected TaskOutput evidence, got %#v", got)
	}
	assertExecutionLedgerToolNames(t, got, []string{"TaskOutput"})
}

func assertExecutionLedgerToolNames(t *testing.T, got ExecutionLedger, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got.RecentToolNames, want) {
		t.Fatalf("tool names = %#v, want %#v", got.RecentToolNames, want)
	}
}
