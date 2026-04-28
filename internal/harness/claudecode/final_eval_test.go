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
		t.Fatalf("expected four executable Agent calls, got %#v", got.Calls)
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
