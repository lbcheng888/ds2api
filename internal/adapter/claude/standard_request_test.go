package claude

import (
	"testing"

	"ds2api/internal/config"
)

func TestNormalizeClaudeRequest(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-opus-4-6",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"stream": true,
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}
	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if norm.Standard.ResolvedModel == "" {
		t.Fatalf("expected resolved model")
	}
	if !norm.Standard.Stream {
		t.Fatalf("expected stream=true")
	}
	if len(norm.Standard.ToolNames) == 0 {
		t.Fatalf("expected tool names")
	}
	if norm.Standard.FinalPrompt == "" {
		t.Fatalf("expected non-empty final prompt")
	}
}

func TestNormalizeClaudeRequestExtractsToolSchemas(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-sonnet-4-5",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{
				"name": "Read",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{"type": "string"},
					},
					"required": []any{"file_path"},
				},
			},
		},
	}
	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if _, ok := norm.Standard.ToolSchemas["Read"]; !ok {
		t.Fatalf("expected Read schema to be extracted, got %#v", norm.Standard.ToolSchemas)
	}
}

func TestNormalizeClaudeRequestUsesDefaultReasoningEffort(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"compat":{"default_reasoning_effort":"max"}}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if got := norm.Standard.ReasoningEffort; got != "max" {
		t.Fatalf("expected default reasoning effort max, got %q", got)
	}
}

func TestNormalizeClaudeRequestExplicitReasoningEffortOverridesDefault(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"compat":{"default_reasoning_effort":"max"}}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"output_config": map[string]any{
			"effort": "high",
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if got := norm.Standard.ReasoningEffort; got != "high" {
		t.Fatalf("expected explicit reasoning effort high, got %q", got)
	}
}

func TestNormalizeClaudeRequestInjectsToolsIntoExistingSystemMessage(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-sonnet-4-5",
		"messages": []any{
			map[string]any{"role": "system", "content": "baseline rule"},
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}

	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	if !containsStr(norm.Standard.FinalPrompt, "You have access to these tools") {
		t.Fatalf("expected tool prompt injected into final prompt, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "baseline rule") {
		t.Fatalf("expected existing system message preserved, got=%q", norm.Standard.FinalPrompt)
	}
}

func TestNormalizeClaudeRequestInjectsToolsIntoTopLevelSystem(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model":  "claude-sonnet-4-5",
		"system": "top-level system",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}

	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	if !containsStr(norm.Standard.FinalPrompt, "top-level system") {
		t.Fatalf("expected top-level system preserved, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "You have access to these tools") {
		t.Fatalf("expected tool prompt injected, got=%q", norm.Standard.FinalPrompt)
	}
}
