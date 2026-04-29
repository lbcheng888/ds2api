//go:build legacy_openai_adapter

package openai

import (
	"net/http"

	claudecodeharness "ds2api/internal/harness/claudecode"
	"ds2api/internal/toolcall"
)

func invalidTaskOutputCallDetail(calls []toolcall.ParsedToolCall, finalPrompt string) (int, string, string, bool) {
	invalid := claudecodeharness.InvalidTaskOutputIDs(calls, finalPrompt)
	if len(invalid) == 0 {
		return 0, "", "", false
	}
	return http.StatusBadGateway,
		"Upstream model requested TaskOutput for an unknown or inactive task_id.",
		upstreamInvalidToolCallCode,
		true
}
