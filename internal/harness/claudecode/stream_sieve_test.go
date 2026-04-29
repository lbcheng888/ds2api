package claudecode

import (
	"strings"
	"testing"
)

func TestStreamSieveSuppressesFencedJSONToolText(t *testing.T) {
	state := &StreamSieveState{}
	events := ProcessStreamSieveChunk(state, "Plan approved - implement now\n```json\n{\n  \"tool\": \"TaskCreate\",\n  \"arguments\": {\"subject\": \"Add TokenTracker\"}\n}\n```\n", []string{"TaskCreate", "Read", "Edit", "Bash"})
	events = append(events, FlushStreamSieve(state, []string{"TaskCreate", "Read", "Edit", "Bash"})...)

	var visible strings.Builder
	for _, event := range events {
		visible.WriteString(event.Content)
		if len(event.ToolCalls) > 0 {
			t.Fatalf("fenced JSON text must not be promoted to executable calls, got %#v", event.ToolCalls)
		}
	}
	if strings.Contains(visible.String(), `"tool"`) || strings.Contains(visible.String(), "TaskCreate") || strings.Contains(visible.String(), "```json") {
		t.Fatalf("expected fenced JSON tool text to be suppressed, got %q", visible.String())
	}
	if !strings.Contains(visible.String(), "Plan approved") {
		t.Fatalf("expected prefix text to be preserved, got %q", visible.String())
	}
}

func TestStreamSieveKeepsNonToolJSONFence(t *testing.T) {
	state := &StreamSieveState{}
	text := "Example:\n```json\n{\"ok\":true}\n```\nDone."
	events := ProcessStreamSieveChunk(state, text, []string{"Read", "Bash"})
	events = append(events, FlushStreamSieve(state, []string{"Read", "Bash"})...)

	var visible strings.Builder
	for _, event := range events {
		visible.WriteString(event.Content)
		if len(event.ToolCalls) > 0 {
			t.Fatalf("non-tool JSON fence should not produce tool calls, got %#v", event.ToolCalls)
		}
	}
	if visible.String() != text {
		t.Fatalf("expected non-tool JSON fence to pass through, got %q", visible.String())
	}
}
