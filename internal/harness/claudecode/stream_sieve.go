package claudecode

import (
	"regexp"
	"strings"

	"ds2api/internal/toolcall"
)

type StreamSieveState struct {
	pending               strings.Builder
	capture               strings.Builder
	capturing             bool
	codeFenceStack        []int
	codeFencePendingTicks int
	codeFenceLineStart    bool
	pendingToolRaw        string
	pendingToolCalls      []toolcall.ParsedToolCall
	disableDeltas         bool
	toolNameSent          bool
	toolName              string
	toolArgsStart         int
	toolArgsSent          int
	toolArgsString        bool
	toolArgsDone          bool
}

type StreamSieveEvent struct {
	Content        string
	ToolCalls      []toolcall.ParsedToolCall
	ToolCallDeltas []StreamToolCallDelta
	ErrorCode      string
	ErrorMessage   string
}

type StreamToolCallDelta struct {
	Index     int
	Name      string
	Arguments string
}

var streamXMLToolCallOpeningTags = []string{"<tool_calls", "<tool_call", "<invoke", "<function_call", "<function_calls", "<tool_use",
	"<attempt_completion", "<ask_followup_question", "<new_task>", "<result", "<parameter"}

var streamXMLToolCallTagPairs = []struct{ open, close string }{
	{"<tool_calls", "</tool_calls>"},
	{"<tool_call", "</tool_call>"},
	{"<function_calls", "</function_calls>"},
	{"<function_call", "</function_call>"},
	{"<invoke", "</invoke>"},
	{"<tool_use", "</tool_use>"},
	{"<attempt_completion", "</attempt_completion>"},
	{"<ask_followup_question", "</ask_followup_question>"},
	{"<new_task", "</new_task>"},
}

var streamXMLToolCallBlockPattern = regexp.MustCompile(`(?is)(<tool_calls>\s*(?:.*?)\s*</tool_calls>|<tool_call>\s*(?:.*?)\s*</tool_call>|<invoke\b[^>]*>(?:.*?)</invoke>|<function_calls?\b[^>]*>(?:.*?)</function_calls?>|<tool_use>(?:.*?)</tool_use>|<attempt_completion>(?:.*?)</attempt_completion>|<ask_followup_question>(?:.*?)</ask_followup_question>|<new_task>(?:.*?)</new_task>)`)

var streamXMLToolTagsToDetect = []string{"<tool_calls>", "<tool_calls\n", "tool_calls>", "tool_calls\n", "<tool_call>", "<tool_call\n", "tool_call>", "tool_call\n",
	"<invoke ", "<invoke>", "<function_call", "<function_calls", "<tool_use>",
	"<attempt_completion>", "<ask_followup_question>", "<new_task>",
	"<parameter name=\"description\"", "<parameter name='description'", "<parameter name=description",
	"<param name=\"description\"", "<argument name=\"description\""}

func ProcessStreamSieveChunk(state *StreamSieveState, chunk string, toolNames []string) []StreamSieveEvent {
	return ProcessStreamSieveChunkWithMeta(state, chunk, toolNames, false)
}

