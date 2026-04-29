//go:build legacy_openai_adapter

package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
	"ds2api/internal/util"
)

var taskOutputTestSchemas = toolcall.ParameterSchemas{
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

var readToolTestSchemas = toolcall.ParameterSchemas{
	"Read": {
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{"type": "string"},
		},
		"required": []any{"file_path"},
	},
}

var agentToolTestSchemas = toolcall.ParameterSchemas{
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

func TestSynthesizeTaskOutputToolCallsFromTaskNotification(t *testing.T) {
	prompt := "<｜User｜>older <task-notification><task-id>old</task-id></task-notification><｜Assistant｜>done<｜User｜><task-notification><task_id>task_a</task_id><status>completed</status></task-notification><｜Assistant｜>"
	got := claudecodeharness.SynthesizeTaskOutputToolCallsFromTaskNotification(prompt, []string{"Read", "TaskOutput"}, true)
	if len(got) != 1 {
		t.Fatalf("expected one TaskOutput call, got %#v", got)
	}
	if got[0].Name != "TaskOutput" {
		t.Fatalf("expected TaskOutput, got %#v", got[0])
	}
	if got[0].Input["task_id"] != "task_a" || got[0].Input["block"] != false || got[0].Input["timeout"] != 5000 {
		t.Fatalf("unexpected TaskOutput input: %#v", got[0].Input)
	}
}

func TestSynthesizeTaskOutputRequiresMetaAgentToolsAllowed(t *testing.T) {
	prompt := "<｜User｜><task-notification><task_id>task_a</task_id></task-notification><｜Assistant｜>"
	got := claudecodeharness.SynthesizeTaskOutputToolCallsFromTaskNotification(prompt, []string{"TaskOutput"}, false)
	if len(got) != 0 {
		t.Fatalf("expected no synthetic call when meta tools are disabled, got %#v", got)
	}
}

func TestSynthesizeTaskOutputFromAgentWaitingText(t *testing.T) {
	prompt := `<｜Assistant｜>Task Output(non-blocking) a43c25c4d63ec3d42
Task is still running.
Task Output(non-blocking) ac595bbc81cb03cef
Task is still running.<｜Assistant｜>`
	got := claudecodeharness.SynthesizeTaskOutputToolCallsFromAgentWaiting(prompt, "等待剩余全部代理完成后汇总全部审查结果。", []string{"TaskOutput"}, true)
	if len(got) != 2 {
		t.Fatalf("expected two TaskOutput calls, got %#v", got)
	}
	if got[0].Input["task_id"] != "a43c25c4d63ec3d42" || got[1].Input["task_id"] != "ac595bbc81cb03cef" {
		t.Fatalf("unexpected task ids: %#v", got)
	}
}

func TestSynthesizeTaskOutputFromAgentWaitingPrefersRunningTaskIDs(t *testing.T) {
	prompt := `Task Output(non-blocking) completed_task_1
Done.
Task Output(non-blocking) running_task_2
Task is still running.
Task Output(non-blocking) running_task_3
仍在运行。`
	got := claudecodeharness.SynthesizeTaskOutputToolCallsFromAgentWaiting(prompt, "等待剩余代理完成后汇总。", []string{"TaskOutput"}, true)
	if len(got) != 2 {
		t.Fatalf("expected two running TaskOutput calls, got %#v", got)
	}
	if got[0].Input["task_id"] != "running_task_2" || got[1].Input["task_id"] != "running_task_3" {
		t.Fatalf("unexpected running task ids: %#v", got)
	}
}

func TestSynthesizeTaskOutputFromAgentWaitingDoesNotReuseMissingTaskID(t *testing.T) {
	prompt := `Task Output ae1f00a446213300f
Error: No task found with ID: ae1f00a446213300f`
	got := claudecodeharness.SynthesizeTaskOutputToolCallsFromAgentWaiting(prompt, "等待剩余代理完成后汇总。", []string{"TaskOutput"}, true)
	if len(got) != 0 {
		t.Fatalf("expected no synthetic TaskOutput for missing task id, got %#v", got)
	}
}

func TestInvalidTaskOutputCallDetailRejectsUnknownTaskID(t *testing.T) {
	prompt := `Task Output(non-blocking) a5e2ba8830a7d773d
Task is still running.
Task Output a5e2ba8830a7d773d
Error: No task found with ID: a5e2ba8830a7d773d`
	calls := []toolcall.ParsedToolCall{{
		Name:  "TaskOutput",
		Input: map[string]any{"task_id": "a5e2ba8830a7d773d"},
	}}
	if _, _, code, ok := invalidTaskOutputCallDetail(calls, prompt); !ok || code != upstreamInvalidToolCallCode {
		t.Fatalf("expected unknown TaskOutput to be rejected, ok=%v code=%q", ok, code)
	}
}

func TestInvalidTaskOutputCallDetailAllowsRunningAndNotificationTaskIDs(t *testing.T) {
	prompt := `Task Output(non-blocking) running_task_1
Task is still running.
<｜User｜><task-notification><task_id>done_task_2</task_id><status>completed</status></task-notification><｜Assistant｜>`
	calls := []toolcall.ParsedToolCall{
		{Name: "TaskOutput", Input: map[string]any{"task_id": "running_task_1"}},
		{Name: "TaskOutput", Input: map[string]any{"task_id": "done_task_2"}},
	}
	if _, _, _, ok := invalidTaskOutputCallDetail(calls, prompt); ok {
		t.Fatalf("expected known TaskOutput ids to pass")
	}
}

func TestSynthesizeTaskOutputFromAgentWaitingRequiresExplicitRunningState(t *testing.T) {
	prompt := `Async agent launched successfully.
agentId: a644b314cca1a0eb0`
	got := claudecodeharness.SynthesizeTaskOutputToolCallsFromAgentWaiting(prompt, "等待 agent 返回后汇总。", []string{"TaskOutput"}, true)
	if len(got) != 0 {
		t.Fatalf("expected no synthetic TaskOutput without running state, got %#v", got)
	}
}

func TestSynthesizeTaskOutputFromAgentWaitingDoesNotHideMalformedToolCall(t *testing.T) {
	prompt := `Task Output(non-blocking) a43c25c4d63ec3d42`
	text := "3 of 4 background agents completed.\n\n<tool_calls>\n<tool_calls>"
	got := claudecodeharness.SynthesizeTaskOutputToolCallsFromAgentWaiting(prompt, text, []string{"TaskOutput"}, true)
	if len(got) != 0 {
		t.Fatalf("expected malformed tool syntax to remain visible to invalid-tool gate, got %#v", got)
	}
}

func TestSynthesizeAgentLaunchFromPromise(t *testing.T) {
	prompt := "<｜User｜>请使用 Team Agents 一口气完成多实落地路线和终局愿景<｜Assistant｜>"
	got := claudecodeharness.SynthesizeAgentToolCallsFromLaunchPromise(prompt, "先提交，再启动 Team Agents。", []string{"Read", "Agent"}, true)
	if len(got) != 4 {
		t.Fatalf("expected four Agent calls, got %#v", got)
	}
	for _, call := range got {
		if call.Name != "Agent" {
			t.Fatalf("expected Agent call, got %#v", call)
		}
		if call.Input["description"] == "" || call.Input["prompt"] == "" || call.Input["subagent_type"] == "" {
			t.Fatalf("expected complete Agent input, got %#v", call.Input)
		}
		if call.Input["run_in_background"] != true {
			t.Fatalf("expected background Agent launch, got %#v", call.Input)
		}
	}
}

func TestHandleNonStreamConvertsTeamAgentLaunchPromiseToAgentCalls(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		sseContentLine("先提交当前修复，然后启动 4 个并行代理。"),
		`data: [DONE]`,
	)
	prompt := "<｜User｜>请使用 Team Agents 一口气完成多实落地路线和终局愿景<｜Assistant｜>"
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-agent-launch", "deepseek-chat", prompt, false, false, []string{"Agent", "Read"}, agentToolTestSchemas, util.DefaultToolChoicePolicy(), true, nil, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	choices, _ := out["choices"].([]any)
	choice, _ := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish, got %#v", choice)
	}
	message, _ := choice["message"].(map[string]any)
	toolCalls, _ := message["tool_calls"].([]any)
	if len(toolCalls) != 4 {
		t.Fatalf("expected four Agent tool calls, got %#v", message)
	}
	if strings.Contains(rec.Body.String(), upstreamMissingToolCallCode) {
		t.Fatalf("expected repair instead of missing-tool error, body=%s", rec.Body.String())
	}
}

