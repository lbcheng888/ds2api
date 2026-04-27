package openai

import (
	"strings"

	"ds2api/internal/toolcall"
)

func findVisibleJSONToolSegmentStart(state *toolStreamSieveState, s string) int {
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
		if isLikelyStandaloneJSONToolStart(s, idx) && hasVisibleJSONToolHints(candidateLower) && !insideCodeFenceWithState(state, s[:idx]) {
			return idx
		}
		offset = idx + 1
	}
}

func findPartialVisibleJSONToolSegmentStart(state *toolStreamSieveState, s string) int {
	offset := 0
	for {
		idx := nextVisibleJSONCandidateIndex(s, offset)
		if idx < 0 {
			return -1
		}
		if isLikelyStandaloneJSONToolStart(s, idx) && jsonLikeStandaloneToolJSONEnd(s[idx:]) < 0 && !insideCodeFenceWithState(state, s[:idx]) {
			return idx
		}
		offset = idx + 1
	}
}

func consumeVisibleJSONToolCapture(captured string, toolNames []string, allowMetaAgentTools bool) (prefix string, calls []toolcall.ParsedToolCall, suffix string, ready bool) {
	trimmed := strings.TrimLeft(captured, " \t\r\n")
	end := jsonLikeStandaloneToolJSONEnd(trimmed)
	if end < 0 {
		return "", nil, "", false
	}
	leadingLen := len(captured) - len(strings.TrimLeft(captured, " \t\r\n"))
	end += leadingLen
	block := captured[leadingLen:end]
	suffix = captured[end:]
	parsed := toolcall.ParseToolCalls(block, toolNames)
	if len(parsed) == 0 {
		return captured[:end], nil, suffix, true
	}
	if !allowMetaAgentTools && toolcall.AllCallsAreMetaAgentTools(parsed) {
		return toolcall.MetaAgentToolBlockedMessage(), nil, suffix, true
	}
	return "", parsed, suffix, true
}

func visibleJSONToolCaptureMayContinue(captured string) bool {
	trimmed := strings.TrimSpace(captured)
	if !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		return false
	}
	if jsonLikeStandaloneToolJSONEnd(trimmed) >= 0 {
		return false
	}
	return strings.HasPrefix(trimmed, "{") || hasVisibleJSONToolHints(strings.ToLower(trimmed))
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

func jsonLikeStandaloneToolJSONEnd(s string) int {
	if strings.HasPrefix(s, "[") {
		return jsonLikeValueEnd(s)
	}
	if strings.HasPrefix(s, "{") {
		return jsonLikeObjectSequenceEnd(s)
	}
	return -1
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
	depth := 0
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
			depth++
		case '{':
			stack = append(stack, '}')
			depth++
		case ']', '}':
			if depth == 0 || len(stack) == 0 || stack[len(stack)-1] != ch {
				return -1
			}
			stack = stack[:len(stack)-1]
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}
