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

func TestChatStreamAllowsBackgroundAgentAcknowledgement(t *testing.T) {
	rec := httptest.NewRecorder()
	prompt := "<｜Tool｜>Async agent launched successfully.\nThe agent is working in the background.\n<｜Assistant｜>"
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		false,
		"cid-stream-agent-background",
		1,
		"deepseek-chat",
		prompt,
		false,
		false,
		false,
		[]string{"Agent", "Read"},
		readToolTestSchemas,
		false,
		false,
		true,
		true,
		262144,
	)
	runtime.text.WriteString("核心差距已先确认。等 agent 返回详细差距清单后，我会按依赖关系给出具体复用路径。")

	runtime.finalize("stop")

	body := rec.Body.String()
	if strings.Contains(body, upstreamMissingToolCallCode) {
		t.Fatalf("background agent acknowledgement should not be rejected, body=%s", body)
	}
	if !strings.Contains(body, "具体复用路径") {
		t.Fatalf("expected acknowledgement content, body=%s", body)
	}
}

func TestFutureActionGateAllowsFinalSummary(t *testing.T) {
	if _, _, _, ok := futureActionMissingToolCallDetail("已完成 4 项，剩余建议见上文。", "", []string{"Read"}, readToolTestSchemas, false); ok {
		t.Fatalf("expected concise final summary not to be rejected")
	}
}

func TestFutureActionGateAllowsBackgroundAgentAcknowledgement(t *testing.T) {
	prompt := "<｜Tool｜>Async agent launched successfully.\nThe agent is working in the background.\n<｜Assistant｜>"
	text := "核心差距已先确认。等 agent 返回详细差距清单后，我会按依赖关系给出具体复用路径。"
	if _, _, _, ok := futureActionMissingToolCallDetail(text, prompt, []string{"Agent", "Read"}, readToolTestSchemas, true); ok {
		t.Fatalf("expected background agent acknowledgement not to be rejected")
	}
}

func TestFutureActionGateDoesNotTreatAgentDocsAsLaunch(t *testing.T) {
	prompt := "Tool docs: run_in_background can start an agent in the background."
	text := "等 agent 返回详细差距清单后，我会继续分析具体复用路径。"
	if _, _, _, ok := futureActionMissingToolCallDetail(text, prompt, []string{"Agent", "Read"}, readToolTestSchemas, true); !ok {
		t.Fatalf("expected agent docs alone not to suppress missing-tool detection")
	}
}

func TestFutureActionGateRejectsChineseNeedToPatchAfterContinue(t *testing.T) {
	prompt := "<｜User｜>请继续<｜Assistant｜>"
	text := "好，那只需要补 elf_object_linker.cheng。旧 elf_linker.cheng 有 1131 行，依赖 obj_buf、aarch64_enc、linker_shared_core。"
	if _, _, code, ok := futureActionMissingToolCallDetail(text, prompt, []string{"Read", "Edit"}, readToolTestSchemas, false); !ok || code != upstreamMissingToolCallCode {
		t.Fatalf("expected missing-tool detection for unexecuted coding action, ok=%v code=%q", ok, code)
	}
}

func TestFutureActionGateRejectsChineseVisibleFileWritePlan(t *testing.T) {
	prompt := "<｜User｜>用中文回复<｜Assistant｜>"
	text := `ELF RISC-V64 链接器已创建成功，Mach-O x86_64 链接器还不存在。现在创建它。

先读取 /Users/lbcheng/cheng-lang/src/core/backend/elf_object_linker.cheng 完整内容作为缓冲区辅助函数模板，然后读取旧链接器 /Users/lbcheng/cheng-lang-82a81020ddea4757e530a43d0a0bc471dadb7f80/src/backend/obj/macho-linker-x86_64.cheng 完整内容。

关键变更：
- 复制 AArch64 ELF 链接器的所有缓冲区辅助函数
- 不要使用 obj_buf
- 只迁移 API，不改逻辑

写入 /Users/lbcheng/cheng-lang/src/core/backend/macho_x86_64_linker.cheng。如果已存在则覆盖。`
	if _, _, code, ok := futureActionMissingToolCallDetail(text, prompt, []string{"Read", "Write"}, readToolTestSchemas, false); !ok || code != upstreamMissingToolCallCode {
		t.Fatalf("expected missing-tool detection for visible file write plan, ok=%v code=%q", ok, code)
	}
}

func TestFutureActionGateAllowsReviewFindingWithoutExecutionRequest(t *testing.T) {
	prompt := "<｜User｜>请审查代码问题<｜Assistant｜>"
	text := "主要问题是需要补 backend_matrix 的测试覆盖。"
	if _, _, _, ok := futureActionMissingToolCallDetail(text, prompt, []string{"Read", "Edit"}, readToolTestSchemas, false); ok {
		t.Fatalf("expected review finding not to be rejected without execution request")
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

func sseTitleLine(text string) string {
	b, _ := json.Marshal(map[string]any{"content": text})
	return "data: " + string(b)
}

func TestChatStreamUsesLateTitleToolCallAfterFinished(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		sseContentLine("Let me start by reading the key files."),
		`data: {"p":"response/status","v":"FINISHED"}`,
		`event: title`,
		sseTitleLine("tool_calls\n<tool_call>\n<tool_name>Read</tool_name>\n<parameter name=\"file_path\" type=\"string\">/tmp/a.txt</parameter>\n<parameter name=\"limit\" type=\"number\">150</parameter>\n"),
		`event: close`,
		`data: {"click_behavior":"none","auto_resume":false}`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	h.handleStream(rec, req, resp, "cid-late-title-tool", "deepseek-chat", "prompt", false, false, []string{"Read"}, readToolTestSchemas, util.DefaultToolChoicePolicy(), false, false, nil)

	body := rec.Body.String()
	if strings.Contains(body, upstreamMissingToolCallCode) {
		t.Fatalf("late title tool call should prevent missing-tool error, body=%s", body)
	}
	if !strings.Contains(body, `"tool_calls"`) || !strings.Contains(body, `"Read"`) {
		t.Fatalf("expected Read tool call from late title, body=%s", body)
	}
}

func TestChatStreamIgnoresTruncatedLateTitleAndReportsMissingTool(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		sseContentLine("一气呵成。先读所有旧文件，然后批量写入。"),
		`data: {"p":"response/status","v":"FINISHED"}`,
		`event: title`,
		sseTitleLine("<tool_calls>\n  <tool_call>\n    <tool_name>Read</tool_name>\n    <parameters>\n      <file_path>/Users/lbcheng/cheng-lang/src/"),
		`event: close`,
		`data: {"click_behavior":"none","auto_resume":false}`,
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	h.handleStream(rec, req, resp, "cid-truncated-late-title", "deepseek-chat", "<｜User｜>请一口气完成<｜Assistant｜>", false, false, []string{"Read"}, readToolTestSchemas, util.DefaultToolChoicePolicy(), false, false, nil)

	body := rec.Body.String()
	if strings.Contains(body, upstreamInvalidToolCallCode) {
		t.Fatalf("truncated late title should not be reported as invalid tool syntax, body=%s", body)
	}
	if !strings.Contains(body, upstreamMissingToolCallCode) {
		t.Fatalf("expected missing-tool error for unexecuted read promise, body=%s", body)
	}
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