func TestChatStreamConvertsTeamAgentLaunchPromiseToAgentCalls(t *testing.T) {
	rec := httptest.NewRecorder()
	prompt := "<｜User｜>请使用 Team Agents 一口气完成多实落地路线和终局愿景<｜Assistant｜>"
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		false,
		"cid-stream-agent-launch",
		1,
		"deepseek-chat",
		prompt,
		false,
		false,
		false,
		[]string{"Agent", "Read"},
		agentToolTestSchemas,
		true,
		false,
		true,
		false,
		262144,
	)
	runtime.text.WriteString("先提交，再启动 Team Agents。")

	runtime.finalize("stop")

	frames, done := parseSSEDataFrames(t, rec.Body.String())
	if !done {
		t.Fatalf("expected [DONE], body=%s", rec.Body.String())
	}
	if !streamHasToolCallsDelta(frames) {
		t.Fatalf("expected tool_calls delta, frames=%#v body=%s", frames, rec.Body.String())
	}
	if streamFinishReason(frames) != "tool_calls" {
		t.Fatalf("expected tool_calls finish, got %q body=%s", streamFinishReason(frames), rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), upstreamMissingToolCallCode) {
		t.Fatalf("expected repair instead of missing-tool error, body=%s", rec.Body.String())
	}
}

