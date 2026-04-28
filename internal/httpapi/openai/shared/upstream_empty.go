package shared

import (
	"net/http"
	"strings"
)

func ShouldWriteUpstreamEmptyOutputError(text string) bool {
	return text == ""
}

func UpstreamEmptyOutputDetail(contentFilter bool, text, thinking string) (int, string, string) {
	_ = text
	if contentFilter {
		return http.StatusBadRequest, "Upstream content filtered the response and returned no output.", "content_filter"
	}
	if thinking != "" {
		return http.StatusTooManyRequests, "Upstream account hit a rate limit and returned reasoning without visible output.", "upstream_empty_output"
	}
	return http.StatusTooManyRequests, "Upstream account hit a rate limit and returned empty output.", "upstream_empty_output"
}

func WriteUpstreamEmptyOutputError(w http.ResponseWriter, text, thinking string, contentFilter bool) bool {
	if !ShouldWriteUpstreamEmptyOutputError(text) {
		return false
	}
	status, message, code := UpstreamEmptyOutputDetail(contentFilter, text, thinking)
	WriteOpenAIErrorWithCode(w, status, message, code)
	return true
}

func UpstreamStreamErrorDetail(code, message string) (int, string, string) {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Upstream stream error."
	}
	switch code {
	case "input_exceeds_limit", "upstream_input_exceeds_limit":
		return http.StatusRequestEntityTooLarge, message, "input_exceeds_limit"
	case "content_filter":
		return http.StatusBadRequest, message, "content_filter"
	default:
		if code == "" {
			code = "upstream_error"
		}
		return http.StatusBadGateway, message, code
	}
}
