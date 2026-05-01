package claudecode

import (
	"strings"
	"testing"

	"ds2api/internal/toolcall"
)

var agentSchema = toolcall.ParameterSchemas{
	"Agent": {
		"type": "object",
		"properties": map[string]any{
			"description":       map[string]any{"type": "string"},
			"prompt":            map[string]any{"type": "string"},
			"subagent_type":     map[string]any{"type": "string"},
			"run_in_background": map[string]any{"type": "boolean"},
		},
		"required": []any{"description", "prompt"},
	},
}

var readSchema = toolcall.ParameterSchemas{
	"Read": {
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{"type": "string"},
		},
		"required": []any{"file_path"},
	},
}

var readSchemaWithLimitOffset = toolcall.ParameterSchemas{
	"Read": {
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{"type": "string"},
			"limit":     map[string]any{"type": "integer"},
			"offset":    map[string]any{"type": "integer"},
		},
		"required": []any{"file_path"},
	},
}

var bashSchema = toolcall.ParameterSchemas{
	"Bash": {
		"type": "object",
		"properties": map[string]any{
			"command":                   map[string]any{"type": "string"},
			"description":               map[string]any{"type": "string"},
			"dangerouslyDisableSandbox": map[string]any{"type": "boolean"},
		},
		"required": []any{"command"},
	},
}

var taskOutputSchema = toolcall.ParameterSchemas{
	"TaskOutput": {
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string"},
			"block":   map[string]any{"type": "boolean"},
			"timeout": map[string]any{"type": "integer"},
		},
		"required": []any{"task_id"},
	},
}

func TestRepairFinalOutputConvertsAgentLaunchPromise(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>请使用 Team Agents 一口气完成<｜Assistant｜>",
		Text:                "先提交，再启动 Team Agents。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected agent launch repair, got %#v", got)
	}
	if !strings.Contains(got.Text, "<tool_name>Agent</tool_name>") {
		t.Fatalf("expected Agent tool call XML, got %s", got.Text)
	}
}

func TestRepairFinalOutputConvertsChineseMultipleSubagentLaunchPromise(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>评估 cheng 语言目前实现了多少 docs/cheng-plan-full.md 并用于代理一口气实现<｜Assistant｜>",
		Text:                "我将启动多个子代理并行评估当前实现状态，然后汇总结果。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected Chinese multiple-subagent launch repair, got %#v", got)
	}
	const expectAgents = 1 // adaptive launch: 1 file ref, no "implement"/"refactor" keywords
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != expectAgents {
		t.Fatalf("expected %d Agent tool calls, got %d in %s", expectAgents, count, got.Text)
	}
}