func TestHandleNonStreamConvertsThinkingOnlyTeamAgentPromiseToAgentCalls(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"先提交，再启动 Team Agents。"}`,
		`data: [DONE]`,
	)
	prompt := "<｜User｜>请使用 Team Agents 一口气完成多实落地路线和终局愿景<｜Assistant｜>"
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-thinking-agent-launch", "deepseek-reasoner", prompt, true, false, []string{"Agent", "Read"}, agentToolTestSchemas, util.DefaultToolChoicePolicy(), true, nil, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tool_calls"`) || !strings.Contains(rec.Body.String(), `"name":"Agent"`) {
		t.Fatalf("expected Agent tool calls, body=%s", rec.Body.String())
	}
}

func TestHandleNonStreamConvertsTaskNotificationThinkingOnlyToTaskOutput(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"I should wait for the completed agent result."}`,
		`data: [DONE]`,
	)
	prompt := "<｜User｜><task-notification><task-id>task_done</task-id><status>completed</status></task-notification><｜Assistant｜>"
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-task-output", "deepseek-reasoner", prompt, true, false, []string{"TaskOutput"}, taskOutputTestSchemas, util.DefaultToolChoicePolicy(), true, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	choices, _ := out["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("expected one choice, got %#v", out)
	}
	choice, _ := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish, got %#v", choice)
	}
	message, _ := choice["message"].(map[string]any)
	toolCalls, _ := message["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", message)
	}
	call, _ := toolCalls[0].(map[string]any)
	fn, _ := call["function"].(map[string]any)
	if fn["name"] != "TaskOutput" {
		t.Fatalf("expected TaskOutput, got %#v", fn)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(asString(fn["arguments"])), &args); err != nil {
		t.Fatalf("decode args: %v, args=%v", err, fn["arguments"])
	}
	if args["task_id"] != "task_done" || args["block"] != false || args["timeout"] != float64(5000) {
		t.Fatalf("unexpected args %#v", args)
	}
}

func TestHandleNonStreamDropsUnknownTaskOutputCall(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		sseContentLine(`<tool_calls><tool_call><tool_name>TaskOutput</tool_name><parameters><task_id>a3505bdfc13bcc88f</task_id></parameters></tool_call></tool_calls>`),
		`data: [DONE]`,
	)
	prompt := `Task Output a3505bdfc13bcc88f
