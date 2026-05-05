package claude

import (
	"context"
	"ds2api/internal/auth"
	dsclient "ds2api/internal/deepseek/client"
	"ds2api/internal/promptcompat"
	"ds2api/internal/sse"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type claudeFrame struct {
	Event   string
	Payload map[string]any
}

func makeClaudeSSEHTTPResponse(lines ...string) *http.Response {
	body := strings.Join(lines, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func makeClaudeContentLine(t *testing.T, text string) string {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"p": "response/content",
		"v": text,
	})
	if err != nil {
		t.Fatalf("marshal content line failed: %v", err)
	}
	return "data: " + string(line)
}

func parseClaudeFrames(t *testing.T, body string) []claudeFrame {
	t.Helper()
	chunks := strings.Split(body, "\n\n")
	frames := make([]claudeFrame, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		lines := strings.Split(chunk, "\n")
		eventName := ""
		dataPayload := ""
		for _, line := range lines {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				dataPayload = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if eventName == "" || dataPayload == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(dataPayload), &payload); err != nil {
			t.Fatalf("decode frame failed: %v, payload=%s", err, dataPayload)
		}
		frames = append(frames, claudeFrame{Event: eventName, Payload: payload})
	}
	return frames
}

func findClaudeFrames(frames []claudeFrame, event string) []claudeFrame {
	out := make([]claudeFrame, 0)
	for _, f := range frames {
		if f.Event == event {
			out = append(out, f)
		}
	}
	return out
}

func collectClaudeTextDeltas(frames []claudeFrame) string {
	var combined strings.Builder
	for _, f := range findClaudeFrames(frames, "content_block_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] == "text_delta" {
			combined.WriteString(asString(delta["text"]))
		}
	}
	return combined.String()
}

func TestHandleClaudeStreamRealtimeTextIncrementsWithEventHeaders(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"Hel"}`,
		`data: {"p":"response/content","v":"lo"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, false, false, nil, nil)

	body := rec.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Fatalf("missing event header: message_start, body=%s", body)
	}
	if !strings.Contains(body, "event: content_block_delta") {
		t.Fatalf("missing event header: content_block_delta, body=%s", body)
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Fatalf("missing event header: message_stop, body=%s", body)
	}

	frames := parseClaudeFrames(t, body)
	deltas := findClaudeFrames(frames, "content_block_delta")
	if len(deltas) < 1 {
		t.Fatalf("expected at least 1 text delta, got=%d body=%s", len(deltas), body)
	}
	combined := strings.Builder{}
	for _, f := range deltas {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] == "text_delta" {
			combined.WriteString(asString(delta["text"]))
		}
	}
	if combined.String() != "Hello" {
		t.Fatalf("unexpected combined text: %q body=%s", combined.String(), body)
	}
}

func TestHandleClaudeStreamRealtimeToolBufferedPlainTextDoesNotRepeatFinalText(t *testing.T) {
	h := &Handler{}
	want := "明白\n\nBash\nIN\npwd\nOUT\nok"
	resp := makeClaudeSSEHTTPResponse(
		makeClaudeContentLine(t, "明"),
		makeClaudeContentLine(t, "白\n\nBash\nIN\npwd\n"),
		makeClaudeContentLine(t, "OUT\nok"),
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "use tool"}}, false, false, []string{"Bash"}, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	if got := collectClaudeTextDeltas(frames); got != want {
		t.Fatalf("unexpected combined text: got %q want %q body=%s", got, want, rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimeToolModeDropsLeakedEndOfSentenceReplay(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		makeClaudeContentLine(t, "<| end_of_sentence |>{\n  \"name\": \"api\",\n  \"version\": \"4.0.0\"\n}"),
		makeClaudeContentLine(t, "<| end_of_toolresults |><| Tool |>{\"compilerOptions\":{}}\n./src/"),
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "请分析当前代码"}}, false, false, []string{"Read", "Bash"}, nil)

	body := rec.Body.String()
	for _, leaked := range []string{"end_of_sentence", "end_of_toolresults", "\"name\": \"api\"", "compilerOptions", "./src/"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("leaked prompt replay %q into Claude stream body=%s", leaked, body)
		}
	}
}

