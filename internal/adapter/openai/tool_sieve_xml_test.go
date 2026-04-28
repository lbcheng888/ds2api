package openai

import (
	"ds2api/internal/toolcall"
	"fmt"
	"strings"
	"testing"
)

func TestProcessToolSieveInterceptsXMLToolCallWithoutLeak(t *testing.T) {
	var state toolStreamSieveState
	// Simulate a model producing XML tool call output chunk by chunk.
	chunks := []string{
		"<tool_calls>\n",
		"  <tool_call>\n",
		"    <tool_name>read_file</tool_name>\n",
		`    <parameters>{"path":"README.MD"}</parameters>` + "\n",
		"  </tool_call>\n",
		"</tool_calls>",
	}
	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"read_file"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(textContent, "<tool_call") {
		t.Fatalf("XML tool call content leaked to text: %q", textContent)
	}
	if strings.Contains(textContent, "read_file") {
		t.Fatalf("tool name leaked to text: %q", textContent)
	}
	if toolCalls == 0 {
		t.Fatal("expected tool calls to be extracted, got none")
	}
}

func TestProcessToolSieveHandlesLongXMLToolCall(t *testing.T) {
	var state toolStreamSieveState
	const toolName = "write_to_file"
	payload := strings.Repeat("x", 4096)
	splitAt := len(payload) / 2
	chunks := []string{
		"<tool_calls>\n  <tool_call>\n    <tool_name>" + toolName + "</tool_name>\n    <parameters>\n      <content><![CDATA[",
		payload[:splitAt],
		payload[splitAt:],
		"]]></content>\n    </parameters>\n  </tool_call>\n</tool_calls>",
	}

	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{toolName})...)
	}
	events = append(events, flushToolSieve(&state, []string{toolName})...)

	var textContent strings.Builder
	toolCalls := 0
	var gotPayload any
	for _, evt := range events {
		if evt.Content != "" {
			textContent.WriteString(evt.Content)
		}
		if len(evt.ToolCalls) > 0 && gotPayload == nil {
			gotPayload = evt.ToolCalls[0].Input["content"]
		}
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 1 {
		t.Fatalf("expected one long XML tool call, got %d events=%#v", toolCalls, events)
	}
	if textContent.Len() != 0 {
		t.Fatalf("expected no leaked text for long XML tool call, got %q", textContent.String())
	}
	got, _ := gotPayload.(string)
	if got != payload {
		t.Fatalf("expected long XML payload to survive intact, got len=%d want=%d", len(got), len(payload))
	}
}

func TestProcessToolSieveInterceptsNestedFunctionBodyAgentCall(t *testing.T) {
	var state toolStreamSieveState
	chunks := []string{
		"<tool_calls>\n <tool_calls>\n   <tool_call id=\"agent_linkerless\">",
		`Agent({"description":"Linkerless + DOD + hotspots","subagent_type":"Explore","prompt":"Search concrete evidence."})`,
		"</tool_call>\n </tool_calls>\n</tool_calls>",
	}
	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunkWithMeta(&state, c, []string{"Agent"}, true)...)
	}
	events = append(events, flushToolSieveWithMeta(&state, []string{"Agent"}, true)...)

	var textContent strings.Builder
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		calls = append(calls, evt.ToolCalls...)
	}
	if text := textContent.String(); strings.Contains(text, "Agent(") || strings.Contains(text, "<tool_call") {
		t.Fatalf("nested function tool call leaked to text: %q", text)
	}
	if len(calls) != 1 || calls[0].Name != "Agent" {
		t.Fatalf("expected one Agent call, got %#v", calls)
	}
	if calls[0].Input["prompt"] != "Search concrete evidence." {
		t.Fatalf("expected prompt argument, got %#v", calls[0].Input)
	}
}

func TestProcessToolSieveXMLWithLeadingText(t *testing.T) {
	var state toolStreamSieveState
	// Model outputs some prose then an XML tool call.
	chunks := []string{
		"Let me check the file.\n",
		"<tool_calls>\n  <tool_call>\n    <tool_name>read_file</tool_name>\n",
		`    <parameters>{"path":"go.mod"}</parameters>` + "\n  </tool_call>\n</tool_calls>",
	}
	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"read_file"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
		toolCalls += len(evt.ToolCalls)
	}

	// Leading text should be emitted.
	if !strings.Contains(textContent, "Let me check the file.") {
		t.Fatalf("expected leading text to be emitted, got %q", textContent)
	}
	// The XML itself should NOT leak.
	if strings.Contains(textContent, "<tool_call") {
		t.Fatalf("XML tool call content leaked to text: %q", textContent)
	}
	if toolCalls == 0 {
		t.Fatal("expected tool calls to be extracted, got none")
	}
}

