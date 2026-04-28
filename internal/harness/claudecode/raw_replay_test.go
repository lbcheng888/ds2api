package claudecode

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"ds2api/internal/sse"
)

var replayToolNames = []string{
	"Agent", "TaskOutput", "Read", "Write", "Edit", "MultiEdit", "Bash",
	"Grep", "Glob", "Search", "TaskCreate", "TaskUpdate", "TodoRead",
	"TodoWrite", "exec_command", "apply_patch",
}

func TestRawFailureSamplesReplayThroughClaudeCodeHarness(t *testing.T) {
	for _, tc := range loadRawFailureGoldenCases(t) {
		t.Run(tc.sampleID, func(t *testing.T) {
			finalPrompt, text, thinking := replayRawSample(t, tc.sampleID)
			evaluated := EvaluateFinalOutput(FinalEvaluationInput{
				FinalPrompt:         finalPrompt,
				Text:                text,
				Thinking:            thinking,
				ToolNames:           replayToolNames,
				AllowMetaAgentTools: true,
			})
			if invalidIDs := evaluated.DroppedTaskOutputIDs; len(invalidIDs) > 0 {
				if tc.wantCode != InvalidToolCallCode {
					t.Fatalf("unexpected invalid TaskOutput ids %#v; text=%q visible=%q repair=%#v calls=%#v", invalidIDs, text, evaluated.Text, evaluated.Repair, evaluated.Calls)
				}
				return
			}
			if tc.wantCall {
				if len(evaluated.Calls) == 0 {
					t.Fatalf("expected executable tool calls; text=%q visible=%q repair=%#v", text, evaluated.Text, evaluated.Repair)
				}
				return
			}
			if !evaluated.MissingToolDecision.Blocked || evaluated.MissingToolDecision.Code != tc.wantCode {
				t.Fatalf("expected %s, got %#v; text=%q visible=%q repair=%#v calls=%#v", tc.wantCode, evaluated.MissingToolDecision, text, evaluated.Text, evaluated.Repair, evaluated.Calls)
			}
		})
	}
}

type rawFailureGoldenCase struct {
	sampleID string
	wantCode string
	wantCall bool
}

func loadRawFailureGoldenCases(t *testing.T) []rawFailureGoldenCase {
	t.Helper()
	path := filepath.Join("..", "..", "..", "tests", "fixtures", "raw_failure_golden.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw failure golden manifest: %v", err)
	}
	var rows []struct {
		SampleID string `json:"sample_id"`
		Expect   string `json:"expect"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("parse raw failure golden manifest: %v", err)
	}
	out := make([]rawFailureGoldenCase, 0, len(rows))
	for _, row := range rows {
		tc := rawFailureGoldenCase{sampleID: row.SampleID}
		switch row.Expect {
		case "tool_call":
			tc.wantCall = true
		case "missing_tool":
			tc.wantCode = MissingToolCallCode
		case "invalid_tool":
			tc.wantCode = InvalidToolCallCode
		default:
			t.Fatalf("unsupported raw failure expectation %q for %s", row.Expect, row.SampleID)
		}
		out = append(out, tc)
	}
	return out
}

func replayRawSample(t *testing.T, sampleID string) (string, string, string) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "tests", "raw_stream_samples", sampleID)
	metaRaw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("raw failure sample %s is not present locally", sampleID)
		}
		t.Fatalf("read meta: %v", err)
	}
	var meta struct {
		Request struct {
			DeepseekRequest struct {
				Prompt string `json:"prompt"`
			} `json:"deepseek_request"`
		} `json:"request"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatalf("parse meta: %v", err)
	}
	streamRaw, err := os.ReadFile(filepath.Join(dir, "upstream.stream.sse"))
	if err != nil {
		t.Fatalf("read upstream stream: %v", err)
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(streamRaw)),
		Header:     make(http.Header),
	}
	result := sse.CollectStream(resp, true, true)
	if result.ErrorMessage != "" {
		t.Fatalf("unexpected upstream error in replay: %s", result.ErrorMessage)
	}
	return meta.Request.DeepseekRequest.Prompt, result.Text, result.Thinking
}