func ProcessStreamSieveChunkWithMeta(state *StreamSieveState, chunk string, toolNames []string, allowMetaAgentTools bool) []StreamSieveEvent {
	if state == nil {
		return nil
	}
	if chunk != "" {
		state.pending.WriteString(chunk)
	}
	events := make([]StreamSieveEvent, 0, 2)
	if len(state.pendingToolCalls) > 0 {
		events = append(events, StreamSieveEvent{ToolCalls: state.pendingToolCalls})
		state.pendingToolRaw = ""
		state.pendingToolCalls = nil
	}

	for {
		if state.capturing {
			if state.pending.Len() > 0 {
				state.capture.WriteString(state.pending.String())
				state.pending.Reset()
			}
			prefix, calls, suffix, ready := consumeStreamToolCapture(state, toolNames, allowMetaAgentTools, false)
			if !ready {
				break
			}
			state.capture.Reset()
			state.capturing = false
			state.resetIncrementalToolState()
			if len(calls) > 0 {
				if prefix != "" {
					state.noteText(prefix)
					events = append(events, StreamSieveEvent{Content: prefix})
				}
				if suffix != "" {
					state.pending.WriteString(suffix)
				}
				state.pendingToolCalls = calls
				continue
			}
			if prefix != "" {
				state.noteText(prefix)
				events = append(events, StreamSieveEvent{Content: prefix})
			}
			if suffix != "" {
				state.pending.WriteString(suffix)
			}
			continue
		}

		pending := state.pending.String()
		if pending == "" {
			break
		}
		start := FindStreamToolSegmentStart(state, pending)
		if start < 0 {
			start = FindVisibleJSONToolSegmentStart(state, pending)
		}
		if start >= 0 {
			prefix := pending[:start]
			if prefix != "" {
				state.noteText(prefix)
				events = append(events, StreamSieveEvent{Content: prefix})
			}
			state.pending.Reset()
			state.capture.WriteString(pending[start:])
			state.capturing = true
			state.resetIncrementalToolState()
			continue
		}

		safe, hold := SplitSafeContentForToolDetection(state, pending)
		if safe == "" {
			break
		}
		state.pending.Reset()
		state.pending.WriteString(hold)
		state.noteText(safe)
		events = append(events, StreamSieveEvent{Content: safe})
	}

	return events
}

func FlushStreamSieve(state *StreamSieveState, toolNames []string) []StreamSieveEvent {
	return FlushStreamSieveWithMeta(state, toolNames, false)
}

func FlushStreamSieveWithMeta(state *StreamSieveState, toolNames []string, allowMetaAgentTools bool) []StreamSieveEvent {
	if state == nil {
		return nil
	}
	events := ProcessStreamSieveChunkWithMeta(state, "", toolNames, allowMetaAgentTools)
	if len(state.pendingToolCalls) > 0 {
		events = append(events, StreamSieveEvent{ToolCalls: state.pendingToolCalls})
		state.pendingToolRaw = ""
		state.pendingToolCalls = nil
	}
	if state.capturing {
		consumedPrefix, consumedCalls, consumedSuffix, ready := consumeStreamToolCapture(state, toolNames, allowMetaAgentTools, true)
		if ready {
			if consumedPrefix != "" {
				state.noteText(consumedPrefix)
				events = append(events, StreamSieveEvent{Content: consumedPrefix})
			}
			if len(consumedCalls) > 0 {
				events = append(events, StreamSieveEvent{ToolCalls: consumedCalls})
			}
			if consumedSuffix != "" {
				state.noteText(consumedSuffix)
				events = append(events, StreamSieveEvent{Content: consumedSuffix})
			}
		} else {
			content := state.capture.String()
			if code, message, ok := incompleteStreamToolTransactionError(content); ok {
				recordFailureDecision(code)
				events = append(events, StreamSieveEvent{ErrorCode: code, ErrorMessage: message})
			} else if content != "" {
				state.noteText(content)
				events = append(events, StreamSieveEvent{Content: content})
			}
		}
		state.capture.Reset()
		state.capturing = false
		state.resetIncrementalToolState()
	}
	if state.pending.Len() > 0 {
		content := state.pending.String()
		state.noteText(content)
		events = append(events, StreamSieveEvent{Content: content})
		state.pending.Reset()
	}
	return events
}

func incompleteStreamToolTransactionError(captured string) (string, string, bool) {
	trimmed := strings.TrimSpace(captured)
	if trimmed == "" {
		return "", "", false
	}
	lower := strings.ToLower(trimmed)
	if HasOpenXMLToolTag(trimmed) || containsAny(lower, []string{
		"<tool_call",
		"<tool_calls",
		"<invoke",
		"<function_call",
		"<function_calls",
		"<tool_use",
		"<attempt_completion",
		"<ask_followup_question",
		"<new_task",
	}) {
		return InvalidToolCallCode, "Upstream model emitted invalid tool call syntax.", true
	}
	if VisibleJSONToolCaptureMayContinue(trimmed) && hasVisibleJSONToolHints(lower) {
		return InvalidToolCallCode, "Upstream model emitted invalid tool call syntax.", true
	}
	return "", "", false
}