func TestProcessToolSieveInterceptsVisibleJSONToolArrayWithoutLeak(t *testing.T) {
	var state toolStreamSieveState
	chunk := `Let me read the next slices.
[
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
	events := processToolSieveChunk(&state, chunk, []string{"Read"})
	events = append(events, flushToolSieve(&state, []string{"Read"})...)

	var textContent strings.Builder
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		calls = append(calls, evt.ToolCalls...)
	}
	if !strings.Contains(textContent.String(), "Let me read the next slices.") {
		t.Fatalf("expected leading text to be preserved, got %q", textContent.String())
	}
	if strings.Contains(textContent.String(), `"tool"`) || strings.Contains(textContent.String(), "file_path") {
		t.Fatalf("visible JSON tool call leaked to text: %q", textContent.String())
	}
	if len(calls) != 2 || calls[0].Name != "Read" || calls[1].Name != "Read" {
		t.Fatalf("expected two Read calls, got %#v", calls)
	}
	if fmt.Sprint(calls[0].Input["offset"]) != "345" || fmt.Sprint(calls[1].Input["offset"]) != "773" {
		t.Fatalf("expected offsets to be preserved, got %#v", calls)
	}
}

func TestProcessToolSieveInterceptsChunkedVisibleJSONToolArray(t *testing.T) {
	var state toolStreamSieveState
	chunks := []string{
		"Reading next.\n",
		"[\n",
		"  {\"tool\":\"Read\",",
		"\"arguments\":{\"file_path\":\"/tmp/a.cheng\",",
		"\"offset\":345,\"limit\":65}}",
		"]\nDone",
	}
	var events []toolStreamEvent
	for _, chunk := range chunks {
		events = append(events, processToolSieveChunk(&state, chunk, []string{"Read"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"Read"})...)

	var textContent strings.Builder
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		calls = append(calls, evt.ToolCalls...)
	}
	if strings.Contains(textContent.String(), `"tool"`) || strings.Contains(textContent.String(), "/tmp/a.cheng") {
		t.Fatalf("chunked visible JSON tool call leaked to text: %q", textContent.String())
	}
	if !strings.Contains(textContent.String(), "Reading next.") || !strings.Contains(textContent.String(), "Done") {
		t.Fatalf("expected surrounding text to be preserved, got %q", textContent.String())
	}
	if len(calls) != 1 || calls[0].Name != "Read" || calls[0].Input["file_path"] != "/tmp/a.cheng" {
		t.Fatalf("expected one chunked Read call, got %#v", calls)
	}
}

func TestProcessToolSieveInterceptsVisibleJSONToolObjectSequenceWithoutLeak(t *testing.T) {
	var state toolStreamSieveState
	chunk := `Let me read the next slices.
{
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
	events := processToolSieveChunk(&state, chunk, []string{"Read"})
	events = append(events, flushToolSieve(&state, []string{"Read"})...)

	var textContent strings.Builder
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		calls = append(calls, evt.ToolCalls...)
	}
	if !strings.Contains(textContent.String(), "Let me read the next slices.") {
		t.Fatalf("expected leading text to be preserved, got %q", textContent.String())
	}
	if strings.Contains(textContent.String(), `"tool"`) || strings.Contains(textContent.String(), "file_path") {
		t.Fatalf("visible JSON object tool call leaked to text: %q", textContent.String())
	}
	if len(calls) != 2 || calls[0].Name != "Read" || calls[1].Name != "Read" {
		t.Fatalf("expected two Read calls, got %#v", calls)
	}
	if fmt.Sprint(calls[0].Input["offset"]) != "345" || fmt.Sprint(calls[1].Input["offset"]) != "773" {
		t.Fatalf("expected offsets to be preserved, got %#v", calls)
	}
}

