package rawsample

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeSampleID(t *testing.T) {
	got := NormalizeSampleID("  Hello, World!  ")
	if got != "hello-world" {
		t.Fatalf("expected hello-world, got %q", got)
	}
}

func TestPersistWritesSampleFilesAndMeta(t *testing.T) {
	root := t.TempDir()
	saved, err := Persist(PersistOptions{
		RootDir:  root,
		SampleID: "My Sample! 01",
		Source:   "unit-test",
		Request: map[string]any{
			"model":  "deepseek-v4-flash",
			"stream": true,
			"messages": []any{
				map[string]any{"role": "user", "content": "广州天气"},
			},
		},
		Capture: CaptureSummary{
			Label:      "deepseek_completion",
			URL:        "https://chat.deepseek.com/api/v0/chat/completion",
			StatusCode: 200,
		},
		UpstreamBody: []byte("data: {\"p\":\"response/content\",\"v\":\"hello [reference:1]\"}\n\n" +
			"data: {\"v\":\"FINISHED\",\"p\":\"response/status\"}\n\n"),
	})
	if err != nil {
		t.Fatalf("Persist failed: %v", err)
	}

	if saved.SampleID != "my-sample-01" {
		t.Fatalf("expected normalized sample id, got %q", saved.SampleID)
	}
	if _, err := os.Stat(saved.Dir); err != nil {
		t.Fatalf("sample dir missing: %v", err)
	}
	if _, err := os.Stat(saved.UpstreamPath); err != nil {
		t.Fatalf("upstream file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(saved.Dir, "openai.stream.sse")); !os.IsNotExist(err) {
		t.Fatalf("unexpected processed stream file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(saved.Dir, "openai.output.txt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected processed text file: %v", err)
	}

	metaBytes, err := os.ReadFile(saved.MetaPath)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var meta Meta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if meta.SampleID != saved.SampleID {
		t.Fatalf("expected meta sample id %q, got %q", saved.SampleID, meta.SampleID)
	}
	if meta.Capture.ReferenceMarkerCount != 1 {
		t.Fatalf("expected one reference marker, got %+v", meta.Capture)
	}
	if meta.Capture.FinishedTokenCount != 1 {
		t.Fatalf("expected one finished token, got %+v", meta.Capture)
	}
	if meta.Analysis == nil {
		t.Fatal("expected persisted analysis")
	}
	if meta.Analysis.Category != "ok" {
		t.Fatalf("expected ok analysis, got %+v", meta.Analysis)
	}
	if strings.Contains(string(metaBytes), "\"processed\"") {
		t.Fatalf("meta should not include processed payload: %s", string(metaBytes))
	}
}

func TestAnalyzeUpstreamBodyClassifiesReasoningOnly(t *testing.T) {
	analysis := AnalyzeUpstreamBody([]byte(
		`data: {"p":"response/thinking_content","v":"I will inspect files."}` + "\n\n" +
			`data: {"p":"response/status","v":"FINISHED"}` + "\n\n",
	))
	if analysis.Category != "reasoning_without_visible_output" {
		t.Fatalf("unexpected category: %+v", analysis)
	}
	if !analysis.SawFinish || analysis.ReasoningChars == 0 || analysis.VisibleChars != 0 {
		t.Fatalf("unexpected analysis counters: %+v", analysis)
	}
}

func TestAnalyzeUpstreamBodyClassifiesToolSyntax(t *testing.T) {
	analysis := AnalyzeUpstreamBody([]byte(
		`data: {"p":"response/content","v":"<tool_call><tool_name>read</tool_name></tool_call>"}` + "\n\n" +
			`data: {"p":"response/status","v":"FINISHED"}` + "\n\n",
	))
	if analysis.Category != "tool_syntax_candidate" {
		t.Fatalf("unexpected category: %+v", analysis)
	}
	if analysis.ToolSyntaxCount != 1 {
		t.Fatalf("expected one tool syntax hit, got %+v", analysis)
	}
}

func TestAnalyzeUpstreamBodyClassifiesEmptyStream(t *testing.T) {
	analysis := AnalyzeUpstreamBody(nil)
	if analysis.Category != "empty_stream" {
		t.Fatalf("unexpected category: %+v", analysis)
	}
}
