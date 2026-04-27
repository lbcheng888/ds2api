package openai

import (
	"fmt"
	"strings"

	"ds2api/internal/config"
	"ds2api/internal/protocol"
	"ds2api/internal/toolcall"
	"ds2api/internal/util"
)

func normalizeOpenAIChatRequest(store ConfigReader, req map[string]any, traceID string) (util.StandardRequest, error) {
	return normalizeOpenAIChatRequestWithProfile(store, req, traceID, protocol.ClientProfile{Name: protocol.ProfileUnknown})
}

func normalizeOpenAIChatRequestWithProfile(store ConfigReader, req map[string]any, traceID string, profile protocol.ClientProfile) (util.StandardRequest, error) {
	model, _ := req["model"].(string)
	messagesRaw, _ := req["messages"].([]any)
	if strings.TrimSpace(model) == "" || len(messagesRaw) == 0 {
		return util.StandardRequest{}, fmt.Errorf("request must include 'model' and 'messages'")
	}
	resolvedModel, ok := config.ResolveModel(store, model)
	if !ok {
		return util.StandardRequest{}, fmt.Errorf("model %q is not available", model)
	}
	thinkingEnabled, searchEnabled, _ := config.GetModelConfig(resolvedModel)
	var err error
	thinkingEnabled, err = applyOpenAIThinkingOverride(req["thinking"], thinkingEnabled)
	if err != nil {
		return util.StandardRequest{}, err
	}
	if err := rejectUnsupportedThinkingParams(req, thinkingEnabled); err != nil {
		return util.StandardRequest{}, err
	}
	if err := toolcall.ValidateStrictFunctionTools(req["tools"]); err != nil {
		return util.StandardRequest{}, err
	}
	responseModel := strings.TrimSpace(model)
	if responseModel == "" {
		responseModel = resolvedModel
	}
	toolPolicy, err := parseToolChoicePolicy(req["tool_choice"], req["tools"])
	if err != nil {
		return util.StandardRequest{}, err
	}
	allowMetaAgentTools := store != nil && store.CompatAllowMetaAgentTools()
	messagesRaw = appendOpenAIResponseFormatInstruction(messagesRaw, req["response_format"])
	messagesRaw = appendOpenAIClientProfileInstruction(messagesRaw, profile)
	finalPrompt, toolNames := buildOpenAIFinalPromptWithPolicy(messagesRaw, req["tools"], traceID, toolPolicy, thinkingEnabled, allowMetaAgentTools)
	toolNames = ensureToolDetectionEnabled(toolNames, req["tools"])
	if !toolPolicy.IsNone() {
		toolPolicy.Allowed = namesToSet(toolNames)
	}
	passThrough := collectOpenAIChatPassThrough(req, thinkingEnabled)
	refFileIDs := collectOpenAIRefFileIDs(req)

	return util.StandardRequest{
		Surface:             "openai_chat",
		RequestedModel:      strings.TrimSpace(model),
		ResolvedModel:       resolvedModel,
		ResponseModel:       responseModel,
		Messages:            messagesRaw,
		ToolsRaw:            req["tools"],
		ToolSchemas:         toolcall.ExtractParameterSchemas(req["tools"]),
		FinalPrompt:         finalPrompt,
		ToolNames:           toolNames,
		ToolChoice:          toolPolicy,
		AllowMetaAgentTools: allowMetaAgentTools,
		Stream:              util.ToBool(req["stream"]),
		StreamIncludeUsage:  streamIncludeUsage(req["stream_options"]),
		Thinking:            thinkingEnabled,
		ReasoningEffort:     openAIReasoningEffort(store, thinkingEnabled, req["thinking"], req["reasoning_effort"]),
		Search:              searchEnabled,
		RefFileIDs:          refFileIDs,
		PassThrough:         passThrough,
		ClientProfile:       profile.Name,
	}, nil
}

func normalizeOpenAIResponsesRequest(store ConfigReader, req map[string]any, traceID string) (util.StandardRequest, error) {
	return normalizeOpenAIResponsesRequestWithProfile(store, req, traceID, protocol.ClientProfile{Name: protocol.ProfileUnknown})
}

