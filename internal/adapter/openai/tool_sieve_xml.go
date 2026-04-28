package openai

import (
	"regexp"

	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

//nolint:unused // kept for package-level test compatibility while implementation lives in claudecode harness.
var xmlToolCallBlockPattern = regexp.MustCompile(claudecodeharness.StreamXMLToolCallBlockPattern().String())

//nolint:unused // package tests keep legacy helper names while implementation lives in claudecode harness.
func consumeXMLToolCapture(captured string, toolNames []string, allowMetaAgentTools bool) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	return claudecodeharness.ConsumeXMLToolCapture(captured, toolNames, allowMetaAgentTools)
}

func hasOpenXMLToolTag(captured string) bool {
	return claudecodeharness.HasOpenXMLToolTag(captured)
}

func findPartialXMLToolTagStart(s string) int {
	return claudecodeharness.FindPartialXMLToolTagStart(s)
}
