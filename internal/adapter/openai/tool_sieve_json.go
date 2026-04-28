package openai

import (
	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

//nolint:unused // package tests keep legacy helper names while implementation lives in claudecode harness.
func findVisibleJSONToolSegmentStart(state *toolStreamSieveState, s string) int {
	return claudecodeharness.FindVisibleJSONToolSegmentStart(state, s)
}

//nolint:unused // package tests keep legacy helper names while implementation lives in claudecode harness.
func findPartialVisibleJSONToolSegmentStart(state *toolStreamSieveState, s string) int {
	return claudecodeharness.FindPartialVisibleJSONToolSegmentStart(state, s)
}

//nolint:unused // package tests keep legacy helper names while implementation lives in claudecode harness.
func consumeVisibleJSONToolCapture(captured string, toolNames []string, allowMetaAgentTools bool) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	return claudecodeharness.ConsumeVisibleJSONToolCapture(captured, toolNames, allowMetaAgentTools)
}

//nolint:unused // package tests keep legacy helper names while implementation lives in claudecode harness.
func visibleJSONToolCaptureMayContinue(captured string) bool {
	return claudecodeharness.VisibleJSONToolCaptureMayContinue(captured)
}

//nolint:unused // package tests keep legacy helper names while implementation lives in claudecode harness.
func jsonLikeStandaloneToolJSONEnd(s string) int {
	return claudecodeharness.JSONLikeStandaloneToolJSONEnd(s)
}
