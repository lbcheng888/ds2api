package accounts

import "net/http"

func (h *Handler) queueStatus(w http.ResponseWriter, _ *http.Request) {
	status := h.Pool.Status()
	if h.AccountHealth != nil {
		status["account_health"] = h.AccountHealth.AccountHealthStatus()
	}
	writeJSON(w, http.StatusOK, status)
}
