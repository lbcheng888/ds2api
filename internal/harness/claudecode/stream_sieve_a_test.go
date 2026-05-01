package claudecode

import (
	"strings"
	"testing"
)

func TestStreamSieveExpandXMLToolCallStreaming(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read", "Bash"}
	chunks := []string{
		"prefix text\n",
		"<tool_calls><invoke name=\"Read\">",
		"<parameter name=\"file_path\">/tmp/a.txt</parameter>",
		"</invoke></tool_calls>\n",
		"suffix text",
	}
	var allEvents []StreamSieveEvent
	for _, ch := range chunks {
		allEvents = append(allEvents, ProcessStreamSieveChunk(state, ch, toolNames)...)
	}
	allEvents = append(allEvents, FlushStreamSieve(state, toolNames)...)
	contentCount := 0
	callCount := 0
	for _, e := range allEvents {
		if e.Content != "" {
			contentCount++
		}
		callCount += len(e.ToolCalls)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 tool call, got %d: %#v", callCount, allEvents)
	}
	if contentCount < 2 {
		t.Fatalf("expected at least 2 content events, got %d", contentCount)
	}
}

func TestStreamSieveExpandJSONToolCallStreaming(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read", "Bash"}
	// JSON visible tool calls are NOT parsed as tool calls by the stream sieve.
	// They pass through as visible text content.
	chunks := []string{
		"before ",
		`{"tool":"Read","arguments":{"file_path":"`,
		`/tmp/b.txt"}}`,
		" after",
	}
	var allEvents []StreamSieveEvent
	for _, ch := range chunks {
		allEvents = append(allEvents, ProcessStreamSieveChunk(state, ch, toolNames)...)
	}
	allEvents = append(allEvents, FlushStreamSieve(state, toolNames)...)
	callCount := 0
	hasBefore := false
	hasAfter := false
	hasJSON := false
	for _, e := range allEvents {
		callCount += len(e.ToolCalls)
		if strings.Contains(e.Content, "before") {
			hasBefore = true
		}
		if strings.Contains(e.Content, "after") {
			hasAfter = true
		}
		if strings.Contains(e.Content, `"arguments"`) {
			hasJSON = true
		}
	}
	// JSON is NOT extracted as tool calls - it passes through as text
	if callCount != 0 {
		t.Fatalf("expected 0 tool calls (JSON is text), got %d", callCount)
	}
	if !hasBefore {
		t.Fatal("expected 'before' text")
	}
	if !hasAfter {
		t.Fatal("expected 'after' text")
	}
	if !hasJSON {
		t.Fatal("expected JSON content to pass through as text")
	}
}

func TestStreamSieveExpandOrphanAgentParameter(t *testing.T) {
	toolNames := []string{"Agent", "Read"}
	text := "prose\n<parameter name=\"description\">desc here</parameter>\n<parameter name=\"prompt\">prompt here</parameter>\nAgent"

	state := &StreamSieveState{}
	events := ProcessStreamSieveChunk(state, text, toolNames)
	events = append(events, FlushStreamSieve(state, toolNames)...)
	callCount := 0
	for _, e := range events {
		callCount += len(e.ToolCalls)
	}
	if callCount > 0 {
		t.Fatalf("expected no tool calls without allowMetaAgentTools, got %d", callCount)
	}

	state2 := &StreamSieveState{}
	events2 := ProcessStreamSieveChunkWithMeta(state2, text, toolNames, true, "unknown")
	events2 = append(events2, FlushStreamSieveWithMeta(state2, toolNames, true, "unknown")...)
	callCount = 0
	for _, e := range events2 {
		callCount += len(e.ToolCalls)
	}
	if callCount == 0 {
		t.Fatal("expected orphan agent parameter capture with allowMetaAgentTools")
	}
}

