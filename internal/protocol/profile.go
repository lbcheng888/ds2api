package protocol

import (
	"net/http"
	"strings"
)

type ClientProfile struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
}

const (
	ProfileUnknown    = "unknown"
	ProfileClaudeCode = "claude_code"
	ProfileOpenCode   = "opencode"
	ProfileRoo        = "roo"
	ProfileCodex      = "codex"
	ProfileAdminWebUI = "admin_webui"
)

func DetectClientProfile(r *http.Request, payload map[string]any) ClientProfile {
	candidates := []profileCandidate{}
	if r != nil {
		for _, header := range []string{"X-Ds2-Source", "X-Client-Name", "User-Agent", "Anthropic-Beta"} {
			if v := strings.TrimSpace(r.Header.Get(header)); v != "" {
				candidates = append(candidates, profileCandidate{value: v, source: "header:" + header})
			}
		}
		if v := strings.TrimSpace(r.URL.Query().Get("client")); v != "" {
			candidates = append(candidates, profileCandidate{value: v, source: "query:client"})
		}
	}
	for _, key := range []string{"client", "source", "app", "application"} {
		if v := strings.TrimSpace(stringField(payload, key)); v != "" {
			candidates = append(candidates, profileCandidate{value: v, source: "body:" + key})
		}
	}
	if meta, _ := payload["metadata"].(map[string]any); meta != nil {
		for _, key := range []string{"client", "source", "app", "application"} {
			if v := strings.TrimSpace(stringField(meta, key)); v != "" {
				candidates = append(candidates, profileCandidate{value: v, source: "metadata:" + key})
			}
		}
	}
	for _, candidate := range candidates {
		if name := profileNameFromText(candidate.value); name != ProfileUnknown {
			return ClientProfile{Name: name, Source: candidate.source}
		}
	}
	return ClientProfile{Name: ProfileUnknown}
}

func ClientProfilePromptInstruction(profile ClientProfile) string {
	switch profile.Name {
	case ProfileClaudeCode:
		return "Client profile: Claude Code. Prefer complete Claude-compatible tool calls. If Agent and TaskOutput tools are available and meta-agent tools are enabled, launch real Agent calls and retrieve results with TaskOutput; do not merely promise background agent work. TaskCreate, TaskUpdate, TodoWrite, and TodoRead are only client task UI bookkeeping, not implementation progress. For large files, Cheng repositories, or broad code style work, first locate targets with Grep/Glob/Bash. Every Read call for a .cheng file must include limit <= 200, and known line targets must also include offset; never Read whole .cheng files. Do not claim files were updated, tests passed, or code was integrated unless you emitted real Edit/MultiEdit/Write/Bash tool calls and observed their results in this task. Do not invent hash-suffixed repository paths; use cwd-relative paths or verify absolute paths with Bash before Read/Edit."
	case ProfileOpenCode:
		return "Client profile: OpenCode. Emit complete XML tool_calls in one assistant turn. Do not end with future-tense setup text when a tool is needed. Roo/OpenCode invoke-style parameters must include every required field."
	case ProfileRoo:
		return "Client profile: Roo/Cline. Emit complete XML tool_calls or invoke blocks with named parameters. Include all required fields and avoid empty parameters blocks."
	case ProfileCodex:
		return "Client profile: Codex. Keep tool calls concrete and minimal; do not use task-tracking tools as a substitute for file, shell, or edit work. Codex search budget: do not repeat semantically identical Search/Grep/Bash rg calls; once a search returns file:line or a useful path, immediately Read a small bounded window or edit/verify that target. Do not invent hash-suffixed repository paths; use cwd-relative paths or verify absolute paths with Bash before Read/Edit."
	case ProfileAdminWebUI:
		return "Client profile: ds2api Admin WebUI. Return concise visible output and avoid client-specific background-agent assumptions."
	default:
		return ""
	}
}

func profileNameFromText(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case lower == "":
		return ProfileUnknown
	case strings.Contains(lower, "claude-code") || strings.Contains(lower, "claude code") || strings.Contains(lower, "claudecode"):
		return ProfileClaudeCode
	case strings.Contains(lower, "opencode") || strings.Contains(lower, "open-code"):
		return ProfileOpenCode
	case strings.Contains(lower, "roo-code") || strings.Contains(lower, "roo code") || strings.Contains(lower, "cline"):
		return ProfileRoo
	case strings.Contains(lower, "codex"):
		return ProfileCodex
	case strings.Contains(lower, "admin-webui") || strings.Contains(lower, "ds2api-admin") || strings.Contains(lower, "admin webui"):
		return ProfileAdminWebUI
	default:
		return ProfileUnknown
	}
}

type profileCandidate struct {
	value  string
	source string
}

func stringField(m map[string]any, key string) string {
	if len(m) == 0 {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
