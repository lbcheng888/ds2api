package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ds2api/internal/config"
	"ds2api/internal/devcapture"
	"ds2api/internal/rawsample"
)

func (h *Handler) getDevDiagnostics(w http.ResponseWriter, r *http.Request) {
	limit := intFromQuery(r, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	queueStatus := map[string]any{}
	if h.Pool != nil {
		queueStatus = h.Pool.Status()
	}
	if h.AccountHealth != nil {
		queueStatus["account_health"] = h.AccountHealth.AccountHealthStatus()
	}

	captureChains := buildCaptureChains(devcapture.Global().Snapshot())
	captures := make([]map[string]any, 0, minInt(len(captureChains), limit))
	for _, chain := range captureChains {
		captures = append(captures, buildCaptureChainQueryItem(chain, ""))
		if len(captures) >= limit {
			break
		}
	}

	root := config.RawStreamSampleRoot()
	failureSamples, sampleErr := listFailureSamples(root, limit)
	body := map[string]any{
		"raw_sample_root":  root,
		"queue_status":     queueStatus,
		"failure_samples":  failureSamples,
		"capture_chains":   captures,
		"capture_count":    len(captureChains),
		"dev_capture_on":   devcapture.Global().Enabled(),
		"replay_help":      "Use ./tests/scripts/compare-raw-stream-sample.sh <sample-id> to replay one failure sample.",
		"generated_at_utc": time.Now().UTC().Format(time.RFC3339),
	}
	if sampleErr != nil {
		body["sample_error"] = sampleErr.Error()
	}
	writeJSON(w, http.StatusOK, body)
}

func listFailureSamples(root string, limit int) ([]map[string]any, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("raw sample root is empty")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]any{}, nil
		}
		return nil, err
	}

	items := make([]failureSampleItem, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		item, ok := readFailureSample(root, entry.Name())
		if ok {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].SortUnix > items[j].SortUnix
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.Payload)
	}
	return out, nil
}

type failureSampleItem struct {
	SortUnix int64
	Payload  map[string]any
}

func readFailureSample(root, sampleID string) (failureSampleItem, bool) {
	sampleID = rawsample.NormalizeSampleID(sampleID)
	if sampleID == "" {
		return failureSampleItem{}, false
	}
	dir := filepath.Join(root, sampleID)
	metaPath := filepath.Join(dir, "meta.json")
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return failureSampleItem{}, false
	}
	var meta rawsample.Meta
	if err := json.Unmarshal(b, &meta); err != nil {
		return failureSampleItem{}, false
	}
	if !isFailureSample(sampleID, meta.Source) {
		return failureSampleItem{}, false
	}

	capturedAt := strings.TrimSpace(meta.CapturedAtUTC)
	sortUnix := parseRFC3339Unix(capturedAt)
	if sortUnix == 0 {
		if info, err := os.Stat(metaPath); err == nil {
			sortUnix = info.ModTime().Unix()
		}
	}
	code := strings.TrimPrefix(strings.TrimSpace(meta.Source), "openai/failure/")
	payload := map[string]any{
		"sample_id":       sampleID,
		"sample_dir":      dir,
		"meta_path":       metaPath,
		"upstream_path":   filepath.Join(dir, "upstream.stream.sse"),
		"captured_at_utc": capturedAt,
		"source":          strings.TrimSpace(meta.Source),
		"error_code":      nilIfEmpty(code),
		"capture":         meta.Capture,
		"request_summary": failureRequestSummary(meta.Request),
		"replay_command":  "./tests/scripts/compare-raw-stream-sample.sh " + sampleID,
	}
	return failureSampleItem{SortUnix: sortUnix, Payload: payload}, true
}

func isFailureSample(sampleID, source string) bool {
	return strings.HasPrefix(sampleID, "failure-") || strings.HasPrefix(strings.TrimSpace(source), "openai/failure/")
}

func failureRequestSummary(v any) map[string]any {
	out := map[string]any{}
	root, _ := v.(map[string]any)
	if root == nil {
		return out
	}
	if chainKey := fieldString(root, "chain_key"); chainKey != "" {
		out["chain_key"] = chainKey
	}
	req, _ := root["deepseek_request"].(map[string]any)
	if req == nil {
		req = root
	}
	for _, key := range []string{"chat_session_id", "model", "model_preference", "thinking_enabled", "search_enabled"} {
		if value, ok := req[key]; ok && value != nil {
			out[key] = value
		}
	}
	return out
}

func parseRFC3339Unix(raw string) int64 {
	if raw == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return 0
	}
	return t.Unix()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
