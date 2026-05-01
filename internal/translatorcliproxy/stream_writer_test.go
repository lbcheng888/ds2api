package translatorcliproxy

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

func TestOpenAIStreamTranslatorWriterClaude(t *testing.T) {
	original := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	translated := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":29,\"total_tokens\":40}}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Fatalf("expected claude message_start event, got: %s", body)
	}
	if !strings.Contains(body, `"output_tokens":29`) {
		t.Fatalf("expected claude stream usage to preserve output tokens, got: %s", body)
	}
}

func TestOpenAIStreamTranslatorWriterClaudeEstimatesMissingUsage(t *testing.T) {
	original := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"review the repo"}],"stream":true}`)
	translated := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"review the repo"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if strings.Contains(body, `"input_tokens":0`) {
		t.Fatalf("expected estimated input usage instead of zero, got: %s", body)
	}
	if !strings.Contains(body, `"output_tokens":1`) {
		t.Fatalf("expected estimated output usage on message_delta, got: %s", body)
	}
}

func TestOpenAIStreamTranslatorWriterClaudeToolUseStreamsInputJSONDelta(t *testing.T) {
	original := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Task","input_schema":{"type":"object","properties":{"description":{"type":"string"},"prompt":{"type":"string"},"subagent_type":{"type":"string"}},"required":["description","prompt","subagent_type"]}}],"stream":true}`)
	translated := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"Task\",\"arguments\":\"{\\\"description\\\":\\\"Explore\\\",\\\"prompt\\\":\\\"Inspect files\\\",\\\"subagent_type\\\":\\\"general\\\"}\"}}]},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"tool_use"`) {
		t.Fatalf("expected Claude tool_use event, got: %s", body)
	}
	if !strings.Contains(body, `"type":"input_json_delta"`) {
		t.Fatalf("expected Claude tool_use input to be streamed as input_json_delta, got: %s", body)
	}
	if !strings.Contains(body, `"input":{}`) {
		t.Fatalf("expected Claude tool_use start block to use official empty input, got: %s", body)
	}
	if !strings.Contains(body, `\"description\":\"Explore\"`) || !strings.Contains(body, `\"prompt\":\"Inspect files\"`) || !strings.Contains(body, `\"subagent_type\":\"general\"`) {
		t.Fatalf("expected tool_use input_json_delta to include required fields, got: %s", body)
	}
}

func TestOpenAIStreamTranslatorWriterClaudeAgentUsesInputDelta(t *testing.T) {
	original := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Agent","input_schema":{"type":"object","properties":{"description":{"type":"string"},"prompt":{"type":"string"},"subagent_type":{"type":"string"}},"required":["description","prompt"]}}],"stream":true}`)
	translated := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_agent\",\"type\":\"function\",\"function\":{\"name\":\"Agent\",\"arguments\":\"{\\\"description\\\":\\\"Explore Cheng\\\",\\\"prompt\\\":\\\"Inspect repository\\\",\\\"subagent_type\\\":\\\"Explore\\\"}\"}}]},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"claude-sonnet-4-5\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, `"name":"Agent"`) || !strings.Contains(body, `"type":"input_json_delta"`) {
		t.Fatalf("expected Agent tool_use with input_json_delta, got: %s", body)
	}
	if !strings.Contains(body, `\"subagent_type\":\"Explore\"`) {
		t.Fatalf("expected Agent subagent_type in input_json_delta, got: %s", body)
	}
}