func TestHandleClaudeStreamRealtimeDropsSplitLeakedEndOfSentenceReplay(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		makeClaudeContentLine(t, "可见摘要<| end_of_"),
		makeClaudeContentLine(t, "sentence |>export type ApiMessage = {}"),
		makeClaudeContentLine(t, "export type ApiStreamEvent = {}"),
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "请分析当前代码"}}, false, false, []string{"Read", "Bash"}, nil)

	body := rec.Body.String()
	if !strings.Contains(body, "可见摘要") {
		t.Fatalf("expected visible prefix to remain, body=%s", body)
	}
	for _, leaked := range []string{"end_of_sentence", "ApiMessage", "ApiStreamEvent"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("split leaked prompt replay %q into Claude stream body=%s", leaked, body)
		}
	}
}

func TestHandleClaudeStreamRealtimeSuppressesShortPreambleBeforeToolUse(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		makeClaudeContentLine(t, "Let me first"),
		makeClaudeContentLine(t, `<tool_calls><invoke name="Bash"><parameter name="command">find . -type f</parameter></invoke></tool_calls>`),
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "请并行分析当前代码问题和改进点"}}, false, false, []string{"Bash"}, nil)

	body := rec.Body.String()
	if strings.Contains(body, "Let me first") {
		t.Fatalf("short preamble must not be emitted before tool_use, body=%s", body)
	}
	frames := parseClaudeFrames(t, body)
	var foundTool bool
	for _, frame := range findClaudeFrames(frames, "content_block_start") {
		block, _ := frame.Payload["content_block"].(map[string]any)
		if block["type"] == "tool_use" && block["name"] == "Bash" {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatalf("expected Bash tool_use, body=%s", body)
	}
}

func TestHandleClaudeStreamRealtimeTrimsContinuationReplay(t *testing.T) {
	h := &Handler{}
	prefix := strings.Repeat("A", 40)
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"`+prefix+`"}`,
		`data: {"p":"response/content","v":"`+prefix+` tail"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, false, false, nil, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	combined := strings.Builder{}
	for _, f := range findClaudeFrames(frames, "content_block_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] == "text_delta" {
			combined.WriteString(asString(delta["text"]))
		}
	}
	if got, want := combined.String(), prefix+" tail"; got != want {
		t.Fatalf("unexpected combined text: got %q want %q body=%s", got, want, rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimeThinkingDelta(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"思"}`,
		`data: {"p":"response/thinking_content","v":"考"}`,
		`data: {"p":"response/content","v":"ok"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, true, false, nil, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	foundThinkingDelta := false
	for _, f := range findClaudeFrames(frames, "content_block_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] == "thinking_delta" {
			foundThinkingDelta = true
			break
		}
	}
	if !foundThinkingDelta {
		t.Fatalf("expected thinking_delta event, body=%s", rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimeSkipsThinkingFallbackWhenFinalTextExists(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"{\"tool_calls\":[{\"name\":\"search\""}`,
		`data: {"p":"response/thinking_content","v":",\"input\":{\"q\":\"go\"}}]}"}`,
		`data: {"p":"response/content","v":"normal answer"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "use tool"}}, true, false, []string{"search"}, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	for _, f := range findClaudeFrames(frames, "content_block_start") {
		contentBlock, _ := f.Payload["content_block"].(map[string]any)
		if contentBlock["type"] == "tool_use" {
			t.Fatalf("unexpected tool_use block when final text exists, body=%s", rec.Body.String())
		}
	}

	foundEndTurn := false
	for _, f := range findClaudeFrames(frames, "message_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["stop_reason"] == "end_turn" {
			foundEndTurn = true
			break
		}
	}
	if !foundEndTurn {
		t.Fatalf("expected stop_reason=end_turn, body=%s", rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimeSynthesizesSafeExplorationPromise(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"The user is asking about cold bootstrap performance breakdown and wants to explore this in parallel.\n\nLet me examine the current codebase state and then investigate the three phases concurrently."}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "analyze and measure in parallel"}}, false, false, []string{"Read", "Bash", "Agent"}, nil)

	body := rec.Body.String()
	if strings.Contains(body, "Let me examine") {
		t.Fatalf("missing-tool promise must not be emitted as text, body=%s", body)
	}
	if strings.Contains(body, "upstream_missing_tool_call") {
		t.Fatalf("safe exploration promise should become a tool call, body=%s", body)
	}
	frames := parseClaudeFrames(t, body)
	var foundTool bool
	for _, frame := range findClaudeFrames(frames, "content_block_start") {
		block, _ := frame.Payload["content_block"].(map[string]any)
		if block["type"] == "tool_use" && block["name"] == "Bash" {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatalf("expected synthesized Bash tool_use, body=%s", body)
	}
}

func TestHandleClaudeStreamRealtimeSuppressesMalformedDSMLToolText(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"<|DSML|tool_calls>\n<|DSML|invoke name=\"Agent\">\n<|DSML|parameter name=\"description\">分析</|DSML|parameter>\n<|DSML|parameter name=\"prompt\">分析当前项目</|DSML|parameter>\n</|DSML|invoke>\n</DSML|"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "use agent"}}, false, false, []string{"Agent", "Read"}, nil)

	body := rec.Body.String()
	if strings.Contains(body, "DSML") || strings.Contains(body, "<|") {
		t.Fatalf("malformed DSML tool syntax must not leak as visible text, body=%s", body)
	}
}

