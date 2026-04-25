package stream

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestConsumeSSEStopsAtMaxDuration(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()

	done := make(chan StopReason, 1)
	ConsumeSSE(ConsumeConfig{
		Context:     context.Background(),
		Body:        reader,
		MaxDuration: 20 * time.Millisecond,
	}, ConsumeHooks{
		OnFinalize: func(reason StopReason, _ error) {
			done <- reason
		},
	})

	select {
	case reason := <-done:
		if reason != StopReasonMaxDuration {
			t.Fatalf("expected max duration stop, got %q", reason)
		}
	default:
		t.Fatal("expected finalize to run synchronously")
	}
}
