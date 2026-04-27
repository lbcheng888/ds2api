package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/devcapture"
	"ds2api/internal/toolcall"
	"ds2api/internal/util"
)

func makeSSEHTTPResponse(lines ...string) *http.Response {
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

func TestFormatFinalStreamToolCallsDropsSchemaInvalidCall(t *testing.T) {
	schemas := toolcall.ParameterSchemas{
		"task": {
			"type": "object",
			"properties": map[string]any{
				"description":   map[string]any{"type": "string"},
				"prompt":        map[string]any{"type": "string"},
				"subagent_type": map[string]any{"type": "string"},
			},
			"required": []any{"description", "prompt", "subagent_type"},
		},
	}
	calls := []toolcall.ParsedToolCall{{Name: "task", Input: map[string]any{}}}
	if got := formatFinalStreamToolCallsWithStableIDs(calls, nil, schemas, false); len(got) != 0 {
		t.Fatalf("expected invalid task call to be dropped, got %#v", got)
	}
}

func TestFormatFinalStreamToolCallsAcceptsRooInvokeTaskParameters(t *testing.T) {
	schemas := toolcall.ParameterSchemas{
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
	raw := `<tool_calls>
<invoke name="task">
<parameter name="description" string="true">审查cheng语言代码结构</parameter>
<parameter name="prompt" string="true">探索 /Users/lbcheng/cheng-lang 项目的完整目录结构。</parameter>
<parameter name="subagent_type" string="true">explore</parameter>
<parameter name="max_retries" string="false">2</parameter>
</invoke>
</tool_calls>`
	calls := toolcall.ParseToolCalls(raw, []string{"task"})
	got := formatFinalStreamToolCallsWithStableIDs(calls, nil, schemas, true)
	if len(got) != 1 {
		t.Fatalf("expected one formatted task tool call, got %#v", got)
	}
	fn, _ := got[0]["function"].(map[string]any)
	if fn["name"] != "task" {
		t.Fatalf("expected function name task, got %#v", got[0])
	}
	args, _ := fn["arguments"].(string)
	if !strings.Contains(args, `"description":"审查cheng语言代码结构"`) {
		t.Fatalf("expected description argument, got %s", args)
	}
	if !strings.Contains(args, `"max_retries":2`) {
		t.Fatalf("expected numeric max_retries argument, got %s", args)
	}
}

func TestInjectToolPromptSkipsMetaAgentTools(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "Agent",
				"description": "Launch a subagent",
				"parameters":  map[string]any{"type": "object"},
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "TaskOutput",
				"description": "Fetch subagent output",
				"parameters":  map[string]any{"type": "object"},
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "read",
				"description": "Read a file",
				"parameters":  map[string]any{"type": "object"},
			},
		},
	}
	messages, names := injectToolPrompt([]map[string]any{{"role": "user", "content": "hi"}}, tools, util.ToolChoicePolicy{}, false)
	if len(names) != 1 || names[0] != "read" {
		t.Fatalf("expected only read tool name, got %#v", names)
	}
	system, _ := messages[0]["content"].(string)
	if strings.Contains(system, "Tool: Agent") {
		t.Fatalf("expected Agent tool to be removed from prompt, got %q", system)
	}
	if strings.Contains(system, "Tool: TaskOutput") {
		t.Fatalf("expected TaskOutput tool to be removed from prompt, got %q", system)
	}
	if !strings.Contains(system, "Tool: read") {
		t.Fatalf("expected read tool in prompt, got %q", system)
	}
}