func TestHandleClaudeDirectStreamRetriesMissingToolBeforeError(t *testing.T) {
	ds := &claudeStreamRetryDS{responses: []*http.Response{
		makeClaudeSSEHTTPResponse(
			`data: {"response_message_id":88,"p":"response/content","v":"Now let me check the current working tree state and build/test status."}`,
			`data: [DONE]`,
		),
		makeClaudeSSEHTTPResponse(
			`data: {"response_message_id":89,"p":"response/content","v":"<tool_calls><invoke name=\"Bash\"><parameter name=\"command\">git status --short</parameter></invoke></tool_calls>"}`,
			`data: [DONE]`,
		),
	}}
	h := &Handler{DS: ds}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	stdReq := promptcompat.StandardRequest{
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "please check status",
		Messages:        []any{map[string]any{"role": "user", "content": "please check status"}},
		ToolNames:       []string{"Bash", "Read", "Edit"},
		ToolsRaw: []any{map[string]any{
			"name": "Bash",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
			},
		}},
	}

	h.handleClaudeDirectStream(rec, req, &auth.RequestAuth{}, stdReq, nil)

	body := rec.Body.String()
	if strings.Contains(body, "upstream_missing_tool_call") {
		t.Fatalf("missing-tool retry should not leak first-attempt error, body=%s", body)
	}
	frames := parseClaudeFrames(t, body)
	var foundTool bool
	for _, frame := range findClaudeFrames(frames, "content_block_start") {
		block, _ := frame.Payload["content_block"].(map[string]any)
		if block["type"] == "tool_use" && block["name"] == "Bash" {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatalf("expected retried Bash tool_use, body=%s", body)
	}
	if len(ds.payloads) != 2 {
		t.Fatalf("expected two completion calls, got %d", len(ds.payloads))
	}
	if got := ds.payloads[1]["parent_message_id"]; got != 88 {
		t.Fatalf("retry parent_message_id mismatch: %#v", got)
	}
	prompt, _ := ds.payloads[1]["prompt"].(string)
	if !strings.Contains(prompt, "promised tool work but emitted no valid tool call") {
		t.Fatalf("expected missing-tool retry prompt, got %q", prompt)
	}
}

