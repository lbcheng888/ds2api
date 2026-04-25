package openai

import (
	"net/http"
	"strings"

	"ds2api/internal/devcapture"
)

func annotateFailureCaptureHeaders(w http.ResponseWriter, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if w == nil || sessionID == "" {
		return
	}
	chain, ok := devcapture.Global().LatestChainBySession(sessionID)
	if !ok {
		return
	}
	ids := chain.IDs()
	if len(ids) == 0 {
		return
	}
	w.Header().Set("X-Ds2-Capture-Chain", chain.Key)
	w.Header().Set("X-Ds2-Capture-Ids", strings.Join(ids, ","))
	w.Header().Set("X-Ds2-Capture-Save", "POST /admin/dev/raw-samples/save chain_key="+chain.Key)
}
