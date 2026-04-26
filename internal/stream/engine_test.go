package stream

import (
	"context"
	"io"
	"testing"
	"time"

	"ds2api/internal/sse"
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

func TestConsumeSSEStopsWhenNoActionProgress(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()

	done := make(chan StopReason, 1)
	go func() {
		ConsumeSSE(ConsumeConfig{
			Context:           context.Background(),
			Body:              reader,
			KeepAliveInterval: 5 * time.Millisecond,
			ActionTimeout:     20 * time.Millisecond,
		}, ConsumeHooks{
			OnParsed: func(_ sse.LineResult) ParsedDecision {
				return ParsedDecision{ContentSeen: true}
			},
			OnFinalize: func(reason StopReason, _ error) {
				done <- reason
			},
		})
	}()

	_, _ = writer.Write([]byte("data: {\"p\":\"response/content\",\"v\":\"thinking\"}\n"))

	select {
	case reason := <-done:
		if reason != StopReasonNoActionTimeout {
			t.Fatalf("expected no-action timeout, got %q", reason)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected no-action timeout")
	}
}
