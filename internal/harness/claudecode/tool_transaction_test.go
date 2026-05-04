package claudecode

import (
	"strings"
	"testing"
)

func TestRunFinalToolTransactionConvertsAgentLaunchPromise(t *testing.T) {
	got := RunFinalToolTransaction(FinalToolTransactionInput{
		FinalPrompt:         "<｜User｜>请使用 Team Agents 一口气完成多实现落地<｜Assistant｜>",
		Text:                "先提交当前修复，然后启动 4 个并行代理。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if len(got.Calls) != 4 {
		t.Fatalf("expected four Agent calls, got %#v", got.Calls)
	}
	for _, call := range got.Calls {
		if call.Name != "Agent" {
			t.Fatalf("expected Agent call, got %#v", call)
		}
	}
	if strings.Contains(got.VisibleText, "<tool") || strings.TrimSpace(got.VisibleText) != "" {
		t.Fatalf("expected no leaked XML visible text, got %q", got.VisibleText)
	}
	if !got.Repair.Changed || got.Repair.Reason != "agent_launch_promise" {
		t.Fatalf("expected agent launch repair, got %#v", got.Repair)
	}
}

func TestRunFinalToolTransactionInvalidTaskOutputBecomesVisibleNotice(t *testing.T) {
	got := RunFinalToolTransaction(FinalToolTransactionInput{
		FinalPrompt: `<｜User｜>请继续<｜Assistant｜>`,
		Text: `<tool_calls>
<tool_call>
<tool_name>TaskOutput</tool_name>
<parameters><task_id>unknown-task</task_id></parameters>
</tool_call>
</tool_calls>`,
		ToolNames:           []string{"TaskOutput"},
		ToolSchemas:         taskOutputSchema,
		AllowMetaAgentTools: true,
	})
	if len(got.Calls) != 0 {
		t.Fatalf("invalid TaskOutput should be dropped, got %#v", got.Calls)
	}
	if got.VisibleText != "Background result unavailable." {
		t.Fatalf("expected invalid TaskOutput notice, got %q", got.VisibleText)
	}
	if len(got.DroppedTaskOutputIDs) != 1 || got.DroppedTaskOutputIDs[0] != "unknown-task" {
		t.Fatalf("expected dropped unknown task id, got %#v", got.DroppedTaskOutputIDs)
	}
}

func TestRunFinalToolTransactionDedupesRepeatedBashWithReport(t *testing.T) {
	got := RunFinalToolTransaction(FinalToolTransactionInput{
		Text: `<tool_calls>
<tool_call>
<tool_name>Bash</tool_name>
<parameters><command>go test ./internal/harness/claudecode</command></parameters>
</tool_call>
<tool_call>
<tool_name>Bash</tool_name>
<parameters><command>go test ./internal/harness/claudecode</command></parameters>
</tool_call>
</tool_calls>`,
		ToolNames:   []string{"Bash"},
		ToolSchemas: bashSchema,
	})
	if len(got.Calls) != 1 {
		t.Fatalf("expected one Bash call after dedupe, got %#v", got.Calls)
	}
	if got.Calls[0].Name != "Bash" {
		t.Fatalf("expected Bash call, got %#v", got.Calls[0])
	}
	if got.DedupeReport.ToolCallsDropped != 1 {
		t.Fatalf("expected one dropped duplicate tool call, got %#v", got.DedupeReport)
	}
}

func TestRunFinalToolTransactionSerializesParallelBashCalls(t *testing.T) {
	got := RunFinalToolTransaction(FinalToolTransactionInput{
		Text: `<tool_calls>
<tool_call>
<tool_name>Bash</tool_name>
<parameters><command>git diff --stat HEAD</command></parameters>
</tool_call>
<tool_call>
<tool_name>Bash</tool_name>
<parameters><command>wc -l src/core/lang/parser.cheng src/core/lang/typed_expr.cheng</command></parameters>
</tool_call>
<tool_call>
<tool_name>Read</tool_name>
<parameters><file_path>README.md</file_path></parameters>
</tool_call>
</tool_calls>`,
		ToolNames: []string{"Bash", "Read"},
		ToolSchemas: map[string]map[string]any{
			"Bash": bashSchema["Bash"],
			"Read": readSchema["Read"],
		},
	})
	if len(got.Calls) != 2 {
		t.Fatalf("expected one Bash plus Read after shell serialization, got %#v", got.Calls)
	}
	if got.Calls[0].Name != "Bash" || got.Calls[1].Name != "Read" {
		t.Fatalf("expected first Bash and Read to survive, got %#v", got.Calls)
	}
	if got.Calls[0].Input["command"] != "git diff --stat HEAD" {
		t.Fatalf("expected first Bash command to survive, got %#v", got.Calls[0])
	}
	if got.DedupeReport.ToolCallsDropped != 1 {
		t.Fatalf("expected one dropped parallel Bash call, got %#v", got.DedupeReport)
	}
}
