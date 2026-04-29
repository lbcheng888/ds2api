package shared

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"ds2api/internal/protocol"
)

func RequestAffinityKey(req *http.Request, payload map[string]any) string {
	for _, value := range requestAffinityCandidates(req, payload) {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	if protocol.DetectClientProfile(req, payload).Name == protocol.ProfileClaudeCode {
		return derivedClaudeCodeAffinityKey(payload)
	}
	return ""
}

func requestAffinityCandidates(req *http.Request, payload map[string]any) []string {
	out := []string{}
	if req != nil {
		for _, header := range []string{
			"X-Ds2-Conversation-ID",
			"X-Ds2-Conversation-Key",
			"X-Ds2-Session-ID",
			"X-Conversation-ID",
			"X-Thread-ID",
			"X-Session-ID",
		} {
			out = append(out, req.Header.Get(header))
		}
		if req.URL != nil {
			q := req.URL.Query()
			for _, key := range []string{"conversation_id", "conversation", "thread_id", "thread", "session_id", "session"} {
				out = append(out, q.Get(key))
			}
		}
	}
	for _, key := range []string{"conversation_id", "conversation", "thread_id", "thread", "session_id", "session"} {
		out = append(out, affinityString(payload[key]))
	}
	if meta, _ := payload["metadata"].(map[string]any); meta != nil {
		for _, key := range []string{"conversation_id", "conversation", "thread_id", "thread", "session_id", "session", "user_id"} {
			out = append(out, affinityString(meta[key]))
		}
	}
	return out
}

func affinityString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return ""
	}
}

func derivedClaudeCodeAffinityKey(payload map[string]any) string {
	firstUser := firstUserMessageText(payload)
	if len([]rune(firstUser)) < 24 {
		return ""
	}
	workspace := workspaceHintFromPayload(payload)
	model := strings.TrimSpace(affinityString(payload["model"]))
	sum := sha256.Sum256([]byte("claude_code\x00" + model + "\x00" + workspace + "\x00" + firstUser))
	return "derived:claude_code:" + hex.EncodeToString(sum[:8])
}

func firstUserMessageText(payload map[string]any) string {
	messages, _ := payload["messages"].([]any)
	for _, item := range messages {
		msg, _ := item.(map[string]any)
		if strings.ToLower(strings.TrimSpace(affinityString(msg["role"]))) != "user" {
			continue
		}
		text := compactAffinityText(contentText(msg["content"]))
		if text != "" {
			return text
		}
	}
	return ""
}

func workspaceHintFromPayload(payload map[string]any) string {
	messages, _ := payload["messages"].([]any)
	for _, item := range messages {
		msg, _ := item.(map[string]any)
		text := contentText(msg["content"])
		for _, marker := range []string{"Workspace Path:", "Primary working directory:", "Working directory:"} {
			if idx := strings.Index(text, marker); idx >= 0 {
				tail := strings.TrimSpace(text[idx+len(marker):])
				if fields := strings.Fields(tail); len(fields) > 0 {
					return fields[0]
				}
			}
		}
	}
	return ""
}

func contentText(raw any) string {
	switch x := raw.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			m, _ := item.(map[string]any)
			if text := affinityString(m["text"]); text != "" {
				parts = append(parts, text)
				continue
			}
			if text := affinityString(m["content"]); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func compactAffinityText(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	const maxRunes = 512
	runes := []rune(text)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes)
}