func TestStreamSieveExpandCodeFenceStateMachine(t *testing.T) {
	if InsideCodeFence("") {
		t.Fatal("expected not inside code fence initially")
	}
	if InsideCodeFenceWithState(nil, "") {
		t.Fatal("expected not inside fence with nil state")
	}

	// codeFenceLineStart must be true initially for fence detection at text start
	state := &StreamSieveState{codeFenceLineStart: true}
	updateCodeFenceState(state, "```go\n")
	if !InsideCodeFenceWithState(state, "") {
		t.Fatal("expected inside code fence after opening")
	}
	updateCodeFenceState(state, "func main() {\n")
	if !InsideCodeFenceWithState(state, "") {
		t.Fatal("expected still inside code fence")
	}
	updateCodeFenceState(state, "```\n")
	if InsideCodeFenceWithState(state, "") {
		t.Fatal("expected outside code fence after closing")
	}
}

func TestStreamSieveExpandNestedCodeFence(t *testing.T) {
	// codeFenceLineStart must be true initially
	state := &StreamSieveState{codeFenceLineStart: true}
	updateCodeFenceState(state, "````markdown\n")
	if !InsideCodeFenceWithState(state, "") {
		t.Fatal("expected inside 4-tick code fence")
	}
	updateCodeFenceState(state, "```\n")
	if !InsideCodeFenceWithState(state, "") {
		t.Fatal("expected still inside 4-tick fence after 3-tick close")
	}
	// applyFenceMarker only pops one level per call, so the first ````\n
	// pops the inner 3-tick fence; a second ````\n pops the outer 4-tick fence
	updateCodeFenceState(state, "````\n")
	if !InsideCodeFenceWithState(state, "") {
		t.Fatal("expected still inside outer 4-tick fence after first 4-tick close")
	}
	updateCodeFenceState(state, "````\n")
	if InsideCodeFenceWithState(state, "") {
		t.Fatal("expected outside 4-tick fence after second 4-tick close")
	}
}

func TestStreamSieveExpandXMLIncrementalDeltas(t *testing.T) {
	// xmlIncrementalDeltas uses <tool_name> regex, not <invoke name="...">
	state := &StreamSieveState{}
	state.resetIncrementalToolState()
	state.capture.WriteString(`<tool_calls><tool_name>Read</tool_name><parameter name="file_path">/tmp/x.txt</parameter></tool_calls>`)
	deltas := generateIncrementalDeltas(state)
	if len(deltas) == 0 {
		t.Fatal("expected deltas for XML tool call")
	}
	foundName := false
	hasArgs := false
	for _, d := range deltas {
		if d.Name == "Read" {
			foundName = true
		}
		if d.Arguments != "" {
			hasArgs = true
		}
	}
	if !foundName {
		t.Fatal("expected name delta for Read")
	}
	if !hasArgs {
		t.Fatal("expected arguments delta")
	}
}

func TestStreamSieveExpandJSONIncrementalDeltas(t *testing.T) {
	state := &StreamSieveState{}
	state.resetIncrementalToolState()
	state.capture.WriteString(`{"tool":"Read","arguments":{"file_path":"/tmp/y.txt"}}`)
	deltas := generateIncrementalDeltas(state)
	if len(deltas) == 0 {
		t.Fatal("expected deltas for JSON tool call")
	}
	foundName := false
	hasArgs := false
	for _, d := range deltas {
		if d.Name == "Read" {
			foundName = true
		}
		if d.Arguments != "" {
			hasArgs = true
		}
	}
	if !foundName {
		t.Fatal("expected name delta for Read")
	}
	if !hasArgs {
		t.Fatal("expected arguments delta")
	}
}

