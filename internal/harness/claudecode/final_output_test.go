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
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != 4 {
		t.Fatalf("expected four Agent tool calls, got %d in %s", count, got.Text)
	}
}

func TestRepairFinalOutputStripsEmptyToolCallNoiseBeforeAgentRepair(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>请用子代理一口气实现<｜Assistant｜>",
		Text:                "评估实现状态并启动子代理实现</tool_calls>",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected agent repair after stripping empty wrapper, got %#v", got)
	}
	if !strings.Contains(got.Text, "<tool_name>Agent</tool_name>") {
		t.Fatalf("expected Agent tool call XML, got %s", got.Text)
	}
}

func TestRepairFinalOutputConvertsLongChineseImplementationAgentLaunch(t *testing.T) {
	text := strings.Join([]string{
		"结论：核心架构约 30% 落地，大量工作集中在 tooling 治理和 no-alias 基础。",
		"第一项：后端驱动仍有缺口。",
		"第二项：验证矩阵需要补齐。",
		"第三项：风险集中在子任务编排。",
		"第四项：需要并行推进。",
		"第五项：先处理最高优先级。",
		"第六项：保持主线收敛。",
		"第七项：不要再停在计划。",
		"现在启动 4 个实现代理，并行推进最高优先级缺口。",
	}, "\n")
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜>请使用 Team Agents 一口气完成多实落地路线和终局愿景<｜Assistant｜>",
		Text:                text,
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "agent_launch_promise" || !got.ToolCall {
		t.Fatalf("expected long Chinese implementation agent launch repair, got %#v", got)
	}
	if count := strings.Count(got.Text, "<tool_name>Agent</tool_name>"); count != 4 {
		t.Fatalf("expected four Agent tool calls, got %d in %s", count, got.Text)
	}
	parsed, visible := DetectFinalToolCalls(FinalToolCallInput{
		Text:      got.Text,
		ToolNames: []string{"Agent"},
	})
	if len(parsed.Calls) != 4 {
		t.Fatalf("expected synthesized XML to parse into four calls, got %#v", parsed)
	}
	if visible != "" {
		t.Fatalf("expected synthesized Agent XML not to leak as visible text, got %q", visible)
	}
}

func TestRepairFinalOutputDoesNotDuplicateExistingBackgroundAgents(t *testing.T) {
	prompt := `<｜User｜>请启动 4 个实现代理并行推进缺口<｜Assistant｜><tool_calls><tool_call><tool_name>Agent</tool_name><parameters><description>Map implementation route</description><prompt>x</prompt></parameters></tool_call></tool_calls><｜Tool｜>4 background agents launched
Async agent launched successfully.
The agent is working in the background.<｜Assistant｜>`
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         prompt,
		Text:                "4 个实现代理并行启动中。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if got.ToolCall || strings.Contains(got.Text, "<tool_name>Agent</tool_name>") {
		t.Fatalf("expected no duplicate Agent repair, got %#v", got)
	}
}

func TestRepairFinalOutputAllowsExplicitAdditionalAgentLaunch(t *testing.T) {
	prompt := `<｜User｜>请再启动 4 个实现代理<｜Assistant｜><｜Tool｜>4 background agents launched
Async agent launched successfully.
The agent is working in the background.<｜Assistant｜>`
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         prompt,
		Text:                "现在启动 4 个实现代理。",
		ToolNames:           []string{"Agent", "Read"},
		ToolSchemas:         agentSchema,
		AllowMetaAgentTools: true,
	})
	if !got.ToolCall || !strings.Contains(got.Text, "<tool_name>Agent</tool_name>") {
		t.Fatalf("expected explicit additional Agent repair, got %#v", got)
	}
}

func TestStripEmptyToolCallContainerNoiseKeepsPayloadSyntax(t *testing.T) {
	text := `<tool_calls><parameter name="file_path">/tmp/a.txt</parameter></tool_calls>`
	if got, changed := StripEmptyToolCallContainerNoise(text); changed || got != text {
		t.Fatalf("expected payload-like malformed syntax to stay visible, got changed=%v text=%q", changed, got)
	}
}

func TestDetectFinalToolCallsIgnoresRepeatedEmptyToolContainers(t *testing.T) {
	text := strings.Repeat("<tool_calls>", 12)
	got, visible := DetectFinalToolCalls(FinalToolCallInput{
		Text:      text,
		ToolNames: []string{"Read"},
	})
	if len(got.Calls) != 0 || got.SawToolCallSyntax {
		t.Fatalf("expected empty wrapper noise to be ignored, got %#v", got)
	}
	if visible != "" {
		t.Fatalf("expected empty visible text, got %q", visible)
	}
}

