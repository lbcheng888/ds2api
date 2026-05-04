package claudecode

import (
	"strings"
	"testing"
)

func TestRepairFinalOutputConvertsChineseNumberedPreparedSubagents(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>调用子代理并行推进<｜Assistant｜>",
		Text:                "准备三个子代理并行推进：一个改 lowering 的函数循环并行化，一个改 parser 层的文件级并行收集，一个研究 emit 的并行方案。三个同时启动。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected prepared subagent launch repair, got %#v", got)
	}
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != 3 {
		t.Fatalf("expected 3 Agent tool calls, got %d in %s", count, got.Text)
	}
	for _, want := range []string{"lowering", "parser", "emit"} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("expected synthesized Agent prompt to preserve %q task, got %s", want, got.Text)
		}
	}
	if !strings.Contains(got.Text, "Edit files directly") {
		t.Fatalf("expected execution-oriented Agent prompt, got %s", got.Text)
	}
}

func TestRepairFinalOutputConvertsChineseInvertedSimultaneousLaunch(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>请继续<｜Assistant｜>",
		Text:                "我需要先了解每个后台代理的具体产出状态，然后继续推进。让我同时启动三个代理来并行实现三个并行化任务。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected inverted simultaneous launch repair, got %#v", got)
	}
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != 3 {
		t.Fatalf("expected 3 Agent tool calls, got %d in %s", count, got.Text)
	}
}

func TestRepairFinalOutputHonorsExplicitCountWhenTaskListIsShort(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>调用子代理并行推进<｜Assistant｜>",
		Text:                "准备三个子代理并行推进：一个改 lowering 的函数循环并行化，一个改 parser 层的文件级并行收集。三个同时启动。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected partial task-list launch repair, got %#v", got)
	}
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != 3 {
		t.Fatalf("expected explicit count to be honored, got %d in %s", count, got.Text)
	}
	if !strings.Contains(got.Text, "Repair integration risks") {
		t.Fatalf("expected remaining Agent slot to use existing generic role, got %s", got.Text)
	}
}

func TestRepairFinalOutputKeepsAssessmentAgentsReadOnly(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>评估当前实现状态<｜Assistant｜>",
		Text:                "准备三个子代理并行评估：一个分析 lowering 当前状态，一个分析 parser 当前状态，一个分析 emit 当前状态。三个同时启动。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected assessment launch repair, got %#v", got)
	}
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != 3 {
		t.Fatalf("expected 3 Agent tool calls, got %d in %s", count, got.Text)
	}
	if !strings.Contains(got.Text, "Read-only analysis") {
		t.Fatalf("expected assessment Agent prompts to remain read-only, got %s", got.Text)
	}
	if strings.Contains(got.Text, "Edit files directly") {
		t.Fatalf("assessment Agent prompt must not request edits, got %s", got.Text)
	}
}
