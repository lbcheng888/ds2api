package sse

import (
	"bufio"
	"context"
	"io"
	"strings"
)

const (
	parsedLineBufferSize = 128
	scannerBufferSize    = 256 * 1024
	maxScannerLineSize   = 2 * 1024 * 1024
)

// StartParsedLinePump scans an upstream DeepSeek SSE body and emits normalized
// line parse results. It centralizes scanner setup + current fragment type
// tracking for all streaming adapters.
func StartParsedLinePump(ctx context.Context, body io.Reader, thinkingEnabled bool, initialType string) (<-chan LineResult, <-chan error) {
	out := make(chan LineResult, parsedLineBufferSize)
	done := make(chan error, 1)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, scannerBufferSize), maxScannerLineSize)
		currentType := initialType
		eventName := ""
		for scanner.Scan() {
			line := append([]byte{}, scanner.Bytes()...)
			trimmed := strings.TrimSpace(string(line))
			if strings.HasPrefix(trimmed, "event:") {
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
				continue
			}
			result := ParseDeepSeekContentLineWithEvent(line, eventName, thinkingEnabled, currentType)
			if result.Parsed {
				eventName = ""
			}
			currentType = result.NextType
			select {
			case out <- result:
			case <-ctx.Done():
				done <- ctx.Err()
				return
			}
		}
		done <- scanner.Err()
	}()
	return out, done
}
