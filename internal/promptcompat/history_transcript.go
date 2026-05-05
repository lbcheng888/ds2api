package promptcompat

import (
	"fmt"
	"strings"
)

const CurrentInputContextFilename = "DS2API_HISTORY.txt"

const historyTranscriptTitle = "# DS2API_HISTORY.txt"
const historyTranscriptSummary = "Prior conversation history and tool progress."

func BuildOpenAIHistoryTranscript(messages []any) string {
	return buildOpenAIHistoryTranscript(messages)
}

func BuildOpenAICurrentUserInputTranscript(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return buildOpenAIHistoryTranscript([]any{
		map[string]any{"role": "user", "content": text},
	})
}

func BuildOpenAICurrentInputContextTranscript(messages []any) string {
	return buildOpenAIHistoryTranscript(messages)
}

func buildOpenAIHistoryTranscript(messages []any) string {
	if len(messages) == 0 {
		return ""
	}
	entries := make([]historyTranscriptEntry, 0, len(messages))
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := normalizeOpenAIRoleForPrompt(strings.ToLower(strings.TrimSpace(asString(msg["role"]))))
		content := strings.TrimSpace(buildOpenAIHistoryEntry(role, msg))
		if content == "" {
			continue
		}
		entries = append(entries, historyTranscriptEntry{
			Role:    roleLabelForHistory(role),
			Content: content,
		})
	}
	if len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(historyTranscriptTitle)
	b.WriteString("\n")
	b.WriteString(historyTranscriptSummary)
	b.WriteString("\n\n")
	writeHistoryTranscriptMetadata(&b, entries)
	b.WriteString("\n")

	for i, entry := range entries {
		fmt.Fprintf(&b, "=== %d. %s ===\n%s\n\n", i+1, strings.ToUpper(entry.Role), entry.Content)
	}

	transcript := strings.TrimSpace(b.String())
	if transcript == "" {
		return ""
	}
	return transcript + "\n"
}

type historyTranscriptEntry struct {
	Role    string
	Content string
}

func writeHistoryTranscriptMetadata(b *strings.Builder, entries []historyTranscriptEntry) {
	fmt.Fprintf(b, "metadata:\n")
	fmt.Fprintf(b, "total_entries=%d\n", len(entries))
	fmt.Fprintf(b, "latest_user_entry=%d\n", latestHistoryEntryIndex(entries, "user"))
	fmt.Fprintf(b, "latest_tool_entry=%d\n", latestHistoryEntryIndex(entries, "tool"))
}

func latestHistoryEntryIndex(entries []historyTranscriptEntry, role string) int {
	for i := len(entries) - 1; i >= 0; i-- {
		if strings.EqualFold(entries[i].Role, role) {
			return i + 1
		}
	}
	return 0
}

func buildOpenAIHistoryEntry(role string, msg map[string]any) string {
	switch role {
	case "assistant":
		return strings.TrimSpace(buildAssistantContentForPrompt(msg))
	case "tool", "function":
		return strings.TrimSpace(buildToolHistoryContent(msg))
	case "system", "user":
		return strings.TrimSpace(NormalizeOpenAIContentForPrompt(msg["content"]))
	default:
		return strings.TrimSpace(NormalizeOpenAIContentForPrompt(msg["content"]))
	}
}

func buildToolHistoryContent(msg map[string]any) string {
	content := strings.TrimSpace(NormalizeOpenAIContentForPrompt(msg["content"]))
	parts := make([]string, 0, 2)
	if name := strings.TrimSpace(asString(msg["name"])); name != "" {
		parts = append(parts, "name="+name)
	}
	if callID := strings.TrimSpace(asString(msg["tool_call_id"])); callID != "" {
		parts = append(parts, "tool_call_id="+callID)
	}
	header := ""
	if len(parts) > 0 {
		header = "[" + strings.Join(parts, " ") + "]"
	}
	switch {
	case header != "" && content != "":
		return header + "\n" + content
	case header != "":
		return header
	default:
		return content
	}
}

func roleLabelForHistory(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "function":
		return "tool"
	case "":
		return "unknown"
	default:
		return role
	}
}
