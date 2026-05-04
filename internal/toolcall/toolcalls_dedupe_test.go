package toolcall

import "testing"

func TestDedupeParsedToolCallsDedupesReadByPathOffsetLimit(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go", "offset": "10", "limit": int64(20), "description": "first"}},
		{Name: "Read", Input: map[string]any{"filePath": "/tmp/a.go", "offset": int64(10), "limit": "20", "description": "second"}},
		{Name: "Read", Input: map[string]any{"file_path": "/tmp/b.go", "offset": "10", "limit": "20"}},
	}

	got, report := DedupeParsedToolCallsWithReport(calls)
	if len(got) != 2 {
		t.Fatalf("expected duplicate Read range to collapse, got %#v", got)
	}
	if report.ToolCallsDropped != 1 {
		t.Fatalf("expected one dropped tool call, got %#v", report)
	}
	if got[0].Input["description"] != "first" {
		t.Fatalf("expected first duplicate Read call to survive, got %#v", got[0])
	}
}

func TestNormalizeParsedToolCallsForSchemasReportsTodoItemDedupe(t *testing.T) {
	calls := []ParsedToolCall{{
		Name: "TodoWrite",
		Input: map[string]any{
			"todos": []any{
				map[string]any{"content": "探索编译器代码库架构与性能关键路径", "status": "pending"},
				map[string]any{"content": "探索编译器代码库架构与性能关键路径", "status": "pending"},
				map[string]any{"content": "定位冷启动瓶颈", "status": "pending"},
			},
		},
	}}

	got, report := NormalizeParsedToolCallsForSchemasWithReport(calls, nil)
	if report.TodoItemsDropped != 1 {
		t.Fatalf("expected one dropped todo item, got %#v", report)
	}
	todos, _ := got[0].Input["todos"].([]any)
	if len(todos) != 2 {
		t.Fatalf("expected duplicate todo item collapsed, got %#v", got[0].Input["todos"])
	}
}

func TestNormalizeParsedToolCallsDropsCompletedTodoItems(t *testing.T) {
	calls := []ParsedToolCall{{
		Name: "TodoWrite",
		Input: map[string]any{
			"todos": []any{
				map[string]any{"content": "前置优化：TypedExpr 复用 source context", "status": "pending"},
				map[string]any{"content": "前置优化：TypedExpr 复用 source context", "status": "completed"},
				map[string]any{"content": "Lowering 优化：消除 O(n²) call fact 查找", "status": "pending"},
				map[string]any{"content": "Emit 优化：DirectObjectEmit 消除冗余计算", "completed": true},
			},
		},
	}}

	got, report := NormalizeParsedToolCallsForSchemasWithReport(calls, nil)
	if report.TodoItemsDropped != 3 {
		t.Fatalf("expected completed todo items and stale pending duplicate dropped, got %#v", report)
	}
	todos, _ := got[0].Input["todos"].([]any)
	if len(todos) != 1 {
		t.Fatalf("expected only unfinished todo to remain, got %#v", got[0].Input["todos"])
	}
	remaining, _ := todos[0].(map[string]any)
	if remaining["content"] != "Lowering 优化：消除 O(n²) call fact 查找" {
		t.Fatalf("unexpected remaining todo: %#v", remaining)
	}
}

func TestNormalizeParsedToolCallsDedupesUpdatePlanSteps(t *testing.T) {
	calls := []ParsedToolCall{{
		Name: "update_plan",
		Input: map[string]any{
			"explanation": "继续迁移",
			"plan": []any{
				map[string]any{"step": "梳理 harness.md 进度并继续", "status": "pending"},
				map[string]any{"step": "梳理 harness.md 进度并继续", "status": "pending"},
				map[string]any{"step": "梳理 Agents Window 1:1 逆向还原进度并继续", "status": "pending"},
				map[string]any{"step": "梳理 Agents Window 1:1 逆向还原进度并继续", "status": "pending"},
				map[string]any{"step": "已经完成的旧任务", "status": "completed"},
			},
		},
	}}

	got, report := NormalizeParsedToolCallsForSchemasWithReport(calls, nil)
	if report.TodoItemsDropped != 3 {
		t.Fatalf("expected duplicate and completed plan items dropped, got %#v", report)
	}
	plan, _ := got[0].Input["plan"].([]any)
	if len(plan) != 2 {
		t.Fatalf("expected duplicate update_plan steps collapsed, got %#v", got[0].Input["plan"])
	}
	first, _ := plan[0].(map[string]any)
	second, _ := plan[1].(map[string]any)
	if first["step"] != "梳理 harness.md 进度并继续" || second["step"] != "梳理 Agents Window 1:1 逆向还原进度并继续" {
		t.Fatalf("unexpected remaining plan steps: %#v", plan)
	}
}

