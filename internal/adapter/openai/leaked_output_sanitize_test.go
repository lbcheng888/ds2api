package openai

import "testing"

func TestSanitizeLeakedOutputRemovesEmptyJSONFence(t *testing.T) {
	raw := "before\n```json\n```\nafter"
	got := sanitizeLeakedOutput(raw)
	if got != "before\n\nafter" {
		t.Fatalf("unexpected sanitized empty json fence: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesLeakedWireToolCallAndResult(t *testing.T) {
	raw := "开始\n[{\"function\":{\"arguments\":\"{\\\"command\\\":\\\"java -version\\\"}\",\"name\":\"exec\"},\"id\":\"callb9a321\",\"type\":\"function\"}]< | Tool | >{\"content\":\"openjdk version 21\",\"tool_call_id\":\"callb9a321\"}\n结束"
	got := sanitizeLeakedOutput(raw)
	if got != "开始\n\n结束" {
		t.Fatalf("unexpected sanitize result for leaked wire format: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesStandaloneMetaMarkers(t *testing.T) {
	raw := "A<| end_of_sentence |><| Assistant |>B<| end_of_thinking |>C<｜end▁of▁thinking｜>D<｜end▁of▁sentence｜>E<| end_of_toolresults |>F<｜end▁of▁instructions｜>G"
	got := sanitizeLeakedOutput(raw)
	if got != "ABCDEFG" {
		t.Fatalf("unexpected sanitize result for meta markers: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesThinkAndBosMarkers(t *testing.T) {
	raw := "A<think>B</think>C<｜begin▁of▁sentence｜>D<| begin_of_sentence |>E<｜begin_of_sentence｜>F"
	got := sanitizeLeakedOutput(raw)
	if got != "ABCDEF" {
		t.Fatalf("unexpected sanitize result for think/BOS markers: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesDanglingThinkBlock(t *testing.T) {
	raw := "Answer prefix<think>internal reasoning that never closes"
	got := sanitizeLeakedOutput(raw)
	if got != "Answer prefix" {
		t.Fatalf("unexpected sanitize result for dangling think block: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesDanglingToolTagFragment(t *testing.T) {
	raw := "Let me inspect more files.\n\n\ntool_calls>\n"
	got := sanitizeLeakedOutput(raw)
	if got != "Let me inspect more files.\n" {
		t.Fatalf("unexpected sanitize result for dangling tool tag fragment: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesSystemReminderBlocks(t *testing.T) {
	raw := "前文\n<system-reminder>\ninternal planning instructions\n</system-reminder>\n后文"
	got := sanitizeLeakedOutput(raw)
	if got != "前文\n\n后文" {
		t.Fatalf("unexpected sanitize result for system reminder block: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesDanglingSystemReminder(t *testing.T) {
	raw := "前文\n<system-reminder>\ninternal planning instructions"
	got := sanitizeLeakedOutput(raw)
	if got != "前文\n" {
		t.Fatalf("unexpected sanitize result for dangling system reminder: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesCodexCorePrinciplesLeak(t *testing.T) {
	raw := "可见结论\nAs you answer the user's questions, you can use the following context:\n# Codex Core Principles\n- Codex is an AI-first coding agent, built on GPT-5.\n- User messages are in the Codex thread."
	got := sanitizeLeakedOutput(raw)
	if got != "可见结论" {
		t.Fatalf("unexpected sanitize result for codex core principles leak: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesAgentXMLLeaks(t *testing.T) {
	raw := "Done.<attempt_completion><result>Some final answer</result></attempt_completion>"
	got := sanitizeLeakedOutput(raw)
	if got != "Done.Some final answer" {
		t.Fatalf("unexpected sanitize result for agent XML leak: %q", got)
	}
}

func TestSanitizeLeakedOutputPreservesStandaloneResultTags(t *testing.T) {
	raw := "Example XML: <result>value</result>"
	got := sanitizeLeakedOutput(raw)
	if got != raw {
		t.Fatalf("unexpected sanitize result for standalone result tag: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesDanglingAgentXMLOpeningTags(t *testing.T) {
	raw := "Done.<attempt_completion><result>Some final answer"
	got := sanitizeLeakedOutput(raw)
	if got != "Done.Some final answer" {
		t.Fatalf("unexpected sanitize result for dangling opening tags: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesDanglingAgentXMLClosingTags(t *testing.T) {
	raw := "Done.Some final answer</result></attempt_completion>"
	got := sanitizeLeakedOutput(raw)
	if got != "Done.Some final answer" {
		t.Fatalf("unexpected sanitize result for dangling closing tags: %q", got)
	}
}

func TestSanitizeLeakedOutputPreservesUnrelatedResultTagsWhenWrapperLeaks(t *testing.T) {
	raw := "Done.<attempt_completion><result>Some final answer\nExample XML: <result>value</result>"
	got := sanitizeLeakedOutput(raw)
	want := "Done.Some final answer\nExample XML: <result>value</result>"
	if got != want {
		t.Fatalf("unexpected sanitize result for mixed leaked wrapper + xml example: %q", got)
	}
}