func TestDetectFinalToolCallsPromotesThinkingVisibleJSONWhenTextExists(t *testing.T) {
	text := "4 个实现代理并行启动中。"
	thinking := `{
  "tool": "Agent",
  "arguments": {
    "description": "Implement ELF writer backend",
    "prompt": "Read direct_object_emit.cheng and report the design.",
    "subagent_type": "Explore"
  }
}`
	got, visible := DetectFinalToolCalls(FinalToolCallInput{
		Text:      text,
		Thinking:  thinking,
		ToolNames: []string{"Agent"},
	})
	if len(got.Calls) != 1 || got.Calls[0].Name != "Agent" {
		t.Fatalf("expected Agent call from thinking JSON, got %#v", got)
	}
	if visible != text {
		t.Fatalf("expected visible text preserved, got %q", visible)
	}
}

func TestRepairFinalOutputPromotesThinkingToolCall(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt: "<｜User｜>继续<｜Assistant｜>",
		Thinking:    `<tool_calls><tool_call><tool_name>Read</tool_name><parameters><file_path>/tmp/a.txt</file_path></parameters></tool_call></tool_calls>`,
		ToolNames:   []string{"Read"},
		ToolSchemas: readSchema,
	})
	if !got.Changed || got.Reason != "thinking_tool_call" || !got.ToolCall {
		t.Fatalf("expected thinking tool repair, got %#v", got)
	}
	if !strings.Contains(got.Text, "<tool_name>Read</tool_name>") {
		t.Fatalf("expected Read tool call XML, got %s", got.Text)
	}
}

func TestRepairFinalOutputConvertsIncompleteReadIntentToRequestedFile(t *testing.T) {
	prompt := "<｜User｜>Environment\n - Primary working directory: /Users/lbcheng/cheng-lang\n\n评估 cheng 语言目前实现了多少 docs/cheng-plan-full.md 并用于代理一口气实现<｜Assistant｜>"
	text := "The user wants me to evaluate `docs/cheng-plan-full.md`.\nLet me first read the plan document.\n\n<tool_calls>\n<tool_calls>\n<tool_call name=\"Read\">\n<tool_call_id>agent-0</tool_call_id>\n<tool_args>{}</tool_args>\n</tool_call>\n</tool_calls>"
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt: prompt,
		Text:        text,
		ToolNames:   []string{"Read", "Agent"},
		ToolSchemas: readSchema,
	})
	if !got.Changed || got.Reason != "read_intent_from_incomplete_call" || !got.ToolCall {
		t.Fatalf("expected incomplete Read repair, got %#v", got)
	}
	parsed, visible := DetectFinalToolCalls(FinalToolCallInput{
		Text:      got.Text,
		ToolNames: []string{"Read"},
	})
	if len(parsed.Calls) != 1 || visible != "" {
		t.Fatalf("expected one executable Read call, got calls=%#v visible=%q text=%s", parsed.Calls, visible, got.Text)
	}
	if parsed.Calls[0].Name != "Read" ||
		parsed.Calls[0].Input["file_path"] != "/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md" ||
		parsed.Calls[0].Input["limit"] != "200" {
		t.Fatalf("expected synthesized bounded Read call, got %#v", parsed.Calls[0])
	}
}

func TestRepairFinalOutputConvertsTaskNotificationToTaskOutput(t *testing.T) {
	got := RepairFinalOutput(FinalOutputInput{
		FinalPrompt:         "<｜User｜><task-notification><task_id>task_done</task_id><status>completed</status></task-notification><｜Assistant｜>",
		Thinking:            "The agent completed; retrieve the result.",
		ToolNames:           []string{"TaskOutput"},
		ToolSchemas:         taskOutputSchema,
		AllowMetaAgentTools: true,
	})
	if !got.Changed || got.Reason != "task_notification_task_output" || !got.ToolCall {
		t.Fatalf("expected task notification repair, got %#v", got)
	}
	if !strings.Contains(got.Text, "<tool_name>TaskOutput</tool_name>") || !strings.Contains(got.Text, "task_done") {
		t.Fatalf("expected TaskOutput XML, got %s", got.Text)
	}
}

func TestInvalidTaskOutputIDsRejectsUnknown(t *testing.T) {
	calls := []toolcall.ParsedToolCall{{
		Name:  "TaskOutput",
		Input: map[string]any{"task_id": "missing"},
	}}
	if got := InvalidTaskOutputIDs(calls, "Task Output known\nTask is still running."); len(got) != 1 || got[0] != "missing" {
		t.Fatalf("expected missing task id, got %#v", got)
	}
}

func TestInvalidTaskOutputIDsReadsToolIDAlias(t *testing.T) {
	calls := []toolcall.ParsedToolCall{{
		Name:  "TaskOutput",
		Input: map[string]any{"tool_id": "missing"},
	}}
	if got := InvalidTaskOutputIDs(calls, "Task Output known\nTask is still running."); len(got) != 1 || got[0] != "missing" {
		t.Fatalf("expected missing aliased task id, got %#v", got)
	}
}

