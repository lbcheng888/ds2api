package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	writeOpenAIErrorWithCode(w, status, message, "")
}

func writeOpenAIErrorWithCode(w http.ResponseWriter, status int, message, code string) {
	message = normalizeOpenAIErrorMessage(message)
	if code == "" {
		code = openAIErrorCode(status)
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    openAIErrorType(status),
			"code":    code,
			"param":   nil,
		},
	})
}

func normalizeOpenAIErrorMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "request failed"
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raw); err == nil {
		if errObj, ok := raw["error"].(map[string]any); ok {
			if msg := strings.TrimSpace(fmt.Sprint(errObj["message"])); msg != "" && msg != "<nil>" {
				return msg
			}
		}
		if msg := strings.TrimSpace(fmt.Sprint(raw["message"])); msg != "" && msg != "<nil>" {
			return msg
		}
	}
	return trimmed
}

func openAIErrorType(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusServiceUnavailable:
		return "service_unavailable_error"
	default:
		if status >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

func openAIErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "authentication_failed"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusTooManyRequests:
		return "rate_limit_exceeded"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		if status >= 500 {
			return "internal_error"
		}
		return "invalid_request"
	}
}