func SplitSafeContentForToolDetection(state *StreamSieveState, s string) (safe, hold string) {
	if s == "" {
		return "", ""
	}
	if xmlIdx := FindPartialXMLToolTagStart(s); xmlIdx >= 0 {
		if InsideCodeFenceWithState(state, s[:xmlIdx]) {
			return s, ""
		}
		if xmlIdx > 0 {
			return s[:xmlIdx], s[xmlIdx:]
		}
		return "", s
	}
	if jsonIdx := FindPartialVisibleJSONToolSegmentStart(state, s); jsonIdx >= 0 {
		if jsonIdx > 0 {
			return s[:jsonIdx], s[jsonIdx:]
		}
		return "", s
	}
	return s, ""
}

func FindStreamToolSegmentStart(state *StreamSieveState, s string) int {
	if s == "" {
		return -1
	}
	lower := strings.ToLower(s)
	offset := 0
	for {
		bestKeyIdx := -1
		matchedTag := ""
		for _, tag := range streamXMLToolTagsToDetect {
			idx := strings.Index(lower[offset:], tag)
			if idx >= 0 {
				idx += offset
				if bestKeyIdx < 0 || idx < bestKeyIdx {
					bestKeyIdx = idx
					matchedTag = tag
				}
			}
		}
		if bestKeyIdx < 0 {
			return -1
		}
		if !InsideCodeFenceWithState(state, s[:bestKeyIdx]) {
			return bestKeyIdx
		}
		offset = bestKeyIdx + len(matchedTag)
	}
}

func ConsumeStreamToolCapture(state *StreamSieveState, toolNames []string, allowMetaAgentTools bool) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	return consumeStreamToolCapture(state, toolNames, allowMetaAgentTools, false)
}

func consumeStreamToolCapture(state *StreamSieveState, toolNames []string, allowMetaAgentTools bool, final bool) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	captured := state.capture.String()
	if captured == "" {
		return "", nil, "", false
	}

	if xmlPrefix, xmlCalls, xmlSuffix, xmlReady := ConsumeXMLToolCapture(captured, toolNames, allowMetaAgentTools); xmlReady {
		return xmlPrefix, xmlCalls, xmlSuffix, true
	}
	if HasOpenXMLToolTag(captured) {
		return "", nil, "", false
	}
	if prefix, calls, suffix, ready := ConsumeOrphanAgentParameterCapture(captured, toolNames, allowMetaAgentTools, final); ready {
		return prefix, calls, suffix, true
	}
	if jsonPrefix, jsonCalls, jsonSuffix, jsonReady := ConsumeVisibleJSONToolCapture(captured, toolNames, allowMetaAgentTools); jsonReady {
		return jsonPrefix, jsonCalls, jsonSuffix, true
	}
	if VisibleJSONToolCaptureMayContinue(captured) {
		return "", nil, "", false
	}
	return "", nil, "", false
}

func ConsumeOrphanAgentParameterCapture(captured string, toolNames []string, allowMetaAgentTools bool, final bool) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	if !final || !allowMetaAgentTools {
		return "", nil, "", false
	}
	toolName, ok := FindBackgroundAgentToolName(toolNames)
	if !ok {
		return "", nil, "", false
	}
	lower := strings.ToLower(captured)
	start := orphanAgentParameterStart(lower)
	if start < 0 {
		return "", nil, "", false
	}
	parsed := toolcall.ParseToolCalls(captured[start:], []string{toolName})
	if len(parsed) == 0 {
		return "", nil, "", false
	}
	for i := range parsed {
		parsed[i].Name = toolName
	}
	return captured[:start], parsed, "", true
}

