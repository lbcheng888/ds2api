package openai

import (
	"ds2api/internal/deepseek"
	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/util"
)

func buildOpenAIFinalPrompt(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return buildOpenAIFinalPromptWithPolicy(messagesRaw, toolsRaw, traceID, util.DefaultToolChoicePolicy(), thinkingEnabled, false)
}

func buildOpenAIFinalPromptWithPolicy(messagesRaw []any, toolsRaw any, traceID string, toolPolicy util.ToolChoicePolicy, thinkingEnabled bool, allowMetaAgentTools bool) (string, []string) {
	messages := normalizeOpenAIMessagesForPrompt(messagesRaw, traceID)
	toolNames := []string{}
	if tools, ok := toolsRaw.([]any); ok && len(tools) > 0 {
		preToolPrompt := deepseek.MessagesPrepareWithThinking(messages, thinkingEnabled)
		allowTaskOutput := claudecodeharness.HasAllowedTaskOutputIDs(preToolPrompt)
		messages, toolNames = injectToolPrompt(messages, tools, toolPolicy, allowMetaAgentTools, allowTaskOutput)
	}
	return deepseek.MessagesPrepareWithThinking(messages, thinkingEnabled), toolNames
}

// BuildPromptForAdapter exposes the OpenAI-compatible prompt building flow so
// other protocol adapters (for example Gemini) can reuse the same tool/history
// normalization logic and remain behavior-compatible with chat/completions.
func BuildPromptForAdapter(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return buildOpenAIFinalPrompt(messagesRaw, toolsRaw, traceID, thinkingEnabled)
}
