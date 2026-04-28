package claude

import (
	"encoding/json"
	"net/http"

	"ds2api/internal/util"
)

func (h *Handler) CountTokens(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.Determine(r)
	if err != nil {
		writeClaudeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	defer h.Auth.Release(a)

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClaudeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	model, _ := req["model"].(string)
	messages, _ := req["messages"].([]any)
	if model == "" || len(messages) == 0 {
		writeClaudeError(w, http.StatusBadRequest, "Request must include 'model' and 'messages'.")
		return
	}
	inputTokens := 0
	if sys, ok := req["system"].(string); ok {
		inputTokens += util.EstimateTokens(sys)
	}
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		inputTokens += 2
		inputTokens += util.EstimateTokens(extractMessageContent(msg["content"]))
	}
	if tools, ok := req["tools"].([]any); ok {
		for _, t := range tools {
			b, _ := json.Marshal(t)
			inputTokens += util.EstimateTokens(string(b))
		}
	}
	if inputTokens < 1 {
		inputTokens = 1
	}
	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": inputTokens})
}