func TestHandleClaudeDirectStreamSynthesizesSafeExplorationBeforeRetry(t *testing.T) {
	ds := &claudeStreamRetryDS{responses: []*http.Response{
		makeClaudeSSEHTTPResponse(
			`data: {"response_message_id":90,"p":"response/content","v":"Let me read the main source files to understand the codebase."}`,
			`data: [DONE]`,
		),
	}}
	h := &Handler{DS: ds}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	stdReq := promptcompat.StandardRequest{
		ResponseModel:   "deepseek-v4-pro",
		PromptTokenText: "please analyze current code",
		Messages:        []any{map[string]any{"role": "user", "content": "please analyze current code"}},
		ToolNames:       []string{"Bash", "Read"},
		ToolsRaw: []any{map[string]any{
			"name": "Bash",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":     map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
				},
				"required": []any{"command"},
			},
		}},
	}

	h.handleClaudeDirectStream(rec, req, &auth.RequestAuth{}, stdReq, nil)

	body := rec.Body.String()
	if strings.Contains(body, "upstream_missing_tool_call") || strings.Contains(body, "Let me read") {
		t.Fatalf("safe exploration promise should not leak or error, body=%s", body)
	}
	frames := parseClaudeFrames(t, body)
	var foundTool bool
	for _, frame := range findClaudeFrames(frames, "content_block_start") {
		block, _ := frame.Payload["content_block"].(map[string]any)
		if block["type"] == "tool_use" && block["name"] == "Bash" {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatalf("expected synthesized Bash tool_use, body=%s", body)
	}
	if len(ds.payloads) != 1 {
		t.Fatalf("safe exploration synthesis should avoid retry, got %d completion calls", len(ds.payloads))
	}
}

type claudeStreamRetryDS struct {
	responses []*http.Response
	payloads  []map[string]any
}

func (d *claudeStreamRetryDS) CreateSession(context.Context, *auth.RequestAuth, int) (string, error) {
	return "session-claude-retry", nil
}

func (d *claudeStreamRetryDS) GetPow(context.Context, *auth.RequestAuth, int) (string, error) {
	return "pow", nil
}

func (d *claudeStreamRetryDS) UploadFile(context.Context, *auth.RequestAuth, dsclient.UploadFileRequest, int) (*dsclient.UploadFileResult, error) {
	return &dsclient.UploadFileResult{ID: "unused"}, nil
}

func (d *claudeStreamRetryDS) CallCompletion(_ context.Context, _ *auth.RequestAuth, payload map[string]any, _ string, _ int) (*http.Response, error) {
	d.payloads = append(d.payloads, payload)
	if len(d.responses) == 0 {
		return makeClaudeSSEHTTPResponse(`data: {"p":"response/content","v":"fallback"}`), nil
	}
	resp := d.responses[0]
	d.responses = d.responses[1:]
	return resp, nil
}