func TestStreamSieveExpandJSONIncrementalDeltasAlternateFields(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"name field", `{"name":"Bash","arguments":{"cmd":"ls"}}`},
		{"function field", `{"function":"Edit","arguments":{"file_path":"/tmp/a.txt"}}`},
		{"input field", `{"tool":"Read","input":{"file_path":"/tmp/b.txt"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := &StreamSieveState{}
			state.resetIncrementalToolState()
			state.capture.WriteString(c.input)
			deltas := generateIncrementalDeltas(state)
			if len(deltas) == 0 {
				t.Fatal("expected deltas")
			}
		})
	}
}

func TestStreamSieveExpandIncompleteError(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", false},
		{"unclosed tool_call", "<tool_call><invoke name=\"Read\"><parameter name=\"file_path\">/tmp/a.txt</parameter></invoke>", true},
		{"unclosed tool_calls", "<tool_calls><invoke name=\"Read\"><parameter name=\"file_path\">/tmp/a.txt</parameter></invoke>", true},
		{"unclosed invoke", "<invoke name=\"Read\"><parameter name=\"file_path\">/tmp/a.txt</parameter></invoke>", true},
		// Complete <tool_calls> block returns true because <tool_call open tag
		// matches <tool_calls> but </tool_call> close tag doesn't match </tool_calls>
		{"complete", "<tool_calls><invoke name=\"Read\"><parameter name=\"file_path\">/tmp/a.txt</parameter></invoke></tool_calls>", true},
		{"visible JSON incomplete", `{"tool":"Read","arguments":{"file_path":"/tmp/a.txt"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := incompleteStreamToolTransactionError(tt.input)
			if ok != tt.wantErr {
				t.Errorf("incompleteStreamToolTransactionError(%q) returned ok=%v, want %v", tt.input, ok, tt.wantErr)
			}
		})
	}
}

func TestStreamSieveExpandConsumeStreamToolCapture(t *testing.T) {
	state := &StreamSieveState{}
	state.capture.WriteString(`<tool_calls><invoke name="Bash"><parameter name="command">pwd</parameter></invoke></tool_calls>`)
	_, calls, _, ready := ConsumeStreamToolCapture(state, []string{"Bash"}, false, "unknown")
	if !ready || len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("expected Bash call, got ready=%v calls=%#v", ready, calls)
	}

	// JSON visible tool calls are returned as text, not as tool calls
	state2 := &StreamSieveState{}
	state2.capture.WriteString(`{"tool":"Read","arguments":{"file_path":"/tmp/a.txt"}}`)
	prefix2, calls2, _, ready2 := ConsumeStreamToolCapture(state2, []string{"Read"}, false, "unknown")
	if !ready2 || len(calls2) != 0 || prefix2 == "" {
		t.Fatalf("expected ready=true with text content, got ready=%v calls=%#v prefix=%q", ready2, calls2, prefix2)
	}

	state3 := &StreamSieveState{}
	_, _, _, ready3 := ConsumeStreamToolCapture(state3, []string{"Read"}, false, "unknown")
	if ready3 {
		t.Fatal("expected not ready for empty capture")
	}
}

func TestStreamSieveExpandMixedContent(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read", "Bash"}
	text := "First.\n<tool_calls><invoke name=\"Read\"><parameter name=\"file_path\">/tmp/a.txt</parameter></invoke></tool_calls>\nThen.\n<tool_calls><invoke name=\"Bash\"><parameter name=\"command\">ls</parameter></invoke></tool_calls>\nDone."
	events := ProcessStreamSieveChunk(state, text, toolNames)
	events = append(events, FlushStreamSieve(state, toolNames)...)
	callCount := 0
	for _, e := range events {
		callCount += len(e.ToolCalls)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 tool calls, got %d", callCount)
	}
}

func TestStreamSieveExpandVisibleJSON(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read", "Bash"}
	// JSON visible tool calls are NOT parsed - they pass through as text.
	text := "Read file:\n{\"tool\":\"Read\",\"arguments\":{\"file_path\":\"/tmp/data.json\"}}\nDone."
	events := ProcessStreamSieveChunk(state, text, toolNames)
	events = append(events, FlushStreamSieve(state, toolNames)...)
	callCount := 0
	hasRead := false
	for _, e := range events {
		callCount += len(e.ToolCalls)
		if strings.Contains(e.Content, "Read file:") {
			hasRead = true
		}
	}
	if callCount != 0 {
		t.Fatalf("expected 0 tool calls (JSON is text), got %d", callCount)
	}
	if !hasRead {
		t.Fatal("expected 'Read file:' text")
	}
}

