package prompt

import (
	"strings"
	"testing"
)

func TestStringifyToolCallArgumentsPreservesConcatenatedJSON(t *testing.T) {
	got := StringifyToolCallArguments(`{}{"query":"测试工具调用"}`)
	if got != `{}{"query":"测试工具调用"}` {
		t.Fatalf("expected raw concatenated JSON to be preserved, got %q", got)
	}
}

func TestFormatToolCallsForPromptXML(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"id": "call_1",
			"function": map[string]any{
				"name":      "search_web",
				"arguments": map[string]any{"query": "latest"},
			},
		},
	})
	if got == "" {
		t.Fatal("expected non-empty formatted tool calls")
	}
	if got != "<tool_calls>\n  <tool_call>\n    <tool_name>search_web</tool_name>\n    <parameters>\n      <query><![CDATA[latest]]></query>\n    </parameters>\n  </tool_call>\n</tool_calls>" {
		t.Fatalf("unexpected formatted tool call XML: %q", got)
	}
}

func TestFormatToolCallsForPromptEscapesXMLEntities(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"name":      "search<&>",
			"arguments": `{"q":"a < b && c > d"}`,
		},
	})
	want := "<tool_calls>\n  <tool_call>\n    <tool_name>search&lt;&amp;&gt;</tool_name>\n    <parameters>\n      <q><![CDATA[a < b && c > d]]></q>\n    </parameters>\n  </tool_call>\n</tool_calls>"
	if got != want {
		t.Fatalf("unexpected escaped tool call XML: %q", got)
	}
}

func TestFormatToolCallsForPromptUsesCDATAForMultilineContent(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"name": "write_file",
			"arguments": map[string]any{
				"path":    "script.sh",
				"content": "#!/bin/bash\nprintf \"hello\"\n",
			},
		},
	})
	want := "<tool_calls>\n  <tool_call>\n    <tool_name>write_file</tool_name>\n    <parameters>\n      <content><![CDATA[#!/bin/bash\nprintf \"hello\"\n]]></content>\n      <path><![CDATA[script.sh]]></path>\n    </parameters>\n  </tool_call>\n</tool_calls>"
	if got != want {
		t.Fatalf("unexpected multiline cdata tool call XML: %q", got)
	}
}

func TestFormatToolCallsForPromptDropsTaskTrackingCalls(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"name": "TaskCreate",
			"input": map[string]any{
				"subject":     "Track work",
				"description": "Only updates the client task list",
			},
		},
		map[string]any{
			"name": "Read",
			"input": map[string]any{
				"file_path": "/tmp/a.txt",
			},
		},
	})
	if got == "" {
		t.Fatal("expected real tool call to remain")
	}
	if contains(got, "TaskCreate") {
		t.Fatalf("expected task tracking call to be dropped, got %q", got)
	}
	if !contains(got, "<tool_name>Read</tool_name>") {
		t.Fatalf("expected Read call to remain, got %q", got)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
