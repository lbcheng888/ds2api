package claudecode

import (
	"regexp"
	"strings"

	"ds2api/internal/toolcall"
)

type FinalToolCallInput struct {
	Text      string
	Thinking  string
	ToolNames []string
}

var finalXMLToolCallsBlockPattern = regexp.MustCompile(`(?is)<\s*tool_calls\b.*?</\s*tool_calls\s*>`)
var finalXMLSingleToolCallBlockPattern = regexp.MustCompile(`(?is)<\s*(?:tool_call|ToolCall)\b.*?</\s*(?:tool_call|ToolCall)\s*>`)

func DetectFinalToolCalls(in FinalToolCallInput) (toolcall.ToolCallParseResult, string) {
	text := in.Text
	if stripped, changed := StripEmptyToolCallContainerNoise(text); changed {
		text = stripped
	}
	detected, visibleText := detectToolCallsInText(text, in.ToolNames)
	if len(detected.Calls) == 0 && strings.TrimSpace(in.Thinking) != "" {
		thinkingDetected, _ := detectToolCallsInText(in.Thinking, in.ToolNames)
		if len(thinkingDetected.Calls) > 0 {
			detected = thinkingDetected
		}
	}
	return detected, visibleText
}

func detectToolCallsInText(text string, toolNames []string) (toolcall.ToolCallParseResult, string) {
	detected := toolcall.ParseStandaloneToolCallsDetailed(text, toolNames)
	visibleText := text
	if len(detected.Calls) > 0 {
		visibleText = joinExtractedToolText(stripFinalXMLToolCallBlocks(text))
	} else {
		prefix, calls, suffix, ok := toolcall.ExtractVisibleJSONToolCalls(text, toolNames)
		if ok {
			detected = toolcall.ToolCallParseResult{
				Calls:             calls,
				SawToolCallSyntax: true,
			}
			visibleText = joinExtractedToolText(prefix, suffix)
		}
	}
	return detected, visibleText
}

func stripFinalXMLToolCallBlocks(text string) string {
	text = finalXMLToolCallsBlockPattern.ReplaceAllString(text, "")
	text = finalXMLSingleToolCallBlockPattern.ReplaceAllString(text, "")
	lower := strings.ToLower(text)
	if idx := orphanAgentParameterStart(lower); idx >= 0 && strings.Contains(lower[idx:], "name=\"prompt") {
		text = text[:idx]
	}
	return text
}

func joinExtractedToolText(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "\n")
}
