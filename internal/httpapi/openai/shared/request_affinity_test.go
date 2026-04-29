package shared

import (
	"net/http/httptest"
	"testing"
)

func TestRequestAffinityKeyPrefersExplicitHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions?thread_id=query-thread", nil)
	req.Header.Set("X-Ds2-Conversation-ID", "header-thread")
	got := RequestAffinityKey(req, map[string]any{"conversation_id": "body-thread"})
	if got != "header-thread" {
		t.Fatalf("affinity key=%q want header-thread", got)
	}
}

func TestRequestAffinityKeyReadsPayloadMetadata(t *testing.T) {
	got := RequestAffinityKey(nil, map[string]any{
		"metadata": map[string]any{
			"session_id": "session-123",
		},
	})
	if got != "session-123" {
		t.Fatalf("affinity key=%q want session-123", got)
	}
}

func TestRequestAffinityKeyDerivesClaudeCodeKeyFromFirstUserMessage(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("User-Agent", "Claude-Code/2.1")
	payload := map[string]any{
		"model": "claude-sonnet-4-5",
		"messages": []any{
			map[string]any{"role": "system", "content": "Workspace Path: /Users/lbcheng/ds2api"},
			map[string]any{"role": "user", "content": "Please implement the account affinity strategy for this repository."},
			map[string]any{"role": "assistant", "content": "ok"},
			map[string]any{"role": "user", "content": "continue"},
		},
	}
	first := RequestAffinityKey(req, payload)
	second := RequestAffinityKey(req, payload)
	if first == "" || first != second {
		t.Fatalf("expected stable derived Claude Code affinity key, first=%q second=%q", first, second)
	}
}

func TestRequestAffinityKeyDoesNotDeriveFromShortClaudeCodeMessage(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("User-Agent", "Claude-Code/2.1")
	got := RequestAffinityKey(req, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "继续"}},
	})
	if got != "" {
		t.Fatalf("expected no weak key for short generic message, got %q", got)
	}
}