func TestStreamSieveExpandMultipleCallsInOneChunk(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read", "Bash"}
	text := `<tool_calls><invoke name="Read"><parameter name="file_path">/tmp/a.txt</parameter></invoke></tool_calls>
	<tool_calls><invoke name="Bash"><parameter name="command">echo test</parameter></invoke></tool_calls>
	<tool_calls><invoke name="Read"><parameter name="file_path">/tmp/b.txt</parameter></invoke></tool_calls>`
	events := ProcessStreamSieveChunk(state, text, toolNames)
	events = append(events, FlushStreamSieve(state, toolNames)...)
	callCount := 0
	for _, e := range events {
		callCount += len(e.ToolCalls)
	}
	if callCount != 3 {
		t.Fatalf("expected 3 tool calls, got %d", callCount)
	}
}

func TestStreamSieveExpandFlushResidual(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read"}
	ProcessStreamSieveChunk(state, "some text <tool_calls><invoke name=\"Read\">", toolNames)
	events := FlushStreamSieve(state, toolNames)
	hasError := false
	for _, e := range events {
		if e.ErrorCode != "" {
			hasError = true
		}
	}
	if !hasError {
		t.Fatal("expected error for incomplete tool call on flush")
	}
}

func TestStreamSieveExpandNilState(t *testing.T) {
	events := FlushStreamSieve(nil, []string{"Read"})
	if len(events) != 0 {
		t.Fatalf("expected 0 events from nil state, got %d", len(events))
	}
	events = ProcessStreamSieveChunk(nil, "hello", []string{"Read"})
	if len(events) != 0 {
		t.Fatalf("expected 0 events from nil state, got %d", len(events))
	}
}

func TestStreamSieveExpandEmptyChunk(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read"}
	// Empty chunk produces no events
	events := ProcessStreamSieveChunk(state, "", toolNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for empty chunk, got %d", len(events))
	}
	// "hello" has no tool call syntax - ProcessStreamSieveChunk emits it directly
	events = ProcessStreamSieveChunk(state, "hello", toolNames)
	if len(events) != 1 || events[0].Content != "hello" {
		t.Fatalf("expected 'hello' event, got %#v", events)
	}
	// Flush after simple text should produce no additional events
	events = FlushStreamSieve(state, toolNames)
	if len(events) != 0 {
		t.Fatalf("expected 0 events from flush, got %d", len(events))
	}
}

func TestStreamSieveExpandToolCallInsideFence(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read", "Bash"}
	text := "Example:\n```xml\n<tool_calls><invoke name=\"Read\"><parameter name=\"file_path\">/tmp/a.txt</parameter></invoke></tool_calls>\n```\nDone."
	events := ProcessStreamSieveChunk(state, text, toolNames)
	events = append(events, FlushStreamSieve(state, toolNames)...)
	callCount := 0
	for _, e := range events {
		callCount += len(e.ToolCalls)
	}
	if callCount > 0 {
		t.Fatalf("expected no tool calls inside code fence, got %d", callCount)
	}
}

func TestStreamSieveExpandFencedJSONChunks(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read", "Bash"}
	chunks := []string{
		"before fence\n```json\n",
		"{\"tool\":\"Read\",",
		"\"arguments\":{\"file_path\":\"/tmp/a.txt\"}}",
		"\n```\nafter fence",
	}
	var allEvents []StreamSieveEvent
	for _, ch := range chunks {
		allEvents = append(allEvents, ProcessStreamSieveChunk(state, ch, toolNames)...)
	}
	allEvents = append(allEvents, FlushStreamSieve(state, toolNames)...)
	callCount := 0
	for _, e := range allEvents {
		callCount += len(e.ToolCalls)
	}
	if callCount > 0 {
		t.Fatalf("expected no tool calls from fenced JSON text, got %d", callCount)
	}
}

func TestStreamSieveExpandXMLToolCallWithMeta(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read", "Agent"}
	text := "text <tool_calls><invoke name=\"Read\"><parameter name=\"file_path\">/tmp/a.txt</parameter></invoke></tool_calls>"
	events := ProcessStreamSieveChunkWithMeta(state, text, toolNames, true, "unknown")
	events = append(events, FlushStreamSieveWithMeta(state, toolNames, true, "unknown")...)
	callCount := 0
	for _, e := range events {
		callCount += len(e.ToolCalls)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 tool call with meta, got %d", callCount)
	}
}