func orphanAgentParameterStart(lower string) int {
	candidates := []string{
		"<parameter name=\"description\"",
		"<parameter name='description'",
		"<parameter name=description",
		"<param name=\"description\"",
		"<argument name=\"description\"",
	}
	best := -1
	for _, candidate := range candidates {
		if idx := strings.Index(lower, candidate); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	return best
}

func FindVisibleJSONToolSegmentStart(state *StreamSieveState, s string) int {
	if s == "" {
		return -1
	}
	lower := strings.ToLower(s)
	offset := 0
	for {
		idx := nextVisibleJSONCandidateIndex(s, offset)
		if idx < 0 {
			return -1
		}
		candidateLower := lower[idx:]
		if isLikelyStandaloneJSONToolStart(s, idx) && hasVisibleJSONToolHints(candidateLower) && !InsideCodeFenceWithState(state, s[:idx]) {
			return idx
		}
		offset = idx + 1
	}
}

func FindPartialVisibleJSONToolSegmentStart(state *StreamSieveState, s string) int {
	offset := 0
	for {
		idx := nextVisibleJSONCandidateIndex(s, offset)
		if idx < 0 {
			return -1
		}
		if isLikelyStandaloneJSONToolStart(s, idx) && JSONLikeStandaloneToolJSONEnd(s[idx:]) < 0 && !InsideCodeFenceWithState(state, s[:idx]) {
			return idx
		}
		offset = idx + 1
	}
}

func ConsumeVisibleJSONToolCapture(captured string, toolNames []string, allowMetaAgentTools bool) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	trimmed := strings.TrimLeft(captured, " \t\r\n")
	end := JSONLikeStandaloneToolJSONEnd(trimmed)
	if end < 0 {
		return "", nil, "", false
	}
	leadingLen := len(captured) - len(strings.TrimLeft(captured, " \t\r\n"))
	end += leadingLen
	block := captured[leadingLen:end]
	suffix = captured[end:]
	parsed := ParseStreamToolBlock(block, toolNames, allowMetaAgentTools)
	if len(parsed.Calls) == 0 {
		if parsed.Parsed {
			return parsed.Text, nil, suffix, true
		}
		return captured[:end], nil, suffix, true
	}
	return "", parsed.Calls, suffix, true
}

func VisibleJSONToolCaptureMayContinue(captured string) bool {
	trimmed := strings.TrimSpace(captured)
	if !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		return false
	}
	if JSONLikeStandaloneToolJSONEnd(trimmed) >= 0 {
		return false
	}
	return strings.HasPrefix(trimmed, "{") || hasVisibleJSONToolHints(strings.ToLower(trimmed))
}

func JSONLikeStandaloneToolJSONEnd(s string) int {
	if strings.HasPrefix(s, "[") {
		return jsonLikeValueEnd(s)
	}
	if strings.HasPrefix(s, "{") {
		return jsonLikeObjectSequenceEnd(s)
	}
	return -1
}

func ConsumeXMLToolCapture(captured string, toolNames []string, allowMetaAgentTools bool) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	captured = toolcall.RepairMalformedToolCallXML(captured)
	lower := strings.ToLower(captured)
	for _, pair := range streamXMLToolCallTagPairs {
		openIdx := strings.Index(lower, pair.open)
		if openIdx < 0 {
			continue
		}
		closeIdx := strings.LastIndex(lower, pair.close)
		if closeIdx < openIdx {
			return "", nil, "", false
		}
		closeEnd := closeIdx + len(pair.close)

		xmlBlock := captured[openIdx:closeEnd]
		prefixPart := captured[:openIdx]
		suffixPart := captured[closeEnd:]
		parsed := ParseStreamToolBlock(xmlBlock, toolNames, allowMetaAgentTools)
		if len(parsed.Calls) > 0 {
			prefixPart, suffixPart = TrimWrappingJSONFence(prefixPart, suffixPart)
			return prefixPart, parsed.Calls, suffixPart, true
		}
		if parsed.Parsed {
			if strings.TrimSpace(prefixPart) != "" && !strings.HasSuffix(prefixPart, "\n") {
				prefixPart += "\n"
			}
			return prefixPart + parsed.Text, nil, suffixPart, true
		}
		return prefixPart + xmlBlock, nil, suffixPart, true
	}
	return "", nil, "", false
}

func HasOpenXMLToolTag(captured string) bool {
	lower := strings.ToLower(captured)
	for _, pair := range streamXMLToolCallTagPairs {
		if strings.Contains(lower, pair.open) {
			if !strings.Contains(lower, pair.close) {
				return true
			}
		}
	}
	return false
}

