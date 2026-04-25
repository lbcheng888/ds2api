package openai

import (
	"strings"

	"ds2api/internal/prompt"
	"ds2api/internal/toolcall"
)

func executableToolCallTextFromThinking(finalThinking string, toolNames []string, schemas toolcall.ParameterSchemas, allowMetaAgentTools bool) string {
	if strings.TrimSpace(finalThinking) == "" {
		return ""
	}
	detected := toolcall.ParseStandaloneToolCallsDetailed(finalThinking, toolNames)
	if len(detected.Calls) == 0 {
		return ""
	}
	if !allowMetaAgentTools && toolcall.AllCallsAreMetaAgentTools(detected.Calls) {
		return toolcall.MetaAgentToolBlockedMessage()
	}
	calls := toolcall.NormalizeCallsForSchemasWithMeta(detected.Calls, schemas, allowMetaAgentTools)
	if len(calls) == 0 {
		return ""
	}
	return formatParsedToolCallsAsPromptXML(calls)
}

func formatParsedToolCallsAsPromptXML(calls []toolcall.ParsedToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	raw := make([]any, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		input := call.Input
		if input == nil {
			input = map[string]any{}
		}
		raw = append(raw, map[string]any{
			"name":  name,
			"input": input,
		})
	}
	return prompt.FormatToolCallsForPrompt(raw)
}