func normalizeOpenAIResponsesRequestWithProfile(store ConfigReader, req map[string]any, traceID string, profile protocol.ClientProfile) (util.StandardRequest, error) {
	model, _ := req["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return util.StandardRequest{}, fmt.Errorf("request must include 'model'")
	}
	resolvedModel, ok := config.ResolveModel(store, model)
	if !ok {
		return util.StandardRequest{}, fmt.Errorf("model %q is not available", model)
	}
	thinkingEnabled, searchEnabled, _ := config.GetModelConfig(resolvedModel)
	var err error
	thinkingEnabled, err = applyOpenAIThinkingOverride(req["thinking"], thinkingEnabled)
	if err != nil {
		return util.StandardRequest{}, err
	}
	if err := rejectUnsupportedThinkingParams(req, thinkingEnabled); err != nil {
		return util.StandardRequest{}, err
	}
	if err := toolcall.ValidateStrictFunctionTools(req["tools"]); err != nil {
		return util.StandardRequest{}, err
	}
	allowMetaAgentTools := store != nil && store.CompatAllowMetaAgentTools()

	// Keep width-control as an explicit policy hook even if current default is true.
	allowWideInput := true
	if store != nil {
		allowWideInput = store.CompatWideInputStrictOutput()
	}
	var messagesRaw []any
	if allowWideInput {
		messagesRaw = responsesMessagesFromRequest(req)
	} else if msgs, ok := req["messages"].([]any); ok && len(msgs) > 0 {
		messagesRaw = msgs
	}
	if len(messagesRaw) == 0 {
		return util.StandardRequest{}, fmt.Errorf("request must include 'input' or 'messages'")
	}
	toolPolicy, err := parseToolChoicePolicy(req["tool_choice"], req["tools"])
	if err != nil {
		return util.StandardRequest{}, err
	}
	messagesRaw = appendOpenAIResponseFormatInstruction(messagesRaw, req["response_format"])
	messagesRaw = appendOpenAIClientProfileInstruction(messagesRaw, profile)
	finalPrompt, toolNames := buildOpenAIFinalPromptWithPolicy(messagesRaw, req["tools"], traceID, toolPolicy, thinkingEnabled, allowMetaAgentTools)
	toolNames = ensureToolDetectionEnabled(toolNames, req["tools"])
	if !toolPolicy.IsNone() {
		toolPolicy.Allowed = namesToSet(toolNames)
	}
	passThrough := collectOpenAIChatPassThrough(req, thinkingEnabled)
	refFileIDs := collectOpenAIRefFileIDs(req)

	return util.StandardRequest{
		Surface:             "openai_responses",
		RequestedModel:      model,
		ResolvedModel:       resolvedModel,
		ResponseModel:       model,
		Messages:            messagesRaw,
		ToolsRaw:            req["tools"],
		ToolSchemas:         toolcall.ExtractParameterSchemas(req["tools"]),
		FinalPrompt:         finalPrompt,
		ToolNames:           toolNames,
		ToolChoice:          toolPolicy,
		AllowMetaAgentTools: allowMetaAgentTools,
		Stream:              util.ToBool(req["stream"]),
		StreamIncludeUsage:  streamIncludeUsage(req["stream_options"]),
		Thinking:            thinkingEnabled,
		ReasoningEffort:     openAIReasoningEffort(store, thinkingEnabled, req["thinking"], req["reasoning_effort"]),
		Search:              searchEnabled,
		RefFileIDs:          refFileIDs,
		PassThrough:         passThrough,
		ClientProfile:       profile.Name,
	}, nil
}

func appendOpenAIClientProfileInstruction(messages []any, profile protocol.ClientProfile) []any {
	instruction := protocol.ClientProfilePromptInstruction(profile)
	if strings.TrimSpace(instruction) == "" {
		return messages
	}
	out := make([]any, 0, len(messages)+1)
	out = append(out, map[string]any{"role": "system", "content": instruction})
	out = append(out, messages...)
	return out
}

func ensureToolDetectionEnabled(toolNames []string, toolsRaw any) []string {
	if len(toolNames) > 0 {
		return toolNames
	}
	tools, _ := toolsRaw.([]any)
	if len(tools) == 0 {
		return toolNames
	}
	// Keep stream sieve/tool buffering enabled even when client tool schemas
	// are malformed or lack explicit names; parsed tool payload names are no
	// longer filtered by this list.
	return []string{"__any_tool__"}
}