func TestStreamSieveExpandJSONArrayWrapper(t *testing.T) {
	state := &StreamSieveState{}
	toolNames := []string{"Read", "Bash"}
	// Array-wrapped JSON is visible text, not parsed as tool calls.
	text := `before [{"tool":"Read","arguments":{"file_path":"/tmp/a.txt"}}] after`
	events := ProcessStreamSieveChunk(state, text, toolNames)
	events = append(events, FlushStreamSieve(state, toolNames)...)
	callCount := 0
	for _, e := range events {
		callCount += len(e.ToolCalls)
	}
	if callCount != 0 {
		t.Fatalf("expected 0 tool calls (JSON is text), got %d", callCount)
	}
}

func TestStreamSieveExpandIsLineStart(t *testing.T) {
	tests := []struct {
		s   string
		idx int
		exp bool
	}{
		{"hello", 0, true},
		{"a\nb", 2, true},
		{"hello world", 3, false},
		{"   hello", 3, true},
	}
	for _, tc := range tests {
		got := isLineStart(tc.s, tc.idx)
		if got != tc.exp {
			t.Errorf("isLineStart(%q, %d) = %v, want %v", tc.s, tc.idx, got, tc.exp)
		}
	}
}

func TestStreamSieveExpandIsJSONFenceHeader(t *testing.T) {
	tests := []struct {
		h   string
		exp bool
	}{
		{"json", true},
		{"JSON", true},
		{"jsonc", true},
		{"javascript", true},
		{"js", true},
		{"go", false},
		{"xml", false},
		{"", false},
	}
	for _, tc := range tests {
		got := isJSONFenceHeader(tc.h)
		if got != tc.exp {
			t.Errorf("isJSONFenceHeader(%q) = %v, want %v", tc.h, got, tc.exp)
		}
	}
}

func TestStreamSieveExpandJSONEscape(t *testing.T) {
	tests := []struct {
		in  string
		exp string
	}{
		{"simple", "simple"},
		{"has\"quote", `has\"quote`},
		{"has\nnewline", `has\nnewline`},
	}
	for _, tc := range tests {
		got := jsonEscape(tc.in)
		if got != tc.exp {
			t.Errorf("jsonEscape(%q) = %q, want %q", tc.in, got, tc.exp)
		}
	}
}

func TestStreamSieveExpandExtractJSONStringField(t *testing.T) {
	tests := []struct {
		name string
		json string
		fld  string
		exp  string
	}{
		{"tool field", `{"tool":"Read","args":{}}`, "tool", "Read"},
		{"name field", `{"name":"Bash","arguments":{}}`, "name", "Bash"},
		{"function field", `{"function":"Edit","input":{}}`, "function", "Edit"},
		{"not found", `{"foo":"bar"}`, "tool", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractJSONStringField(tc.json, tc.fld)
			if got != tc.exp {
				t.Errorf("extractJSONStringField(%q, %q) = %q, want %q", tc.json, tc.fld, got, tc.exp)
			}
		})
	}
}

func TestStreamSieveExpandHasOpenXMLToolTag(t *testing.T) {
	tests := []struct {
		name string
		in   string
		exp  bool
	}{
		{"unclosed tool_call", "<tool_call><invoke name=\"Read\"></invoke>", true},
		// Complete <tool_calls> returns true because <tool_call open tag
		// matches <tool_calls> but </tool_call> close tag doesn't match </tool_calls>
		{"complete", "<tool_calls><invoke name=\"Read\"><parameter name=\"x\">y</parameter></invoke></tool_calls>", true},
		{"unclosed tool_calls", "<tool_calls>text", true},
		{"no tool tag", "just some text", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := HasOpenXMLToolTag(tc.in)
			if got != tc.exp {
				t.Errorf("HasOpenXMLToolTag(%q) = %v, want %v", tc.in, got, tc.exp)
			}
		})
	}
}

func TestStreamSieveExpandHasVisibleJSONToolHints(t *testing.T) {
	tests := []struct {
		in  string
		exp bool
	}{
		{`{"tool":"Read","arguments":{}}`, true},
		{`{"name":"Bash","input":{}}`, true},
		{`{"function":"Edit","arguments":{}}`, true},
		{`{"tool":"Read"}`, false},
		{`{"foo":"bar"}`, false},
	}
	for _, tc := range tests {
		got := hasVisibleJSONToolHints(tc.in)
		if got != tc.exp {
			t.Errorf("hasVisibleJSONToolHints(%q) = %v, want %v", tc.in, got, tc.exp)
		}
	}
}