func TestInjectToolPromptAllowsMetaAgentToolsWhenConfigured(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "Agent",
				"description": "Launch a subagent",
				"parameters":  map[string]any{"type": "object"},
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "TaskOutput",
				"description": "Fetch subagent output",
				"parameters":  map[string]any{"type": "object"},
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "TaskCreate",
				"description": "Create UI task",
				"parameters":  map[string]any{"type": "object"},
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "TodoWrite",
				"description": "Update UI todos",
				"parameters":  map[string]any{"type": "object"},
			},
		},
	}
	messages, names := injectToolPrompt([]map[string]any{{"role": "user", "content": "hi"}}, tools, util.ToolChoicePolicy{}, true)
	if len(names) != 2 || names[0] != "Agent" || names[1] != "TaskOutput" {
		t.Fatalf("expected Agent and TaskOutput tool names, got %#v", names)
	}
	system, _ := messages[0]["content"].(string)
	if !strings.Contains(system, "Tool: Agent") {
		t.Fatalf("expected Agent tool in prompt, got %q", system)
	}
	if !strings.Contains(system, "Tool: TaskOutput") {
		t.Fatalf("expected TaskOutput tool in prompt, got %q", system)
	}
	for _, bad := range []string{"Tool: TaskCreate", "Tool: TodoWrite"} {
		if strings.Contains(system, bad) {
			t.Fatalf("expected task-tracking tool %s to be removed from prompt, got %q", bad, system)
		}
	}
}

func TestInjectToolPromptSortsToolsForStableCachePrefix(t *testing.T) {
	tools := []any{
		map[string]any{"type": "function", "function": map[string]any{"name": "zeta", "description": "Z", "parameters": map[string]any{"type": "object"}}},
		map[string]any{"type": "function", "function": map[string]any{"name": "alpha", "description": "A", "parameters": map[string]any{"type": "object"}}},
	}
	messages, names := injectToolPrompt([]map[string]any{{"role": "user", "content": "hi"}}, tools, util.ToolChoicePolicy{}, false)
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zeta" {
		t.Fatalf("expected stable sorted tool names, got %#v", names)
	}
	system, _ := messages[0]["content"].(string)
	if strings.Index(system, "Tool: alpha") > strings.Index(system, "Tool: zeta") {
		t.Fatalf("expected alpha before zeta in prompt, got %q", system)
	}
}

func decodeJSONBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode json failed: %v, body=%s", err, body)
	}
	return out
}

func parseSSEDataFrames(t *testing.T, body string) ([]map[string]any, bool) {
	t.Helper()
	lines := strings.Split(body, "\n")
	frames := make([]map[string]any, 0, len(lines))
	done := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			done = true
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			t.Fatalf("decode sse frame failed: %v, payload=%s", err, payload)
		}
		frames = append(frames, frame)
	}
	return frames, done
}

func streamHasToolCallsDelta(frames []map[string]any) bool {
	for _, frame := range frames {
		choices, _ := frame["choices"].([]any)
		for _, item := range choices {
			choice, _ := item.(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			if _, ok := delta["tool_calls"]; ok {
				return true
			}
		}
	}
	return false
}

func streamFinishReason(frames []map[string]any) string {
	for _, frame := range frames {
		choices, _ := frame["choices"].([]any)
		for _, item := range choices {
			choice, _ := item.(map[string]any)
			if reason, ok := choice["finish_reason"].(string); ok && reason != "" {
				return reason
			}
		}
	}
	return ""
}

// Backward-compatible alias for historical test name used in CI logs.
func TestHandleNonStreamReturns429WhenUpstreamOutputEmpty(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/content","v":""}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-empty", "deepseek-chat", "prompt", false, false, nil, nil, util.DefaultToolChoicePolicy(), false, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429 for empty upstream output, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	errObj, _ := out["error"].(map[string]any)
	if asString(errObj["code"]) != "upstream_empty_output" {
		t.Fatalf("expected code=upstream_empty_output, got %#v", out)
	}
}

func TestHandleNonStreamFailureAnnotatesCaptureChain(t *testing.T) {
	t.Setenv("DS2API_RAW_STREAM_SAMPLE_ROOT", t.TempDir())
	store := devcapture.Global()
	store.Clear()
	defer store.Clear()
	session := store.Start("deepseek_completion", "http://upstream.test/completion", "acc1", map[string]any{"chat_session_id": "cid-empty-capture"})
	if session == nil {
		t.Skip("dev capture disabled")
	}
	body := session.WrapBody(io.NopCloser(strings.NewReader("data: [DONE]\n")), http.StatusOK)
	_, _ = io.ReadAll(body)
	_ = body.Close()

	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/content","v":""}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-empty-capture", "deepseek-chat", "prompt", false, false, nil, nil, util.DefaultToolChoicePolicy(), false, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429 for empty upstream output, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Ds2-Capture-Chain"); got != "session:cid-empty-capture" {
		t.Fatalf("expected capture chain header, got %q", got)
	}
	if got := rec.Header().Get("X-Ds2-Capture-Ids"); got == "" {
		t.Fatalf("expected capture ids header")
	}
	if got := rec.Header().Get("X-Ds2-Failure-Sample-Id"); got == "" {
		t.Fatalf("expected failure sample id header")
	}
	if !strings.Contains(rec.Body.String(), "Raw sample saved:") {
		t.Fatalf("expected error message to include sample id, body=%s", rec.Body.String())
	}
}

