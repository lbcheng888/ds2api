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

func TestDetectMissingToolCallBlocksChineseInProgressParallelWork(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "我正在并行处理三项改动。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected in-progress work promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksChineseRunTestsPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "编译通过，现在运行测试验证。</｜Assistant｜>",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected test-run promise without Bash call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksFencedJSONToolCallText(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "Plan approved - implement now\n```json\n{\n  \"tool\": \"TaskCreate\",\n  \"arguments\": {\n    \"subject\": \"Add TokenTracker\",\n    \"description\": \"Create internal/auth/token_tracker.go\"\n  }\n}\n```",
		ToolNames: []string{"TaskCreate", "Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected fenced JSON tool text to be blocked as missing real tool call, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksUnsupportedCompletionClaim(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:        "缓存已集成到 handler_chat.go 的非流式路径，stop_reason 映射也已更新，这两项改动现在都已完成。",
		FinalPrompt: "<｜User｜>请完善Claude Code专属harness<｜Assistant｜>",
		ToolNames:   []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected completion claim without execution tool evidence to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallAllowsCompletionClaimAfterExecutionTool(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text: "handler_chat.go 已更新，测试通过。",
		FinalPrompt: `<｜User｜>请修复 handler_chat.go<｜Assistant｜><|DSML|tool_calls>
  <|DSML|invoke name="Edit">
    <|DSML|parameter name="file_path">handler_chat.go</|DSML|parameter>
  </|DSML|invoke>
</|DSML|tool_calls><｜Tool｜>edited<｜end▁of▁toolresults｜><｜Assistant｜>`,
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if got.Blocked {
		t.Fatalf("expected completion summary after execution tool evidence to be allowed, got %#v", got)
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
func TestDetectMissingBlockNoToolReadVerb(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "先阅读现有代码，理解当前实现，然后扩展。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '阅读' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolUnderstandVerb(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "先理解当前CFG lowering实现情况。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '理解' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolEvaluateVerb(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "先评估当前的实现状态和进度。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '评估' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolLearnAboutVerb(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "先了解项目的整体结构。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '了解' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolINeedToPrefix(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "I need to read the file and understand the structure.",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for 'i need to' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolNextStepPrefix(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "下一步开始推进剩余的修复工作。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '下一步' prefix, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolNeedCreateCodingAction(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:        "需要创建一个新的文件来处理配置。",
		FinalPrompt: "<user>请继续</user>",
		ToolNames:   []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '需要创建' coding action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolCreateFilePlan(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "创建文件 /tmp/test_config.json 来保存配置。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '创建文件' file plan, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolWriteToFilePlan(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "write to the config file at /tmp/settings.json",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for 'write to ' file plan, got %#v", got)
	}
}

func TestDetectMissingToolCallAllowsSimpleChatResponse(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "好的，我已经完成了所有的修改，测试也通过了。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if got.Blocked {
		t.Fatalf("expected simple completion message to be allowed, got %#v", got)
	}
}

