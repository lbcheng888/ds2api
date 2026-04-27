package protocol

import "testing"

func TestStreamStateRetryableFailure(t *testing.T) {
	state := StreamState{ErrorCode: "upstream_empty_output"}
	if !state.RetryableFailure() {
		t.Fatalf("expected upstream_empty_output to be retryable before output")
	}
	state.VisibleContent = true
	if state.RetryableFailure() {
		t.Fatalf("expected visible content to disable retry")
	}
}

func TestStreamStateRejectsUnknownFailure(t *testing.T) {
	if (StreamState{ErrorCode: "upstream_error"}).RetryableFailure() {
		t.Fatalf("expected generic upstream_error not to be protocol-retryable")
	}
}
