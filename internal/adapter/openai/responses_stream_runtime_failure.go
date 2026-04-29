//go:build legacy_openai_adapter

package openai

import openaifmt "ds2api/internal/format/openai"

func (s *responsesStreamRuntime) failResponse(message, code string) {
	if s.failed {
		return
	}
	s.failed = true
	if s.markFailure != nil {
		s.markFailure()
	}
	s.closeReasoningItem()
	capture := annotateFailureCaptureHeaders(s.w, s.sessionID)
	message = withFailureCaptureMessage(message, capture)
	failedResp := map[string]any{
		"id":          s.responseID,
		"type":        "response",
		"object":      "response",
		"model":       s.model,
		"status":      "failed",
		"output":      []any{},
		"output_text": "",
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
			"param":   nil,
		},
	}
	if s.persistResponse != nil {
		s.persistResponse(failedResp)
	}
	s.sendEvent("response.failed", openaifmt.BuildResponsesFailedPayload(s.responseID, s.model, message, code))
	s.sendDone()
}
