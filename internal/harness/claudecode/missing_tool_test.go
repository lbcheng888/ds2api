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

func TestDetectMissingToolCallBlocksChineseConflictPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "三个文件冲突较多。我先逐个分析，然后处理。每个冲突块都是 HEAD（当前分支）和 7aa650b 之间的差异，策略是保留 HEAD 的内容。",
		ToolNames: []string{"Read", "Bash", "Edit"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected missing tool call decision, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksTaskTrackingOnlyToolCalls(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text: `<tool_calls>
<invoke name="TodoWrite">
<parameter name="todos"><item><content>评估 cheng 语言实现进度并补齐</content><status>pending</status></item></parameter>
</invoke>
</tool_calls>`,
		ToolNames: []string{"TodoWrite", "Read", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected task-tracking-only call to be treated as missing real work, got %#v", got)
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
