package textclean

import "testing"

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

func TestStreamSanitizerKeepsNormalText(t *testing.T) {
	var s StreamSanitizer
	got := s.Sanitize("正常 <tag> 文本\n") + s.Sanitize("继续")
	if got != "正常 <tag> 文本\n继续" {
		t.Fatalf("unexpected normal stream sanitize result: %q", got)
	}
}