func TestProcessToolSieveInterceptsLooseVisibleJSONBashQuotes(t *testing.T) {
	var state toolStreamSieveState
	chunk := `Let me inspect files.
{
  "tool": "Bash",
  "arguments": {
    "command": "cd /Users/lbcheng/cheng-lang && git ls-files | head -80 && echo "---" && git ls-files | wc -l",
    "description": "List tracked files and count"
  }
}`
	events := processToolSieveChunk(&state, chunk, []string{"Bash"})
	events = append(events, flushToolSieve(&state, []string{"Bash"})...)

	var textContent strings.Builder
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		calls = append(calls, evt.ToolCalls...)
	}
	if strings.Contains(textContent.String(), `"tool"`) || strings.Contains(textContent.String(), "git ls-files") {
		t.Fatalf("loose visible JSON Bash call leaked to text: %q", textContent.String())
	}
	if len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected one Bash call, got %#v", calls)
	}
	if !strings.Contains(fmt.Sprint(calls[0].Input["command"]), `echo "---"`) {
		t.Fatalf("expected command quotes preserved, got %#v", calls[0])
	}
}

func TestProcessToolSieveInterceptsChunkedVisibleJSONToolObjectSequence(t *testing.T) {
	var state toolStreamSieveState
	chunks := []string{
		"Reading next.\n",
		"{\"tool\":\"Read\",",
		"\"arguments\":{\"file_path\":\"/tmp/a.cheng\",",
		"\"offset\":345,\"limit\":65}}\n",
		"{\"tool\":\"Read\",\"arguments\":{\"file_path\":\"/tmp/b.cheng\",",
		"\"offset\":773,\"limit\":30}}\nDone",
	}
	var events []toolStreamEvent
	for _, chunk := range chunks {
		events = append(events, processToolSieveChunk(&state, chunk, []string{"Read"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"Read"})...)

	var textContent strings.Builder
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		calls = append(calls, evt.ToolCalls...)
	}
	if strings.Contains(textContent.String(), `"tool"`) || strings.Contains(textContent.String(), "/tmp/a.cheng") {
		t.Fatalf("chunked visible JSON object tool call leaked to text: %q", textContent.String())
	}
	if !strings.Contains(textContent.String(), "Reading next.") || !strings.Contains(textContent.String(), "Done") {
		t.Fatalf("expected surrounding text to be preserved, got %q", textContent.String())
	}
	if len(calls) != 2 || calls[0].Input["file_path"] != "/tmp/a.cheng" || calls[1].Input["file_path"] != "/tmp/b.cheng" {
		t.Fatalf("expected two chunked Read calls, got %#v", calls)
	}
}

func TestProcessToolSievePassesThroughFencedVisibleJSONToolArray(t *testing.T) {
	var state toolStreamSieveState
	input := "Example:\n```json\n[{\"tool\":\"Read\",\"arguments\":{\"file_path\":\"/tmp/a.cheng\"}}]\n```\nDone."
	events := processToolSieveChunk(&state, input, []string{"Read"})
	events = append(events, flushToolSieve(&state, []string{"Read"})...)

	var textContent strings.Builder
	var toolCalls int
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 0 {
		t.Fatalf("expected fenced JSON tool example to stay text, got %d calls events=%#v", toolCalls, events)
	}
	if textContent.String() != input {
		t.Fatalf("expected fenced JSON to pass through unchanged, got %q", textContent.String())
	}
}