func TestStreamSieveExpandJSONLikeValueEnd(t *testing.T) {
	tests := []struct {
		in  string
		exp int
	}{
		{"", -1},
		{"hello", -1},
		{`{"a":1}`, 7},
		{`{"a":{"b":2}}`, 13},
		{`[1,2,3]`, 7},
		{`{"a":1`, -1},
	}
	for _, tc := range tests {
		got := jsonLikeValueEnd(tc.in)
		if got != tc.exp {
			t.Errorf("jsonLikeValueEnd(%q) = %d, want %d", tc.in, got, tc.exp)
		}
	}
}

func TestStreamSieveExpandResetIncrementalToolState(t *testing.T) {
	state := &StreamSieveState{
		disableDeltas:  true,
		toolNameSent:   true,
		toolName:       "Read",
		toolArgsStart:  10,
		toolArgsSent:   20,
		toolArgsString: true,
		toolArgsDone:   true,
	}
	state.resetIncrementalToolState()
	if state.disableDeltas || state.toolNameSent || state.toolName != "" || state.toolArgsStart != -1 || state.toolArgsSent != -1 || state.toolArgsString || state.toolArgsDone {
		t.Fatal("expected all fields reset to defaults")
	}
}

func TestStreamSieveExpandTrimWrappingJSONFence(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		suffix string
		wpref  string
		wsuf   string
	}{
		{"no fence", "before", "after", "before", "after"},
		// TrimWrappingJSONFence only strips when the content between prefix and suffix
		// forms a complete JSON fence block with content
		{"with fence", "text\n```json\n<tool_calls>", "```\nend", "text\n```json\n<tool_calls>", "```\nend"},
		{"not json fence", "text\n```go\ncontent", "```\nend", "text\n```go\ncontent", "```\nend"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gp, gs := TrimWrappingJSONFence(tc.prefix, tc.suffix)
			if gp != tc.wpref || gs != tc.wsuf {
				t.Errorf("TrimWrappingJSONFence(%q,%q) = (%q,%q), want (%q,%q)", tc.prefix, tc.suffix, gp, gs, tc.wpref, tc.wsuf)
			}
		})
	}
}

func TestStreamSieveExpandNextVisibleJSONCandidateIndex(t *testing.T) {
	tests := []struct {
		s   string
		off int
		exp int
	}{
		{"", 0, -1},
		{`{"a":1} text`, 0, 0},
		{`[1,2] text`, 0, 0},
		{"text {here}", 0, 5},
	}
	for _, tc := range tests {
		got := nextVisibleJSONCandidateIndex(tc.s, tc.off)
		if got != tc.exp {
			t.Errorf("nextVisibleJSONCandidateIndex(%q,%d) = %d, want %d", tc.s, tc.off, got, tc.exp)
		}
	}
}

func TestStreamSieveExpandSplitSafeContent(t *testing.T) {
	state := &StreamSieveState{}
	tests := []struct {
		in    string
		wsafe string
		whold string
	}{
		{"", "", ""},
		{"hello world", "hello world", ""},
	}
	for _, tc := range tests {
		safe, hold := SplitSafeContentForToolDetection(state, tc.in)
		if safe != tc.wsafe || hold != tc.whold {
			t.Errorf("SplitSafeContentForToolDetection(%q) = (%q,%q), want (%q,%q)", tc.in, safe, hold, tc.wsafe, tc.whold)
		}
	}
}

func TestStreamSieveExpandNoteTextAndFenceState(t *testing.T) {
	state := &StreamSieveState{codeFenceLineStart: true}
	state.noteText("")
	if len(state.codeFenceStack) != 0 {
		t.Fatal("expected empty codeFenceStack after empty noteText")
	}
	state.noteText("hello world\n")
	if len(state.codeFenceStack) != 0 {
		t.Fatal("expected still empty codeFenceStack")
	}
	state.noteText("```go\n")
	if len(state.codeFenceStack) == 0 {
		t.Fatal("expected non-empty codeFenceStack after opening fence")
	}
}