Error: No task found with ID: a3505bdfc13bcc88f`
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-unknown-task-output", "deepseek-chat", prompt, false, false, []string{"TaskOutput"}, taskOutputTestSchemas, util.DefaultToolChoicePolicy(), true, nil, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	choices, _ := out["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("expected one choice, got %#v", out)
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	if !strings.Contains(asString(message["content"]), "Background result unavailable") {
		t.Fatalf("expected unavailable task output notice, got %#v", out)
	}
}

func TestChatStreamConvertsTaskNotificationThinkingOnlyToTaskOutput(t *testing.T) {
	rec := httptest.NewRecorder()
	prompt := "<｜User｜><task-notification><task_id>task_stream</task_id><status>completed</status></task-notification><｜Assistant｜>"
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		false,
		"cid-stream-task-output",
		1,
		"deepseek-reasoner",
		prompt,
		true,
		false,
		false,
		[]string{"TaskOutput"},
		taskOutputTestSchemas,
		true,
		false,
		true,
		false,
		262144,
	)
	runtime.thinking.WriteString("The task completed; retrieve its output.")

	runtime.finalize("stop")

	frames, done := parseSSEDataFrames(t, rec.Body.String())
	if !done {
		t.Fatalf("expected [DONE], body=%s", rec.Body.String())
	}
	if !streamHasToolCallsDelta(frames) {
		t.Fatalf("expected tool_calls delta, frames=%#v body=%s", frames, rec.Body.String())
	}
	if streamFinishReason(frames) != "tool_calls" {
		t.Fatalf("expected tool_calls finish, got %q body=%s", streamFinishReason(frames), rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "TaskOutput") || !strings.Contains(rec.Body.String(), "task_stream") {
		t.Fatalf("expected TaskOutput task id in stream, body=%s", rec.Body.String())
	}
}

func TestHandleNonStreamPromotesThinkingToolCalls(t *testing.T) {
	h := &Handler{}
	resp := makeSSEHTTPResponse(
		`data: {"p":"response/thinking_content","v":"<tool_calls>\n  <tool_call>\n    <tool_name>Read</tool_name>\n    <parameters>\n      <file_path>/tmp/a.txt</file_path>\n    </parameters>\n  </tool_call>\n</tool_calls>"}`,
		`data: [DONE]`,
	)
	rec := httptest.NewRecorder()

	h.handleNonStream(rec, resp, "cid-thinking-tool", "deepseek-reasoner", "<｜User｜>continue<｜Assistant｜>", true, false, []string{"Read"}, readToolTestSchemas, util.DefaultToolChoicePolicy(), false, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	out := decodeJSONBody(t, rec.Body.String())
	choices, _ := out["choices"].([]any)
	choice, _ := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish, got %#v", choice)
	}
	message, _ := choice["message"].(map[string]any)
	toolCalls, _ := message["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("expected one promoted tool call, got %#v", message)
	}
	if !strings.Contains(rec.Body.String(), `"name":"Read"`) || !strings.Contains(rec.Body.String(), `/tmp/a.txt`) {
		t.Fatalf("expected Read tool call in response, body=%s", rec.Body.String())
	}
}

func TestChatStreamPromotesThinkingToolCalls(t *testing.T) {
	rec := httptest.NewRecorder()
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		false,
		"cid-stream-thinking-tool",
		1,
		"deepseek-reasoner",
		"<｜User｜>continue<｜Assistant｜>",
		true,
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
	runtime.thinking.WriteString("<tool_calls><tool_call><tool_name>Read</tool_name><parameters><file_path>/tmp/a.txt</file_path></parameters></tool_call></tool_calls>")

	runtime.finalize("stop")

	frames, done := parseSSEDataFrames(t, rec.Body.String())
	if !done {
		t.Fatalf("expected [DONE], body=%s", rec.Body.String())
	}
	if !streamHasToolCallsDelta(frames) {
		t.Fatalf("expected tool_calls delta, frames=%#v body=%s", frames, rec.Body.String())
	}
	if streamFinishReason(frames) != "tool_calls" {
		t.Fatalf("expected tool_calls finish, got %q body=%s", streamFinishReason(frames), rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"Read"`) || !strings.Contains(rec.Body.String(), `/tmp/a.txt`) {
		t.Fatalf("expected Read tool call in stream, body=%s", rec.Body.String())
	}
}