func TestHandleNonStreamReturnsContentFilterErrorWhenUpstreamFilteredWithoutOutput(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"code":"content_filter"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-empty-filtered", "deepseek-chat", "prompt", false, false, nil, nil, util.DefaultToolChoicePolicy(), false, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for filtered upstream output, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	errObj, _ := out["error"].(map[string]any)
	if asString(errObj["code"]) != "content_filter" {
		t.Fatalf("expected code=content_filter, got %#v", out)
	}
}

func TestHandleNonStreamReturns429WhenUpstreamHasOnlyThinking(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"Only thinking"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-thinking-only", "deepseek-reasoner", "prompt", true, false, nil, nil, util.DefaultToolChoicePolicy(), false, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429 for thinking-only upstream output, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	errObj, _ := out["error"].(map[string]any)
	if asString(errObj["code"]) != "upstream_empty_output" {
		t.Fatalf("expected code=upstream_empty_output, got %#v", out)
	}
}

func TestHandleNonStreamPromotesVisibleTextAfterReasoningClose(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"internal reasoning</reasoning>visible answer"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-reasoning-close", "deepseek-reasoner", "prompt", true, false, nil, nil, util.DefaultToolChoicePolicy(), false, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 when visible text follows reasoning close, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	choices, _ := out["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("expected one choice, got %#v", out)
	}
	choice, _ := choices[0].(map[string]any)
	msg, _ := choice["message"].(map[string]any)
	if msg["content"] != "visible answer" {
		t.Fatalf("expected visible content to be promoted, got %#v", msg)
	}
}

func TestHandleNonStreamRequiredToolChoiceFailure(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/content","v":"plain text only"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	policy := util.ToolChoicePolicy{
		Mode:    util.ToolChoiceRequired,
		Allowed: map[string]struct{}{"Read": {}},
	}

	h.handleNonStream(rec, resp, "cid-required-tool", "deepseek-chat", "prompt", false, false, []string{"Read"}, readToolTestSchemas, policy, false, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tool_choice_violation") {
		t.Fatalf("expected tool_choice violation body, got %s", rec.Body.String())
	}
}

func TestChatStreamRequiredToolChoiceFailure(t *testing.T) {
	rec := httptest.NewRecorder()
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		false,
		"cid-stream-required-tool",
		1,
		"deepseek-chat",
		"prompt",
		false,
		false,
		false,
		[]string{"Read"},
		readToolTestSchemas,
		false,
		false,
		true,
		false,
		262144,
	)
	runtime.toolChoice = util.ToolChoicePolicy{
		Mode:    util.ToolChoiceRequired,
		Allowed: map[string]struct{}{"Read": {}},
	}
	runtime.text.WriteString("plain text only")

	runtime.finalize("stop")

	body := rec.Body.String()
	if !strings.Contains(body, "tool_choice_violation") {
		t.Fatalf("expected tool_choice violation, body=%s", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Fatalf("expected stream to close, body=%s", body)
	}
}

