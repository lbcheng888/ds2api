package claudecode

import (
	"strings"
	"testing"

	"ds2api/internal/toolcall"
)

func TestDetectRepeatedExplorationBlocksSameBatchRead(t *testing.T) {
	got := DetectRepeatedExploration(ExplorationGuardInput{
		Calls: []toolcall.ParsedToolCall{
			{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go", "offset": int64(0), "limit": int64(100)}},
			{Name: "read_file", Input: map[string]any{"path": "/tmp/a.go", "offset": "0", "limit": "100"}},
		},
	})
	if !got.Blocked || got.Reason != ExplorationGuardDuplicateReason || len(got.DuplicateKeys) != 1 {
		t.Fatalf("expected duplicate Read to be blocked, got %#v", got)
	}
	if !strings.Contains(got.DuplicateKeys[0], "read|") {
		t.Fatalf("expected Read duplicate key, got %#v", got.DuplicateKeys)
	}
}

func TestDetectRepeatedExplorationBlocksCurrentTurnHistoryRead(t *testing.T) {
	finalPrompt := `<｜User｜>继续当前任务<｜Assistant｜>
<|DSML|tool_calls>
  <|DSML|invoke name="Read">
    <|DSML|parameter name="file_path"><![CDATA[/tmp/a.go]]></|DSML|parameter>
    <|DSML|parameter name="offset">0</|DSML|parameter>
    <|DSML|parameter name="limit">120</|DSML|parameter>
  </|DSML|invoke>
</|DSML|tool_calls>
<｜Tool｜>file content<｜Assistant｜>`

	got := DetectRepeatedExploration(ExplorationGuardInput{
		FinalPrompt: finalPrompt,
		Calls: []toolcall.ParsedToolCall{
			{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go", "offset": int64(0), "limit": int64(120)}},
		},
	})
	if !got.Blocked || len(got.DuplicateKeys) != 1 {
		t.Fatalf("expected history Read duplicate to be blocked, got %#v", got)
	}
}

func TestDetectRepeatedExplorationBlocksSearchToolsByScope(t *testing.T) {
	tests := []struct {
		name  string
		calls []toolcall.ParsedToolCall
	}{
		{
			name: "grep",
			calls: []toolcall.ParsedToolCall{
				{Name: "Grep", Input: map[string]any{"pattern": "TODO", "path": "/tmp/src", "output_mode": "content"}},
				{Name: "Grep", Input: map[string]any{"query": "TODO", "path": "/tmp/src", "outputMode": "content"}},
			},
		},
		{
			name: "search",
			calls: []toolcall.ParsedToolCall{
				{Name: "Search", Input: map[string]any{"query": "func main", "path": "/tmp/src", "scope": "go"}},
				{Name: "Search", Input: map[string]any{"q": "func main", "path": "/tmp/src", "scope": "go"}},
			},
		},
		{
			name: "glob",
			calls: []toolcall.ParsedToolCall{
				{Name: "Glob", Input: map[string]any{"pattern": "**/*.go", "path": "/tmp/src"}},
				{Name: "Glob", Input: map[string]any{"glob": "**/*.go", "path": "/tmp/src"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectRepeatedExploration(ExplorationGuardInput{Calls: tt.calls})
			if !got.Blocked || len(got.DuplicateKeys) != 1 {
				t.Fatalf("expected duplicate %s call to be blocked, got %#v", tt.name, got)
			}
		})
	}
}

func TestDetectRepeatedExplorationBlocksRepeatedRgButAllowsGoTest(t *testing.T) {
	rg := DetectRepeatedExploration(ExplorationGuardInput{
		Calls: []toolcall.ParsedToolCall{
			{Name: "Bash", Input: map[string]any{"command": "rg Foo internal/harness/claudecode"}},
			{Name: "exec_command", Input: map[string]any{"cmd": "rg Foo internal/harness/claudecode"}},
		},
	})
	if !rg.Blocked || len(rg.DuplicateKeys) != 1 {
		t.Fatalf("expected repeated rg command to be blocked, got %#v", rg)
	}

	goTest := DetectRepeatedExploration(ExplorationGuardInput{
		Calls: []toolcall.ParsedToolCall{
			{Name: "Bash", Input: map[string]any{"command": "go test ./internal/harness/claudecode"}},
			{Name: "exec_command", Input: map[string]any{"cmd": "go test ./internal/harness/claudecode"}},
		},
	})
	if goTest.Blocked {
		t.Fatalf("expected repeated go test to be allowed, got %#v", goTest)
	}
}

func TestDetectRepeatedExplorationBlocksRepeatedGitInspectionVariants(t *testing.T) {
	finalPrompt := `<｜User｜>检查工作区
<｜Assistant｜><|DSML|tool_calls>
  <|DSML|invoke name="Bash">
    <|DSML|parameter name="command"><![CDATA[git -C /Users/lbcheng/cheng-lang status --short 2>/dev/null]]></|DSML|parameter>
  </|DSML|invoke>
</|DSML|tool_calls>
<｜Tool｜>
<｜Assistant｜><|DSML|tool_calls>
  <|DSML|invoke name="Bash">
    <|DSML|parameter name="command"><![CDATA[git -C /Users/lbcheng/cheng-lang diff --stat HEAD]]></|DSML|parameter>
  </|DSML|invoke>
</|DSML|tool_calls>
<｜Tool｜>no diff`

	got := DetectRepeatedExploration(ExplorationGuardInput{
		FinalPrompt: finalPrompt,
		Calls: []toolcall.ParsedToolCall{
			{Name: "Bash", Input: map[string]any{"command": `git -C /Users/lbcheng/cheng-lang status --short 2>&1; echo "---"; git -C /Users/lbcheng/cheng-lang diff --stat HEAD 2>&1`}},
		},
	})
	if !got.Blocked || len(got.DuplicateKeys) == 0 {
		t.Fatalf("expected repeated git status/diff inspection to be blocked, got %#v", got)
	}
}

func TestDetectRepeatedExplorationBlocksSameBatchGitInspectionSegments(t *testing.T) {
	got := DetectRepeatedExploration(ExplorationGuardInput{
		Calls: []toolcall.ParsedToolCall{
			{Name: "Bash", Input: map[string]any{"command": `git -C /Users/lbcheng/cheng-lang status --short 2>/dev/null`}},
			{Name: "Bash", Input: map[string]any{"command": `git -C /Users/lbcheng/cheng-lang status --short 2>&1; echo "---"; git -C /Users/lbcheng/cheng-lang diff --stat HEAD 2>&1`}},
		},
	})
	if !got.Blocked || len(got.DuplicateKeys) == 0 {
		t.Fatalf("expected same-batch git inspection segment repeat to be blocked, got %#v", got)
	}
}

func TestDetectRepeatedExplorationReadsCurrentInputHistoryTranscript(t *testing.T) {
	finalPrompt := `# DS2API_HISTORY.txt
Prior conversation history and tool progress.

=== 1. USER ===
继续修复重复 git 检查

=== 2. ASSISTANT ===
<|DSML|tool_calls>
  <|DSML|invoke name="Bash">
    <|DSML|parameter name="command"><![CDATA[git status --short && echo "===" && git diff --stat HEAD]]></|DSML|parameter>
  </|DSML|invoke>
</|DSML|tool_calls>

=== 3. TOOL ===
 M internal/harness/claudecode/exploration_guard.go

<｜begin▁of▁sentence｜><｜User｜>Continue from the latest state in the attached DS2API_HISTORY.txt context.<｜Assistant｜>`

	got := DetectRepeatedExploration(ExplorationGuardInput{
		FinalPrompt: finalPrompt,
		Calls: []toolcall.ParsedToolCall{
			{Name: "Bash", Input: map[string]any{"command": `git status --short && echo "===" && git diff --stat HEAD && echo "===" && git diff --name-only HEAD`}},
		},
	})
	if !got.Blocked || len(got.DuplicateKeys) == 0 {
		t.Fatalf("expected DS2API_HISTORY git inspection repeat to be blocked, got %#v", got)
	}
}

func TestDetectRepeatedExplorationBlocksGitCwdVariant(t *testing.T) {
	got := DetectRepeatedExploration(ExplorationGuardInput{
		Calls: []toolcall.ParsedToolCall{
			{Name: "Bash", Input: map[string]any{"command": `git status --short && git diff --stat HEAD`}},
			{Name: "Bash", Input: map[string]any{"command": `git -C /Users/lbcheng/cheng-lang status --short 2>&1`}},
		},
	})
	if !got.Blocked || len(got.DuplicateKeys) == 0 {
		t.Fatalf("expected git -C status variant to be blocked, got %#v", got)
	}
}

func TestDetectRepeatedExplorationAllowsGitWriteSubcommands(t *testing.T) {
	got := DetectRepeatedExploration(ExplorationGuardInput{
		Calls: []toolcall.ParsedToolCall{
			{Name: "Bash", Input: map[string]any{"command": `git add internal/harness/claudecode/exploration_guard.go`}},
			{Name: "Bash", Input: map[string]any{"command": `git add internal/harness/claudecode/exploration_guard.go`}},
		},
	})
	if got.Blocked {
		t.Fatalf("expected repeated git add to be allowed by exploration guard, got %#v", got)
	}
}

func TestDetectRepeatedExplorationBlocksRepeatedAgentDescription(t *testing.T) {
	got := DetectRepeatedExploration(ExplorationGuardInput{
		Calls: []toolcall.ParsedToolCall{
			{Name: "Agent", Input: map[string]any{"description": "Inspect harness state", "prompt": "Read state.go"}},
			{Name: "Agent", Input: map[string]any{"description": " Inspect  harness state ", "prompt": "Read final_output.go"}},
		},
	})
	if !got.Blocked || len(got.DuplicateKeys) != 1 {
		t.Fatalf("expected repeated Agent description to be blocked, got %#v", got)
	}
	if !strings.Contains(got.DuplicateKeys[0], "agent|") {
		t.Fatalf("expected Agent duplicate key, got %#v", got.DuplicateKeys)
	}
}

func TestDetectRepeatedExplorationIgnoresTaskTrackingTools(t *testing.T) {
	got := DetectRepeatedExploration(ExplorationGuardInput{
		Calls: []toolcall.ParsedToolCall{
			{Name: "TaskCreate", Input: map[string]any{"subject": "Explore", "description": "Track only"}},
			{Name: "TaskCreate", Input: map[string]any{"subject": "Explore", "description": "Track only"}},
			{Name: "TodoWrite", Input: map[string]any{"todos": []any{"one", "one"}}},
			{Name: "TodoWrite", Input: map[string]any{"todos": []any{"one", "one"}}},
		},
	})
	if got.Blocked {
		t.Fatalf("expected task tracking tools to be ignored, got %#v", got)
	}
}

func TestDetectRepeatedExplorationProvidesRecoveryDirective(t *testing.T) {
	got := DetectRepeatedExploration(ExplorationGuardInput{
		Calls: []toolcall.ParsedToolCall{
			{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go", "offset": int64(0), "limit": int64(100)}},
			{Name: "read_file", Input: map[string]any{"path": "/tmp/a.go", "offset": "0", "limit": "100"}},
		},
	})
	if !got.Blocked {
		t.Fatalf("expected duplicate Read to be blocked, got %#v", got)
	}
	if got.RecoveryDirective == "" {
		t.Fatalf("expected RecoveryDirective to be set for duplicate calls")
	}
	if !strings.Contains(got.RecoveryDirective, "重复探索") && !strings.Contains(got.RecoveryDirective, "停止搜索") {
		t.Fatalf("unexpected RecoveryDirective content: %q", got.RecoveryDirective)
	}
}