func TestOpenAIStreamTranslatorWriterGemini(t *testing.T) {
	original := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	translated := []byte(`{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatGemini, "gemini-2.5-pro", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gemini-2.5-pro\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
	_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gemini-2.5-pro\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":29,\"total_tokens\":40}}\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "candidates") {
		t.Fatalf("expected gemini stream payload, got: %s", body)
	}
	if !strings.Contains(body, `"promptTokenCount":11`) || !strings.Contains(body, `"candidatesTokenCount":29`) {
		t.Fatalf("expected gemini stream usageMetadata to preserve usage, got: %s", body)
	}
}

func TestOpenAIStreamTranslatorWriterPreservesKeepAliveComment(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatGemini, "gemini-2.5-pro", []byte(`{}`), []byte(`{}`))
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(": keep-alive\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") {
		t.Fatalf("expected keep-alive comment passthrough, got %q", body)
	}
}

func TestOpenAIStreamTranslatorWriterClaudeTranslatesOpenAIErrorChunk(t *testing.T) {
	original := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	translated := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	rec := httptest.NewRecorder()
	w := NewOpenAIStreamTranslatorWriter(rec, sdktranslator.FormatClaude, "claude-sonnet-4-5", original, translated)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(`data: {"status_code":429,"error":{"message":"Upstream model returned empty output.","type":"rate_limit_error","code":"upstream_empty_output"}}` + "\n\n"))
	_, _ = w.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected Claude error event, got: %s", body)
	}
	if !strings.Contains(body, "Upstream model returned empty output.") {
		t.Fatalf("expected upstream message in Claude error event, got: %s", body)
	}
}

func TestInjectStreamUsageMetadataPreservesSSEFrameTerminator(t *testing.T) {
	chunk := []byte("data: {\"candidates\":[{\"index\":0}],\"model\":\"gemini-2.5-pro\"}\n\n")
	usage := openAIUsage{PromptTokens: 11, CompletionTokens: 29, TotalTokens: 40}
	got := injectStreamUsageMetadata(chunk, sdktranslator.FormatGemini, usage)
	if !strings.HasSuffix(string(got), "\n\n") {
		t.Fatalf("expected injected chunk to preserve \\n\\n frame terminator, got %q", string(got))
	}
	if !strings.Contains(string(got), `"usageMetadata"`) {
		t.Fatalf("expected usageMetadata injected, got %q", string(got))
	}
}

func TestStripDSMLFromTranslatedChunkStripsContentBlockDeltaText(t *testing.T) {
	// Simulates a Claude SSE content_block_delta with DSML in text_delta
	chunk := []byte(`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"visible text\n<|DSML|tool_calls>\n<|DSML|invoke name=\"Bash\">\n<|DSML|parameter name=\"command\" string=\"true\">echo hi</|DSML|parameter>\n</|DSML|invoke>\n</|DSML|tool_calls>"}}

`)
	got := stripDSMLFromTranslatedChunk(chunk)
	if bytes.Contains(got, []byte("DSML")) || bytes.Contains(got, []byte("tool_calls")) || bytes.Contains(got, []byte("invoke")) {
		t.Fatalf("DSML not stripped from text_delta: %s", string(got))
	}
	if !bytes.Contains(got, []byte("visible text")) {
		t.Fatalf("visible text lost: %s", string(got))
	}
}

func TestStripDSMLFromTranslatedChunkPreservesToolUse(t *testing.T) {
	// tool_use chunks should NOT be modified
	chunk := []byte(`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}}

`)
	got := stripDSMLFromTranslatedChunk(chunk)
	if !bytes.Equal(chunk, got) {
		t.Fatalf("tool_use chunk was modified: %s", string(got))
	}
}

func TestStripDSMLFromTranslatedChunkNoDSMLPassthrough(t *testing.T) {
	chunk := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"normal text\"}}\n\n")
	got := stripDSMLFromTranslatedChunk(chunk)
	if !bytes.Equal(chunk, got) {
		t.Fatalf("normal chunk was modified: %s", string(got))
	}
}

func TestExtractOpenAIUsageSupportsResponsesUsageFields(t *testing.T) {
	line := []byte(`data: {"usage":{"input_tokens":"11","output_tokens":"29","total_tokens":"40"}}`)
	got, ok := extractOpenAIUsage(line)
	if !ok {
		t.Fatal("expected usage extracted from input/output usage fields")
	}
	if got.PromptTokens != 11 || got.CompletionTokens != 29 || got.TotalTokens != 40 {
		t.Fatalf("unexpected usage extracted: %#v", got)
	}
}
