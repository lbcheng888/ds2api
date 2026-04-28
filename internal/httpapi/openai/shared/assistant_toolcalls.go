package shared

import (
	"net/http"
	"strings"

	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

func DetectAssistantToolCalls(text, exposedThinking, detectionThinking string, toolNames []string) toolcall.ToolCallParseResult {
	textParsed := toolcall.ParseStandaloneToolCallsDetailed(text, toolNames)
	if len(textParsed.Calls) > 0 {
		return textParsed
	}
	if strings.TrimSpace(text) != "" {
		return textParsed
	}
	thinking := detectionThinking
	if strings.TrimSpace(thinking) == "" {
		thinking = exposedThinking
	}
	thinkingParsed := toolcall.ParseStandaloneToolCallsDetailed(thinking, toolNames)
	if len(thinkingParsed.Calls) > 0 {
		return thinkingParsed
	}
	return textParsed
}

func InvalidTaskOutputCallDetail(calls []toolcall.ParsedToolCall, finalPrompt string) (int, string, string, bool) {
	_, dropped := claudecodeharness.FilterInvalidTaskOutputCallsWithReport(calls, finalPrompt)
	if len(dropped) == 0 {
		return 0, "", "", false
	}
	return http.StatusBadGateway, "Upstream model requested TaskOutput for an unknown or inactive task_id.", claudecodeharness.InvalidToolCallCode, true
}

func InvalidToolCallDetail(message string) (int, string, string) {
	if strings.TrimSpace(message) == "" {
		message = "Upstream model emitted invalid tool call syntax."
	}
	return http.StatusBadGateway, message, claudecodeharness.InvalidToolCallCode
}
