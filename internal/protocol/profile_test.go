package protocol

import (
	"net/http"
	"strings"
	"testing"
)

func TestDetectClientProfileFromHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("User-Agent", "Claude-Code/2.1")

	got := DetectClientProfile(req, nil)
	if got.Name != ProfileClaudeCode {
		t.Fatalf("expected claude_code, got %#v", got)
	}
	if got.Source != "header:User-Agent" {
		t.Fatalf("unexpected source: %#v", got)
	}
}

func TestDetectClientProfilePrefersExplicitSource(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("X-Ds2-Source", "opencode")
	req.Header.Set("User-Agent", "Claude-Code/2.1")

	got := DetectClientProfile(req, nil)
	if got.Name != ProfileOpenCode {
		t.Fatalf("expected opencode, got %#v", got)
	}
}

func TestClientProfilePromptInstruction(t *testing.T) {
	got := ClientProfilePromptInstruction(ClientProfile{Name: ProfileClaudeCode})
	if !strings.Contains(got, "Agent") || !strings.Contains(got, "TaskOutput") {
		t.Fatalf("expected ClaudeCode Agent/TaskOutput instruction, got %q", got)
	}
	if !strings.Contains(got, "limit <= 200") || !strings.Contains(got, ".cheng") {
		t.Fatalf("expected bounded Cheng Read instruction, got %q", got)
	}
	if !strings.Contains(got, "hash-suffixed repository paths") || !strings.Contains(got, "verify absolute paths") {
		t.Fatalf("expected ClaudeCode path verification instruction, got %q", got)
	}

	codex := ClientProfilePromptInstruction(ClientProfile{Name: ProfileCodex})
	if !strings.Contains(codex, "Codex search budget") || !strings.Contains(codex, "file:line") || !strings.Contains(codex, "hash-suffixed repository paths") {
		t.Fatalf("expected Codex search budget instruction, got %q", codex)
	}
}
