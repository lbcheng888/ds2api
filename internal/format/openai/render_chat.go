package openai

import (
	"ds2api/internal/toolcall"
	"strings"
	"time"
)

const deepSeekSystemFingerprint = "fp_ds2api_deepseek_v4"

func DeepSeekSystemFingerprint() string {
	return deepSeekSystemFingerprint
}

func BuildChatCompletion(completionID, model, finalPrompt, finalThinking, finalText string, toolNames []string, opts ...any) map[string]any {
	detected := toolcall.ParseAssistantToolCallsDetailed(finalText, finalThinking, toolNames)
	calls := detected.Calls
	if len(calls) == 0 {
		if _, visibleJSONCalls, _, ok := toolcall.ExtractVisibleJSONToolCalls(finalText, toolNames); ok {
			calls = visibleJSONCalls
		}
	}
	schemas, allowMetaAgentTools := chatCompletionToolOptions(opts...)
	if len(schemas) > 0 {
		normalized := toolcall.NormalizeCallsForSchemasWithMeta(calls, schemas, allowMetaAgentTools)
		if len(normalized) == 0 && !allowMetaAgentTools && toolcall.AllCallsAreMetaAgentTools(calls) {
			return BuildChatCompletionWithToolCalls(completionID, model, finalPrompt, finalThinking, toolcall.MetaAgentToolBlockedMessage(), nil)
		}
		calls = normalized
	}
	return BuildChatCompletionWithToolCalls(completionID, model, finalPrompt, finalThinking, finalText, calls)
}

func BuildChatCompletionWithToolCalls(completionID, model, finalPrompt, finalThinking, finalText string, detected []toolcall.ParsedToolCall) map[string]any {
	finishReason := "stop"
	messageObj := map[string]any{"role": "assistant", "content": finalText}
	if strings.TrimSpace(finalThinking) != "" {
		messageObj["reasoning_content"] = finalThinking
	}
	if len(detected) > 0 {
		finishReason = "tool_calls"
		messageObj["tool_calls"] = toolcall.FormatOpenAIToolCalls(detected)
		messageObj["content"] = nil
	}

	return map[string]any{
		"id":                 completionID,
		"object":             "chat.completion",
		"created":            time.Now().Unix(),
		"model":              model,
		"choices":            []map[string]any{{"index": 0, "message": messageObj, "finish_reason": finishReason}},
		"usage":              BuildChatUsage(finalPrompt, finalThinking, finalText),
		"system_fingerprint": DeepSeekSystemFingerprint(),
	}
}

func chatCompletionToolOptions(opts ...any) (toolcall.ParameterSchemas, bool) {
	var schemas toolcall.ParameterSchemas
	allowMetaAgentTools := false
	for _, opt := range opts {
		switch v := opt.(type) {
		case toolcall.ParameterSchemas:
			schemas = v
		case bool:
			allowMetaAgentTools = v
		}
	}
	return schemas, allowMetaAgentTools
}

func BuildChatStreamDeltaChoice(index int, delta map[string]any) map[string]any {
	return map[string]any{
		"delta": delta,
		"index": index,
	}
}

func BuildChatStreamFinishChoice(index int, finishReason string) map[string]any {
	return map[string]any{
		"delta":         map[string]any{},
		"index":         index,
		"finish_reason": finishReason,
	}
}

func BuildChatStreamChunk(completionID string, created int64, model string, choices []map[string]any, usage map[string]any) map[string]any {
	out := map[string]any{
		"id":                 completionID,
		"object":             "chat.completion.chunk",
		"created":            created,
		"model":              model,
		"choices":            choices,
		"system_fingerprint": DeepSeekSystemFingerprint(),
	}
	if len(usage) > 0 {
		out["usage"] = usage
	}
	return out
}

func BuildChatStreamUsageChunk(completionID string, created int64, model string, usage map[string]any) map[string]any {
	if usage == nil {
		usage = map[string]any{}
	}
	return BuildChatStreamChunk(completionID, created, model, []map[string]any{}, usage)
}
