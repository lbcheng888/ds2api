package claudecode

import "testing"

func TestDetectMissingToolCallBlocksTraceAndFillPromise(t *testing.T) {
	text := "Looking at the code structure, the pure self-build is blocked by functions with Result patterns. Let me trace the remaining gaps and fill them."
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      text,
		ToolNames: []string{"Read", "Bash", "Edit"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected missing tool call decision, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksNowReadingPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:        "Now reading the rest of the plan document and key source files in parallel to assess implementation status.",
		FinalPrompt: "<user>请继续</user>",
		ToolNames:   []string{"Read", "Bash", "Grep"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected reading promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallAllowsTraceTextWithoutTools(t *testing.T) {
	text := "Let me trace the remaining gaps and fill them."
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      text,
		ToolNames: nil,
	})
	if got.Blocked {
		t.Fatalf("expected no block without callable tools, got %#v", got)
	}
}