func TestHandleClaudeStreamRealtimeUpstreamErrorEvent(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"error":{"message":"boom"}}`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, false, false, nil, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	errFrames := findClaudeFrames(frames, "error")
	if len(errFrames) == 0 {
		t.Fatalf("expected error event frame, body=%s", rec.Body.String())
	}
	if errFrames[0].Payload["type"] != "error" {
		t.Fatalf("expected error payload type, body=%s", rec.Body.String())
	}
}

func TestHandleClaudeStreamRealtimePingEvent(t *testing.T) {
	h := &Handler{}
	oldPing := claudeStreamPingInterval
	oldIdle := claudeStreamIdleTimeout
	oldKeepalive := claudeStreamMaxKeepaliveCnt
	claudeStreamPingInterval = 10 * time.Millisecond
	claudeStreamIdleTimeout = 300 * time.Millisecond
	claudeStreamMaxKeepaliveCnt = 50
	defer func() {
		claudeStreamPingInterval = oldPing
		claudeStreamIdleTimeout = oldIdle
		claudeStreamMaxKeepaliveCnt = oldKeepalive
	}()

	pr, pw := io.Pipe()
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: pr}
	go func() {
		time.Sleep(40 * time.Millisecond)
		_, _ = io.WriteString(pw, "data: {\"p\":\"response/content\",\"v\":\"hi\"}\n")
		_, _ = io.WriteString(pw, "data: [DONE]\n")
		_ = pw.Close()
	}()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "hi"}}, false, false, nil, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	if len(findClaudeFrames(frames, "ping")) == 0 {
		t.Fatalf("expected ping event in stream, body=%s", rec.Body.String())
	}
}

func TestCollectDeepSeekRegression(t *testing.T) {
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"想"}`,
		`data: {"p":"response/content","v":"答"}`,
		`data: [DONE]`,
	)
	result := sse.CollectStream(resp, true, true)
	if result.Thinking != "想" {
		t.Fatalf("unexpected thinking: %q", result.Thinking)
	}
	if result.Text != "答" {
		t.Fatalf("unexpected text: %q", result.Text)
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func TestHandleClaudeStreamRealtimeToolSafetyAcrossStructuredFormats(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		wantToolUse bool
	}{
		{name: "invoke_parameter_wrapper", payload: `<tool_calls><invoke name="Bash"><parameter name="command">pwd</parameter></invoke></tool_calls>`, wantToolUse: true},
		{name: "legacy_single_tool_root", payload: `<tool><tool_name>Bash</tool_name><param><command>pwd</command></param></tool>`, wantToolUse: false},
		{name: "legacy_tool_call_json", payload: `<tool>{"tool":"Bash","params":{"command":"pwd"}}</tool>`, wantToolUse: false},
		{name: "legacy_nested_tool_tag_style", payload: `<tool><tool name="Bash"><command>pwd</command></tool_call></tool>`, wantToolUse: false},
		{name: "legacy_function_tag_style", payload: `<function_call>Bash</function_call><function parameter name="command">pwd</function parameter>`, wantToolUse: false},
		{name: "legacy_antml_argument_style", payload: `<antml:function_calls><antml:function_call id="1" name="Bash"><antml:argument name="command">pwd</antml:argument></antml:function_call></antml:function_calls>`, wantToolUse: false},
		{name: "legacy_antml_function_attr_parameters", payload: `<antml:function_calls><antml:function_call id="1" function="Bash"><antml:parameters>{"command":"pwd"}</antml:parameters></antml:function_call></antml:function_calls>`, wantToolUse: false},
		{name: "legacy_function_calls_wrapper", payload: `<function_calls><invoke name="Bash"><parameter name="command">pwd</parameter></invoke></function_calls>`, wantToolUse: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{}
			resp := makeClaudeSSEHTTPResponse(
				`data: {"p":"response/content","v":"`+strings.ReplaceAll(tc.payload, `"`, `\"`)+`"}`,
				`data: [DONE]`,
			)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

			h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "use tool"}}, false, false, []string{"Bash"}, nil)

			frames := parseClaudeFrames(t, rec.Body.String())
			foundToolUse := false
			for _, f := range findClaudeFrames(frames, "content_block_start") {
				contentBlock, _ := f.Payload["content_block"].(map[string]any)
				if contentBlock["type"] == "tool_use" {
					foundToolUse = true
					break
				}
			}
			if foundToolUse != tc.wantToolUse {
				t.Fatalf("unexpected tool_use=%v for format %s, body=%s", foundToolUse, tc.name, rec.Body.String())
			}
		})
	}
}