func TestCompleteToolCallsSchemaDefaultsReadFillsLimitOffset(t *testing.T) {
	schemas := readSchemaWithLimitOffset
	calls := []toolcall.ParsedToolCall{
		{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.txt"}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if got[0].Input["file_path"] != "/tmp/a.txt" {
		t.Fatalf("expected file_path preserved, got %v", got[0].Input["file_path"])
	}
	limit, ok := got[0].Input["limit"]
	if !ok {
		t.Fatal("expected limit default to be injected")
	}
	if limit != int64(2000) {
		t.Fatalf("expected limit=2000, got %v (type %T)", limit, limit)
	}
	offset, ok := got[0].Input["offset"]
	if !ok {
		t.Fatal("expected offset default to be injected")
	}
	if offset != int64(0) {
		t.Fatalf("expected offset=0, got %v", offset)
	}
}

func TestCompleteToolCallsSchemaDefaultsReadDoesNotOverrideExistingLimit(t *testing.T) {
	schemas := readSchemaWithLimitOffset
	calls := []toolcall.ParsedToolCall{
		{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.txt", "limit": int64(100)}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if got[0].Input["limit"] != int64(100) {
		t.Fatalf("expected existing limit=100 preserved, got %v", got[0].Input["limit"])
	}
	offset, ok := got[0].Input["offset"]
	if !ok {
		t.Fatal("expected offset default to be injected")
	}
	if offset != int64(0) {
		t.Fatalf("expected offset=0, got %v", offset)
	}
}

func TestCompleteToolCallsSchemaDefaultsBashAddsSandboxDefault(t *testing.T) {
	schemas := bashSchema
	calls := []toolcall.ParsedToolCall{
		{Name: "Bash", Input: map[string]any{"command": "echo hello"}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	sandbox, ok := got[0].Input["dangerouslyDisableSandbox"]
	if !ok {
		t.Fatal("expected dangerouslyDisableSandbox default to be injected")
	}
	if sandbox != false {
		t.Fatalf("expected dangerouslyDisableSandbox=false, got %v", sandbox)
	}
}

func TestCompleteToolCallsSchemaDefaultsBashDoesNotOverrideExistingSandbox(t *testing.T) {
	schemas := bashSchema
	calls := []toolcall.ParsedToolCall{
		{Name: "Bash", Input: map[string]any{"command": "echo hello", "dangerouslyDisableSandbox": true}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	sandbox, ok := got[0].Input["dangerouslyDisableSandbox"]
	if !ok {
		t.Fatal("expected dangerouslyDisableSandbox to be present")
	}
	if sandbox != true {
		t.Fatalf("expected existing dangerouslyDisableSandbox=true preserved, got %v", sandbox)
	}
}

func TestCompleteToolCallsSchemaDefaultsAgentAddsSubagentTypeAndBackground(t *testing.T) {
	schemas := agentSchema
	calls := []toolcall.ParsedToolCall{
		{Name: "Agent", Input: map[string]any{"description": "test", "prompt": "do it"}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if got[0].Input["subagent_type"] != "Explore" {
		t.Fatalf("expected subagent_type=Explore, got %v", got[0].Input["subagent_type"])
	}
	if got[0].Input["run_in_background"] != true {
		t.Fatalf("expected run_in_background=true, got %v", got[0].Input["run_in_background"])
	}
}

func TestCompleteToolCallsSchemaDefaultsAgentDoesNotOverrideExistingValues(t *testing.T) {
	schemas := agentSchema
	calls := []toolcall.ParsedToolCall{
		{Name: "Agent", Input: map[string]any{
			"description":       "test",
			"prompt":            "do it",
			"subagent_type":     "code-reviewer",
			"run_in_background": false,
		}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if got[0].Input["subagent_type"] != "code-reviewer" {
		t.Fatalf("expected existing subagent_type preserved, got %v", got[0].Input["subagent_type"])
	}
	if got[0].Input["run_in_background"] != false {
		t.Fatalf("expected existing run_in_background preserved, got %v", got[0].Input["run_in_background"])
	}
}

func TestCompleteToolCallsSchemaDefaultsUnknownToolGetsNoDefaults(t *testing.T) {
	schemas := readSchemaWithLimitOffset
	calls := []toolcall.ParsedToolCall{
		{Name: "UnknownTool", Input: map[string]any{"foo": "bar"}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if _, ok := got[0].Input["limit"]; ok {
		t.Fatal("expected no limit default for unknown tool")
	}
	if _, ok := got[0].Input["offset"]; ok {
		t.Fatal("expected no offset default for unknown tool")
	}
}

func TestCompleteToolCallsSchemaDefaultsNilInput(t *testing.T) {
	schemas := readSchemaWithLimitOffset
	calls := []toolcall.ParsedToolCall{
		{Name: "Read", Input: nil},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if got[0].Input == nil {
		t.Fatal("expected Input to be initialized, got nil")
	}
	limit, ok := got[0].Input["limit"]
	if !ok {
		t.Fatal("expected limit default on nil input")
	}
	if limit != int64(2000) {
		t.Fatalf("expected limit=2000, got %v", limit)
	}
}

func TestCompleteToolCallsSchemaDefaultsEmptyCalls(t *testing.T) {
	schemas := readSchemaWithLimitOffset
	got := CompleteToolCallsWithSchemaDefaults(nil, schemas)
	if got != nil {
		t.Fatal("expected nil for nil input calls")
	}
	got = CompleteToolCallsWithSchemaDefaults([]toolcall.ParsedToolCall{}, schemas)
	if len(got) != 0 {
		t.Fatalf("expected empty result for empty input calls, got %d", len(got))
	}
}

func TestCompleteToolCallsSchemaDefaultsEmptySchemas(t *testing.T) {
	calls := []toolcall.ParsedToolCall{
		{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.txt"}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, nil)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if _, ok := got[0].Input["limit"]; ok {
		t.Fatal("expected no defaults injected with nil schemas")
	}
	got2 := CompleteToolCallsWithSchemaDefaults(calls, toolcall.ParameterSchemas{})
	if len(got2) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got2))
	}
}

func TestCompleteToolCallsSchemaDefaultsMixedTools(t *testing.T) {
	schemas := toolcall.ParameterSchemas{}
	for k, v := range readSchemaWithLimitOffset {
		schemas[k] = v
	}
	for k, v := range bashSchema {
		schemas[k] = v
	}
	for k, v := range agentSchema {
		schemas[k] = v
	}
	calls := []toolcall.ParsedToolCall{
		{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.txt"}},
		{Name: "Bash", Input: map[string]any{"command": "echo hi"}},
		{Name: "Agent", Input: map[string]any{"description": "d", "prompt": "p"}},
		{Name: "Unknown", Input: map[string]any{"x": "y"}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(got))
	}
	if got[0].Input["limit"] != int64(2000) {
		t.Fatalf("expected limit=2000 for Read, got %v", got[0].Input["limit"])
	}
	if got[1].Input["dangerouslyDisableSandbox"] != false {
		t.Fatalf("expected dangerouslyDisableSandbox=false for Bash, got %v", got[1].Input["dangerouslyDisableSandbox"])
	}
	if got[2].Input["subagent_type"] != "Explore" {
		t.Fatalf("expected subagent_type=Explore for Agent, got %v", got[2].Input["subagent_type"])
	}
	if got[2].Input["run_in_background"] != true {
		t.Fatalf("expected run_in_background=true for Agent, got %v", got[2].Input["run_in_background"])
	}
	if _, ok := got[3].Input["limit"]; ok {
		t.Fatal("expected no limit for unknown tool")
	}
}

func TestCompleteToolCallsSchemaDefaultsReadAliases(t *testing.T) {
	schemas := readSchemaWithLimitOffset
	aliases := []string{"read_file", "Read", "readfile"}
	for _, alias := range aliases {
		calls := []toolcall.ParsedToolCall{
			{Name: alias, Input: map[string]any{"file_path": "/tmp/a.txt"}},
		}
		got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
		if len(got) != 1 {
			t.Fatalf("alias %q: expected 1 call, got %d", alias, len(got))
		}
		if _, ok := got[0].Input["limit"]; !ok {
			t.Fatalf("alias %q: expected limit default", alias)
		}
	}
}

func TestCompleteToolCallsSchemaDefaultsBashAliases(t *testing.T) {
	schemas := bashSchema
	calls := []toolcall.ParsedToolCall{
		{Name: "execute_command", Input: map[string]any{"command": "echo hi"}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if got[0].Input["dangerouslyDisableSandbox"] != false {
		t.Fatalf("expected sandbox=false for execute_command alias")
	}
}

func TestCompleteToolCallsSchemaDefaultsAgentAliasTask(t *testing.T) {
	schemas := agentSchema
	calls := []toolcall.ParsedToolCall{
		{Name: "Task", Input: map[string]any{"description": "test", "prompt": "do it"}},
	}
	got := CompleteToolCallsWithSchemaDefaults(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if got[0].Input["subagent_type"] != "Explore" {
		t.Fatalf("expected subagent_type=Explore for Task alias, got %v", got[0].Input["subagent_type"])
	}
}

func TestRepairFinalOutputAgentLaunchExplicit3English(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>请使用 Team Agents 一口气完成<｜Assistant｜>",
		Text:                "Review the results and start 3 agents to implement the changes.",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected agent launch repair, got %#v", got)
	}
	const expectAgents = 3
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != expectAgents {
		t.Fatalf("expected %d Agent tool calls, got %d in %s", expectAgents, count, got.Text)
	}
	if !strings.Contains(got.Text, "implementation route") &&
		!strings.Contains(got.Text, "code risks") &&
		!strings.Contains(got.Text, "end-state") {
		t.Fatalf("expected diverse agent descriptions, got %s", got.Text)
	}
}

func TestRepairFinalOutputAgentLaunchExplicit5Subagents(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>请使用 Team Agents 一口气完成<｜Assistant｜>",
		Text:                "启动5个子代理并行实现",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected agent launch repair, got %#v", got)
	}
	const expectAgents = 5
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != expectAgents {
		t.Fatalf("expected %d Agent tool calls, got %d in %s", expectAgents, count, got.Text)
	}
}

func TestRepairFinalOutputAgentLaunchChineseMultipleWithImplement(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>请实现以下功能\n\n需要修改的文件有:\ndocs/cheng-plan-full.md\ndocs/cheng-syntax.md\ndocs/cheng-types.md\ndocs/cheng-standard-lib.md\ndocs/cheng-tool.md\ndocs/cheng-evolution.md\ndocs/cheng-runtime.md\n\n每个文件都有具体的实现步骤<｜Assistant｜>",
		Text:                "我将启动子代理并行实现这些功能。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected agent launch repair for complex task, got %#v", got)
	}
	const expectAgents = 4 // adaptive launch: has implement keyword + >=6 files
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != expectAgents {
		t.Fatalf("expected %d Agent tool calls for complex adaptive task, got %d in %s", expectAgents, count, got.Text)
	}
}

func TestRepairFinalOutputAgentLaunchBoundedToMax8(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>请使用 Team Agents 一口气完成<｜Assistant｜>",
		Text:                "Let's launch 15 agents to handle all tasks in parallel.",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected agent launch repair, got %#v", got)
	}
	const expectAgents = 8 // clamped to max
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != expectAgents {
		t.Fatalf("expected %d Agent tool calls (clamped), got %d in %s", expectAgents, count, got.Text)
	}
}
