package toolcall

import "testing"

func TestLooksLikeToolCallSyntaxIgnoresFencedExamples(t *testing.T) {
	if LooksLikeToolCallSyntax("```xml\n<tool_calls></tool_calls>\n```") {
		t.Fatalf("expected fenced tool example not to count as executable syntax")
	}
}

func TestLooksLikeToolCallSyntaxDetectsUnfencedMalformedBlock(t *testing.T) {
	if !LooksLikeToolCallSyntax("text\n<tool_calls>\n<parameter name=\"file_path\">/tmp/a</parameter>\n</tool_calls>") {
		t.Fatalf("expected unfenced malformed tool block to count as tool syntax")
	}
}