func FindPartialXMLToolTagStart(s string) int {
	lastLT := strings.LastIndex(s, "<")
	if lastLT < 0 {
		return -1
	}
	tail := s[lastLT:]
	if strings.Contains(tail, ">") {
		return -1
	}
	lowerTail := strings.ToLower(tail)
	for _, tag := range streamXMLToolCallOpeningTags {
		tagWithLT := tag
		if !strings.HasPrefix(tagWithLT, "<") {
			tagWithLT = "<" + tagWithLT
		}
		if strings.HasPrefix(tagWithLT, lowerTail) {
			return lastLT
		}
	}
	return -1
}

func StreamXMLToolCallBlockPattern() *regexp.Regexp {
	return streamXMLToolCallBlockPattern
}

func TrimWrappingJSONFence(prefix, suffix string) (string, string) {
	trimmedPrefix := strings.TrimRight(prefix, " \t\r\n")
	fenceIdx := strings.LastIndex(trimmedPrefix, "```")
	if fenceIdx < 0 {
		return prefix, suffix
	}
	if strings.Count(trimmedPrefix[:fenceIdx+3], "```")%2 == 0 {
		return prefix, suffix
	}
	fenceHeader := strings.TrimSpace(trimmedPrefix[fenceIdx+3:])
	if fenceHeader != "" && !strings.EqualFold(fenceHeader, "json") {
		return prefix, suffix
	}

	trimmedSuffix := strings.TrimLeft(suffix, " \t\r\n")
	if !strings.HasPrefix(trimmedSuffix, "```") {
		return prefix, suffix
	}
	consumedLeading := len(suffix) - len(trimmedSuffix)
	return trimmedPrefix[:fenceIdx], suffix[consumedLeading+3:]
}

func hasVisibleJSONToolHints(lower string) bool {
	hasName := strings.Contains(lower, `"tool"`) ||
		strings.Contains(lower, `"name"`) ||
		strings.Contains(lower, `"tool_name"`) ||
		strings.Contains(lower, `"function"`)
	hasArgs := strings.Contains(lower, `"arguments"`) ||
		strings.Contains(lower, `"input"`) ||
		strings.Contains(lower, `"params"`) ||
		strings.Contains(lower, `"parameters"`)
	return hasName && hasArgs
}

func isLikelyStandaloneJSONArrayStart(s string, idx int) bool {
	if idx < 0 || idx >= len(s) || s[idx] != '[' {
		return false
	}
	lineStart := strings.LastIndex(s[:idx], "\n") + 1
	if strings.TrimSpace(s[lineStart:idx]) != "" {
		return false
	}
	after := strings.TrimLeft(s[idx+1:], " \t\r\n")
	return after == "" || strings.HasPrefix(after, "{")
}

func isLikelyStandaloneJSONObjectStart(s string, idx int) bool {
	if idx < 0 || idx >= len(s) || s[idx] != '{' {
		return false
	}
	lineStart := strings.LastIndex(s[:idx], "\n") + 1
	if strings.TrimSpace(s[lineStart:idx]) != "" {
		return false
	}
	after := strings.TrimLeft(s[idx+1:], " \t\r\n")
	return after == "" || strings.HasPrefix(after, `"`) || strings.HasPrefix(after, "}")
}

func isLikelyStandaloneJSONToolStart(s string, idx int) bool {
	if idx < 0 || idx >= len(s) {
		return false
	}
	if s[idx] == '[' {
		return isLikelyStandaloneJSONArrayStart(s, idx)
	}
	return isLikelyStandaloneJSONObjectStart(s, idx)
}

func nextVisibleJSONCandidateIndex(s string, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(s) {
		return -1
	}
	arrayIdx := strings.Index(s[offset:], "[")
	objectIdx := strings.Index(s[offset:], "{")
	if arrayIdx < 0 && objectIdx < 0 {
		return -1
	}
	if arrayIdx < 0 {
		return offset + objectIdx
	}
	if objectIdx < 0 {
		return offset + arrayIdx
	}
	if arrayIdx < objectIdx {
		return offset + arrayIdx
	}
	return offset + objectIdx
}