func TestProcessToolSievePassesThroughNonToolXMLBlock(t *testing.T) {
	var state toolStreamSieveState
	chunk := `<tool_call><title>示例 XML</title><body>plain text xml payload</body></tool_call>`
	events := processToolSieveChunk(&state, chunk, []string{"read_file"})
	events = append(events, flushToolSieve(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 0 {
		t.Fatalf("expected no tool calls for plain XML payload, got %d events=%#v", toolCalls, events)
	}
	if textContent.String() != chunk {
		t.Fatalf("expected XML payload to pass through unchanged, got %q", textContent.String())
	}
}

func TestProcessToolSieveNonToolXMLKeepsSuffixForToolParsing(t *testing.T) {
	var state toolStreamSieveState
	chunk := `<tool_call><title>plain xml</title></tool_call><invoke name="read_file"><parameters>{"path":"README.MD"}</parameters></invoke>`
	events := processToolSieveChunk(&state, chunk, []string{"read_file"})
	events = append(events, flushToolSieve(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if !strings.Contains(textContent.String(), `<tool_call><title>plain xml</title></tool_call>`) {
		t.Fatalf("expected leading non-tool XML to be preserved, got %q", textContent.String())
	}
	if strings.Contains(textContent.String(), `<invoke name="read_file">`) {
		t.Fatalf("expected invoke tool XML to be intercepted, got %q", textContent.String())
	}
	if toolCalls != 1 {
		t.Fatalf("expected exactly one parsed tool call from suffix, got %d events=%#v", toolCalls, events)
	}
}

func TestProcessToolSieveRepairsMissingToolCallCloseBeforeWrapperClose(t *testing.T) {
	var state toolStreamSieveState
	chunk := `<tool_calls>
<tool_call>
<tool_name>bash</tool_name>
<parameters><command>pwd</command><description>Show current directory</description></parameters>
</tool_calls></tool_calls>`
	events := processToolSieveChunk(&state, chunk, []string{"bash"})
	events = append(events, flushToolSieve(&state, []string{"bash"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if textContent.String() != "" {
		t.Fatalf("expected malformed tool XML to be intercepted, got content %q", textContent.String())
	}
	if toolCalls != 1 {
		t.Fatalf("expected one repaired tool call, got %d events=%#v", toolCalls, events)
	}
}

func TestProcessToolSieveRepairsMissingWrapperAngleAndLooseDirectTools(t *testing.T) {
	var state toolStreamSieveState
	chunk := `Let me inspect first.tool_calls>
  <tool_call>
    <exec_command>
      <parameters><cmd>ls -la /Users/lbcheng/cheng-lang/</parameters><justification>List root</justification></parameters>
    </tool_call>
</tool_calls>`
	events := processToolSieveChunk(&state, chunk, []string{"exec_command"})
	events = append(events, flushToolSieve(&state, []string{"exec_command"})...)

	var textContent strings.Builder
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		calls = append(calls, evt.ToolCalls...)
	}
	if !strings.Contains(textContent.String(), "Let me inspect first.") {
		t.Fatalf("expected prefix text to be preserved, got %q", textContent.String())
	}
	if strings.Contains(textContent.String(), "tool_calls>") || strings.Contains(textContent.String(), "<tool_call") {
		t.Fatalf("expected malformed tool XML to be intercepted, got %q", textContent.String())
	}
	if len(calls) != 1 || calls[0].Name != "exec_command" || calls[0].Input["cmd"] != "ls -la /Users/lbcheng/cheng-lang/" {
		t.Fatalf("expected repaired exec_command call, got %#v", calls)
	}
}

func TestProcessToolSieveInfersNamelessReadToolCall(t *testing.T) {
	var state toolStreamSieveState
	chunk := `<tool_calls>
<tool_call>
<parameter name="file_path">/Users/lbcheng/cheng-lang/src/core/lang/parser.cheng</parameter>
</tool_calls>`
	events := processToolSieveChunk(&state, chunk, []string{"Read", "Bash"})
	events = append(events, flushToolSieve(&state, []string{"Read", "Bash"})...)

	var textContent strings.Builder
	var calls []string
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		for _, call := range evt.ToolCalls {
			calls = append(calls, call.Name)
			if call.Input["file_path"] != "/Users/lbcheng/cheng-lang/src/core/lang/parser.cheng" {
				t.Fatalf("expected file_path argument, got %#v", call.Input)
			}
		}
	}
	if textContent.String() != "" {
		t.Fatalf("expected nameless tool XML to be intercepted, got content %q", textContent.String())
	}
	if len(calls) != 1 || calls[0] != "Read" {
		t.Fatalf("expected one inferred Read call, got %#v events=%#v", calls, events)
	}
}

func TestProcessToolSieveExtractsDirectToolElementsInMalformedWrapper(t *testing.T) {
	var state toolStreamSieveState
	chunk := `<tool_calls>
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
	events := processToolSieveChunk(&state, chunk, []string{"Read"})
	events = append(events, flushToolSieve(&state, []string{"Read"})...)

	var textContent strings.Builder
	var paths []string
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		for _, call := range evt.ToolCalls {
			paths = append(paths, call.Input["file_path"].(string))
		}
	}
	if textContent.String() != "" {
		t.Fatalf("expected malformed direct tool XML to be intercepted, got content %q", textContent.String())
	}
	if len(paths) != 2 || paths[0] != "/Users/lbcheng/cheng-lang/README.md" || paths[1] != "/Users/lbcheng/cheng-lang/task_plan.md" {
		t.Fatalf("expected two Read calls, got %#v events=%#v", paths, events)
	}
}

func TestProcessToolSievePassesThroughMalformedExecutableXMLBlock(t *testing.T) {
	var state toolStreamSieveState
	chunk := `<tool_call><parameters>{"path":"README.md"}</parameters></tool_call>`
	events := processToolSieveChunk(&state, chunk, []string{"read_file"})
	events = append(events, flushToolSieve(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected malformed executable-looking XML to stay text, got %d events=%#v", toolCalls, events)
	}
	if textContent.String() != chunk {
		t.Fatalf("expected malformed executable-looking XML to pass through unchanged, got %q", textContent.String())
	}
}

func TestProcessToolSieveBlocksMetaAgentToolCall(t *testing.T) {
	var state toolStreamSieveState
	chunk := `<tool_calls>
  <tool_call>
    <tool_name>Agent</tool_name>
    <parameters>
      <description>Explore</description>
      <prompt>Explore the repository</prompt>
      <subagent_type>general</subagent_type>
    </parameters>
  </tool_call>
</tool_calls>`
	events := processToolSieveChunk(&state, chunk, []string{"Agent", "read"})
	events = append(events, flushToolSieve(&state, []string{"Agent", "read"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 0 {
		t.Fatalf("expected meta agent call to be blocked, got %d events=%#v", toolCalls, events)
	}
	if strings.Contains(textContent.String(), "<tool_call") {
		t.Fatalf("expected blocked meta agent XML not to leak, got %q", textContent.String())
	}
	if !strings.Contains(textContent.String(), "Agent/subagent tools are disabled") {
		t.Fatalf("expected visible blocked-tool message, got %q", textContent.String())
	}
}

func TestProcessToolSievePromotesOrphanAgentParameterGroups(t *testing.T) {
	var state toolStreamSieveState
	chunk := strings.Join([]string{
		"先并行探查各维度。\n\n",
		`<parameter name="description">Assess Linkerless + DOD implementation</parameter>`,
		"\n",
		`<parameter name="prompt">Search the codebase and report concrete paths.</parameter>`,
		"\nExplore\n\n",
		`<parameter name="description">Assess function-level parallelism</parameter>`,
		"\n",
		`<parameter name="prompt">Check worker scheduling.</parameter>`,
		"\ncode-reviewer",
	}, "")

	events := processToolSieveChunkWithMeta(&state, chunk, []string{"Agent", "Read"}, true)
	events = append(events, flushToolSieveWithMeta(&state, []string{"Agent", "Read"}, true)...)

	var textContent strings.Builder
	toolCalls := []toolcall.ParsedToolCall{}
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls = append(toolCalls, evt.ToolCalls...)
	}
	if strings.Contains(textContent.String(), "<parameter") {
		t.Fatalf("expected orphan parameter XML not to leak, got %q", textContent.String())
	}
	if len(toolCalls) != 2 {
		t.Fatalf("expected two Agent tool calls, got %#v events=%#v", toolCalls, events)
	}
	if toolCalls[0].Name != "Agent" || toolCalls[0].Input["subagent_type"] != "Explore" {
		t.Fatalf("unexpected first Agent call: %#v", toolCalls[0])
	}
	if toolCalls[1].Input["subagent_type"] != "code-reviewer" {
		t.Fatalf("unexpected second Agent call: %#v", toolCalls[1])
	}
}

func TestProcessToolSievePassesThroughFencedXMLToolCallExamples(t *testing.T) {
	var state toolStreamSieveState
	input := strings.Join([]string{
		"Before first example.\n```",
		"xml\n<tool_call><tool_name>read_file</tool_name><parameters>{\"path\":\"README.md\"}</parameters></tool_call>\n```\n",
		"Between examples.\n```xml\n",
		"<tool_call><tool_name>search</tool_name><parameters>{\"q\":\"golang\"}</parameters></tool_call>\n",
		"```\nAfter examples.",
	}, "")

	chunks := []string{
		"Before first example.\n```",
		"xml\n<tool_call><tool_name>read_file</tool_name><parameters>{\"path\":\"README.md\"}</parameters></tool_call>\n```\n",
		"Between examples.\n```xml\n",
		"<tool_call><tool_name>search</tool_name><parameters>{\"q\":\"golang\"}</parameters></tool_call>\n",
		"```\nAfter examples.",
	}

	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"read_file", "search"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"read_file", "search"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		if evt.Content != "" {
			textContent.WriteString(evt.Content)
		}
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected fenced XML examples to stay text, got %d tool calls events=%#v", toolCalls, events)
	}
	if textContent.String() != input {
		t.Fatalf("expected fenced XML examples to pass through unchanged, got %q", textContent.String())
	}
}

func TestProcessToolSieveKeepsPartialXMLTagInsideFencedExample(t *testing.T) {
	var state toolStreamSieveState
	input := strings.Join([]string{
		"Example:\n```xml\n<tool_ca",
		"ll><tool_name>read_file</tool_name><parameters>{\"path\":\"README.md\"}</parameters></tool_call>\n```\n",
		"Done.",
	}, "")

	chunks := []string{
		"Example:\n```xml\n<tool_ca",
		"ll><tool_name>read_file</tool_name><parameters>{\"path\":\"README.md\"}</parameters></tool_call>\n```\n",
		"Done.",
	}

	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		if evt.Content != "" {
			textContent.WriteString(evt.Content)
		}
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected partial fenced XML to stay text, got %d tool calls events=%#v", toolCalls, events)
	}
	if textContent.String() != input {
		t.Fatalf("expected partial fenced XML to pass through unchanged, got %q", textContent.String())
	}
}

func TestProcessToolSievePartialXMLTagHeldBack(t *testing.T) {
	var state toolStreamSieveState
	// Chunk ends with a partial XML tool tag.
	events := processToolSieveChunk(&state, "Hello <tool_ca", []string{"read_file"})

	var textContent string
	for _, evt := range events {
		textContent += evt.Content
	}

	// "Hello " should be emitted, but "<tool_ca" should be held back.
	if strings.Contains(textContent, "<tool_ca") {
		t.Fatalf("partial XML tag should not be emitted, got %q", textContent)
	}
	if !strings.Contains(textContent, "Hello") {
		t.Fatalf("expected 'Hello' text to be emitted, got %q", textContent)
	}
}

func TestFindToolSegmentStartDetectsXMLToolCalls(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"tool_calls_tag", "some text <tool_calls>\n", 10},
		{"tool_call_tag", "prefix <tool_call>\n", 7},
		{"invoke_tag", "text <invoke name=\"foo\">body</invoke>", 5},
		{"xml_inside_code_fence", "```xml\n<tool_call><tool_name>read_file</tool_name></tool_call>\n```", -1},
		{"function_call_tag", "<function_call name=\"foo\">body</function_call>", 0},
		{"no_xml", "just plain text", -1},
		{"gemini_json_no_detect", `some text {"functionCall":{"name":"search"}}`, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findToolSegmentStart(nil, tc.input)
			if got != tc.want {
				t.Fatalf("findToolSegmentStart(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestFindPartialXMLToolTagStart(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"partial_tool_call", "Hello <tool_ca", 6},
		{"partial_invoke", "Prefix <inv", 7},
		{"partial_lt_only", "Text <", 5},
		{"complete_tag", "Text <tool_call>done", -1},
		{"no_lt", "plain text", -1},
		{"closed_lt", "a < b > c", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findPartialXMLToolTagStart(tc.input)
			if got != tc.want {
				t.Fatalf("findPartialXMLToolTagStart(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestHasOpenXMLToolTag(t *testing.T) {
	if !hasOpenXMLToolTag("<tool_call>\n<tool_name>foo</tool_name>") {
		t.Fatal("should detect open XML tool tag without closing tag")
	}
	if hasOpenXMLToolTag("<tool_call>\n<tool_name>foo</tool_name></tool_call>") {
		t.Fatal("should return false when closing tag is present")
	}
	if hasOpenXMLToolTag("plain text without any XML") {
		t.Fatal("should return false for plain text")
	}
}

// Test the EXACT scenario the user reports: token-by-token streaming where
// <tool_calls> tag arrives in small pieces.
func TestProcessToolSieveTokenByTokenXMLNoLeak(t *testing.T) {
	var state toolStreamSieveState
	// Simulate DeepSeek model generating tokens one at a time.
	chunks := []string{
		"<",
		"tool",
		"_calls",
		">\n",
		"  <",
		"tool",
		"_call",
		">\n",
		"    <",
		"tool",
		"_name",
		">",
		"read",
		"_file",
		"</",
		"tool",
		"_name",
		">\n",
		"    <",
		"parameters",
		">",
		`{"path"`,
		`: "README.MD"`,
		`}`,
		"</",
		"parameters",
		">\n",
		"  </",
		"tool",
		"_call",
		">\n",
		"</",
		"tool",
		"_calls",
		">",
	}
	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"read_file"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(textContent, "<tool_call") {
		t.Fatalf("XML tool call content leaked to text in token-by-token mode: %q", textContent)
	}
	if strings.Contains(textContent, "tool_calls>") {
		t.Fatalf("closing tag fragment leaked to text: %q", textContent)
	}
	if strings.Contains(textContent, "read_file") {
		t.Fatalf("tool name leaked to text: %q", textContent)
	}
	if toolCalls == 0 {
		t.Fatal("expected tool calls to be extracted, got none")
	}
}

// Test that flushToolSieve on incomplete XML fails the tool transaction.
func TestFlushToolSieveIncompleteXMLReturnsProtocolError(t *testing.T) {
	var state toolStreamSieveState
	// XML block starts but stream ends before completion.
	chunks := []string{
		"<tool_calls>\n",
		"  <tool_call>\n",
		"    <tool_name>read_file</tool_name>\n",
	}
	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"read_file"})...)
	}
	// Stream ends abruptly - flush should NOT dump raw XML.
	events = append(events, flushToolSieve(&state, []string{"read_file"})...)

	var textContent string
	var errorCode string
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
		if evt.ErrorCode != "" {
			errorCode = evt.ErrorCode
		}
	}

	if textContent != "" {
		t.Fatalf("incomplete XML leaked as text: %q", textContent)
	}
	if errorCode != "upstream_invalid_tool_call" {
		t.Fatalf("expected invalid tool call error, got %q", errorCode)
	}
}

// Test that the opening tag "<tool_calls>\n  " is NOT emitted as text content.
func TestOpeningXMLTagNotLeakedAsContent(t *testing.T) {
	var state toolStreamSieveState
	// First chunk is the opening tag - should be held, not emitted.
	evts1 := processToolSieveChunk(&state, "<tool_calls>\n  ", []string{"read_file"})
	for _, evt := range evts1 {
		if strings.Contains(evt.Content, "<tool_calls>") {
			t.Fatalf("opening tag leaked on first chunk: %q", evt.Content)
		}
	}

	// Remaining content arrives.
	evts2 := processToolSieveChunk(&state, "<tool_call>\n    <tool_name>read_file</tool_name>\n    <parameters>{\"path\":\"README.MD\"}</parameters>\n  </tool_call>\n</tool_calls>", []string{"read_file"})
	evts2 = append(evts2, flushToolSieve(&state, []string{"read_file"})...)

	var textContent string
	var toolCalls int
	allEvents := append(evts1, evts2...)
	for _, evt := range allEvents {
		if evt.Content != "" {
			textContent += evt.Content
		}
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(textContent, "<tool_call") {
		t.Fatalf("XML content leaked: %q", textContent)
	}
	if toolCalls == 0 {
		t.Fatal("expected tool calls to be extracted")
	}
}

func TestProcessToolSieveFallsBackToRawAttemptCompletion(t *testing.T) {
	var state toolStreamSieveState
	// Simulate an agent outputting attempt_completion XML tag.
	// If it does not parse as a tool call, it should fall back to raw text.
	chunks := []string{
		"Done with task.\n",
		"<attempt_completion>\n",
		"  <result>Here is the answer</result>\n",
		"</attempt_completion>",
	}
	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"attempt_completion"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"attempt_completion"})...)

	var textContent string
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
	}

	if !strings.Contains(textContent, "Done with task.\n") {
		t.Fatalf("expected leading text to be emitted, got %q", textContent)
	}

	if textContent != strings.Join(chunks, "") {
		t.Fatalf("expected agent XML to fall back to raw text, got %q", textContent)
	}
}
