//go:build legacy_openai_adapter

package openai

import (
	"net/http"

	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

const upstreamMissingToolCallCode = claudecodeharness.MissingToolCallCode
const upstreamInvalidToolCallCode = claudecodeharness.InvalidToolCallCode

func futureActionMissingToolCallDetail(finalText, finalPrompt string, toolNames []string, toolSchemas toolcall.ParameterSchemas, allowMetaAgentTools bool) (int, string, string, bool) {
	decision := claudecodeharness.DetectMissingToolCall(claudecodeharness.MissingToolCallInput{
		Text:                finalText,
		FinalPrompt:         finalPrompt,
		ToolNames:           toolNames,
		ToolSchemas:         toolSchemas,
		AllowMetaAgentTools: allowMetaAgentTools,
	})
	if !decision.Blocked {
		return 0, "", "", false
	}
	return http.StatusBadGateway, decision.Message, decision.Code, true
}
