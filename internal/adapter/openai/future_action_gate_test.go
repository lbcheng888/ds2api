package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/util"
)

func TestHandleNonStreamRejectsFutureActionWithoutToolCall(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/content","v":"继续推进剩余的审查建议。先并行读取需修改的文件。"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-future-no-tool", "deepseek-chat", "prompt", false, false, []string{"Read"}, readToolTestSchemas, util.DefaultToolChoicePolicy(), false, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	errObj, _ := out["error"].(map[string]any)
	if asString(errObj["code"]) != upstreamMissingToolCallCode {
		t.Fatalf("expected code=%s, got %#v", upstreamMissingToolCallCode, out)
	}
}

func TestChatStreamRejectsFutureActionWithoutToolCall(t *testing.T) {
	rec := httptest.NewRecorder()
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		false,
		"cid-stream-future-no-tool",
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
	runtime.text.WriteString("继续推进剩余的审查建议。先并行读取需修改的文件。")

	runtime.finalize("stop")

	frames, done := parseSSEDataFrames(t, rec.Body.String())
	if !done {
		t.Fatalf("expected [DONE], body=%s", rec.Body.String())
	}
	if len(frames) != 1 {
		t.Fatalf("expected only error frame, got %#v body=%s", frames, rec.Body.String())
	}
	errObj, _ := frames[0]["error"].(map[string]any)
	if asString(errObj["code"]) != upstreamMissingToolCallCode {
		t.Fatalf("expected code=%s, got frames=%#v body=%s", upstreamMissingToolCallCode, frames, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"content"`) {
		t.Fatalf("future-action content should not be emitted as normal content, body=%s", rec.Body.String())
	}
}

func TestFutureActionGateAllowsFinalSummary(t *testing.T) {
	if _, _, _, ok := futureActionMissingToolCallDetail("已完成 4 项，剩余建议见上文。", []string{"Read"}, readToolTestSchemas, false); ok {
		t.Fatalf("expected concise final summary not to be rejected")
	}
}

func TestHandleNonStreamRejectsMalformedToolCallBlock(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		sseContentLine("3 of 4 background agents have completed. Let me verify the edits.\n\n<tool_calls>\n<tool_calls>\n<parameter name=\"file_path\">/tmp/a.txt</parameter>\n</tool_calls>"),
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-invalid-tool", "deepseek-chat", "prompt", false, false, []string{"Read"}, readToolTestSchemas, util.DefaultToolChoicePolicy(), false, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	errObj, _ := out["error"].(map[string]any)
	if asString(errObj["code"]) != upstreamInvalidToolCallCode {
		t.Fatalf("expected code=%s, got %#v", upstreamInvalidToolCallCode, out)
	}
}

func sseContentLine(text string) string {
	b, _ := json.Marshal(map[string]any{"p": "response/content", "v": text})
	return "data: " + string(b)
}

func TestChatStreamRejectsMalformedToolCallBlock(t *testing.T) {
	rec := httptest.NewRecorder()
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		false,
		"cid-stream-invalid-tool",
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
	runtime.text.WriteString("3 of 4 background agents have completed.\n\n<tool_calls>\n<tool_calls>\n<parameter name=\"file_path\">/tmp/a.txt</parameter>\n</tool_calls>")

	runtime.finalize("stop")

	frames, done := parseSSEDataFrames(t, rec.Body.String())
	if !done {
		t.Fatalf("expected [DONE], body=%s", rec.Body.String())
	}
	if len(frames) != 1 {
		t.Fatalf("expected only error frame, got %#v body=%s", frames, rec.Body.String())
	}
	errObj, _ := frames[0]["error"].(map[string]any)
	if asString(errObj["code"]) != upstreamInvalidToolCallCode {
		t.Fatalf("expected code=%s, got frames=%#v body=%s", upstreamInvalidToolCallCode, frames, rec.Body.String())
	}
}

func TestChatStreamRejectsOversizedBufferedToolCall(t *testing.T) {
	rec := httptest.NewRecorder()
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		false,
		"cid-stream-tool-too-large",
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
		32,
	)
	runtime.text.WriteString(`<tool_calls><tool_call><tool_name>Read</tool_name><parameters><file_path>/tmp/` + strings.Repeat("a", 80) + `</file_path></parameters></tool_call></tool_calls>`)

	runtime.finalize("stop")

	_, done := parseSSEDataFrames(t, rec.Body.String())
	if !done {
		t.Fatalf("expected [DONE], body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), upstreamToolCallTooLargeCode) {
		t.Fatalf("expected oversized tool-call error, body=%s", rec.Body.String())
	}
}