func TestHandleStreamToolsPlainTextStreamsBeforeFinish(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/content","v":"你好，"}`,
		`data: {"p":"response/content","v":"这是普通文本回复。"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	h.handleStream(rec, req, resp, "cid6", "deepseek-chat", "prompt", false, false, []string{"search"}, nil, util.DefaultToolChoicePolicy(), false, false, nil)

	frames, done := parseSSEDataFrames(t, rec.Body.String())
	if !done {
		t.Fatalf("expected [DONE], body=%s", rec.Body.String())
	}
	if streamHasToolCallsDelta(frames) {
		t.Fatalf("did not expect tool_calls delta for plain text: %s", rec.Body.String())
	}
	content := strings.Builder{}
	for _, frame := range frames {
		choices, _ := frame["choices"].([]any)
		for _, item := range choices {
			choice, _ := item.(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			if c, ok := delta["content"].(string); ok {
				content.WriteString(c)
			}
		}
	}
	if got := content.String(); got == "" {
		t.Fatalf("expected streamed content in tool mode plain text, body=%s", rec.Body.String())
	}
	if streamFinishReason(frames) != "stop" {
		t.Fatalf("expected finish_reason=stop, body=%s", rec.Body.String())
	}
}

func TestHandleStreamIncompleteCapturedToolJSONFlushesAsTextOnFinalize(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/content","v":"{\"tool_calls\":[{\"name\":\"search\""}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	h.handleStream(rec, req, resp, "cid10", "deepseek-chat", "prompt", false, false, []string{"search"}, nil, util.DefaultToolChoicePolicy(), false, false, nil)

	frames, done := parseSSEDataFrames(t, rec.Body.String())
	if !done {
		t.Fatalf("expected [DONE], body=%s", rec.Body.String())
	}
	if streamHasToolCallsDelta(frames) {
		t.Fatalf("did not expect tool_calls delta for incomplete json, body=%s", rec.Body.String())
	}
	content := strings.Builder{}
	for _, frame := range frames {
		choices, _ := frame["choices"].([]any)
		for _, item := range choices {
			choice, _ := item.(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			if c, ok := delta["content"].(string); ok {
				content.WriteString(c)
			}
		}
	}
	if !strings.Contains(strings.ToLower(content.String()), "tool_calls") || !strings.Contains(content.String(), "{") {
		t.Fatalf("expected incomplete capture to flush as plain text instead of stalling, got=%q", content.String())
	}
}

func TestHandleStreamEmitsDistinctToolCallIDsAcrossSeparateToolBlocks(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/content","v":"前置文本\n<tool_calls>\n  <tool_call>\n    <tool_name>read_file</tool_name>\n    <parameters>{\"path\":\"README.MD\"}</parameters>\n  </tool_call>\n</tool_calls>"}`,
		`data: {"p":"response/content","v":"中间文本\n<tool_calls>\n  <tool_call>\n    <tool_name>search</tool_name>\n    <parameters>{\"q\":\"golang\"}</parameters>\n  </tool_call>\n</tool_calls>"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	h.handleStream(rec, req, resp, "cid-multi", "deepseek-chat", "prompt", false, false, []string{"read_file", "search"}, nil, util.DefaultToolChoicePolicy(), false, false, nil)

	frames, done := parseSSEDataFrames(t, rec.Body.String())
	if !done {
		t.Fatalf("expected [DONE], body=%s", rec.Body.String())
	}

	ids := make([]string, 0, 2)
	seen := make(map[string]struct{})
	for _, frame := range frames {
		choices, _ := frame["choices"].([]any)
		for _, item := range choices {
			choice, _ := item.(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			toolCalls, _ := delta["tool_calls"].([]any)
			for _, rawCall := range toolCalls {
				call, _ := rawCall.(map[string]any)
				id := asString(call["id"])
				if id == "" {
					continue
				}
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}

	if len(ids) != 2 {
		t.Fatalf("expected two distinct tool call ids, got %#v body=%s", ids, rec.Body.String())
	}
	if ids[0] == ids[1] {
		t.Fatalf("expected distinct tool call ids across blocks, got %#v body=%s", ids, rec.Body.String())
	}
}