func jsonLikeObjectSequenceEnd(s string) int {
	pos := 0
	end := -1
	count := 0
	for {
		for pos < len(s) {
			switch s[pos] {
			case ' ', '\t', '\r', '\n':
				pos++
			default:
				goto nonSpace
			}
		}
		if count > 0 {
			return end
		}
		return -1
	nonSpace:
		if s[pos] != '{' {
			if count > 0 {
				return end
			}
			return -1
		}
		objectEnd := jsonLikeValueEnd(s[pos:])
		if objectEnd < 0 {
			return -1
		}
		pos += objectEnd
		end = pos
		count++
	}
}

func jsonLikeValueEnd(s string) int {
	if s == "" || (s[0] != '[' && s[0] != '{') {
		return -1
	}
	stack := make([]byte, 0, 4)
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '[':
			stack = append(stack, ']')
		case '{':
			stack = append(stack, '}')
		case ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != ch {
				return -1
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func (s *StreamSieveState) resetIncrementalToolState() {
	s.disableDeltas = false
	s.toolNameSent = false
	s.toolName = ""
	s.toolArgsStart = -1
	s.toolArgsSent = -1
	s.toolArgsString = false
	s.toolArgsDone = false
}

func (s *StreamSieveState) noteText(content string) {
	if !hasMeaningfulText(content) {
		return
	}
	updateCodeFenceState(s, content)
}

func hasMeaningfulText(text string) bool {
	return strings.TrimSpace(text) != ""
}

func InsideCodeFenceWithState(state *StreamSieveState, text string) bool {
	if state == nil {
		return InsideCodeFence(text)
	}
	simulated := simulateCodeFenceState(
		state.codeFenceStack,
		state.codeFencePendingTicks,
		state.codeFenceLineStart,
		text,
	)
	return len(simulated.stack) > 0
}

func InsideCodeFence(text string) bool {
	if text == "" {
		return false
	}
	return len(simulateCodeFenceState(nil, 0, true, text).stack) > 0
}

func updateCodeFenceState(state *StreamSieveState, text string) {
	if state == nil || !hasMeaningfulText(text) {
		return
	}
	next := simulateCodeFenceState(
		state.codeFenceStack,
		state.codeFencePendingTicks,
		state.codeFenceLineStart,
		text,
	)
	state.codeFenceStack = next.stack
	state.codeFencePendingTicks = next.pendingTicks
	state.codeFenceLineStart = next.lineStart
}

type codeFenceSimulation struct {
	stack        []int
	pendingTicks int
	lineStart    bool
}

func simulateCodeFenceState(stack []int, pendingTicks int, lineStart bool, text string) codeFenceSimulation {
	chunk := text
	nextStack := append([]int(nil), stack...)
	ticks := pendingTicks
	atLineStart := lineStart

	flushTicks := func() {
		if ticks > 0 {
			if atLineStart && ticks >= 3 {
				applyFenceMarker(&nextStack, ticks)
			}
			atLineStart = false
			ticks = 0
		}
	}

	for i := 0; i < len(chunk); i++ {
		ch := chunk[i]
		if ch == '`' {
			ticks++
			continue
		}
		flushTicks()
		switch ch {
		case '\n', '\r':
			atLineStart = true
		case ' ', '\t':
			if atLineStart {
				continue
			}
			atLineStart = false
		default:
			atLineStart = false
		}
	}

	return codeFenceSimulation{
		stack:        nextStack,
		pendingTicks: ticks,
		lineStart:    atLineStart,
	}
}

func applyFenceMarker(stack *[]int, ticks int) {
	if stack == nil || ticks <= 0 {
		return
	}
	if len(*stack) == 0 {
		*stack = append(*stack, ticks)
		return
	}
	top := (*stack)[len(*stack)-1]
	if ticks >= top {
		*stack = (*stack)[:len(*stack)-1]
		return
	}
	*stack = append(*stack, ticks)
}
