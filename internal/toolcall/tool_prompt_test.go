package toolcall

import (
	"strings"
	"testing"
)

func TestBuildToolCallInstructions_ExecCommandUsesCmdExample(t *testing.T) {
	out := BuildToolCallInstructions([]string{"exec_command"})
	if !strings.Contains(out, `<tool_name>exec_command</tool_name>`) {
		t.Fatalf("expected exec_command in examples, got: %s", out)
	}
	if !strings.Contains(out, `<parameters><cmd>pwd</cmd></parameters>`) {
		t.Fatalf("expected cmd parameter example for exec_command, got: %s", out)
	}
}

func TestBuildToolCallInstructions_ExecuteCommandUsesCommandExample(t *testing.T) {
	out := BuildToolCallInstructions([]string{"execute_command"})
	if !strings.Contains(out, `<tool_name>execute_command</tool_name>`) {
		t.Fatalf("expected execute_command in examples, got: %s", out)
	}
	if !strings.Contains(out, `<parameters><command>pwd</command></parameters>`) {
		t.Fatalf("expected command parameter example for execute_command, got: %s", out)
	}
}

func TestBuildToolCallInstructions_BashExampleIncludesRequiredDescription(t *testing.T) {
	out := BuildToolCallInstructions([]string{"bash"})
	if !strings.Contains(out, `<tool_name>bash</tool_name>`) {
		t.Fatalf("expected bash in examples, got: %s", out)
	}
	if !strings.Contains(out, `<command>pwd</command><description>Show current directory</description>`) {
		t.Fatalf("expected bash example to include required description, got: %s", out)
	}
	if !strings.Contains(out, `<description>Run test shell script</description>`) {
		t.Fatalf("expected long bash example to include required description, got: %s", out)
	}
}

func TestBuildToolCallInstructions_OpenCodeLowercaseToolExamples(t *testing.T) {
	out := BuildToolCallInstructions([]string{"read", "bash", "task"})
	for _, want := range []string{
		`<tool_name>read</tool_name>`,
		`<filePath>README.md</filePath>`,
		`<tool_name>bash</tool_name>`,
		`Include every field marked required in the tool schema.`,
		`Use task/subagent tools only for genuinely independent large subtasks`,
		`Launch at most 4 Agent/task calls`,
		`Do not end with future-tense text`,
		`If you receive <task-notification>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected OpenCode example to contain %s, got: %s", want, out)
		}
	}
}

func TestBuildToolCallInstructions_TaskExampleIncludesRequiredFields(t *testing.T) {
	out := BuildToolCallInstructions([]string{"task"})
	if !strings.Contains(out, `<tool_name>task</tool_name>`) {
		t.Fatalf("expected task in examples, got: %s", out)
	}
	for _, want := range []string{
		`<description>Investigate flaky tests</description>`,
		`<prompt>Run targeted tests and summarize failures</prompt>`,
		`<subagent_type>general</subagent_type>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected task example to contain %s, got: %s", want, out)
		}
	}
}

func TestBuildToolCallInstructions_AgentExampleIncludesRequiredFields(t *testing.T) {
	out := BuildToolCallInstructions([]string{"Agent"})
	if !strings.Contains(out, `<tool_name>Agent</tool_name>`) {
		t.Fatalf("expected Agent in examples, got: %s", out)
	}
	for _, want := range []string{
		`<description>Explore Cheng codebase</description>`,
		`<prompt>Inspect the repository structure and report concise actionable findings.</prompt>`,
		`<subagent_type>Explore</subagent_type>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected Agent example to contain %s, got: %s", want, out)
		}
	}
}

func TestBuildToolCallInstructions_TaskOutputExampleIncludesRequiredFields(t *testing.T) {
	out := BuildToolCallInstructions([]string{"TaskOutput"})
	if !strings.Contains(out, `<tool_name>TaskOutput</tool_name>`) {
		t.Fatalf("expected TaskOutput in examples, got: %s", out)
	}
	for _, want := range []string{
		`<task_id>task_123</task_id>`,
		`<block>false</block>`,
		`<timeout>5000</timeout>`,
		`If you receive <task-notification>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected TaskOutput example to contain %s, got: %s", want, out)
		}
	}
}

func TestBuildToolCallInstructions_TaskTrackingToolsAreNotExamples(t *testing.T) {
	out := BuildToolCallInstructions([]string{"TaskCreate", "TodoWrite"})
	for _, bad := range []string{
		`<tool_name>TaskCreate</tool_name>`,
		`<tool_name>TodoWrite</tool_name>`,
		`<subject>Review Cheng codebase</subject>`,
		`<todos><item>`,
	} {
		if strings.Contains(out, bad) {
			t.Fatalf("expected task-tracking example %s to be suppressed, got: %s", bad, out)
		}
	}
	for _, want := range []string{
		`Do not call TaskCreate, TaskUpdate, TodoWrite, or TodoRead`,
		`A response whose only tool calls are task-tracking tools is invalid`,
		`Parameters must be XML, not JSON.`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected task-tracking suppression instruction %s, got: %s", want, out)
		}
	}
}

func TestBuildToolCallInstructions_EditToolsRequireExactOldString(t *testing.T) {
	out := BuildToolCallInstructions([]string{"Read", "Edit", "MultiEdit"})
	for _, want := range []string{
		`old_string must be copied exactly from the latest file content you read`,
		`If an edit fails, read that file again before retrying`,
		`Never build old_string from a diff hunk or from memory`,
		`Do not use Write/write_to_file to rewrite an existing source file`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected edit safety instruction %q, got: %s", want, out)
		}
	}
}

func TestBuildToolCallInstructions_OptimizeMeansExecute(t *testing.T) {
	out := BuildToolCallInstructions([]string{"question", "Read", "Edit", "bash"})
	for _, want := range []string{
		`If the user asks to optimize`,
		`请优化`,
		`choose the highest-priority actionable change`,
		`Do not use question/ask_followup_question`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected optimize execution instruction %q, got: %s", want, out)
		}
	}
}