func TestHandleClaudeStreamRealtimeDetectsToolUseWithLeadingProse(t *testing.T) {
	h := &Handler{}
	payload := "I'll call a tool now.\\n<tool_calls><invoke name=\\\"write_file\\\"><parameter name=\\\"path\\\">/tmp/a.txt</parameter><parameter name=\\\"content\\\">abc</parameter></invoke></tool_calls>"
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"`+payload+`"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "use tool"}}, false, false, []string{"write_file"}, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	foundToolUse := false
	for _, f := range findClaudeFrames(frames, "content_block_start") {
		contentBlock, _ := f.Payload["content_block"].(map[string]any)
		if contentBlock["type"] == "tool_use" && contentBlock["name"] == "write_file" {
			foundToolUse = true
			break
		}
	}
	if !foundToolUse {
		t.Fatalf("expected tool_use block with leading prose payload, body=%s", rec.Body.String())
	}

	for _, f := range findClaudeFrames(frames, "message_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["stop_reason"] == "tool_use" {
			return
		}
	}
	t.Fatalf("expected stop_reason=tool_use, body=%s", rec.Body.String())
}

func TestHandleClaudeStreamRealtimeIgnoresUnclosedFencedToolExample(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		"data: {\"p\":\"response/content\",\"v\":\"Here is an example:\\n```json\\n{\\\"tool_calls\\\":[{\\\"name\\\":\\\"Bash\\\",\\\"input\\\":{\\\"command\\\":\\\"pwd\\\"}}]}\"}",
		"data: {\"p\":\"response/content\",\"v\":\"\\n```\\nDo not execute it.\"}",
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "show example only"}}, false, false, []string{"Bash"}, nil)

	frames := parseClaudeFrames(t, rec.Body.String())
	foundToolUse := false
	for _, f := range findClaudeFrames(frames, "content_block_start") {
		contentBlock, _ := f.Payload["content_block"].(map[string]any)
		if contentBlock["type"] == "tool_use" {
			foundToolUse = true
			break
		}
	}
	if foundToolUse {
		t.Fatalf("expected no tool_use for fenced example, body=%s", rec.Body.String())
	}

	foundToolStop := false
	for _, f := range findClaudeFrames(frames, "message_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["stop_reason"] == "tool_use" {
			foundToolStop = true
			break
		}
	}
	if foundToolStop {
		t.Fatalf("expected stop_reason to remain content-only, body=%s", rec.Body.String())
	}
}

// Backward-compatible alias for historical test name used in CI logs.
func TestHandleClaudeStreamRealtimePromotesUnclosedFencedToolExample(t *testing.T) {
	TestHandleClaudeStreamRealtimeIgnoresUnclosedFencedToolExample(t)
}

func TestHandleClaudeStreamRealtimeNormalizesToolInputBySchema(t *testing.T) {
	h := &Handler{}
	resp := makeClaudeSSEHTTPResponse(
		`data: {"p":"response/content","v":"<tool_calls><invoke name=\"Write\">{\"input\":{\"content\":{\"message\":\"hi\"},\"taskId\":1}}</invoke></tool_calls>"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	toolsRaw := []any{
		map[string]any{
			"name": "Write",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
					"taskId":  map[string]any{"type": "string"},
				},
			},
		},
	}

	h.handleClaudeStreamRealtime(rec, req, resp, "claude-sonnet-4-5", []any{map[string]any{"role": "user", "content": "write"}}, false, false, []string{"Write"}, toolsRaw)

	frames := parseClaudeFrames(t, rec.Body.String())
	for _, f := range findClaudeFrames(frames, "content_block_delta") {
		delta, _ := f.Payload["delta"].(map[string]any)
		if delta["type"] != "input_json_delta" {
			continue
		}
		partial := asString(delta["partial_json"])
		var args map[string]any
		if err := json.Unmarshal([]byte(partial), &args); err != nil {
			t.Fatalf("decode partial_json failed: %v payload=%s", err, partial)
		}
		if args["content"] != `{"message":"hi"}` {
			t.Fatalf("expected content normalized to string, got %#v", args["content"])
		}
		if args["taskId"] != "1" {
			t.Fatalf("expected taskId normalized to string, got %#v", args["taskId"])
		}
		return
	}
	t.Fatalf("expected input_json_delta frame, body=%s", rec.Body.String())
}