func TestDetectFinalToolCallsExtractsVisibleJSONAndKeepsText(t *testing.T) {
	text := `我先读文件。
{"tool":"Read","arguments":{"file_path":"/tmp/a.txt"}}
等结果。`
	got, visible := DetectFinalToolCalls(FinalToolCallInput{
		Text:      text,
		ToolNames: []string{"Read"},
	})
	if len(got.Calls) != 1 || got.Calls[0].Name != "Read" {
		t.Fatalf("expected Read call, got %#v", got.Calls)
	}
	if !got.SawToolCallSyntax {
		t.Fatalf("expected tool syntax signal")
	}
	if strings.Contains(visible, `"tool"`) || !strings.Contains(visible, "我先读文件") || !strings.Contains(visible, "等结果") {
		t.Fatalf("unexpected visible text %q", visible)
	}
}

func TestDetectFinalToolCallsInfersVisibleBashJSONObjects(t *testing.T) {
	text := `I'll evaluate the plan document against the codebase.
{
  "command": "cd /Users/lbcheng/cheng-lang && git log --oneline -20",
  "description": "Check recent commits for related work"
}
{
  "command": "cd /Users/lbcheng/cheng-lang && rg -n 'function_task|Schedule' src/core -l | head -40",
  "description": "Check function-level parallelism status"
}`
	got, visible := DetectFinalToolCalls(FinalToolCallInput{
		Text:      text,
		ToolNames: []string{"Bash", "Read"},
	})
	if len(got.Calls) != 2 {
		t.Fatalf("expected two Bash calls, got %#v", got.Calls)
	}
	if got.Calls[0].Name != "Bash" || got.Calls[0].Input["command"] != "cd /Users/lbcheng/cheng-lang && git log --oneline -20" {
		t.Fatalf("expected first inferred Bash call, got %#v", got.Calls[0])
	}
	if strings.Contains(visible, `"command"`) || !strings.Contains(visible, "I'll evaluate") {
		t.Fatalf("unexpected visible text %q", visible)
	}
}

func TestDetectFinalToolCallsPromotesOrphanAgentParametersWithoutLeak(t *testing.T) {
	text := `先并行探查各维度。

<parameter name="description">Assess Linkerless + DOD implementation</parameter>
<parameter name="prompt">Search the codebase and report paths.</parameter>
Explore

<parameter name="description">Assess function-level parallelism</parameter>
<parameter name="prompt">Check worker scheduling.</parameter>
code-reviewer`
	got, visible := DetectFinalToolCalls(FinalToolCallInput{
		Text:      text,
		ToolNames: []string{"Agent"},
	})
	if len(got.Calls) != 2 {
		t.Fatalf("expected two Agent calls, got %#v", got)
	}
	if strings.Contains(visible, "<parameter") {
		t.Fatalf("expected orphan parameter block not to leak, got %q", visible)
	}
	if !strings.Contains(visible, "先并行探查") {
		t.Fatalf("expected leading prose preserved, got %q", visible)
	}
}

func TestDetectFinalToolCallsPromotesThinkingWhenTextEmpty(t *testing.T) {
	got, visible := DetectFinalToolCalls(FinalToolCallInput{
		Text:      "",
		Thinking:  `<tool_calls><tool_call><tool_name>Read</tool_name><parameters><file_path>/tmp/a.txt</file_path></parameters></tool_call></tool_calls>`,
		ToolNames: []string{"Read"},
	})
	if len(got.Calls) != 1 || got.Calls[0].Name != "Read" {
		t.Fatalf("expected thinking Read call, got %#v", got.Calls)
	}
	if visible != "" {
		t.Fatalf("expected empty visible text, got %q", visible)
	}
}

func TestParseStreamToolBlockRepairsMalformedXML(t *testing.T) {
	got := ParseStreamToolBlock(`tool_calls><tool_call><tool_name>Read</tool_name><parameters><file_path>/tmp/a.txt</file_path></parameters></tool_call></tool_calls>`, []string{"Read"}, true)
	if !got.Parsed || len(got.Calls) != 1 || got.Calls[0].Name != "Read" {
		t.Fatalf("expected repaired Read call, got %#v", got)
	}
}

func TestParseStreamToolBlockBlocksMetaAgentsWhenDisabled(t *testing.T) {
	got := ParseStreamToolBlock(`<tool_calls><tool_call><tool_name>Agent</tool_name><parameters><description>x</description><prompt>p</prompt></parameters></tool_call></tool_calls>`, []string{"Agent"}, false)
	if !got.Parsed || len(got.Calls) != 0 || !strings.Contains(got.Text, "Agent/subagent tools are disabled") {
		t.Fatalf("expected blocked meta agent text, got %#v", got)
	}
}

func TestParseStreamToolBlockSupportsNestedFunctionBodyToolCall(t *testing.T) {
	got := ParseStreamToolBlock(`<tool_calls><tool_calls><tool_call id="agent_linkerless">Agent({"description":"Linkerless","subagent_type":"Explore","prompt":"Search evidence."})</tool_call></tool_calls></tool_calls>`, []string{"Agent"}, true)
	if !got.Parsed || len(got.Calls) != 1 {
		t.Fatalf("expected parsed Agent call, got %#v", got)
	}
	if got.Calls[0].Name != "Agent" || got.Calls[0].Input["prompt"] != "Search evidence." {
		t.Fatalf("unexpected call: %#v", got.Calls[0])
	}
}
