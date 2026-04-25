package translatorcliproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

// OpenAIStreamTranslatorWriter translates OpenAI SSE output to another client format in real-time.
type OpenAIStreamTranslatorWriter struct {
	dst           http.ResponseWriter
	target        sdktranslator.Format
	model         string
	originalReq   []byte
	translatedReq []byte
	param         any
	statusCode    int
	headersSent   bool
	lineBuf       bytes.Buffer

	claudeMessageStarted bool
	claudeToolBuffers    map[int]*bufferedClaudeToolCall
}

type bufferedClaudeToolCall struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func NewOpenAIStreamTranslatorWriter(dst http.ResponseWriter, target sdktranslator.Format, model string, originalReq, translatedReq []byte) *OpenAIStreamTranslatorWriter {
	return &OpenAIStreamTranslatorWriter{
		dst:           dst,
		target:        target,
		model:         model,
		originalReq:   originalReq,
		translatedReq: translatedReq,
		statusCode:    http.StatusOK,
	}
}

func (w *OpenAIStreamTranslatorWriter) Header() http.Header {
	return w.dst.Header()
}

func (w *OpenAIStreamTranslatorWriter) WriteHeader(statusCode int) {
	if w.headersSent {
		return
	}
	w.statusCode = statusCode
	w.headersSent = true
	w.dst.WriteHeader(statusCode)
}

func (w *OpenAIStreamTranslatorWriter) Write(p []byte) (int, error) {
	if !w.headersSent {
		w.WriteHeader(http.StatusOK)
	}
	if w.statusCode < 200 || w.statusCode >= 300 {
		return w.dst.Write(p)
	}
	w.lineBuf.Write(p)
	for {
		line, ok := w.readOneLine()
		if !ok {
			break
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if bytes.HasPrefix(trimmed, []byte(":")) {
			if _, err := w.dst.Write(trimmed); err != nil {
				return len(p), err
			}
			if _, err := w.dst.Write([]byte("\n\n")); err != nil {
				return len(p), err
			}
			if f, ok := w.dst.(http.Flusher); ok {
				f.Flush()
			}
			continue
		}
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		if w.writeTranslatedErrorIfPresent(trimmed) {
			continue
		}
		if w.target == sdktranslator.FormatClaude {
			if w.bufferClaudeToolCallChunk(trimmed) {
				continue
			}
			if w.flushClaudeToolCallsBeforeFinish(trimmed) {
				if f, ok := w.dst.(http.Flusher); ok {
					f.Flush()
				}
			}
		}
		usage, hasUsage := extractOpenAIUsage(trimmed)
		chunks := sdktranslator.TranslateStream(context.Background(), sdktranslator.FormatOpenAI, w.target, w.model, w.originalReq, w.translatedReq, trimmed, &w.param)
		if hasUsage {
			for i := range chunks {
				chunks[i] = injectStreamUsageMetadata(chunks[i], w.target, usage)
			}
		}
		for i := range chunks {
			if len(chunks[i]) == 0 {
				continue
			}
			if w.target == sdktranslator.FormatClaude && bytes.Contains(chunks[i], []byte("event: message_start")) {
				w.claudeMessageStarted = true
			}
			if _, err := w.dst.Write(chunks[i]); err != nil {
				return len(p), err
			}
			if !bytes.HasSuffix(chunks[i], []byte("\n")) {
				if _, err := w.dst.Write([]byte("\n")); err != nil {
					return len(p), err
				}
			}
		}
		if f, ok := w.dst.(http.Flusher); ok {
			f.Flush()
		}
	}
	return len(p), nil
}

func (w *OpenAIStreamTranslatorWriter) bufferClaudeToolCallChunk(line []byte) bool {
	payload := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return false
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return false
	}
	choices, _ := body["choices"].([]any)
	hasToolCall := false
	for _, choiceRaw := range choices {
		choice, _ := choiceRaw.(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		toolCalls, _ := delta["tool_calls"].([]any)
		for _, tcRaw := range toolCalls {
			tc, _ := tcRaw.(map[string]any)
			index := toInt(tc["index"])
			if index < 0 {
				index = 0
			}
			if w.claudeToolBuffers == nil {
				w.claudeToolBuffers = map[int]*bufferedClaudeToolCall{}
			}
			buf := w.claudeToolBuffers[index]
			if buf == nil {
				buf = &bufferedClaudeToolCall{}
				w.claudeToolBuffers[index] = buf
			}
			if id, _ := tc["id"].(string); strings.TrimSpace(id) != "" {
				buf.ID = strings.TrimSpace(id)
			}
			fn, _ := tc["function"].(map[string]any)
			if name, _ := fn["name"].(string); strings.TrimSpace(name) != "" {
				buf.Name = strings.TrimSpace(name)
			}
			if args, _ := fn["arguments"].(string); args != "" {
				buf.Arguments.WriteString(args)
			}
			hasToolCall = true
		}
	}
	return hasToolCall
}

func (w *OpenAIStreamTranslatorWriter) flushClaudeToolCallsBeforeFinish(line []byte) bool {
	if len(w.claudeToolBuffers) == 0 {
		return false
	}
	payload := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return w.flushClaudeToolCalls()
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return false
	}
	choices, _ := body["choices"].([]any)
	for _, choiceRaw := range choices {
		choice, _ := choiceRaw.(map[string]any)
		finishReason, _ := choice["finish_reason"].(string)
		if strings.TrimSpace(finishReason) == "tool_calls" {
			return w.flushClaudeToolCalls()
		}
	}
	return false
}

