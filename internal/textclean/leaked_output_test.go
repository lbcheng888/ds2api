package textclean

import (
	"strings"
	"testing"
)

func TestStreamSanitizerRemovesSplitSystemReminder(t *testing.T) {
	var s StreamSanitizer
	got := s.Sanitize("前文\n<system-rem") +
		s.Sanitize("inder>\nsecret line\n") +
		s.Sanitize("</system-reminder>\n后文")
	if got != "前文\n\n后文" {
		t.Fatalf("unexpected stream sanitize result: %q", got)
	}
}

func TestStreamSanitizerRemovesCodexCorePrinciplesToEnd(t *testing.T) {
	var s StreamSanitizer
	got := s.Sanitize("可见结论\nAs you answer the user's questions, you can use the following context:\n") +
		s.Sanitize("# Codex Core Principles\n- Codex is an AI-first coding agent, built on GPT-5.\n") +
		s.Sanitize("more internal rules")
	if got != "可见结论\n" {
		t.Fatalf("unexpected codex core stream sanitize result: %q", got)
	}
}

func TestStripDSMLContentRemovesFullBlock(t *testing.T) {
	input := "可见内容\n<|DSML|tool_calls>\n<|DSML|invoke name=\"Bash\">\n<|DSML|parameter name=\"command\" string=\"true\">echo hi</|DSML|parameter>\n</|DSML|invoke>\n</|DSML|tool_calls>"
	got := StripDSMLContent(input)
	if strings.TrimSpace(got) != "可见内容" {
		t.Fatalf("DSML block not stripped: %q", got)
	}
}

func TestStripDSMLContentRemovesTagLines(t *testing.T) {
	input := "前文\n<|DSML|tool_calls>\n<|DSML|invoke name=\"Read\">\n后文"
	got := StripDSMLContent(input)
	if got != "前文\n后文" {
		t.Fatalf("DSML tag lines not stripped: %q", got)
	}
}

func TestStripDSMLContentKeepsNormalText(t *testing.T) {
	input := "这是正常的文章内容"
	got := StripDSMLContent(input)
	if got != input {
		t.Fatalf("normal text was wrongly modified: %q", got)
	}
}

func TestStripDSMLContentEmptyInput(t *testing.T) {
	got := StripDSMLContent("")
	if got != "" {
		t.Fatalf("empty input should return empty: %q", got)
	}
}

func TestStripDSMLContentInlineNoNewlines(t *testing.T) {
	// All on one line - common leak pattern
	input := "visible text <|DSML|tool_calls><|DSML|invoke name=\"Bash\"><|DSML|parameter name=\"command\" string=\"true\">echo hi</|DSML|parameter></|DSML|invoke></|DSML|tool_calls>"
	got := StripDSMLContent(input)
	if strings.TrimSpace(got) != "visible text" {
		t.Fatalf("inline DSML not stripped: %q", got)
	}
}

func TestStripDSMLContentStripsParameterInnerText(t *testing.T) {
	// Tags stripped but parameter content (description text) must also be removed
	input := "visible\n<|DSML|tool_calls>\n<|DSML|invoke name=\"Agent\">\n<|DSML|parameter name=\"description\">The user's task: About 10 minutes ago...</|DSML|parameter>\n<|DSML|parameter name=\"prompt\">Verify whether...</|DSML|parameter>\n</|DSML|invoke>\n</|DSML|tool_calls>"
	got := StripDSMLContent(input)
	if strings.Contains(got, "user's task") || strings.Contains(got, "Verify whether") || strings.Contains(got, "DSML") {
		t.Fatalf("parameter content not stripped: %q", got)
	}
	if strings.TrimSpace(got) != "visible" {
		t.Fatalf("visible text lost or extra content: %q", got)
	}
}

func TestStripDSMLContentMixedWithText(t *testing.T) {
	// DSML after visible text with line breaks
	input := "根据搜索结果...\n\n<|DSML|tool_calls>\n<|DSML|invoke name=\"WebFetch\">\n<|DSML|parameter name=\"url\" string=\"true\">https://example.com</|DSML|parameter>\n</|DSML|invoke>\n</|DSML|tool_calls>"
	got := StripDSMLContent(input)
	if strings.Contains(got, "DSML") || strings.Contains(got, "tool_calls") || strings.Contains(got, "invoke") {
		t.Fatalf("mixed DSML not stripped: %q", got)
	}
	if !strings.Contains(got, "搜索结果") {
		t.Fatalf("visible text lost: %q", got)
	}
}

func TestStreamSanitizerKeepsNormalText(t *testing.T) {
	var s StreamSanitizer
	got := s.Sanitize("正常 <tag> 文本\n") + s.Sanitize("继续")
	if got != "正常 <tag> 文本\n继续" {
		t.Fatalf("unexpected normal stream sanitize result: %q", got)
	}
}
