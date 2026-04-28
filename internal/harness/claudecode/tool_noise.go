package claudecode

import (
	"regexp"
	"strings"

	"ds2api/internal/toolcall"
)

var emptyToolCallContainerTagPattern = regexp.MustCompile(`(?is)</?\s*(?:tool_calls|tool_call)\b[^>]*>`)
var executableToolCallPayloadPattern = regexp.MustCompile(`(?is)<\s*(?:tool_name|function_name|tool_call_name|parameters?|arguments?|args?|input|parameter|argument|param|tool\b|invoke\b|function_call\b|tool_use\b)\b|"\s*(?:tool|name|tool_name|function|input|arguments|parameters|params)\s*"`)

func StripEmptyToolCallContainerNoise(text string) (string, bool) {
	if strings.TrimSpace(text) == "" {
		return text, false
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "tool_call") && !strings.Contains(lower, "toolcall") {
		return text, false
	}
	if len(toolcall.ParseToolCalls(text, nil)) > 0 || executableToolCallPayloadPattern.MatchString(text) {
		return text, false
	}
	stripped := emptyToolCallContainerTagPattern.ReplaceAllString(text, "")
	if stripped == text {
		return text, false
	}
	stripped = strings.TrimSpace(stripped)
	if toolcall.LooksLikeToolCallSyntax(stripped) {
		return text, false
	}
	return stripped, true
}
