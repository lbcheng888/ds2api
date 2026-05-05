package claude

import (
	"strings"
	"testing"
)

func TestCleanVisibleOutputRemovesDanglingToolTagFragment(t *testing.T) {
	got := cleanVisibleOutput("Let me inspect more files.\n\n\ntool_calls>\n", false)
	if strings.Contains(got, "tool_calls>") {
		t.Fatalf("expected dangling tool tag fragment to be removed, got %q", got)
	}
	if !strings.Contains(got, "Let me inspect more files.") {
		t.Fatalf("expected prose to remain, got %q", got)
	}
}

func TestCleanVisibleOutputTruncatesLeakedEndOfSentenceReplay(t *testing.T) {
	got := cleanVisibleOutput("<｜end▁of▁sentence｜>#!!/usr/bin/env node\nimport 'x';", false)
	if got != "" {
		t.Fatalf("expected leaked prompt replay to be dropped, got %q", got)
	}
}
