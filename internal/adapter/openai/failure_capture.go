//go:build legacy_openai_adapter

package openai

import (
	"encoding/json"
	"net/http"
	"strings"

	"ds2api/internal/config"
	"ds2api/internal/devcapture"
	"ds2api/internal/rawsample"
)

type failureCaptureSample struct {
	ChainKey     string
	CaptureIDs   []string
	SampleID     string
	SampleDir    string
	UpstreamPath string
}

func writeOpenAIErrorWithCodeAndFailureCapture(w http.ResponseWriter, status int, message, code, sessionID string) {
	capture := persistFailureCapture(w, sessionID, code)
	writeOpenAIErrorWithCode(w, status, withFailureCaptureMessage(message, capture), code)
}

func annotateFailureCaptureHeaders(w http.ResponseWriter, sessionID string) *failureCaptureSample {
	return persistFailureCapture(w, sessionID, "")
}

func persistFailureCapture(w http.ResponseWriter, sessionID, code string) *failureCaptureSample {
	sessionID = strings.TrimSpace(sessionID)
	if w == nil || sessionID == "" {
		return nil
	}
	chain, ok := devcapture.Global().LatestChainBySession(sessionID)
	if !ok {
		return nil
	}
	ids := chain.IDs()
	if len(ids) == 0 {
		return nil
	}
	w.Header().Set("X-Ds2-Capture-Chain", chain.Key)
	w.Header().Set("X-Ds2-Capture-Ids", strings.Join(ids, ","))
	w.Header().Set("X-Ds2-Capture-Save", "POST /admin/dev/raw-samples/save chain_key="+chain.Key)
	upstreamBody := combineFailureCaptureBodies(chain.Entries)
	if len(upstreamBody) == 0 {
		return &failureCaptureSample{ChainKey: chain.Key, CaptureIDs: ids}
	}
	saved, err := rawsample.Persist(rawsample.PersistOptions{
		RootDir:      config.RawStreamSampleRoot(),
		SampleID:     rawsample.DefaultSampleID("failure-" + code),
		Source:       "openai/failure/" + strings.TrimSpace(code),
		Request:      failureCaptureRequestPayload(chain),
		Capture:      failureCaptureSummary(chain.Entries),
		UpstreamBody: upstreamBody,
	})
	if err != nil {
		config.Logger.Warn("[failure_capture] failed to persist raw sample", "chain", chain.Key, "code", code, "error", err)
		return &failureCaptureSample{ChainKey: chain.Key, CaptureIDs: ids}
	}
	w.Header().Set("X-Ds2-Failure-Sample-Id", saved.SampleID)
	w.Header().Set("X-Ds2-Failure-Sample-Dir", saved.Dir)
	w.Header().Set("X-Ds2-Failure-Sample-Upstream", saved.UpstreamPath)
	return &failureCaptureSample{
		ChainKey:     chain.Key,
		CaptureIDs:   ids,
		SampleID:     saved.SampleID,
		SampleDir:    saved.Dir,
		UpstreamPath: saved.UpstreamPath,
	}
}

func withFailureCaptureMessage(message string, capture *failureCaptureSample) string {
	message = strings.TrimSpace(message)
	if capture == nil || strings.TrimSpace(capture.SampleID) == "" {
		return message
	}
	return strings.TrimSpace(message + " Raw sample saved: " + capture.SampleID + " (" + capture.SampleDir + ").")
}

func failureCaptureRequestPayload(chain devcapture.Chain) any {
	request := parseFailureCaptureRequest(chain.Entries[0].RequestBody)
	return map[string]any{
		"chain_key":        chain.Key,
		"capture_ids":      chain.IDs(),
		"deepseek_request": request,
	}
}

func failureCaptureSummary(entries []devcapture.Entry) rawsample.CaptureSummary {
	if len(entries) == 0 {
		return rawsample.CaptureSummary{}
	}
	summary := rawsample.CaptureSummary{
		Label:      strings.TrimSpace(entries[0].Label),
		URL:        strings.TrimSpace(entries[0].URL),
		StatusCode: entries[0].StatusCode,
	}
	totalBytes := 0
	rounds := make([]rawsample.CaptureRound, 0, len(entries))
	for _, entry := range entries {
		responseBytes := len(entry.ResponseBody)
		totalBytes += responseBytes
		rounds = append(rounds, rawsample.CaptureRound{
			Label:         strings.TrimSpace(entry.Label),
			URL:           strings.TrimSpace(entry.URL),
			StatusCode:    entry.StatusCode,
			ResponseBytes: responseBytes,
		})
	}
	summary.ResponseBytes = totalBytes
	if len(rounds) > 1 {
		summary.Rounds = rounds
	}
	return summary
}

func combineFailureCaptureBodies(entries []devcapture.Entry) []byte {
	var b strings.Builder
	needsNewline := false
	for _, entry := range entries {
		body := entry.ResponseBody
		if body == "" {
			continue
		}
		if b.Len() > 0 && needsNewline {
			b.WriteByte('\n')
		}
		b.WriteString(body)
		needsNewline = !strings.HasSuffix(body, "\n")
	}
	return []byte(b.String())
}

func parseFailureCaptureRequest(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	return raw
}
