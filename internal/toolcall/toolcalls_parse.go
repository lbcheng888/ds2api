package toolcall

import (
	"strings"
)

type ParsedToolCall struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type ToolCallParseResult struct {
	Calls             []ParsedToolCall
	SawToolCallSyntax bool
	RejectedByPolicy  bool
	RejectedToolNames []string
}

func ParseToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	return ParseToolCallsDetailed(text, availableToolNames).Calls
}

func ParseToolCallsDetailed(text string, availableToolNames []string) ToolCallParseResult {
	return parseToolCallsDetailedXMLOnly(text, availableToolNames)
}

func ParseStandaloneToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	return ParseStandaloneToolCallsDetailed(text, availableToolNames).Calls
}

func ParseStandaloneToolCallsDetailed(text string, availableToolNames []string) ToolCallParseResult {
	return parseToolCallsDetailedXMLOnly(text, availableToolNames)
}

func parseToolCallsDetailedXMLOnly(text string, availableToolNames []string) ToolCallParseResult {
	result := ToolCallParseResult{}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return result
	}
	trimmed = RepairMalformedToolCallXML(trimmed)
	result.SawToolCallSyntax = LooksLikeToolCallSyntax(trimmed)
	trimmed = stripFencedCodeBlocks(trimmed)
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return result
	}
	trimmed = RepairMalformedToolCallXML(trimmed)
	trimmed = repairMissingToolCallClose(trimmed)

	parsed := parseXMLToolCalls(trimmed)
	if len(parsed) == 0 {
		parsed = parseMarkupToolCalls(trimmed)
	}
	if len(parsed) == 0 {
		parsed = parseVisibleJSONToolCalls(trimmed)
	}
	if len(parsed) == 0 {
		return result
	}

	result.SawToolCallSyntax = true
	calls, rejectedNames := filterToolCallsDetailed(parsed, availableToolNames)
	result.Calls = calls
	result.RejectedToolNames = rejectedNames
	result.RejectedByPolicy = len(rejectedNames) > 0 && len(calls) == 0
	return result
}

func filterToolCallsDetailed(parsed []ParsedToolCall, availableToolNames []string) ([]ParsedToolCall, []string) {
	out := make([]ParsedToolCall, 0, len(parsed))
	rejectedNames := make([]string, 0)
	for _, tc := range parsed {
		if tc.Input == nil {
			tc.Input = map[string]any{}
		}
		tc.Input = normalizeToolInputStrings(tc.Input)
		if rewritten, ok := rewriteUnavailableLocalReadFileCallForAvailable(tc, availableToolNames); ok {
			tc = rewritten
		}
		if rewritten, ok := rewriteLocalResourceReadCallForAvailable(tc, availableToolNames); ok {
			tc = rewritten
		}
		if strings.TrimSpace(tc.Name) == "" {
			name, ok := inferToolNameFromKnownRequiredFields(tc.Input, availableToolNames)
			if !ok {
				continue
			}
			tc.Name = name
		} else if name, ok := resolveToolNameForAvailable(tc.Name, availableToolNames); ok {
			tc.Name = name
		} else {
			rejectedNames = append(rejectedNames, strings.TrimSpace(tc.Name))
			continue
		}
		if rewritten, ok := rewriteLocalResourceReadCallForAvailable(tc, availableToolNames); ok {
			tc = rewritten
		}
		if isInvalidKnownClientToolCall(tc.Name, tc.Input) {
			continue
		}
		for _, expanded := range expandKnownClientToolCalls(tc) {
			if isInvalidKnownClientToolCall(expanded.Name, expanded.Input) {
				continue
			}
			out = append(out, expanded)
		}
	}
	return out, rejectedNames
}

func resolveToolNameForAvailable(name string, availableToolNames []string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if len(availableToolNames) == 0 {
		return name, true
	}
	if hasAnyToolWildcard(availableToolNames) {
		return name, true
	}
	for _, candidate := range availableToolNames {
		candidate = strings.TrimSpace(candidate)
		if strings.EqualFold(candidate, name) {
			return name, true
		}
	}
	for _, alias := range knownToolNameAliases(strings.ToLower(name)) {
		for _, candidate := range availableToolNames {
			candidate = strings.TrimSpace(candidate)
			if strings.EqualFold(candidate, alias) {
				return candidate, true
			}
		}
	}
	return "", false
}

func hasAnyToolWildcard(availableToolNames []string) bool {
	for _, candidate := range availableToolNames {
		if strings.EqualFold(strings.TrimSpace(candidate), "__any_tool__") {
			return true
		}
	}
	return false
}

func inferToolNameFromKnownRequiredFields(input map[string]any, availableToolNames []string) (string, bool) {
	if len(input) == 0 || len(availableToolNames) == 0 {
		return "", false
	}
	bestName := ""
	bestScore := 0
	tied := false
	for _, name := range availableToolNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		required := knownRequiredToolFields(name)
		if len(required) == 0 {
			continue
		}
		ok := true
		for _, field := range required {
			value, hasValue := inputValueForKnownRequiredField(input, field)
			if !hasValue || isEmptyKnownRequiredValue(value) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		score := len(required)
		if score > bestScore {
			bestName = name
			bestScore = score
			tied = false
			continue
		}
		if score == bestScore {
			tied = true
		}
	}
	if bestName == "" || tied {
		return "", false
	}
	return bestName, true
}

func looksLikeToolCallSyntax(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "<tool_calls") ||
		strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "<toolcall") ||
		strings.Contains(lower, "<tool ") ||
		strings.Contains(lower, "<function_calls") ||
		strings.Contains(lower, "<function_call") ||
		strings.Contains(lower, "<invoke") ||
		strings.Contains(lower, "<tool_use") ||
		strings.Contains(lower, "<attempt_completion") ||
		strings.Contains(lower, "<ask_followup_question") ||
		strings.Contains(lower, "<new_task") ||
		strings.Contains(lower, "<result") ||
		looksLikeVisibleJSONToolCallSyntax(text)
}

func LooksLikeToolCallSyntax(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	trimmed = stripFencedCodeBlocks(trimmed)
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return false
	}
	return looksLikeToolCallSyntax(trimmed)
}

func stripFencedCodeBlocks(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text))

	lines := strings.SplitAfter(text, "\n")
	inFence := false
	fenceMarker := ""
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if !inFence {
			if marker, ok := parseFenceOpen(trimmed); ok {
				inFence = true
				fenceMarker = marker
				continue
			}
			b.WriteString(line)
			continue
		}

		if isFenceClose(trimmed, fenceMarker) {
			inFence = false
			fenceMarker = ""
		}
	}

	if inFence {
		return ""
	}
	return b.String()
}

func parseFenceOpen(line string) (string, bool) {
	if len(line) < 3 {
		return "", false
	}
	ch := line[0]
	if ch != '`' && ch != '~' {
		return "", false
	}
	count := countLeadingFenceChars(line, ch)
	if count < 3 {
		return "", false
	}
	return strings.Repeat(string(ch), count), true
}

func isFenceClose(line, marker string) bool {
	if marker == "" {
		return false
	}
	ch := marker[0]
	if line == "" || line[0] != ch {
		return false
	}
	count := countLeadingFenceChars(line, ch)
	if count < len(marker) {
		return false
	}
	rest := strings.TrimSpace(line[count:])
	return rest == ""
}

func countLeadingFenceChars(line string, ch byte) int {
	count := 0
	for count < len(line) && line[count] == ch {
		count++
	}
	return count
}