func TestStreamSieveExpandHasMeaningfulText(t *testing.T) {
	tests := []struct {
		in  string
		exp bool
	}{
		{"", false},
		{"   ", false},
		{"\n\t", false},
		{"hello", true},
		{"  text  ", true},
	}
	for _, tc := range tests {
		got := hasMeaningfulText(tc.in)
		if got != tc.exp {
			t.Errorf("hasMeaningfulText(%q) = %v, want %v", tc.in, got, tc.exp)
		}
	}
}

func TestStreamSieveExpandConsumeXMLToolCapture(t *testing.T) {
	tests := []struct {
		name      string
		captured  string
		wantCalls int
		wantReady bool
	}{
		{"complete", `<tool_calls><invoke name="Read"><parameter name="file_path">/tmp/a.txt</parameter></invoke></tool_calls>`, 1, true},
		{"no tool tag", "hello world", 0, false},
		{"incomplete", `<tool_calls><invoke name="Read">`, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, calls, _, ready := ConsumeXMLToolCapture(tc.captured, []string{"Read"}, false, "unknown")
			if ready != tc.wantReady {
				t.Errorf("ready=%v, want %v", ready, tc.wantReady)
			}
			if len(calls) != tc.wantCalls {
				t.Errorf("%d calls, want %d", len(calls), tc.wantCalls)
			}
		})
	}
}

func TestStreamSieveExpandFindStreamToolSegmentStart(t *testing.T) {
	state := &StreamSieveState{}
	tests := []struct {
		in  string
		exp int
	}{
		{"", -1},
		{"hello world", -1},
		{"before<tool_calls>after", 6},
		{"text<invoke name=\"Read\">more", 4},
		{"pre<parameter name=\"description\">post", 3},
	}
	for _, tc := range tests {
		got := FindStreamToolSegmentStart(state, tc.in)
		if got != tc.exp {
			t.Errorf("FindStreamToolSegmentStart(%q) = %d, want %d", tc.in, got, tc.exp)
		}
	}
}

func TestStreamSieveExpandFindStreamToolSegmentStartInsideFence(t *testing.T) {
	state := &StreamSieveState{}
	state.codeFenceStack = []int{3}
	state.codeFenceLineStart = true
	idx := FindStreamToolSegmentStart(state, "text<tool_calls>more")
	if idx >= 0 {
		t.Fatalf("expected no tool tag inside code fence, got idx=%d", idx)
	}
}

func TestStreamSieveExpandGenerateIncrementalDeltasNil(t *testing.T) {
	if d := generateIncrementalDeltas(nil); len(d) != 0 {
		t.Fatal("expected nil deltas from nil state")
	}
	if d := generateIncrementalDeltas(&StreamSieveState{}); len(d) != 0 {
		t.Fatal("expected nil deltas from empty state")
	}
	s := &StreamSieveState{disableDeltas: true}
	s.capture.WriteString("x")
	if d := generateIncrementalDeltas(s); len(d) != 0 {
		t.Fatal("expected nil deltas when disabled")
	}
}

func TestStreamSieveExpandInterfaceNil(t *testing.T) {
	FlushStreamSieve(nil, nil)
	FlushStreamSieveWithMeta(nil, nil, false, "unknown")
	ProcessStreamSieveChunk(nil, "hello", nil)
	ProcessStreamSieveChunkWithMeta(nil, "hello", nil, false, "unknown")
	InsideCodeFenceWithState(nil, "hello")
	SplitSafeContentForToolDetection(nil, "hello")
	FindStreamToolSegmentStart(nil, "hello")
	FindFencedJSONToolTextStart(nil, "hello")
	FindPartialFencedJSONToolTextStart(nil, "hello")
	FindVisibleJSONToolSegmentStart(nil, "hello")
	FindPartialVisibleJSONToolSegmentStart(nil, "hello")
}