func collectOpenAIChatPassThrough(req map[string]any, thinkingEnabled bool) map[string]any {
	out := map[string]any{}
	for _, k := range []string{
		"temperature",
		"top_p",
		"max_tokens",
		"presence_penalty",
		"frequency_penalty",
		"stop",
	} {
		if thinkingEnabled && openAIThinkingIgnoredParam(k) {
			continue
		}
		if v, ok := req[k]; ok {
			out[k] = v
		}
	}
	if v, ok := req["max_completion_tokens"]; ok {
		if _, hasMaxTokens := out["max_tokens"]; !hasMaxTokens {
			out["max_tokens"] = v
		}
	}
	return out
}

func applyOpenAIThinkingOverride(raw any, current bool) (bool, error) {
	if raw == nil {
		return current, nil
	}
	switch v := raw.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "enabled", "enable", "true":
			return true, nil
		case "disabled", "disable", "false":
			return false, nil
		default:
			return current, fmt.Errorf("thinking.type must be 'enabled' or 'disabled'")
		}
	case map[string]any:
		typ := strings.ToLower(strings.TrimSpace(asString(v["type"])))
		switch typ {
		case "", "enabled":
			return true, nil
		case "disabled":
			return false, nil
		default:
			return current, fmt.Errorf("thinking.type must be 'enabled' or 'disabled'")
		}
	default:
		return current, fmt.Errorf("thinking must be an object")
	}
}

func rejectUnsupportedThinkingParams(req map[string]any, thinkingEnabled bool) error {
	if !thinkingEnabled {
		return nil
	}
	for _, key := range []string{"logprobs", "top_logprobs"} {
		if _, ok := req[key]; ok {
			return fmt.Errorf("%s is not supported when thinking is enabled", key)
		}
	}
	return nil
}

func openAIThinkingIgnoredParam(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "temperature", "top_p", "presence_penalty", "frequency_penalty":
		return true
	default:
		return false
	}
}

func openAIReasoningEffort(store ConfigReader, thinkingEnabled bool, thinkingRaw any, topLevel any) string {
	if !thinkingEnabled {
		return ""
	}
	if s := strings.TrimSpace(asString(topLevel)); s != "" {
		return config.NormalizeReasoningEffort(s)
	}
	if m, ok := thinkingRaw.(map[string]any); ok {
		if effort := config.NormalizeReasoningEffort(asString(m["reasoning_effort"])); effort != "" {
			return effort
		}
	}
	if store == nil {
		return ""
	}
	return store.CompatDefaultReasoningEffort()
}

func streamIncludeUsage(raw any) bool {
	m, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	return util.ToBool(m["include_usage"])
}

func appendOpenAIResponseFormatInstruction(messages []any, raw any) []any {
	m, ok := raw.(map[string]any)
	if !ok {
		return messages
	}
	if strings.ToLower(strings.TrimSpace(asString(m["type"]))) != "json_object" {
		return messages
	}
	instruction := "Respond with one valid JSON object only. Do not include markdown fences or explanatory text outside the JSON object."
	out := make([]any, 0, len(messages)+1)
	out = append(out, map[string]any{"role": "system", "content": instruction})
	out = append(out, messages...)
	return out
}

