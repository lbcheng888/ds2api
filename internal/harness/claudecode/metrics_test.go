package claudecode

import "testing"

func TestSnapshotMetricsIncludesDeduplicationCounters(t *testing.T) {
	resetMetricsForTest()
	defer resetMetricsForTest()

	recordRepair("claudecode", "agent_launch_promise")
	recordStreamOutcome("claudecode", "tool_call")
	recordFailureDecision("claudecode", MissingToolCallCode)
	RecordDeduplication("claudecode", "tool_calls", 2)
	RecordDeduplication("claudecode", "todo_items", 3)
	RecordDeduplication("claudecode", "ignored", 0)

	snapshot := SnapshotMetrics()
	profile, ok := snapshot["claudecode"].(map[string]any)
	if !ok {
		t.Fatalf("expected claudecode profile, got %#v", snapshot)
	}
	dedupes, ok := profile["dedupes"].(map[string]int64)
	if !ok {
		t.Fatalf("expected dedupe counters, got %#v", profile["dedupes"])
	}
	if got := dedupes["tool_calls"]; got != 2 {
		t.Fatalf("expected tool call dedupe count 2, got %d", got)
	}
	if got := dedupes["todo_items"]; got != 3 {
		t.Fatalf("expected todo item dedupe count 3, got %d", got)
	}
	if _, exists := dedupes["ignored"]; exists {
		t.Fatalf("zero dropped dedupe should not be recorded: %#v", dedupes)
	}
}

func TestDetectMissingToolCallNoRecordDoesNotIncrementFailureCounters(t *testing.T) {
	resetMetricsForTest()
	defer resetMetricsForTest()

	got := DetectMissingToolCallNoRecord(MissingToolCallInput{
		Text:      "In plan mode - before writing any code - I first need to understand primary_object_plan.cheng.",
		ToolNames: []string{"Read", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected no-record missing decision, got %#v", got)
	}
	if snapshot := SnapshotMetrics(); len(snapshot) != 0 {
		t.Fatalf("no-record missing decision should not mutate metrics, got %#v", snapshot)
	}
}

func resetMetricsForTest() {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	metricsState.profiles = map[string]*profileMetrics{}
}
