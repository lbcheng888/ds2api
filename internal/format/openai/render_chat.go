package openai

import (
	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
	"strings"
	"time"
)

const deepSeekSystemFingerprint = "fp_ds2api_deepseek_v4"

func DeepSeekSystemFingerprint() string {
	return deepSeekSystemFingerprint
}

func BuildChatCompletion(completionID, model, finalPrompt, finalThinking, finalText string, toolNames []string, toolSchemas toolcall.ParameterSchemas, allowMetaAgentTools bool) map[string]any {
	detected, finalText := claudecodeharness.DetectFinalToolCalls(claudecodeharness.FinalToolCallInput{
		Text:      finalText,
		Thinking:  finalThinking,
		ToolNames: toolNames,
	})
	if !allowMetaAgentTools && toolcall.AllCallsAreMetaAgentTools(detected.Calls) {
		finalText = toolcall.MetaAgentToolBlockedMessage()
		detected.Calls = nil
	}
	detected.Calls = claudecodeharness.FilterInvalidTaskOutputCalls(detected.Calls, finalPrompt)
	calls := toolcall.NormalizeCallsForSchemasWithMeta(detected.Calls, toolSchemas, allowMetaAgentTools)
	return BuildChatCompletionFromToolCalls(completionID, model, finalPrompt, finalThinking, finalText, calls)
}

func BuildChatCompletionFromToolCalls(completionID, model, finalPrompt, finalThinking, finalText string, calls []toolcall.ParsedToolCall) map[string]any {
	finishReason := "stop"
	messageObj := map[string]any{"role": "assistant", "content": finalText}
	if strings.TrimSpace(finalThinking) != "" {
		messageObj["reasoning_content"] = finalThinking
	}
	if len(calls) > 0 {
		finishReason = "tool_calls"
		messageObj["tool_calls"] = toolcall.FormatOpenAIToolCalls(calls)
		messageObj["content"] = nil
	}

	return map[string]any{
		"id":                 completionID,
		"object":             "chat.completion",
		"created":            time.Now().Unix(),
		"model":              model,
		"system_fingerprint": DeepSeekSystemFingerprint(),
		"choices":            []map[string]any{{"index": 0, "message": messageObj, "finish_reason": finishReason}},
		"usage":              BuildChatUsageWithPromptCache(finalPrompt, finalThinking, finalText),
	}
}

func BuildChatStreamDeltaChoice(index int, delta map[string]any) map[string]any {
	return map[string]any{
		"delta":         delta,
		"finish_reason": nil,
		"index":         index,
		"logprobs":      nil,
	}
}

func BuildChatStreamFinishChoice(index int, finishReason string) map[string]any {
	return map[string]any{
		"delta":         map[string]any{},
		"index":         index,
		"finish_reason": finishReason,
		"logprobs":      nil,
	}
}

func BuildChatStreamChunk(completionID string, created int64, model string, choices []map[string]any, usage map[string]any) map[string]any {
	out := map[string]any{
		"id":                 completionID,
		"object":             "chat.completion.chunk",
		"created":            created,
		"model":              model,
		"system_fingerprint": DeepSeekSystemFingerprint(),
		"choices":            choices,
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
	return map[string]any{
		"id":                 completionID,
		"object":             "chat.completion.chunk",
		"created":            created,
		"model":              model,
		"system_fingerprint": DeepSeekSystemFingerprint(),
		"choices":            []map[string]any{},
		"usage":              usage,
	}
}
