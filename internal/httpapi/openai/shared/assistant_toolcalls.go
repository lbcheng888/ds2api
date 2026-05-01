package shared

import (
	"net/http"
	"strings"

	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

func DetectAssistantToolCalls(rawText, visibleText, exposedThinking, detectionThinking string, toolNames []string) toolcall.ToolCallParseResult {
	textParsed := toolcall.ParseStandaloneToolCallsDetailed(rawText, toolNames)
	if len(textParsed.Calls) > 0 {
		return textParsed
	}
	if strings.TrimSpace(visibleText) != "" {
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

func MissingToolCallDetail(finalText, finalPrompt string, toolNames []string, schemas toolcall.ParameterSchemas, allowMetaAgentTools bool) (int, string, string, bool) {
	decision := claudecodeharness.DetectMissingToolCall(claudecodeharness.MissingToolCallInput{
		Text:                finalText,
		FinalPrompt:         finalPrompt,
		ToolNames:           toolNames,
		ToolSchemas:         schemas,
		AllowMetaAgentTools: allowMetaAgentTools,
	})
	if !decision.Blocked {
		return 0, "", "", false
	}
	return http.StatusBadGateway, decision.Message, decision.Code, true
}
