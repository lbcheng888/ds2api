package claude

import "testing"

type mockClaudeConfig struct {
	m         map[string]string
	aliases   map[string]string
	allowMeta bool
	effort    string
}

func (m mockClaudeConfig) ClaudeMapping() map[string]string     { return m.m }
func (m mockClaudeConfig) ModelAliases() map[string]string      { return m.aliases }
func (mockClaudeConfig) CompatStripReferenceMarkers() bool      { return true }
func (m mockClaudeConfig) CompatAllowMetaAgentTools() bool      { return m.allowMeta }
func (m mockClaudeConfig) CompatDefaultReasoningEffort() string { return m.effort }

func TestNormalizeClaudeRequestUsesConfigInterfaceMapping(t *testing.T) {
	req := map[string]any{
		"model": "claude-opus-4-6",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	out, err := normalizeClaudeRequest(mockClaudeConfig{
		m: map[string]string{
			"fast": "deepseek-chat",
			"slow": "deepseek-reasoner-search",
		},
	}, req)
	if err != nil {
		t.Fatalf("normalizeClaudeRequest error: %v", err)
	}
	if out.Standard.ResolvedModel != "deepseek-reasoner-search" {
		t.Fatalf("resolved model mismatch: got=%q", out.Standard.ResolvedModel)
	}
	if !out.Standard.Thinking || !out.Standard.Search {
		t.Fatalf("unexpected flags: thinking=%v search=%v", out.Standard.Thinking, out.Standard.Search)
	}
}

func TestNormalizeClaudeRequestAppliesModelAliasAfterClaudeMapping(t *testing.T) {
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	out, err := normalizeClaudeRequest(mockClaudeConfig{
		m: map[string]string{
			"fast": "deepseek-chat",
		},
		aliases: map[string]string{
			"deepseek-v4-pro": "deepseek-v4-pro[1m]",
		},
	}, req)
	if err != nil {
		t.Fatalf("normalizeClaudeRequest error: %v", err)
	}
	if out.Standard.ResolvedModel != "deepseek-v4-pro[1m]" {
		t.Fatalf("resolved model mismatch: got=%q", out.Standard.ResolvedModel)
	}
}

func TestNormalizeClaudeRequestPreservesAllowMetaAgentTools(t *testing.T) {
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "review code"},
		},
		"tools": []any{
			map[string]any{"name": "Agent", "description": "Launch subagent"},
			map[string]any{"name": "TaskOutput", "description": "Fetch subagent output"},
			map[string]any{"name": "TaskCreate", "description": "Create UI task"},
			map[string]any{"name": "TodoWrite", "description": "Update UI todos"},
		},
	}
	out, err := normalizeClaudeRequest(mockClaudeConfig{allowMeta: true}, req)
	if err != nil {
		t.Fatalf("normalizeClaudeRequest error: %v", err)
	}
	if !out.Standard.AllowMetaAgentTools {
		t.Fatalf("expected AllowMetaAgentTools=true")
	}
	if len(out.Standard.ToolNames) != 2 || out.Standard.ToolNames[0] != "Agent" || out.Standard.ToolNames[1] != "TaskOutput" {
		t.Fatalf("expected Agent and TaskOutput tools preserved, got %#v", out.Standard.ToolNames)
	}
	if !containsStr(out.Standard.FinalPrompt, "Tool: Agent") {
		t.Fatalf("expected Agent tool prompt, got %q", out.Standard.FinalPrompt)
	}
	if !containsStr(out.Standard.FinalPrompt, "Tool: TaskOutput") {
		t.Fatalf("expected TaskOutput tool prompt, got %q", out.Standard.FinalPrompt)
	}
	for _, bad := range []string{"Tool: TaskCreate", "Tool: TodoWrite"} {
		if containsStr(out.Standard.FinalPrompt, bad) {
			t.Fatalf("expected task-tracking tool %s to be suppressed, got %q", bad, out.Standard.FinalPrompt)
		}
	}
}
