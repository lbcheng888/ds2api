package claude

import (
	"fmt"
	"strings"

	"ds2api/internal/config"
	"ds2api/internal/prompt"
	"ds2api/internal/promptcompat"
	"ds2api/internal/toolcall"
	"ds2api/internal/util"
)

type claudeNormalizedRequest struct {
	Standard           promptcompat.StandardRequest
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
	allowMetaAgentTools := false
	if store != nil {
		type metaAgentConfig interface {
			CompatAllowMetaAgentTools() bool
		}
		if cfg, ok := store.(metaAgentConfig); ok {
			allowMetaAgentTools = cfg.CompatAllowMetaAgentTools()
		}
	}
	payload := cloneMap(req)
	payload["messages"] = normalizedMessages
	toolsRequested, _ := req["tools"].([]any)
	payload["messages"] = injectClaudeToolPrompt(payload, normalizedMessages, toolsRequested, allowMetaAgentTools, allowMetaAgentTools)

	dsPayload := convertClaudeToDeepSeek(payload, store)
	dsModel, _ := dsPayload["model"].(string)
	_, searchEnabled, ok := config.GetModelConfig(dsModel)
	if !ok {
		searchEnabled = false
	}
	thinkingEnabled := util.ResolveThinkingEnabled(req, false)
	if config.IsNoThinkingModel(dsModel) {
		thinkingEnabled = false
	}
	finalPrompt := prompt.MessagesPrepareWithThinking(toMessageMaps(dsPayload["messages"]), thinkingEnabled)
	toolNames := extractClaudeToolNames(toolsRequested, allowMetaAgentTools, allowMetaAgentTools)
	if len(toolNames) == 0 && len(toolsRequested) > 0 {
		toolNames = []string{"__any_tool__"}
	}
	reasoningEffort := claudeReasoningEffort(req, store)

	return claudeNormalizedRequest{
		Standard: promptcompat.StandardRequest{
			Surface:             "anthropic_messages",
			RequestedModel:      strings.TrimSpace(model),
			ResolvedModel:       dsModel,
			ResponseModel:       strings.TrimSpace(model),
			Messages:            payload["messages"].([]any),
			FinalPrompt:         finalPrompt,
			ToolNames:           toolNames,
			ToolSchemas:         toolcall.ExtractParameterSchemas(toolsRequested),
			AllowMetaAgentTools: allowMetaAgentTools,
			Stream:              util.ToBool(req["stream"]),
			Thinking:            thinkingEnabled,
			ReasoningEffort:     reasoningEffort,
			Search:              searchEnabled,
		},
		NormalizedMessages: normalizedMessages,
	}, nil
}

func claudeReasoningEffort(req map[string]any, store ConfigReader) string {
	if outputConfig, _ := req["output_config"].(map[string]any); outputConfig != nil {
		if effort := config.NormalizeReasoningEffort(strings.TrimSpace(fmt.Sprintf("%v", outputConfig["effort"]))); effort != "" {
			return effort
		}
	}
	if effort := config.NormalizeReasoningEffort(strings.TrimSpace(fmt.Sprintf("%v", req["reasoning_effort"]))); effort != "" {
		return effort
	}
	if store != nil {
		type defaultEffortConfig interface {
			CompatDefaultReasoningEffort() string
		}
		if cfg, ok := store.(defaultEffortConfig); ok {
			return config.NormalizeReasoningEffort(cfg.CompatDefaultReasoningEffort())
		}
	}
	return ""
}

func injectClaudeToolPrompt(payload map[string]any, normalizedMessages []any, tools []any, allowMetaAgentTools bool, allowTaskOutput bool) []any {
	if len(tools) == 0 {
		return normalizedMessages
	}
	toolPrompt := strings.TrimSpace(buildClaudeToolPrompt(tools, allowMetaAgentTools, allowTaskOutput))
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
