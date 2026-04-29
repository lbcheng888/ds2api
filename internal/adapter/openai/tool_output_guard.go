//go:build legacy_openai_adapter

package openai

import (
	"encoding/json"
	"net/http"

	"ds2api/internal/toolcall"
)

const upstreamToolCallTooLargeCode = "upstream_tool_call_too_large"

func normalizedToolCallsExceedInputBytes(calls []toolcall.ParsedToolCall, schemas toolcall.ParameterSchemas, allowMetaAgentTools bool, maxBytes int) bool {
	if maxBytes <= 0 || len(calls) == 0 {
		return false
	}
	normalized := toolcall.NormalizeCallsForSchemasWithMeta(calls, schemas, allowMetaAgentTools)
	for _, call := range normalized {
		b, err := json.Marshal(call.Input)
		if err != nil {
			continue
		}
		if len(b) > maxBytes {
			return true
		}
	}
	return false
}

func toolCallTooLargeError() (int, string, string) {
	return http.StatusBadGateway,
		"Upstream model emitted an oversized tool call payload. Use Edit/MultiEdit/apply_patch style small edits instead of rewriting large files.",
		upstreamToolCallTooLargeCode
}
