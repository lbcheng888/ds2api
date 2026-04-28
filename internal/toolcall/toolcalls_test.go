package toolcall

import (
	"fmt"
	"strings"
	"testing"
)

func TestFormatOpenAIToolCalls(t *testing.T) {
	formatted := FormatOpenAIToolCalls([]ParsedToolCall{{Name: "search", Input: map[string]any{"q": "x"}}})
	if len(formatted) != 1 {
		t.Fatalf("expected 1, got %d", len(formatted))
	}
	fn, _ := formatted[0]["function"].(map[string]any)
	if fn["name"] != "search" {
		t.Fatalf("unexpected function name: %#v", fn)
	}
}

func TestParseToolCallsSupportsClaudeXMLToolCall(t *testing.T) {
	text := `<tool_call><tool_name>Bash</tool_name><parameters><command>pwd</command><description>show cwd</description></parameters></tool_call>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected original tool name Bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsChildParametersWithMismatchedCloseTag(t *testing.T) {
	text := `<tool_calls>
  <tool_call>
    <tool_name>Grep</tool_name>
    <parameters>
      <output_mode><![CDATA[content]]></output_mode>
      <path><![CDATA[/Users/lbcheng/cheng-lang/src/core/backend]]></pattern>
      <parameter>MachOTextObjectWrite|ElfTextObjectWrite</parameter>
    </parameters>
  </tool_call>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"Grep"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Grep" {
		t.Fatalf("expected Grep, got %#v", calls[0])
	}
	if calls[0].Input["path"] != "/Users/lbcheng/cheng-lang/src/core/backend" {
		t.Fatalf("expected path from loose close tag, got %#v", calls[0].Input)
	}
	if calls[0].Input["parameter"] != "MachOTextObjectWrite|ElfTextObjectWrite" {
		t.Fatalf("expected nameless parameter to survive for schema repair, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsCanonicalParameterNameAttributes(t *testing.T) {
	text := `<tool_calls><tool_call><tool_name>task</tool_name><parameters><parameter name="description">list files</parameter><parameter name="prompt">run ls</parameter><parameter name="subagent_type">general</parameter></parameters></tool_call></tool_calls>`
	calls := ParseToolCalls(text, []string{"task"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Input["description"] != "list files" {
		t.Fatalf("expected description argument, got %#v", calls[0].Input)
	}
	if calls[0].Input["prompt"] != "run ls" {
		t.Fatalf("expected prompt argument, got %#v", calls[0].Input)
	}
	if calls[0].Input["subagent_type"] != "general" {
		t.Fatalf("expected subagent_type argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsRepairsTruncatedTitleToolCall(t *testing.T) {
	text := "tool_calls\n<tool_call>\n<tool_name>Read</tool_name>\n<parameter name=\"file_path\" type=\"string\">/tmp/a.txt</parameter>\n<parameter name=\"limit\" type=\"number\">150</parameter>\n"
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 1 {
		t.Fatalf("expected one repaired title tool call, got %#v", calls)
	}
	if calls[0].Name != "Read" {
		t.Fatalf("unexpected tool name: %#v", calls[0])
	}
	if calls[0].Input["file_path"] != "/tmp/a.txt" {
		t.Fatalf("unexpected input: %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsToolCallNameTagWithNamedParameters(t *testing.T) {
	text := `<tool_calls>
<tool_call>
<tool_call_name>read</tool_call_name>
<parameter name="filePath" type="string">/Users/lbcheng/cheng-lang/findings.md</parameter>
</tool_call>
<tool_call>
<tool_call_name>read</tool_call_name>
<parameter name="filePath" type="string">/Users/lbcheng/cheng-lang/progress.md</parameter>
</tool_call>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"read"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %#v", calls)
	}
	if calls[0].Name != "read" || calls[0].Input["filePath"] != "/Users/lbcheng/cheng-lang/findings.md" {
		t.Fatalf("expected first read filePath argument, got %#v", calls[0])
	}
	if calls[1].Name != "read" || calls[1].Input["filePath"] != "/Users/lbcheng/cheng-lang/progress.md" {
		t.Fatalf("expected second read filePath argument, got %#v", calls[1])
	}
}

func TestParseToolCallsInfersReadFromNamelessFilePathParameter(t *testing.T) {
	text := `<tool_calls>
<tool_call>
<parameter name="file_path">/Users/lbcheng/cheng-lang/src/core/lang/parser.cheng</parameter>
</tool_call>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"Read", "Bash", "Edit"})
	if len(calls) != 1 {
		t.Fatalf("expected one inferred Read call, got %#v", calls)
	}
	if calls[0].Name != "Read" {
		t.Fatalf("expected inferred Read tool, got %#v", calls[0])
	}
	if calls[0].Input["file_path"] != "/Users/lbcheng/cheng-lang/src/core/lang/parser.cheng" {
		t.Fatalf("expected file_path argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsInfersBashFromNamelessCommandParameter(t *testing.T) {
	text := `<tool_calls>
<tool_call>
<parameter name="command" description="List files">ls -la /Users/lbcheng/cheng-lang</parameter>
</tool_call>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"Read", "Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected one inferred Bash call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected inferred Bash tool, got %#v", calls[0])
	}
	if calls[0].Input["command"] != "ls -la /Users/lbcheng/cheng-lang" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsDoesNotInferAmbiguousNamelessParameters(t *testing.T) {
	text := `<tool_calls><tool_call><parameter name="description">Review code</parameter></tool_call></tool_calls>`
	calls := ParseToolCalls(text, []string{"Agent", "TaskCreate"})
	if len(calls) != 0 {
		t.Fatalf("expected ambiguous nameless call to be ignored, got %#v", calls)
	}
}

func TestParseToolCallsSupportsSiblingParametersOutsideParametersTag(t *testing.T) {
	text := `<tool_calls>
<tool_call>
<tool_name>read</tool_name>
<parameters>
<filePath>/Users/lbcheng/cheng-lang/src/core/tooling/gate_main.cheng</filePath>
</parameters>
<limit>100</limit>
</tool_call>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"read"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Input["filePath"] != "/Users/lbcheng/cheng-lang/src/core/tooling/gate_main.cheng" {
		t.Fatalf("expected filePath argument, got %#v", calls[0].Input)
	}
	if calls[0].Input["limit"] != float64(100) {
		t.Fatalf("expected sibling limit argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsMalformedToolNameEqualsTag(t *testing.T) {
	text := `<tool_calls>
<tool_call>
<tool_name="read</tool_name>
<parameters>
<filePath>/Users/lbcheng/cheng-lang/src/core/backend/line_map.cheng</filePath>
<limit>50</limit>
</parameters>
</tool_call>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"read"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "read" {
		t.Fatalf("expected read tool name, got %#v", calls[0])
	}
	if calls[0].Input["filePath"] != "/Users/lbcheng/cheng-lang/src/core/backend/line_map.cheng" {
		t.Fatalf("expected filePath argument, got %#v", calls[0].Input)
	}
	if calls[0].Input["limit"] != "50" {
		t.Fatalf("expected limit argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsToolCallNameAttributeWithNamedParameters(t *testing.T) {
	text := `<tool_calls>
<tool_call name="read">
<parameter name="filePath" string="true">/Users/lbcheng/cheng-lang</parameter>
</tool_call>
<tool_call name="glob">
<parameter name="pattern" string="true">**/*.c</parameter>
<parameter name="path" string="true">/Users/lbcheng/cheng-lang</parameter>
</tool_call>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"read", "glob"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %#v", calls)
	}
	if calls[0].Name != "read" || calls[0].Input["filePath"] != "/Users/lbcheng/cheng-lang" {
		t.Fatalf("expected read filePath argument, got %#v", calls[0])
	}
	if calls[1].Name != "glob" || calls[1].Input["pattern"] != "**/*.c" || calls[1].Input["path"] != "/Users/lbcheng/cheng-lang" {
		t.Fatalf("expected glob pattern/path arguments, got %#v", calls[1])
	}
}

func TestParseToolCallsKeepsCompleteToolCallBeforeTruncatedSibling(t *testing.T) {
	text := `<tool_calls>
<tool_call name="Read">
<parameter name="file_path" string="true">/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md</parameter>
<parameter name="limit" number="200">200</parameter>
</tool_call>
<tool_call name`
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 1 {
		t.Fatalf("expected one complete Read call before truncated sibling, got %#v", calls)
	}
	if calls[0].Name != "Read" {
		t.Fatalf("expected Read, got %#v", calls[0])
	}
	if calls[0].Input["file_path"] != "/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md" {
		t.Fatalf("unexpected input: %#v", calls[0].Input)
	}
}

func TestParseToolCallsParsesNamedToolCallWithParamAliasAfterVisibleText(t *testing.T) {
	text := `Design agent 完成但缺少上下文。

<tool_calls>
<tool_call name="Read">
<param name="file_path">/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md</param>
<param name="limit">200</param>
</tool_call>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 1 {
		t.Fatalf("expected one Read call, got %#v", calls)
	}
	if calls[0].Name != "Read" || calls[0].Input["file_path"] != "/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md" {
		t.Fatalf("unexpected call %#v", calls[0])
	}
}

func TestParseToolCallsInfersAgentFromOrphanParameterGroups(t *testing.T) {
	text := `<parameter name="description">
Assess Linkerless + DOD implementation
</parameter>
<parameter name="prompt">
Search the cheng-lang codebase and report concrete file paths.
</parameter>
Explore

<parameter name="description">
Assess function-level parallelism
</parameter>
<parameter name="prompt">
Check serial_task_plan and worker scheduling.
</parameter>
code-reviewer`
	calls := ParseToolCalls(text, []string{"Agent"})
	if len(calls) != 2 {
		t.Fatalf("expected two Agent calls, got %#v", calls)
	}
	if calls[0].Name != "Agent" || calls[0].Input["description"] != "Assess Linkerless + DOD implementation" {
		t.Fatalf("unexpected first call: %#v", calls[0])
	}
	if calls[0].Input["subagent_type"] != "Explore" {
		t.Fatalf("expected plain subagent type to be captured, got %#v", calls[0].Input)
	}
	if calls[1].Input["subagent_type"] != "code-reviewer" {
		t.Fatalf("expected second plain subagent type, got %#v", calls[1].Input)
	}
}

func TestParseToolCallsSupportsDirectToolElementBody(t *testing.T) {
	text := `<tool_calls><tool_call><bash><command>find /Users/lbcheng/cheng-lang -type f | head -100</command><description>List all files</description></bash></tool_call></tool_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected bash tool, got %#v", calls[0])
	}
	if calls[0].Input["command"] != "find /Users/lbcheng/cheng-lang -type f | head -100" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
	if calls[0].Input["description"] != "List all files" {
		t.Fatalf("expected description argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsRepairsMissingToolCallCloseBeforeWrapperClose(t *testing.T) {
	text := `<tool_calls>
<tool_call>
<tool_name>bash</tool_name>
<parameters><command>pwd</command><description>Show current directory</description></parameters>
</tool_calls></tool_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("expected bash tool, got %#v", calls[0])
	}
	if calls[0].Input["command"] != "pwd" || calls[0].Input["description"] != "Show current directory" {
		t.Fatalf("expected repaired bash arguments, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsLooseParameterClosingTag(t *testing.T) {
	text := `<tool_calls>
  <tool_call>
    <tool_name>Bash</tool_name>
    <parameters><description>Show diff</description><command><![CDATA[cd /Users/lbcheng/cheng-lang && git diff --stat HEAD]]></parameter></description>
  </tool_call>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected Bash tool, got %#v", calls[0])
	}
	if calls[0].Input["description"] != "Show diff" {
		t.Fatalf("expected description argument, got %#v", calls[0].Input)
	}
	if calls[0].Input["command"] != "cd /Users/lbcheng/cheng-lang && git diff --stat HEAD" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsRepairsMissingWrapperAngleAndLooseDirectToolElements(t *testing.T) {
	text := `Let me inspect first.tool_calls>
  <tool_call>
    <read_mcp_resource>
      <parameters><server>cheng</server><uri>skill://cheng语言/SKILL.md</uri></parameters>
    </tool_call>
  <tool_call>
    <exec_command>
      <parameters><cmd>find /Users/lbcheng/cheng-lang -type f | head -80</parameters><justification>Get overview</justification></parameters>
    </tool_call>
  <tool_call>
    <exec_command>
      <parameters><cmd>ls -la /Users/lbcheng/cheng-lang/</parameters><justification>List root</justification></parameters>
    </tool_call>
</tool_calls>`

	calls := ParseToolCalls(text, []string{"read_mcp_resource", "exec_command"})
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %#v", calls)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("expected local skill resource read to be rewritten to exec_command, got %#v", calls[0])
	}
	cmd, _ := calls[0].Input["cmd"].(string)
	if !strings.Contains(cmd, `sed -n '1,200p'`) || !strings.Contains(cmd, `/Users/lbcheng/.codex/skills/cheng语言/SKILL.md`) {
		t.Fatalf("expected bounded local skill read command, got %#v", calls[0])
	}
	if calls[1].Name != "exec_command" || calls[1].Input["cmd"] != "find /Users/lbcheng/cheng-lang -type f | head -80" {
		t.Fatalf("expected first exec_command args, got %#v", calls[1])
	}
	if calls[2].Name != "exec_command" || calls[2].Input["cmd"] != "ls -la /Users/lbcheng/cheng-lang/" {
		t.Fatalf("expected second exec_command args, got %#v", calls[2])
	}
}

func TestParseToolCallsRewritesLocalFileResourceReadToReadTool(t *testing.T) {
	text := `<tool_calls>
  <tool_call>
    <tool_name>read_mcp_resource</tool_name>
    <parameters>
      <server>codex-dev</server>
      <url>file:///Users/lbcheng/.codex/skills/cheng%E8%AF%AD%E8%A8%80/SKILL.md</url>
      <limit>120</limit>
    </parameters>
  </tool_call>
</tool_calls>`

	calls := ParseToolCalls(text, []string{"read_mcp_resource", "Read", "exec_command"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 rewritten call, got %#v", calls)
	}
	if calls[0].Name != "Read" {
		t.Fatalf("expected local Read call, got %#v", calls[0])
	}
	if calls[0].Input["file_path"] != "/Users/lbcheng/.codex/skills/cheng语言/SKILL.md" {
		t.Fatalf("expected decoded local file path, got %#v", calls[0].Input)
	}
	if calls[0].Input["limit"] != "120" {
		t.Fatalf("expected limit to be preserved, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsRewritesLocalFileResourceReadToExecCommand(t *testing.T) {
	text := `<tool_calls>
  <tool_call>
    <tool_name>codex-dev.read_mcp_resource</tool_name>
    <parameters>
      <server>codex-dev</server>
      <url>file:///Users/lbcheng/.codex/skills/cheng%E8%AF%AD%E8%A8%80/SKILL.md</url>
    </parameters>
  </tool_call>
</tool_calls>`

	calls := ParseToolCalls(text, []string{"read_mcp_resource", "exec_command"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 rewritten call, got %#v", calls)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("expected exec_command call, got %#v", calls[0])
	}
	cmd, _ := calls[0].Input["cmd"].(string)
	if !strings.Contains(cmd, `sed -n '1,200p'`) || !strings.Contains(cmd, `/Users/lbcheng/.codex/skills/cheng语言/SKILL.md`) {
		t.Fatalf("expected bounded local read command, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsRewritesCodexSkillResourceRead(t *testing.T) {
	text := `<tool_calls>
  <tool_call>
    <tool_name>read_mcp_resource</tool_name>
    <parameters>
      <server>codex</server>
      <uri>skill://cheng%E8%AF%AD%E8%A8%80/SKILL.md</uri>
    </parameters>
  </tool_call>
</tool_calls>`

	calls := ParseToolCalls(text, []string{"read_mcp_resource", "exec_command"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 rewritten call, got %#v", calls)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("expected exec_command call, got %#v", calls[0])
	}
	cmd, _ := calls[0].Input["cmd"].(string)
	if !strings.Contains(cmd, `sed -n '1,200p'`) || !strings.Contains(cmd, `.codex/skills/cheng语言/SKILL.md`) {
		t.Fatalf("expected bounded local skill read command, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsRewritesUnavailableReadFileToExecCommand(t *testing.T) {
	text := `<tool_calls>
  <tool_call>
    <tool_name>read_file</tool_name>
    <parameters>
      <path>/Users/lbcheng/.codex/skills/cheng语言/SKILL.md</path>
    </parameters>
  </tool_call>
</tool_calls>`

	calls := ParseToolCalls(text, []string{"exec_command"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 rewritten call, got %#v", calls)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("expected exec_command call, got %#v", calls[0])
	}
	cmd, _ := calls[0].Input["cmd"].(string)
	if !strings.Contains(cmd, `sed -n '1,200p'`) || !strings.Contains(cmd, `.codex/skills/cheng语言/SKILL.md`) {
		t.Fatalf("expected bounded local read command, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsSelfClosingValueParameters(t *testing.T) {
	text := `<tool_calls>
<tool_call>
<tool_call_name>read_mcp_resource</tool_call_name>
<parameter name="server" value="cheng" />
<parameter name="uri" value="skill://cheng语言/SKILL.md" />
</tool_call>
</tool_calls>`

	calls := ParseToolCalls(text, []string{"read_mcp_resource"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "read_mcp_resource" || calls[0].Input["server"] != "cheng" || calls[0].Input["uri"] != "skill://cheng语言/SKILL.md" {
		t.Fatalf("expected self-closing value parameters, got %#v", calls[0])
	}
}

func TestParseToolCallsSupportsVisibleJSONToolArray(t *testing.T) {
	text := `[
  {
    "tool": "Read",
    "arguments": {
      "file_path": "/Users/lbcheng/cheng-lang/src/core/backend/primary_object_plan.cheng",
      "offset": 345,
      "limit": 65
    }
  },
  {
    "tool": "Read",
    "arguments": {
      "file_path": "/Users/lbcheng/cheng-lang/src/core/backend/primary_object_plan.cheng",
      "offset": 773,
      "limit": 30
    }
  }
]`
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 visible JSON tool calls, got %#v", calls)
	}
	if calls[0].Name != "Read" || calls[0].Input["file_path"] != "/Users/lbcheng/cheng-lang/src/core/backend/primary_object_plan.cheng" {
		t.Fatalf("expected first Read call with file_path, got %#v", calls[0])
	}
	if fmt.Sprint(calls[0].Input["offset"]) != "345" || fmt.Sprint(calls[0].Input["limit"]) != "65" {
		t.Fatalf("expected numeric offset/limit to be preserved, got %#v", calls[0].Input)
	}
	if calls[1].Name != "Read" || fmt.Sprint(calls[1].Input["offset"]) != "773" || fmt.Sprint(calls[1].Input["limit"]) != "30" {
		t.Fatalf("expected second Read call, got %#v", calls[1])
	}
}

func TestParseToolCallsSplitsReadFilePathArray(t *testing.T) {
	text := `{
  "tool": "Read",
  "arguments": {
    "file_path": [
      "/Users/lbcheng/cheng-lang/README.md",
      "/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md"
    ],
    "offset": [1, 200],
    "limit": 120
  }
}`
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 2 {
		t.Fatalf("expected Read file_path array to split into 2 calls, got %#v", calls)
	}
	if calls[0].Name != "Read" || calls[0].Input["file_path"] != "/Users/lbcheng/cheng-lang/README.md" {
		t.Fatalf("expected first scalar Read call, got %#v", calls[0])
	}
	if calls[1].Input["file_path"] != "/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md" {
		t.Fatalf("expected second scalar Read call, got %#v", calls[1])
	}
	if fmt.Sprint(calls[0].Input["offset"]) != "1" || fmt.Sprint(calls[1].Input["offset"]) != "200" {
		t.Fatalf("expected per-file offsets, got %#v %#v", calls[0].Input, calls[1].Input)
	}
	if fmt.Sprint(calls[0].Input["limit"]) != "120" || fmt.Sprint(calls[1].Input["limit"]) != "120" {
		t.Fatalf("expected shared limit on both calls, got %#v %#v", calls[0].Input, calls[1].Input)
	}
}

func TestParseToolCallsDropsDegenerateBashCommand(t *testing.T) {
	text := `{"tool":"Bash","arguments":{"command":">","description":"broken shell redirect"}}`
	calls := ParseToolCalls(text, []string{"Bash"})
	if len(calls) != 0 {
		t.Fatalf("expected degenerate Bash command to be dropped, got %#v", calls)
	}
}

func TestParseToolCallsSupportsVisibleJSONToolObjectSequence(t *testing.T) {
	text := `{
  "tool": "Read",
  "arguments": {
    "file_path": "/Users/lbcheng/cheng-lang/src/core/backend/primary_object_plan.cheng",
    "offset": 345,
    "limit": 65
  }
}
{
  "tool": "Read",
  "arguments": {
    "file_path": "/Users/lbcheng/cheng-lang/src/core/backend/primary_object_plan.cheng",
    "offset": 773,
    "limit": 30
  }
}`
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 visible JSON object tool calls, got %#v", calls)
	}
	if calls[0].Name != "Read" || fmt.Sprint(calls[0].Input["offset"]) != "345" || fmt.Sprint(calls[0].Input["limit"]) != "65" {
		t.Fatalf("expected first Read call offset/limit, got %#v", calls[0])
	}
	if calls[1].Name != "Read" || fmt.Sprint(calls[1].Input["offset"]) != "773" || fmt.Sprint(calls[1].Input["limit"]) != "30" {
		t.Fatalf("expected second Read call offset/limit, got %#v", calls[1])
	}
}

func TestParseToolCallsInfersVisibleBashObjectSequence(t *testing.T) {
	text := `{
  "command": "cd /Users/lbcheng/cheng-lang && git log --oneline -20",
  "description": "Check recent commits for related work"
}
{
  "command": "cd /Users/lbcheng/cheng-lang && rg -n 'function_task|Schedule' src/core -l | head -40",
  "description": "Check function-level parallelism status"
}`
	calls := ParseToolCalls(text, []string{"Bash", "Read"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 inferred Bash calls, got %#v", calls)
	}
	if calls[0].Name != "Bash" || calls[0].Input["command"] != "cd /Users/lbcheng/cheng-lang && git log --oneline -20" {
		t.Fatalf("expected first inferred Bash call, got %#v", calls[0])
	}
	if calls[1].Name != "Bash" || calls[1].Input["description"] != "Check function-level parallelism status" {
		t.Fatalf("expected second inferred Bash call, got %#v", calls[1])
	}
}

func TestParseToolCallsDoesNotTreatArbitraryVisibleJSONAsToolCall(t *testing.T) {
	text := `{
  "description": "normal JSON payload",
  "enabled": true
}`
	res := ParseToolCallsDetailed(text, []string{"Bash", "Read"})
	if len(res.Calls) != 0 || res.SawToolCallSyntax {
		t.Fatalf("expected arbitrary JSON to remain non-tool text, got %#v", res)
	}
}

func TestParseToolCallsAnyToolWildcardAcceptsVisibleJSONName(t *testing.T) {
	text := `{"tool":"Read","arguments":{"file_path":"/tmp/a.txt"}}`
	calls := ParseToolCalls(text, []string{"__any_tool__"})
	if len(calls) != 1 {
		t.Fatalf("expected wildcard to accept parsed tool name, got %#v", calls)
	}
	if calls[0].Name != "Read" || calls[0].Input["file_path"] != "/tmp/a.txt" {
		t.Fatalf("expected Read call through wildcard, got %#v", calls[0])
	}
}

func TestExtractVisibleJSONToolCallsWithLeadingProse(t *testing.T) {
	text := `Let me read the rest of the plan and start examining the codebase in parallel.
{
  "tool": "Read",
  "arguments": {
    "file_path": "/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md",
    "offset": 200,
    "limit": 200
  }
}
{
  "tool": "TaskCreate",
  "arguments": {
    "description": "评估实现进度",
    "prompt": "检查 docs/cheng-plan-full.md"
  }
}`
	prefix, calls, suffix, ok := ExtractVisibleJSONToolCalls(text, []string{"Read", "TaskCreate"})
	if !ok {
		t.Fatalf("expected visible JSON tool calls to be extracted")
	}
	if !strings.Contains(prefix, "Let me read the rest") || strings.TrimSpace(suffix) != "" {
		t.Fatalf("unexpected prefix/suffix: prefix=%q suffix=%q", prefix, suffix)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 extracted calls, got %#v", calls)
	}
	if calls[0].Name != "Read" || calls[0].Input["file_path"] != "/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md" {
		t.Fatalf("expected first Read call, got %#v", calls[0])
	}
	if calls[1].Name != "TaskCreate" {
		t.Fatalf("expected TaskCreate second call, got %#v", calls[1])
	}
}

func TestExtractVisibleJSONToolCallsRepairsLooseBashCommandQuotes(t *testing.T) {
	text := `Let me start by reading the full plan document and surveying the codebase structure in parallel.
{
  "tool": "Read",
  "arguments": {
    "file_path": "/Users/lbcheng/cheng-lang/docs/cheng-plan-full.md",
    "limit": 200
  }
}
{
  "tool": "Bash",
  "arguments": {
    "command": "cd /Users/lbcheng/cheng-lang && git ls-files | head -80 && echo "---" && git ls-files | wc -l",
    "description": "List tracked files and count"
  }
}
{
  "tool": "Bash",
  "arguments": {
    "command": "cd /Users/lbcheng/cheng-lang && git ls-files "*.cheng" | head -60 && echo "COUNT=$(git ls-files "*.cheng" | wc -l)"",
    "description": "List .cheng source files"
  }
}`
	prefix, calls, suffix, ok := ExtractVisibleJSONToolCalls(text, []string{"Read", "Bash"})
	if !ok {
		t.Fatalf("expected loose visible JSON tool calls to be extracted")
	}
	if !strings.Contains(prefix, "Let me start") || strings.TrimSpace(suffix) != "" {
		t.Fatalf("unexpected prefix/suffix: prefix=%q suffix=%q", prefix, suffix)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 extracted calls, got %#v", calls)
	}
	if calls[0].Name != "Read" || calls[1].Name != "Bash" || calls[2].Name != "Bash" {
		t.Fatalf("unexpected call names: %#v", calls)
	}
	if !strings.Contains(fmt.Sprint(calls[1].Input["command"]), `echo "---"`) {
		t.Fatalf("expected first Bash command quotes preserved, got %#v", calls[1])
	}
	if !strings.Contains(fmt.Sprint(calls[2].Input["command"]), `COUNT=$(`) {
		t.Fatalf("expected second Bash command preserved, got %#v", calls[2])
	}
}

func TestExtractVisibleJSONToolCallsIgnoresFencedExamples(t *testing.T) {
	text := "Here is an example:\n```json\n{\"tool\":\"Read\",\"arguments\":{\"file_path\":\"/tmp/a\"}}\n```\nDo not execute it."
	_, calls, _, ok := ExtractVisibleJSONToolCalls(text, []string{"Read"})
	if ok || len(calls) != 0 {
		t.Fatalf("expected fenced JSON tool example to stay text, got ok=%v calls=%#v", ok, calls)
	}
}

func TestParseToolCallsIgnoresVisibleJSONToolArrayInsideFence(t *testing.T) {
	text := "```json\n[{\"tool\":\"Read\",\"arguments\":{\"file_path\":\"/tmp/a\"}}]\n```"
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 0 {
		t.Fatalf("expected fenced JSON tool example to stay text, got %#v", calls)
	}
}

func TestParseToolCallsIgnoresPlainJSONArray(t *testing.T) {
	text := `[{"name":"not a tool","value":1}]`
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 0 {
		t.Fatalf("expected ordinary JSON array to stay text, got %#v", calls)
	}
}

func TestParseToolCallsRepairsDuplicateToolCallNameOpenWithSelfClosingParams(t *testing.T) {
	text := `<tool_calls>
<tool_call>
<tool_call_name>read_mcp_resource</tool_call_name>
<tool_call_name>
<parameter name="server" value="filesystem" />
<parameter name="uri" value="file:///Users/lbcheng/.codex/skills/cheng%E8%AF%AD%E8%A8%80/SKILL.md" />
</parameter>
</tool_call>
</tool_calls>`

	calls := ParseToolCalls(text, []string{"read_mcp_resource", "exec_command"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 rewritten call, got %#v", calls)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("expected exec_command call, got %#v", calls[0])
	}
	cmd, _ := calls[0].Input["cmd"].(string)
	if !strings.Contains(cmd, `sed -n '1,200p'`) || !strings.Contains(cmd, `.codex/skills/cheng语言/SKILL.md`) {
		t.Fatalf("expected bounded local read command, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsMultilineCDATAAndRepeatedXMLTags(t *testing.T) {
	text := `<tool_call><tool_name>write_file</tool_name><parameters><path>script.sh</path><content><![CDATA[#!/bin/bash
echo "hello"
]]></content><item>first</item><item>second</item></parameters></tool_call>`
	calls := ParseToolCalls(text, []string{"write_file"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "write_file" {
		t.Fatalf("expected tool name write_file, got %q", calls[0].Name)
	}
	if calls[0].Input["path"] != "script.sh" {
		t.Fatalf("expected path argument, got %#v", calls[0].Input)
	}
	content, _ := calls[0].Input["content"].(string)
	if !strings.Contains(content, "#!/bin/bash") || !strings.Contains(content, "echo \"hello\"") {
		t.Fatalf("expected multiline CDATA content to be preserved, got %#v", calls[0].Input["content"])
	}
	items, ok := calls[0].Input["item"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("expected repeated XML tags to become an array, got %#v", calls[0].Input["item"])
	}
}

func TestParseToolCallsSupportsCanonicalXMLParametersJSON(t *testing.T) {
	text := `<tool_call><tool_name>get_weather</tool_name><parameters>{"city":"beijing","unit":"c"}</parameters></tool_call>`
	calls := ParseToolCalls(text, []string{"get_weather"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "get_weather" {
		t.Fatalf("expected tool name get_weather, got %q", calls[0].Name)
	}
	if calls[0].Input["city"] != "beijing" || calls[0].Input["unit"] != "c" {
		t.Fatalf("expected parsed json parameters, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsPreservesRawMalformedXMLParameters(t *testing.T) {
	text := `<tool_call><tool_name>execute_command</tool_name><parameters>cd /root && git status</parameters></tool_call>`
	calls := ParseToolCalls(text, []string{"execute_command"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "execute_command" {
		t.Fatalf("expected tool name execute_command, got %q", calls[0].Name)
	}
	raw, ok := calls[0].Input["_raw"].(string)
	if !ok {
		t.Fatalf("expected raw argument tracking, got %#v", calls[0].Input)
	}
	if raw != "cd /root && git status" {
		t.Fatalf("expected raw arguments to be preserved, got %q", raw)
	}
}

func TestParseToolCallsSupportsXMLParametersJSONWithAmpersandCommand(t *testing.T) {
	text := `<tool_calls><tool_call><tool_name>execute_command</tool_name><parameters>{"command":"sshpass -p 'xxx' ssh -o StrictHostKeyChecking=no -p 1111 root@111.111.111.111 'cd /root && git clone https://github.com/ericc-ch/copilot-api.git'","cwd":null,"timeout":null}</parameters></tool_call></tool_calls>`
	calls := ParseToolCalls(text, []string{"execute_command"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "execute_command" {
		t.Fatalf("expected tool name execute_command, got %q", calls[0].Name)
	}
	cmd, _ := calls[0].Input["command"].(string)
	if !strings.Contains(cmd, "&& git clone") {
		t.Fatalf("expected command to keep && segment, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsDoesNotTreatParameterNameTagAsToolName(t *testing.T) {
	text := `<tool_call><tool name="execute_command"><parameters><name>file.txt</name><command>pwd</command></parameters></tool></tool_call>`
	calls := ParseToolCalls(text, []string{"execute_command"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "execute_command" {
		t.Fatalf("expected tool name execute_command, got %q", calls[0].Name)
	}
	if calls[0].Input["name"] != "file.txt" {
		t.Fatalf("expected parameter name preserved, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsDetailedMarksXMLToolCallSyntax(t *testing.T) {
	text := `<tool_call><tool_name>Bash</tool_name><parameters><command>pwd</command></parameters></tool_call>`
	res := ParseToolCallsDetailed(text, []string{"bash"})
	if !res.SawToolCallSyntax {
		t.Fatalf("expected SawToolCallSyntax=true, got %#v", res)
	}
	if len(res.Calls) != 1 {
		t.Fatalf("expected one parsed call, got %#v", res)
	}
}

func TestParseToolCallsDetailedMarksUpperCamelToolCallSyntax(t *testing.T) {
	text := `<ToolCall id="toolu_1"><command>pwd</command></ToolCall>`
	res := ParseToolCallsDetailed(text, []string{"Bash"})
	if !res.SawToolCallSyntax {
		t.Fatalf("expected SawToolCallSyntax=true for ToolCall wrapper, got %#v", res)
	}
	if len(res.Calls) != 0 {
		t.Fatalf("expected nonstandard ToolCall wrapper not to parse silently, got %#v", res.Calls)
	}
}

func TestParseToolCallsSupportsUpperCamelToolCallJSONBody(t *testing.T) {
	text := `先提交，再启动 4 个并行代理。

<ToolCall id="toolu_1">
{"command":"cd /tmp && git status","description":"Check repository status"}
</ToolCall>`
	res := ParseToolCallsDetailed(text, []string{"Bash", "Agent"})
	if !res.SawToolCallSyntax {
		t.Fatalf("expected SawToolCallSyntax=true for ToolCall wrapper, got %#v", res)
	}
	if len(res.Calls) != 1 {
		t.Fatalf("expected one inferred Bash call, got %#v", res.Calls)
	}
	if res.Calls[0].Name != "Bash" {
		t.Fatalf("expected inferred Bash call, got %#v", res.Calls[0])
	}
	if res.Calls[0].Input["command"] != "cd /tmp && git status" {
		t.Fatalf("expected command argument, got %#v", res.Calls[0].Input)
	}
}

func TestParseToolCallsSupportsClaudeXMLJSONToolCall(t *testing.T) {
	text := `<tool_call>{"tool":"Bash","params":{"command":"pwd","description":"show cwd"}}</tool_call>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected original tool name Bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsFunctionBodyInNestedToolCalls(t *testing.T) {
	text := `<tool_calls>
  <tool_calls>
    <tool_call id="agent_linkerless_dod">Agent({"description":"Linkerless + DOD + hotspots","subagent_type":"Explore","prompt":"Search for direct_writer and report evidence."})</tool_call>
  </tool_calls>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"Agent"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Agent" {
		t.Fatalf("expected Agent call, got %#v", calls[0])
	}
	if calls[0].Input["description"] != "Linkerless + DOD + hotspots" {
		t.Fatalf("expected description argument, got %#v", calls[0].Input)
	}
	if calls[0].Input["subagent_type"] != "Explore" {
		t.Fatalf("expected subagent_type argument, got %#v", calls[0].Input)
	}
	if calls[0].Input["prompt"] != "Search for direct_writer and report evidence." {
		t.Fatalf("expected prompt argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsFunctionCallTagStyle(t *testing.T) {
	text := `<function_call>Bash</function_call><function parameter name="command">ls -la</function parameter><function parameter name="description">list</function parameter>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected original tool name Bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "ls -la" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsAntmlFunctionCallStyle(t *testing.T) {
	text := `<antml:function_calls><antml:function_call name="Bash">{"command":"pwd","description":"x"}</antml:function_call></antml:function_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected original tool name Bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsAntmlArgumentStyle(t *testing.T) {
	text := `<antml:function_calls><antml:function_call id="1" name="Bash"><antml:argument name="command">pwd</antml:argument><antml:argument name="description">x</antml:argument></antml:function_call></antml:function_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected original tool name Bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsInvokeFunctionCallStyle(t *testing.T) {
	text := `<function_calls><invoke name="Bash"><parameter name="command">pwd</parameter><parameter name="description">d</parameter></invoke></function_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected original tool name Bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsInvokeParameterTypeAttributes(t *testing.T) {
	text := `<tool_calls>
<invoke name="task">
<parameter name="description" string="true">审查cheng语言代码结构</parameter>
<parameter name="prompt" string="true">探索 /Users/lbcheng/cheng-lang 项目的完整目录结构。</parameter>
<parameter name="subagent_type" string="true">explore</parameter>
<parameter name="max_retries" string="false">2</parameter>
</invoke>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"task"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 task call, got %#v", calls)
	}
	if calls[0].Name != "task" {
		t.Fatalf("expected task tool name, got %q", calls[0].Name)
	}
	for _, key := range []string{"description", "prompt", "subagent_type", "max_retries"} {
		if _, ok := calls[0].Input[key]; !ok {
			t.Fatalf("expected parameter %q in parsed input, got %#v", key, calls[0].Input)
		}
	}

	schemas := ParameterSchemas{
		"task": {
			"type": "object",
			"properties": map[string]any{
				"description":   map[string]any{"type": "string"},
				"prompt":        map[string]any{"type": "string"},
				"subagent_type": map[string]any{"type": "string"},
				"max_retries":   map[string]any{"type": "integer"},
			},
			"required": []any{"description", "prompt", "subagent_type"},
		},
	}
	normalized := NormalizeCallsForSchemasWithMeta(calls, schemas, true)
	if len(normalized) != 1 {
		t.Fatalf("expected normalized task call, got %#v", normalized)
	}
	if normalized[0].Input["max_retries"] != int64(2) {
		t.Fatalf("expected max_retries to normalize to int64(2), got %#v", normalized[0].Input["max_retries"])
	}
}

func TestParseToolCallsSupportsLooseMarkupAttributeQuotes(t *testing.T) {
	text := `<tool_calls>
<invoke name='task'>
<parameter name=description string=true>审查cheng语言代码结构</parameter>
<parameter name='prompt' string='true'>探索 /Users/lbcheng/cheng-lang 项目的完整目录结构。</parameter>
<parameter name=subagent_type string=true>explore</parameter>
</invoke>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"task"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 task call, got %#v", calls)
	}
	if calls[0].Input["description"] != "审查cheng语言代码结构" {
		t.Fatalf("expected description from unquoted attribute, got %#v", calls[0].Input)
	}
	if calls[0].Input["prompt"] != "探索 /Users/lbcheng/cheng-lang 项目的完整目录结构。" {
		t.Fatalf("expected prompt from single-quoted attribute, got %#v", calls[0].Input)
	}
	if calls[0].Input["subagent_type"] != "explore" {
		t.Fatalf("expected subagent_type from unquoted attribute, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsToolUseFunctionParameterStyle(t *testing.T) {
	text := `<tool_use><function name='search_web'><parameter name=query type=string>test</parameter></function></tool_use>`
	calls := ParseToolCalls(text, []string{"search_web"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "search_web" {
		t.Fatalf("expected canonical tool name search_web, got %q", calls[0].Name)
	}
	if calls[0].Input["query"] != "test" {
		t.Fatalf("expected query argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsToolUseNameParametersStyle(t *testing.T) {
	text := `<tool_use><tool_name>write_file</tool_name><parameters>{"path":"/tmp/a.txt","content":"abc"}</parameters></tool_use>`
	calls := ParseToolCalls(text, []string{"write_file"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "write_file" {
		t.Fatalf("expected tool name write_file, got %q", calls[0].Name)
	}
	if calls[0].Input["path"] != "/tmp/a.txt" {
		t.Fatalf("expected path argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsToolUseFunctionNameParametersStyle(t *testing.T) {
	text := `<tool_use><function_name>write_file</function_name><parameters>{"path":"/tmp/b.txt","content":"xyz"}</parameters></tool_use>`
	calls := ParseToolCalls(text, []string{"write_file"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "write_file" {
		t.Fatalf("expected tool name write_file, got %q", calls[0].Name)
	}
	if calls[0].Input["content"] != "xyz" {
		t.Fatalf("expected content argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsToolUseToolNameBodyStyle(t *testing.T) {
	text := `<tool_use><tool_name>write_file</tool_name><path>/tmp/c.txt</path><content>hello</content></tool_use>`
	calls := ParseToolCalls(text, []string{"write_file"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "write_file" {
		t.Fatalf("expected tool name write_file, got %q", calls[0].Name)
	}
	if calls[0].Input["path"] != "/tmp/c.txt" {
		t.Fatalf("expected path argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsNestedToolTagStyle(t *testing.T) {
	text := `<tool_call><tool name="Bash"><command>pwd</command><description>show cwd</description></tool></tool_call>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected original tool name Bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsDirectToolElementsInMalformedWrapper(t *testing.T) {
	text := `<tool_calls>
  <tool_call>
    <tool name="Read">
      <parameter name="file_path" type="string">/Users/lbcheng/cheng-lang/README.md</parameter>
    </tool>
    <parameter name="limit" type="number">200</parameter>
  </tool>
  <tool name="Read">
    <parameter name="file_path" type="string">/Users/lbcheng/cheng-lang/task_plan.md</parameter>
    <parameter name="limit" type="number">200</parameter>
  </tool>
</tool_calls>`
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 direct tool calls, got %#v", calls)
	}
	if calls[0].Name != "Read" || calls[0].Input["file_path"] != "/Users/lbcheng/cheng-lang/README.md" {
		t.Fatalf("unexpected first call: %#v", calls[0])
	}
	if calls[1].Name != "Read" || calls[1].Input["file_path"] != "/Users/lbcheng/cheng-lang/task_plan.md" || calls[1].Input["limit"] != float64(200) {
		t.Fatalf("unexpected second call: %#v", calls[1])
	}
}

func TestParseToolCallsSupportsAntmlFunctionAttributeWithParametersTag(t *testing.T) {
	text := `<antml:function_calls><antml:function_call id="x" function="Bash"><antml:parameters>{"command":"pwd"}</antml:parameters></antml:function_call></antml:function_calls>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %#v", calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected original tool name Bash, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("expected command argument, got %#v", calls[0].Input)
	}
}

func TestParseToolCallsSupportsMultipleAntmlFunctionCalls(t *testing.T) {
	text := `<antml:function_calls><antml:function_call id="1" function="Bash"><antml:parameters>{"command":"pwd"}</antml:parameters></antml:function_call><antml:function_call id="2" function="Read"><antml:parameters>{"file_path":"README.md"}</antml:parameters></antml:function_call></antml:function_calls>`
	calls := ParseToolCalls(text, []string{"bash", "read"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %#v", calls)
	}
	if calls[0].Name != "Bash" || calls[1].Name != "Read" {
		t.Fatalf("expected original names [Bash Read], got %#v", calls)
	}
}

func TestParseToolCallsDoesNotAcceptMismatchedMarkupTags(t *testing.T) {
	text := `<tool_call><name>read_file</function><arguments>{"path":"README.md"}</arguments></tool_call>`
	calls := ParseToolCalls(text, []string{"read_file"})
	if len(calls) != 0 {
		t.Fatalf("expected mismatched tags to be rejected, got %#v", calls)
	}
}

func TestParseToolCallsDoesNotTreatParametersFunctionNameAsToolName(t *testing.T) {
	text := `<tool_call><parameters><function_name>data_only</function_name><path>README.md</path></parameters></tool_call>`
	calls := ParseToolCalls(text, []string{"read_file"})
	if len(calls) != 0 {
		t.Fatalf("expected no tool call when function_name appears only under parameters, got %#v", calls)
	}
}

func TestRepairInvalidJSONBackslashes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"path": "C:\Users\name"}`, `{"path": "C:\\Users\name"}`},
		{`{"cmd": "cd D:\git_codes"}`, `{"cmd": "cd D:\\git_codes"}`},
		{`{"text": "line1\nline2"}`, `{"text": "line1\nline2"}`},
		{`{"path": "D:\\back\\slash"}`, `{"path": "D:\\back\\slash"}`},
		{`{"unicode": "\u2705"}`, `{"unicode": "\u2705"}`},
		{`{"invalid_u": "\u123"}`, `{"invalid_u": "\\u123"}`},
	}

	for _, tt := range tests {
		got := repairInvalidJSONBackslashes(tt.input)
		if got != tt.expected {
			t.Errorf("repairInvalidJSONBackslashes(%s) = %s; want %s", tt.input, got, tt.expected)
		}
	}
}

func TestRepairLooseJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{tool_calls: [{"name": "search", "input": {"q": "go"}}]}`, `{"tool_calls": [{"name": "search", "input": {"q": "go"}}]}`},
		{`{name: "search", input: {q: "go"}}`, `{"name": "search", "input": {"q": "go"}}`},
	}

	for _, tt := range tests {
		got := RepairLooseJSON(tt.input)
		if got != tt.expected {
			t.Errorf("RepairLooseJSON(%s) = %s; want %s", tt.input, got, tt.expected)
		}
	}
}

func TestParseToolCallInputRepairsControlCharsInPath(t *testing.T) {
	in := `{"path":"D:\tmp\new\readme.txt","content":"line1\nline2"}`
	parsed := parseToolCallInput(in)

	path, ok := parsed["path"].(string)
	if !ok {
		t.Fatalf("expected path string in parsed input, got %#v", parsed["path"])
	}
	if path != `D:\tmp\new\readme.txt` {
		t.Fatalf("expected repaired windows path, got %q", path)
	}

	content, ok := parsed["content"].(string)
	if !ok {
		t.Fatalf("expected content string in parsed input, got %#v", parsed["content"])
	}
	if content != "line1\nline2" {
		t.Fatalf("expected non-path field to keep decoded escapes, got %q", content)
	}
}

func TestRepairLooseJSONWithNestedObjects(t *testing.T) {
	// 测试嵌套对象的修复：DeepSeek 幻觉输出，每个元素内部包含嵌套 {}
	// 注意：正则只支持单层嵌套，不支持更深层次的嵌套
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// 1. 单层嵌套对象（核心修复目标）
		{
			name:     "单层嵌套 - 2个元素",
			input:    `"todos": {"content": "研究算法", "input": {"q": "8 queens"}}, {"content": "实现", "input": {"path": "queens.py"}}`,
			expected: `"todos": [{"content": "研究算法", "input": {"q": "8 queens"}}, {"content": "实现", "input": {"path": "queens.py"}}]`,
		},
		// 2. 3个单层嵌套对象
		{
			name:     "3个单层嵌套对象",
			input:    `"items": {"a": {"x":1}}, {"b": {"y":2}}, {"c": {"z":3}}`,
			expected: `"items": [{"a": {"x":1}}, {"b": {"y":2}}, {"c": {"z":3}}]`,
		},
		// 3. 混合嵌套：有些字段是对象，有些是原始值
		{
			name:     "混合嵌套 - 对象和原始值混合",
			input:    `"items": {"name": "test", "config": {"timeout": 30}}, {"name": "test2", "config": {"timeout": 60}}`,
			expected: `"items": [{"name": "test", "config": {"timeout": 30}}, {"name": "test2", "config": {"timeout": 60}}]`,
		},
		// 4. 4个嵌套对象（边界测试）
		{
			name:     "4个嵌套对象",
			input:    `"todos": {"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}`,
			expected: `"todos": [{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}]`,
		},
		// 5. DeepSeek 典型幻觉：无空格逗号分隔
		{
			name:     "无空格逗号分隔",
			input:    `"results": {"name": "a"}, {"name": "b"}, {"name": "c"}`,
			expected: `"results": [{"name": "a"}, {"name": "b"}, {"name": "c"}]`,
		},
		// 6. 嵌套数组（数组在对象内，不是深层嵌套）
		{
			name:     "对象内包含数组",
			input:    `"data": {"items": [1,2,3]}, {"items": [4,5,6]}`,
			expected: `"data": [{"items": [1,2,3]}, {"items": [4,5,6]}]`,
		},
		// 7. 真实的 DeepSeek 8皇后问题输出
		{
			name:     "DeepSeek 8皇后真实输出",
			input:    `"todos": {"content": "研究8皇后算法", "status": "pending"}, {"content": "实现Python脚本", "status": "pending"}, {"content": "验证结果", "status": "pending"}`,
			expected: `"todos": [{"content": "研究8皇后算法", "status": "pending"}, {"content": "实现Python脚本", "status": "pending"}, {"content": "验证结果", "status": "pending"}]`,
		},
		// 8. 简单无嵌套对象（回归测试）
		{
			name:     "简单无嵌套对象",
			input:    `"items": {"a": 1}, {"b": 2}`,
			expected: `"items": [{"a": 1}, {"b": 2}]`,
		},
		// 9. 更复杂的单层嵌套
		{
			name:     "复杂单层嵌套",
			input:    `"functions": {"name": "execute", "input": {"command": "ls"}}, {"name": "read", "input": {"file": "a.txt"}}`,
			expected: `"functions": [{"name": "execute", "input": {"command": "ls"}}, {"name": "read", "input": {"file": "a.txt"}}]`,
		},
		// 10. 5个嵌套对象
		{
			name:     "5个嵌套对象",
			input:    `"tasks": {"id":1}, {"id":2}, {"id":3}, {"id":4}, {"id":5}`,
			expected: `"tasks": [{"id":1}, {"id":2}, {"id":3}, {"id":4}, {"id":5}]`,
		},
	}

	for _, tt := range tests {
		got := RepairLooseJSON(tt.input)
		if got != tt.expected {
			t.Errorf("[%s] RepairLooseJSON with nested objects:\n  input:    %s\n  got:      %s\n  expected: %s", tt.name, tt.input, got, tt.expected)
		}
	}
}

func TestParseToolCallsUnescapesHTMLEntityArguments(t *testing.T) {
	text := `<tool_call><tool_name>Bash</tool_name><parameters>{"command":"echo a &gt; out.txt"}</parameters></tool_call>`
	calls := ParseToolCalls(text, []string{"bash"})
	if len(calls) != 1 {
		t.Fatalf("expected one call, got %#v", calls)
	}
	cmd, _ := calls[0].Input["command"].(string)
	if cmd != "echo a > out.txt" {
		t.Fatalf("expected html entities to be unescaped in command, got %q", cmd)
	}
}

func TestParseToolCallsIgnoresXMLInsideFencedCodeBlock(t *testing.T) {
	text := "Here is an example:\n```xml\n<tool_call><tool_name>read_file</tool_name><parameters>{\"path\":\"README.md\"}</parameters></tool_call>\n```\nDo not execute it."
	res := ParseToolCallsDetailed(text, []string{"read_file"})
	if len(res.Calls) != 0 {
		t.Fatalf("expected no parsed calls for fenced example, got %#v", res.Calls)
	}
}

func TestParseToolCallsParsesOnlyNonFencedXMLToolCall(t *testing.T) {
	text := "```xml\n<tool_call><tool_name>read_file</tool_name><parameters>{\"path\":\"README.md\"}</parameters></tool_call>\n```\n<tool_call><tool_name>search</tool_name><parameters>{\"q\":\"golang\"}</parameters></tool_call>"
	res := ParseToolCallsDetailed(text, []string{"read_file", "search"})
	if len(res.Calls) != 1 {
		t.Fatalf("expected exactly one parsed call outside fence, got %#v", res.Calls)
	}
	if res.Calls[0].Name != "search" {
		t.Fatalf("expected non-fenced tool call to be parsed, got %#v", res.Calls[0])
	}
}

func TestParseToolCallsParsesAfterFourBacktickFence(t *testing.T) {
	text := "````markdown\n```xml\n<tool_call><tool_name>read_file</tool_name><parameters>{\"path\":\"README.md\"}</parameters></tool_call>\n```\n````\n<tool_call><tool_name>search</tool_name><parameters>{\"q\":\"outside\"}</parameters></tool_call>"
	res := ParseToolCallsDetailed(text, []string{"read_file", "search"})
	if len(res.Calls) != 1 {
		t.Fatalf("expected exactly one parsed call outside four-backtick fence, got %#v", res.Calls)
	}
	if res.Calls[0].Name != "search" {
		t.Fatalf("expected non-fenced tool call to be parsed, got %#v", res.Calls[0])
	}
}
