package toolstream

import (
	"ds2api/internal/toolcall"
	"strings"
)

type State struct {
	pending                strings.Builder
	capture                strings.Builder
	capturing              bool
	codeFenceStack         []int
	codeFencePendingTicks  int
	codeFencePendingTildes int
	codeFenceNotLineStart  bool // inverted: zero-value false means "at line start"
	pendingToolRaw         string
	pendingToolCalls       []toolcall.ParsedToolCall
	disableDeltas          bool
	toolNameSent           bool
	toolName               string
	toolArgsStart          int
	toolArgsSent           int
	toolArgsString         bool
	toolArgsDone           bool
}

type Event struct {
	Content        string
	ToolCalls      []toolcall.ParsedToolCall
	ToolCallDeltas []ToolCallDelta
	ErrorCode      string
	ErrorMessage   string
}

type ToolCallDelta struct {
	Index     int
	Name      string
	Arguments string
}

func (s *State) resetIncrementalToolState() {
	s.disableDeltas = false
	s.toolNameSent = false
	s.toolName = ""
	s.toolArgsStart = -1
	s.toolArgsSent = -1
	s.toolArgsString = false
	s.toolArgsDone = false
}

func (s *State) noteText(content string) {
	if !hasMeaningfulText(content) {
		return
	}
	updateCodeFenceState(s, content)
}

func hasMeaningfulText(text string) bool {
	return strings.TrimSpace(text) != ""
}

func insideCodeFenceWithState(state *State, text string) bool {
	if state == nil {
		return toolcall.InsideCodeFence(text)
	}
	return toolcall.InsideCodeFenceWithState(
		state.codeFenceStack,
		state.codeFencePendingTicks,
		state.codeFencePendingTildes,
		!state.codeFenceNotLineStart,
		text,
	)
}

func insideCodeFence(text string) bool {
	return toolcall.InsideCodeFence(text)
}

func updateCodeFenceState(state *State, text string) {
	if state == nil || !hasMeaningfulText(text) {
		return
	}
	next := toolcall.UpdateCodeFenceState(
		state.codeFenceStack,
		state.codeFencePendingTicks,
		state.codeFencePendingTildes,
		!state.codeFenceNotLineStart,
		text,
	)
	state.codeFenceStack = next.Stack
	state.codeFencePendingTicks = next.PendingTicks
	state.codeFencePendingTildes = next.PendingTildes
	state.codeFenceNotLineStart = !next.LineStart
}