func (w *OpenAIStreamTranslatorWriter) flushClaudeToolCalls() bool {
	if len(w.claudeToolBuffers) == 0 {
		return false
	}
	if !w.claudeMessageStarted {
		w.writeClaudeMessageStart()
	}
	indexes := make([]int, 0, len(w.claudeToolBuffers))
	for idx := range w.claudeToolBuffers {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	for _, idx := range indexes {
		tc := w.claudeToolBuffers[idx]
		if tc == nil || strings.TrimSpace(tc.Name) == "" {
			continue
		}
		input := map[string]any{}
		args := strings.TrimSpace(tc.Arguments.String())
		if args != "" {
			_ = json.Unmarshal([]byte(args), &input)
		}
		id := strings.TrimSpace(tc.ID)
		if id == "" {
			id = "toolu_" + strconv.Itoa(idx)
		}
		w.writeClaudeContentBlockStart(idx, id, strings.TrimSpace(tc.Name), map[string]any{})
		w.writeClaudeInputJSONDelta(idx, input)
		w.writeClaudeContentBlockStop(idx)
	}
	w.claudeToolBuffers = nil
	return true
}

func (w *OpenAIStreamTranslatorWriter) writeClaudeMessageStart() {
	payload, _ := json.Marshal(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            "",
			"type":          "message",
			"role":          "assistant",
			"model":         w.model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	})
	_, _ = w.dst.Write([]byte("event: message_start\n"))
	_, _ = w.dst.Write([]byte("data: "))
	_, _ = w.dst.Write(payload)
	_, _ = w.dst.Write([]byte("\n\n"))
	w.claudeMessageStarted = true
}

func (w *OpenAIStreamTranslatorWriter) writeClaudeContentBlockStart(index int, id, name string, input map[string]any) {
	payload, _ := json.Marshal(map[string]any{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]any{
			"type":  "tool_use",
			"id":    id,
			"name":  name,
			"input": input,
		},
	})
	_, _ = w.dst.Write([]byte("event: content_block_start\n"))
	_, _ = w.dst.Write([]byte("data: "))
	_, _ = w.dst.Write(payload)
	_, _ = w.dst.Write([]byte("\n\n"))
}

func (w *OpenAIStreamTranslatorWriter) writeClaudeInputJSONDelta(index int, input map[string]any) {
	inputBytes, _ := json.Marshal(input)
	payload, _ := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": string(inputBytes),
		},
	})
	_, _ = w.dst.Write([]byte("event: content_block_delta\n"))
	_, _ = w.dst.Write([]byte("data: "))
	_, _ = w.dst.Write(payload)
	_, _ = w.dst.Write([]byte("\n\n"))
}