func parseToolChoicePolicy(toolChoiceRaw any, toolsRaw any) (util.ToolChoicePolicy, error) {
	policy := util.DefaultToolChoicePolicy()
	declaredNames := extractDeclaredToolNames(toolsRaw)
	declaredSet := namesToSet(declaredNames)
	if len(declaredNames) > 0 {
		policy.Allowed = declaredSet
	}

	if toolChoiceRaw == nil {
		return policy, nil
	}

	switch v := toolChoiceRaw.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "auto":
			policy.Mode = util.ToolChoiceAuto
		case "none":
			policy.Mode = util.ToolChoiceNone
			policy.Allowed = nil
		case "required":
			policy.Mode = util.ToolChoiceRequired
		default:
			return util.ToolChoicePolicy{}, fmt.Errorf("unsupported tool_choice: %q", v)
		}
	case map[string]any:
		allowedOverride, hasAllowedOverride, err := parseAllowedToolNames(v["allowed_tools"])
		if err != nil {
			return util.ToolChoicePolicy{}, err
		}
		if hasAllowedOverride {
			filtered := make([]string, 0, len(allowedOverride))
			for _, name := range allowedOverride {
				if _, ok := declaredSet[name]; !ok {
					return util.ToolChoicePolicy{}, fmt.Errorf("tool_choice.allowed_tools contains undeclared tool %q", name)
				}
				filtered = append(filtered, name)
			}
			policy.Allowed = namesToSet(filtered)
		}

		typ := strings.ToLower(strings.TrimSpace(asString(v["type"])))
		switch typ {
		case "", "auto":
			if hasFunctionSelector(v) {
				name, err := parseForcedToolName(v)
				if err != nil {
					return util.ToolChoicePolicy{}, err
				}
				policy.Mode = util.ToolChoiceForced
				policy.ForcedName = name
				policy.Allowed = namesToSet([]string{name})
			} else {
				policy.Mode = util.ToolChoiceAuto
			}
		case "none":
			policy.Mode = util.ToolChoiceNone
			policy.Allowed = nil
		case "required":
			policy.Mode = util.ToolChoiceRequired
		case "function":
			name, err := parseForcedToolName(v)
			if err != nil {
				return util.ToolChoicePolicy{}, err
			}
			policy.Mode = util.ToolChoiceForced
			policy.ForcedName = name
			policy.Allowed = namesToSet([]string{name})
		default:
			return util.ToolChoicePolicy{}, fmt.Errorf("unsupported tool_choice.type: %q", typ)
		}
	default:
		return util.ToolChoicePolicy{}, fmt.Errorf("tool_choice must be a string or object")
	}

	if policy.Mode == util.ToolChoiceRequired || policy.Mode == util.ToolChoiceForced {
		if len(declaredNames) == 0 {
			return util.ToolChoicePolicy{}, fmt.Errorf("tool_choice=%s requires non-empty tools", policy.Mode)
		}
	}
	if policy.Mode == util.ToolChoiceForced {
		if _, ok := declaredSet[policy.ForcedName]; !ok {
			return util.ToolChoicePolicy{}, fmt.Errorf("tool_choice forced function %q is not declared in tools", policy.ForcedName)
		}
	}
	if len(policy.Allowed) == 0 && (policy.Mode == util.ToolChoiceRequired || policy.Mode == util.ToolChoiceForced) {
		return util.ToolChoicePolicy{}, fmt.Errorf("tool_choice policy resolved to empty allowed tool set")
	}
	return policy, nil
}

func parseForcedToolName(v map[string]any) (string, error) {
	if name := strings.TrimSpace(asString(v["name"])); name != "" {
		return name, nil
	}
	if fn, ok := v["function"].(map[string]any); ok {
		if name := strings.TrimSpace(asString(fn["name"])); name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("tool_choice function requires name")
}

func parseAllowedToolNames(raw any) ([]string, bool, error) {
	if raw == nil {
		return nil, false, nil
	}
	collectName := func(v any) string {
		if name := strings.TrimSpace(asString(v)); name != "" {
			return name
		}
		if m, ok := v.(map[string]any); ok {
			if name := strings.TrimSpace(asString(m["name"])); name != "" {
				return name
			}
			if fn, ok := m["function"].(map[string]any); ok {
				if name := strings.TrimSpace(asString(fn["name"])); name != "" {
					return name
				}
			}
		}
		return ""
	}

	names := []string{}
	switch x := raw.(type) {
	case []any:
		for _, item := range x {
			name := collectName(item)
			if name == "" {
				return nil, true, fmt.Errorf("tool_choice.allowed_tools contains invalid item")
			}
			names = append(names, name)
		}
	case []string:
		for _, item := range x {
			name := strings.TrimSpace(item)
			if name == "" {
				return nil, true, fmt.Errorf("tool_choice.allowed_tools contains empty name")
			}
			names = append(names, name)
		}
	default:
		return nil, true, fmt.Errorf("tool_choice.allowed_tools must be an array")
	}

	if len(names) == 0 {
		return nil, true, fmt.Errorf("tool_choice.allowed_tools must not be empty")
	}
	return names, true, nil
}

func hasFunctionSelector(v map[string]any) bool {
	if strings.TrimSpace(asString(v["name"])) != "" {
		return true
	}
	if fn, ok := v["function"].(map[string]any); ok {
		return strings.TrimSpace(asString(fn["name"])) != ""
	}
	return false
}

func extractDeclaredToolNames(toolsRaw any) []string {
	tools, ok := toolsRaw.([]any)
	if !ok || len(tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(tools))
	seen := map[string]struct{}{}
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		if len(fn) == 0 {
			fn = tool
		}
		name := strings.TrimSpace(asString(fn["name"]))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func namesToSet(names []string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		out[trimmed] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