func TestStreamSieveExpandConsumeVisibleJSONToolCapture(t *testing.T) {
	captured := `{"tool":"Read","arguments":{"file_path":"/tmp/a.txt"}}`
	prefix, calls, _, ready := ConsumeVisibleJSONToolCapture(captured, []string{"Read"}, false, "unknown")
	// ConsumeVisibleJSONToolCapture returns JSON as prefix text, not as tool calls
	if !ready || len(calls) != 0 || prefix == "" {
		t.Fatalf("expected ready=true with prefix, got ready=%v calls=%#v prefix=%q", ready, calls, prefix)
	}

	captured2 := `{"ok":true}`
	_, calls2, _, ready2 := ConsumeVisibleJSONToolCapture(captured2, []string{"Read"}, false, "unknown")
	if !ready2 || len(calls2) != 0 {
		t.Fatalf("expected ready with 0 calls, got ready=%v calls=%d", ready2, len(calls2))
	}
}

func TestStreamSieveExpandConsumeFencedJSONToolTextCapture(t *testing.T) {
	captured := "```json\n{\"tool\":\"Read\",\"arguments\":{\"file_path\":\"/tmp/a.txt\"}}\n```"
	if _, _, _, ready := ConsumeFencedJSONToolTextCapture(captured, []string{"Read"}); !ready {
		t.Fatal("expected ready=true for fenced JSON tool text")
	}
	if _, _, _, ready := ConsumeFencedJSONToolTextCapture("hello", []string{"Read"}); ready {
		t.Fatal("expected ready=false")
	}
}

func TestStreamSieveExpandFindFencedJSONToolTextStart(t *testing.T) {
	state := &StreamSieveState{}
	tests := []struct {
		in  string
		exp int
	}{
		{"", -1},
		{"hello", -1},
		{"prefix\n```json\ncontent\n```\n", 7},
	}
	for _, tc := range tests {
		got := FindFencedJSONToolTextStart(state, tc.in)
		if got != tc.exp {
			t.Errorf("FindFencedJSONToolTextStart(%q) = %d, want %d", tc.in, got, tc.exp)
		}
	}
}

func TestStreamSieveExpandFindVisibleJSONToolSegmentStart(t *testing.T) {
	state := &StreamSieveState{}
	// JSON must be at line start (no text before it on the same line)
	input := "text\n{\"tool\":\"Read\",\"arguments\":{\"file_path\":\"/tmp/a.txt\"}} more"
	idx := FindVisibleJSONToolSegmentStart(state, input)
	if idx < 0 || input[idx] != '{' {
		t.Fatalf("expected to find visible JSON tool start at line start, got %d", idx)
	}
	input2 := "text2\n{\"ok\":true} more"
	if idx2 := FindVisibleJSONToolSegmentStart(state, input2); idx2 >= 0 {
		t.Fatalf("expected not to find non-tool JSON, got %d", idx2)
	}
}

func TestStreamSieveExpandXMLIncrementalDeltasPartial(t *testing.T) {
	state := &StreamSieveState{}
	state.resetIncrementalToolState()
	state.capture.WriteString(`<tool_calls><tool_name>Bash</tool_name>`)
	deltas := generateIncrementalDeltas(state)
	if len(deltas) != 1 || deltas[0].Name != "Bash" {
		t.Fatalf("expected 1 delta with name 'Bash', got %#v", deltas)
	}
	if !state.toolNameSent {
		t.Fatal("expected toolNameSent")
	}
}

func TestStreamSieveExpandJSONIncrementalDeltasPartialArgs(t *testing.T) {
	state := &StreamSieveState{}
	state.capture.WriteString(`{"tool":"Read","arguments":{"file_path":"/tmp`)
	deltas := jsonIncrementalDeltas(state, state.capture.String())
	if len(deltas) == 0 || deltas[0].Name != "Read" {
		t.Fatalf("expected name delta, got %#v", deltas)
	}
}

func TestStreamSieveExpandXMLDeltasWithParameterOnly(t *testing.T) {
	state := &StreamSieveState{}
	state.capture.WriteString(`<tool_calls><invoke name="Bash"><parameter name="command">ls</parameter></invoke></tool_calls>`)
	deltas := xmlIncrementalDeltas(state, state.capture.String())
	if len(deltas) == 0 {
		t.Fatal("expected deltas")
	}
}
