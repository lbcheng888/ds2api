package claude

import (
	"fmt"
	"strings"

	"ds2api/internal/config"
	"ds2api/internal/deepseek"
	"ds2api/internal/toolcall"
	"ds2api/internal/util"
)

type claudeNormalizedRequest struct {
	Standard           util.StandardRequest
	NormalizedMessages []any
}

func normalizeClaudeRequest(store ConfigReader, req map[string]any) (claudeNormalizedRequest, error) {
	model, _ := req["model"].(string)
	messagesRaw, _ := req["messages"].([]any)
	if strings.TrimSpace(model) == "" || len(messagesRaw) == 0 {
		return claudeNormalizedRequest{}, fmt.Errorf("request must include 'model' and 'messages'")
	}
	if _, ok := req["max_tokens"]; !ok {
		req["max_tokens"] = 8192
	}
	normalizedMessages := normalizeClaudeMessages(messagesRaw)
	payload := cloneMap(req)
	payload["messages"] = normalizedMessages
	toolsRequested, _ := req["tools"].([]any)
	allowMetaAgentTools := store != nil && store.CompatAllowMetaAgentTools()
	payload["messages"] = injectClaudeToolPrompt(payload, normalizedMessages, toolsRequested, allowMetaAgentTools)

	dsPayload := convertClaudeToDeepSeek(payload, store)
	dsModel, _ := dsPayload["model"].(string)
	if resolved, ok := config.ResolveModel(store, dsModel); ok {
		dsModel = resolved
		dsPayload["model"] = resolved
	}
	thinkingEnabled, searchEnabled, ok := config.GetModelConfig(dsModel)
	if !ok {
		thinkingEnabled = false
		searchEnabled = false
	}
	finalPrompt := deepseek.MessagesPrepareWithThinking(toMessageMaps(dsPayload["messages"]), thinkingEnabled)
	toolNames := extractClaudeToolNames(toolsRequested, allowMetaAgentTools)
	if len(toolNames) == 0 && len(toolsRequested) > 0 {
		toolNames = []string{"__any_tool__"}
	}

	return claudeNormalizedRequest{
		Standard: util.StandardRequest{
			Surface:             "anthropic_messages",
			RequestedModel:      strings.TrimSpace(model),
			ResolvedModel:       dsModel,
			ResponseModel:       strings.TrimSpace(model),
			Messages:            payload["messages"].([]any),
			FinalPrompt:         finalPrompt,
			ToolNames:           toolNames,
			ToolSchemas:         toolcall.ExtractParameterSchemas(toolsRequested),
			Stream:              util.ToBool(req["stream"]),
			Thinking:            thinkingEnabled,
			ReasoningEffort:     claudeReasoningEffort(store, req, thinkingEnabled),
			Search:              searchEnabled,
			AllowMetaAgentTools: allowMetaAgentTools,
		},
		NormalizedMessages: normalizedMessages,
	}, nil
}

func claudeReasoningEffort(store ConfigReader, req map[string]any, thinkingEnabled bool) string {
	if !thinkingEnabled {
		return ""
	}
	for _, raw := range []any{
		req["reasoning_effort"],
		mapField(req["output_config"], "reasoning_effort"),
		mapField(req["output_config"], "effort"),
		mapField(req["thinking"], "reasoning_effort"),
		mapField(req["thinking"], "effort"),
	} {
		if effort := config.NormalizeReasoningEffort(anyString(raw)); effort != "" {
			return effort
		}
	}
	if store == nil {
		return ""
	}
	return store.CompatDefaultReasoningEffort()
}

func mapField(raw any, key string) any {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return m[key]
}

func anyString(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	default:
		if raw == nil {
			return ""
		}
		return fmt.Sprintf("%v", raw)
	}
}

func injectClaudeToolPrompt(payload map[string]any, normalizedMessages []any, tools []any, allowMetaAgentTools bool) []any {
	if len(tools) == 0 {
		return normalizedMessages
	}
	toolPrompt := strings.TrimSpace(buildClaudeToolPrompt(tools, allowMetaAgentTools))
	if toolPrompt == "" {
		return normalizedMessages
	}

	// Prefer top-level Anthropic-style system prompt when available.
	if systemText, ok := payload["system"].(string); ok && strings.TrimSpace(systemText) != "" {
		payload["system"] = mergeSystemPrompt(systemText, toolPrompt)
		return normalizedMessages
	}

	messages := cloneAnySlice(normalizedMessages)
	for i := range messages {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if !strings.EqualFold(strings.TrimSpace(role), "system") {
			continue
		}
		copied := cloneMap(msg)
		copied["content"] = mergeSystemPrompt(strings.TrimSpace(fmt.Sprintf("%v", copied["content"])), toolPrompt)
		messages[i] = copied
		return messages
	}

	return append([]any{map[string]any{"role": "system", "content": toolPrompt}}, messages...)
}

func mergeSystemPrompt(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	default:
		return base + "\n\n" + extra
	}
}

func cloneAnySlice(in []any) []any {
	if len(in) == 0 {
		return nil
	}
	out := make([]any, len(in))
	copy(out, in)
	return out
}
