package openai

import (
	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

func processToolSieveChunk(state *toolStreamSieveState, chunk string, toolNames []string) []toolStreamEvent {
	return claudecodeharness.ProcessStreamSieveChunk(state, chunk, toolNames)
}

func processToolSieveChunkWithMeta(state *toolStreamSieveState, chunk string, toolNames []string, allowMetaAgentTools bool) []toolStreamEvent {
	return claudecodeharness.ProcessStreamSieveChunkWithMeta(state, chunk, toolNames, allowMetaAgentTools)
}

func flushToolSieve(state *toolStreamSieveState, toolNames []string) []toolStreamEvent {
	return claudecodeharness.FlushStreamSieve(state, toolNames)
}

func flushToolSieveWithMeta(state *toolStreamSieveState, toolNames []string, allowMetaAgentTools bool) []toolStreamEvent {
	return claudecodeharness.FlushStreamSieveWithMeta(state, toolNames, allowMetaAgentTools)
}

//nolint:unused // package tests keep legacy helper names while implementation lives in claudecode harness.
func splitSafeContentForToolDetection(state *toolStreamSieveState, s string) (safe, hold string) {
	return claudecodeharness.SplitSafeContentForToolDetection(state, s)
}

func findToolSegmentStart(state *toolStreamSieveState, s string) int {
	return claudecodeharness.FindStreamToolSegmentStart(state, s)
}

//nolint:unused // package tests keep legacy helper names while implementation lives in claudecode harness.
func consumeToolCapture(state *toolStreamSieveState, toolNames []string, allowMetaAgentTools bool) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	return claudecodeharness.ConsumeStreamToolCapture(state, toolNames, allowMetaAgentTools)
}
