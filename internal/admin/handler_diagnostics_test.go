package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/devcapture"
	"ds2api/internal/rawsample"
)

func TestGetDevDiagnosticsListsFailureSamplesAndCaptures(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DS2API_RAW_STREAM_SAMPLE_ROOT", root)
	devcapture.Global().Clear()
	defer devcapture.Global().Clear()

	_, err := rawsample.Persist(rawsample.PersistOptions{
		RootDir:  root,
		SampleID: "failure-upstream-missing-tool-call-test",
		Source:   "openai/failure/upstream_missing_tool_call",
		Request: map[string]any{
			"chain_key": "session:session-1",
			"deepseek_request": map[string]any{
				"chat_session_id": "session-1",
				"model":           "deepseek-v4-pro[1m]",
			},
		},
		Capture: rawsample.CaptureSummary{Label: "deepseek_completion", StatusCode: http.StatusOK},
		UpstreamBody: []byte(
			`data: {"p":"response/content","v":"I'll continue by reading files."}` + "\n" +
				`data: [DONE]` + "\n",
		),
	})
	if err != nil {
		t.Fatalf("persist failure sample: %v", err)
	}

	session := devcapture.Global().Start("deepseek_completion", "http://upstream.test/completion", "acc1", map[string]any{"chat_session_id": "session-1"})
	if session != nil {
		body := session.WrapBody(io.NopCloser(strings.NewReader("data: [DONE]\n")), http.StatusOK)
		_, _ = io.ReadAll(body)
		_ = body.Close()
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/dev/diagnostics?limit=10", nil)
	(&Handler{}).getDevDiagnostics(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	samples, _ := out["failure_samples"].([]any)
	if len(samples) != 1 {
		t.Fatalf("expected one failure sample, got %#v", out["failure_samples"])
	}
	first, _ := samples[0].(map[string]any)
	if got := first["error_code"]; got != "upstream_missing_tool_call" {
		t.Fatalf("unexpected error code: %#v", got)
	}
	if got := first["category"]; got != "missing_finish" {
		t.Fatalf("unexpected category: %#v", got)
	}
	if !strings.Contains(first["replay_command"].(string), "compare-raw-stream-sample.sh") {
		t.Fatalf("expected replay command, got %#v", first["replay_command"])
	}
	if count, _ := out["capture_count"].(float64); session != nil && count < 1 {
		t.Fatalf("expected capture count, got %#v", out["capture_count"])
	}
	summary, _ := out["failure_summary"].(map[string]any)
	if total, _ := summary["total"].(float64); total != 1 {
		t.Fatalf("expected one summarized failure sample, got %#v", summary)
	}
	byCode, _ := summary["by_error_code"].(map[string]any)
	if got, _ := byCode["upstream_missing_tool_call"].(float64); got != 1 {
		t.Fatalf("expected missing-tool summary, got %#v", byCode)
	}
	if metrics, _ := out["harness_metrics"].(map[string]any); metrics == nil {
		t.Fatalf("expected harness metrics, got %#v", out["harness_metrics"])
	}
}