func (w *OpenAIStreamTranslatorWriter) writeClaudeContentBlockStop(index int) {
	payload, _ := json.Marshal(map[string]any{
		"type":  "content_block_stop",
		"index": index,
	})
	_, _ = w.dst.Write([]byte("event: content_block_stop\n"))
	_, _ = w.dst.Write([]byte("data: "))
	_, _ = w.dst.Write(payload)
	_, _ = w.dst.Write([]byte("\n\n"))
}

func (w *OpenAIStreamTranslatorWriter) writeTranslatedErrorIfPresent(line []byte) bool {
	payload := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return false
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return false
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok || len(errObj) == 0 {
		return false
	}
	message, _ := errObj["message"].(string)
	if strings.TrimSpace(message) == "" {
		message = "Upstream model returned an error."
	}
	switch w.target {
	case sdktranslator.FormatClaude:
		w.writeClaudeErrorEvent(message)
		return true
	default:
		return false
	}
}

func (w *OpenAIStreamTranslatorWriter) writeClaudeErrorEvent(message string) {
	payload, _ := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "api_error",
			"message": message,
		},
	})
	_, _ = w.dst.Write([]byte("event: error\n"))
	_, _ = w.dst.Write([]byte("data: "))
	_, _ = w.dst.Write(payload)
	_, _ = w.dst.Write([]byte("\n\n"))
	if f, ok := w.dst.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *OpenAIStreamTranslatorWriter) Flush() {
	if f, ok := w.dst.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *OpenAIStreamTranslatorWriter) Unwrap() http.ResponseWriter {
	return w.dst
}

func (w *OpenAIStreamTranslatorWriter) readOneLine() ([]byte, bool) {
	b := w.lineBuf.Bytes()
	idx := bytes.IndexByte(b, '\n')
	if idx < 0 {
		return nil, false
	}
	line := append([]byte(nil), b[:idx]...)
	w.lineBuf.Next(idx + 1)
	return line, true
}

type openAIUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func extractOpenAIUsage(line []byte) (openAIUsage, bool) {
	raw := strings.TrimSpace(strings.TrimPrefix(string(line), "data:"))
	if raw == "" || raw == "[DONE]" {
		return openAIUsage{}, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return openAIUsage{}, false
	}
	usageObj, _ := payload["usage"].(map[string]any)
	if usageObj == nil {
		return openAIUsage{}, false
	}
	p := toInt(usageObj["prompt_tokens"])
	c := toInt(usageObj["completion_tokens"])
	t := toInt(usageObj["total_tokens"])
	if p <= 0 {
		p = toInt(usageObj["input_tokens"])
	}
	if c <= 0 {
		c = toInt(usageObj["output_tokens"])
	}
	if p <= 0 && c <= 0 && t <= 0 {
		return openAIUsage{}, false
	}
	if t <= 0 {
		t = p + c
	}
	return openAIUsage{PromptTokens: p, CompletionTokens: c, TotalTokens: t}, true
}

func injectStreamUsageMetadata(chunk []byte, target sdktranslator.Format, usage openAIUsage) []byte {
	if target != sdktranslator.FormatGemini {
		return chunk
	}
	suffix := ""
	switch {
	case bytes.HasSuffix(chunk, []byte("\n\n")):
		suffix = "\n\n"
	case bytes.HasSuffix(chunk, []byte("\n")):
		suffix = "\n"
	}
	text := strings.TrimSpace(string(chunk))
	if text == "" {
		return chunk
	}
	var (
		hasDataPrefix bool
		jsonText      = text
	)
	if strings.HasPrefix(jsonText, "data:") {
		hasDataPrefix = true
		jsonText = strings.TrimSpace(strings.TrimPrefix(jsonText, "data:"))
	}
	if jsonText == "" || jsonText == "[DONE]" {
		return chunk
	}
	obj := map[string]any{}
	if err := json.Unmarshal([]byte(jsonText), &obj); err != nil {
		return chunk
	}
	if _, ok := obj["candidates"]; !ok {
		return chunk
	}
	obj["usageMetadata"] = map[string]any{
		"promptTokenCount":     usage.PromptTokens,
		"candidatesTokenCount": usage.CompletionTokens,
		"totalTokenCount":      usage.TotalTokens,
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return chunk
	}
	if hasDataPrefix {
		return []byte("data: " + string(b) + suffix)
	}
	if suffix != "" {
		return append(b, []byte(suffix)...)
	}
	return b
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