func TestDedupeParsedToolCallsPreservesReadOffsetLimitDifferences(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go", "offset": 10, "limit": 20}},
		{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go", "offset": 11, "limit": 20}},
		{Name: "Read", Input: map[string]any{"file_path": "/tmp/a.go", "offset": 10, "limit": 30}},
	}

	got := DedupeParsedToolCalls(calls)
	if len(got) != 3 {
		t.Fatalf("expected different Read ranges to be preserved, got %#v", got)
	}
}

func TestDedupeParsedToolCallsDedupesReadFilePathAliases(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "read_file", Input: map[string]any{"path": " /tmp/a.go ", "note": "first"}},
		{Name: "read_file", Input: map[string]any{"file_path": "/tmp/a.go", "note": "second"}},
	}

	got := DedupeParsedToolCalls(calls)
	if len(got) != 1 {
		t.Fatalf("expected duplicate read_file path aliases to collapse, got %#v", got)
	}
	if got[0].Input["note"] != "first" {
		t.Fatalf("expected first duplicate read_file call to survive, got %#v", got[0])
	}
}

func TestDedupeParsedToolCallsDedupesGrepByPatternScope(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Grep", Input: map[string]any{"pattern": "TODO", "path": "/tmp/src", "output_mode": "content", "description": "first"}},
		{Name: "Grep", Input: map[string]any{"pattern": "TODO", "path": "/tmp/src", "output_mode": "content", "description": "second"}},
		{Name: "Grep", Input: map[string]any{"pattern": "TODO", "path": "/tmp/src", "output_mode": "files_with_matches"}},
	}

	got := DedupeParsedToolCalls(calls)
	if len(got) != 2 {
		t.Fatalf("expected duplicate Grep signature to collapse only once, got %#v", got)
	}
	if got[0].Input["description"] != "first" {
		t.Fatalf("expected first duplicate Grep call to survive, got %#v", got[0])
	}
}

func TestDedupeParsedToolCallsDedupesSearchByQueryScope(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Search", Input: map[string]any{"query": "func main", "path": "/tmp/src", "limit": 20, "note": "first"}},
		{Name: "Search", Input: map[string]any{"q": "func main", "path": "/tmp/src", "limit": 20, "note": "second"}},
		{Name: "Search", Input: map[string]any{"q": "func main", "path": "/tmp/tests", "limit": 20}},
	}

	got := DedupeParsedToolCalls(calls)
	if len(got) != 2 {
		t.Fatalf("expected duplicate Search signature to collapse only once, got %#v", got)
	}
	if got[0].Input["note"] != "first" {
		t.Fatalf("expected first duplicate Search call to survive, got %#v", got[0])
	}
}

func TestDedupeParsedToolCallsDedupesGlobByPatternScope(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Glob", Input: map[string]any{"pattern": "**/*.go", "path": "/tmp/src", "note": "first"}},
		{Name: "Glob", Input: map[string]any{"pattern": "**/*.go", "path": "/tmp/src", "note": "second"}},
		{Name: "Glob", Input: map[string]any{"pattern": "**/*.md", "path": "/tmp/src"}},
	}

	got := DedupeParsedToolCalls(calls)
	if len(got) != 2 {
		t.Fatalf("expected duplicate Glob signature to collapse only once, got %#v", got)
	}
	if got[0].Input["note"] != "first" {
		t.Fatalf("expected first duplicate Glob call to survive, got %#v", got[0])
	}
}

func TestDedupeParsedToolCallsPreservesShellCommandInternalWhitespace(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Bash", Input: map[string]any{"command": "printf 'a b'"}},
		{Name: "Bash", Input: map[string]any{"command": "printf 'a  b'"}},
	}

	got := DedupeParsedToolCalls(calls)
	if len(got) != 2 {
		t.Fatalf("expected shell commands with different internal whitespace to be preserved, got %#v", got)
	}
}

func TestDedupeParsedToolCallsDedupesBashGitStatusRedirectionVariants(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Bash", Input: map[string]any{"command": "git -C /Users/lbcheng/cheng-lang status --short 2>/dev/null"}},
		{Name: "Bash", Input: map[string]any{"command": "git -C /Users/lbcheng/cheng-lang status --short 2>&1"}},
	}

	got, report := DedupeParsedToolCallsWithReport(calls)
	if len(got) != 1 {
		t.Fatalf("expected duplicate git status redirection variants to collapse, got %#v", got)
	}
	if report.ToolCallsDropped != 1 {
		t.Fatalf("expected one dropped Bash call, got %#v", report)
	}
}
