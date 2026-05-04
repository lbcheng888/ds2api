package claudecode

import (
	"strings"
	"testing"
)

func TestEvaluateFinalOutputConvertsAgentLaunchPromise(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
		FinalPrompt:         "<｜User｜>请使用 Team Agents 一口气完成多实落地路线和终局愿景<｜Assistant｜>",
		Text:                "先提交当前修复，然后启动 4 个并行代理。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if len(got.Calls) != 4 {
		t.Fatalf("expected four executable Agent calls from explicit launch count, got %#v", got.Calls)
	}
	if got.MissingToolDecision.Blocked {
		t.Fatalf("synthesized Agent calls should suppress missing-tool gate, got %#v", got.MissingToolDecision)
	}
	if strings.TrimSpace(got.Text) != "" {
		t.Fatalf("synthesized Agent XML should not leak as visible text, got %q", got.Text)
	}
}

func TestEvaluateFinalOutputInvalidTaskOutputBecomesVisibleNotice(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
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
	if got.Text != "Background result unavailable." {
		t.Fatalf("expected visible invalid-task notice, got %q", got.Text)
	}
	if got.MissingToolDecision.Blocked {
		t.Fatalf("invalid TaskOutput notice should not be reclassified as missing tool, got %#v", got.MissingToolDecision)
	}
}

func TestEvaluateFinalOutputBlocksFutureActionWithoutTool(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
		FinalPrompt: "<｜User｜>请继续<｜Assistant｜>",
		Text:        "继续推进剩余的审查建议。先并行读取需修改的文件。",
		ToolNames:   []string{"Read"},
		ToolSchemas: readSchema,
	})
	if !got.MissingToolDecision.Blocked || got.MissingToolDecision.Code != MissingToolCallCode {
		t.Fatalf("expected missing tool decision, got %#v", got.MissingToolDecision)
	}
}

func TestEvaluateFinalOutputBlocksTaskTrackingOnlyCalls(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
		FinalPrompt: "<｜User｜>继续实现<｜Assistant｜>",
		Text: `<tool_calls>
<invoke name="update_plan"><parameter name="plan">检查工作树和测试状态</parameter></invoke>
</tool_calls>`,
		ToolNames: []string{"update_plan", "Read", "Bash"},
	})
	if len(got.Calls) != 0 {
		t.Fatalf("task tracking calls should not execute, got %#v", got.Calls)
	}
	if !got.MissingToolDecision.Blocked || got.MissingToolDecision.Code != MissingToolCallCode {
		t.Fatalf("expected task-only output to be blocked as missing work, got %#v", got.MissingToolDecision)
	}
}

func TestEvaluateFinalOutputBlocksEnterPlanModeForExecutionRequest(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
		FinalPrompt: "<｜User｜>请继续推进并完成实现<｜Assistant｜>",
		Text: `<tool_calls>
<invoke name="EnterPlanMode"></invoke>
</tool_calls>`,
		ToolNames: []string{"EnterPlanMode", "Read", "Bash"},
	})
	if len(got.Calls) != 0 {
		t.Fatalf("EnterPlanMode should not execute for an execution request, got %#v", got.Calls)
	}
	if !got.MissingToolDecision.Blocked || got.MissingToolDecision.Code != MissingToolCallCode {
		t.Fatalf("expected EnterPlanMode to be blocked as missing real work, got %#v", got.MissingToolDecision)
	}
}

func TestEvaluateFinalOutputBlocksEnterPlanModeWhenPlanModeAlreadyActive(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
		FinalPrompt: `<｜User｜><system-reminder>
Plan mode is active. The user indicated that they do not want you to execute yet.
</system-reminder>
请继续完善方案<｜Assistant｜>`,
		Text: `<tool_calls>
<invoke name="EnterPlanMode"></invoke>
</tool_calls>`,
		ToolNames: []string{"EnterPlanMode", "Read", "Bash"},
	})
	if len(got.Calls) != 0 {
		t.Fatalf("duplicate EnterPlanMode should not execute, got %#v", got.Calls)
	}
	if !got.MissingToolDecision.Blocked || got.MissingToolDecision.Code != MissingToolCallCode {
		t.Fatalf("expected duplicate EnterPlanMode to be blocked, got %#v", got.MissingToolDecision)
	}
}

func TestEvaluateFinalOutputKeepsExitPlanModeWhenPlanModeActive(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
		FinalPrompt: `<｜User｜><system-reminder>
Plan mode is active. The user indicated that they do not want you to execute yet.
</system-reminder>
请给出方案<｜Assistant｜>`,
		Text: `<tool_calls>
<invoke name="ExitPlanMode"><parameter name="plan">修改 internal/harness/claudecode，并运行测试。</parameter></invoke>
</tool_calls>`,
		ToolNames: []string{"ExitPlanMode", "Read", "Bash"},
	})
	if got.MissingToolDecision.Blocked {
		t.Fatalf("ExitPlanMode is the valid way to leave plan mode, got %#v", got.MissingToolDecision)
	}
	if len(got.Calls) != 1 || got.Calls[0].Name != "ExitPlanMode" {
		t.Fatalf("expected ExitPlanMode call to survive, got %#v", got.Calls)
	}
}

func TestEvaluateFinalOutputBlocksRepeatedExplorationCall(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
		FinalPrompt: `<｜User｜>继续当前任务<｜Assistant｜>
<tool_calls>
<invoke name="Read"><parameter name="file_path">/tmp/a.go</parameter><parameter name="offset">0</parameter><parameter name="limit">120</parameter></invoke>
</tool_calls>
<｜Tool｜>content<｜end▁of▁toolresults｜><｜Assistant｜>`,
		Text: `<tool_calls>
<tool_call>
<tool_name>Read</tool_name>
<parameters><file_path>/tmp/a.go</file_path><offset>0</offset><limit>120</limit></parameters>
</tool_call>
</tool_calls>`,
		ToolNames:   []string{"Read"},
		ToolSchemas: readSchemaWithLimitOffset,
	})
	if len(got.Calls) != 0 {
		t.Fatalf("repeated exploration call should be blocked before execution, got %#v", got.Calls)
	}
	if !got.MissingToolDecision.Blocked || got.MissingToolDecision.Code != RepeatedExplorationCode {
		t.Fatalf("expected repeated exploration decision, got %#v", got.MissingToolDecision)
	}
}
