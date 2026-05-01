package toolcall

import "math"

// CodeFenceState holds the result of a code fence state update.
type CodeFenceState struct {
	Stack         []int
	PendingTicks  int
	PendingTildes int
	LineStart     bool
}

// StripFencedCodeBlocks strips fenced code blocks (``` and ~~~) from text.
func StripFencedCodeBlocks(text string) string {
	return stripFencedCodeBlocks(text)
}

// InsideCodeFence checks whether the given text ends inside an unclosed
// fenced code block (backtick or tilde).
func InsideCodeFence(text string) bool {
	if text == "" {
		return false
	}
	return len(simulateCodeFenceState(nil, 0, 0, true, text).Stack) > 0
}

// InsideCodeFenceWithState checks whether the given text is inside an
// unclosed fenced code block, given the current fence stack and pending
// counters.
func InsideCodeFenceWithState(stack []int, pendingTicks, pendingTildes int, lineStart bool, text string) bool {
	simulated := simulateCodeFenceState(stack, pendingTicks, pendingTildes, lineStart, text)
	return len(simulated.Stack) > 0
}

// UpdateCodeFenceState computes the next code fence state after processing
// the given text chunk, given the current state.
func UpdateCodeFenceState(stack []int, pendingTicks, pendingTildes int, lineStart bool, text string) CodeFenceState {
	return simulateCodeFenceState(stack, pendingTicks, pendingTildes, lineStart, text)
}

// simulateCodeFenceState processes the text character by character to track
// fenced code block nesting. Positive stack values = backtick fences,
// negative = tilde fences. Closing must match fence type.
func simulateCodeFenceState(stack []int, pendingTicks, pendingTildes int, lineStart bool, text string) CodeFenceState {
	nextStack := append([]int(nil), stack...)
	ticks := pendingTicks
	tildes := pendingTildes
	atLineStart := lineStart

	flushPending := func() {
		if ticks > 0 {
			if atLineStart && ticks >= 3 {
				applyFenceMarker(&nextStack, ticks) // positive = backtick
			}
			atLineStart = false
			ticks = 0
		}
		if tildes > 0 {
			if atLineStart && tildes >= 3 {
				applyFenceMarker(&nextStack, -tildes) // negative = tilde
			}
			atLineStart = false
			tildes = 0
		}
	}

	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '`' {
			if tildes > 0 {
				flushPending()
			}
			ticks++
			continue
		}
		if ch == '~' {
			if ticks > 0 {
				flushPending()
			}
			tildes++
			continue
		}
		flushPending()
		switch ch {
		case '\n', '\r':
			atLineStart = true
			continue
		case ' ', '\t':
			if atLineStart {
				continue
			}
		}
		atLineStart = false
	}

	return CodeFenceState{
		Stack:         nextStack,
		PendingTicks:  ticks,
		PendingTildes: tildes,
		LineStart:     atLineStart,
	}
}

// applyFenceMarker pushes or pops a fence marker from the stack.
// Positive values = backtick fences, negative = tilde fences.
// Closing must match fence type. Count is the absolute number of
// fence characters (>= 3).
func applyFenceMarker(stack *[]int, marker int) {
	if stack == nil || marker == 0 {
		return
	}
	absMarker := int(math.Abs(float64(marker)))
	if absMarker < 3 {
		return
	}
	if len(*stack) == 0 {
		*stack = append(*stack, marker)
		return
	}
	top := (*stack)[len(*stack)-1]
	sameType := (top > 0 && marker > 0) || (top < 0 && marker < 0)
	if !sameType {
		*stack = append(*stack, marker)
		return
	}
	absTop := int(math.Abs(float64(top)))
	if absMarker >= absTop {
		*stack = (*stack)[:len(*stack)-1]
		return
	}
	*stack = append(*stack, marker)
}
