package sse

import (
	"bufio"
	"context"
	"io"
	"time"
)

const (
	parsedLineBufferSize = 128
	lineReaderBufferSize = 64 * 1024
	minFlushChars        = 160
	maxFlushWait         = 80 * time.Millisecond
)

// StartParsedLinePump scans an upstream DeepSeek SSE body and emits normalized
// line parse results. It centralizes scanner setup + current fragment type
// tracking for all streaming adapters.
func StartParsedLinePump(ctx context.Context, body io.Reader, thinkingEnabled bool, initialType string) (<-chan LineResult, <-chan error) {
	out := make(chan LineResult, parsedLineBufferSize)
	done := make(chan error, 1)
	go func() {
		defer close(out)
		type scanItem struct {
			line []byte
			err  error
			eof  bool
		}
		lineCh := make(chan scanItem, 1)
		stopReader := make(chan struct{})
		defer close(stopReader)
		go func() {
			sendScanItem := func(item scanItem) bool {
				select {
				case lineCh <- item:
					return true
				case <-ctx.Done():
					return false
				case <-stopReader:
					return false
				}
			}
			defer close(lineCh)
			reader := bufio.NewReaderSize(body, lineReaderBufferSize)
			for {
				line, err := reader.ReadBytes('\n')
				if len(line) > 0 {
					line = append([]byte{}, line...)
					if !sendScanItem(scanItem{line: line}) {
						return
					}
				}
				if err != nil {
					if err == io.EOF {
						err = nil
					}
					_ = sendScanItem(scanItem{err: err, eof: true})
					return
				}
			}
		}()

		ticker := time.NewTicker(maxFlushWait)
		defer ticker.Stop()
		currentType := initialType
		var pending *LineResult
		pendingChars := 0

		sendResult := func(r LineResult) bool {
			select {
			case out <- r:
				return true
			case <-ctx.Done():
				done <- ctx.Err()
				return false
			}
		}

		flushPending := func() bool {
			if pending == nil {
				return true
			}
			if !sendResult(*pending) {
				return false
			}
			pending = nil
			pendingChars = 0
			return true
		}

		for {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
				if !flushPending() {
					return
				}
			case item, ok := <-lineCh:
				if !ok || item.eof {
					if !flushPending() {
						return
					}
					done <- item.err
					return
				}
				line := item.line
				result := ParseDeepSeekContentLine(line, thinkingEnabled, currentType)
				currentType = result.NextType

				canAccumulate := result.Parsed && !result.Stop && result.ErrorMessage == "" && !result.ContentFilter && result.ResponseMessageID == 0
				if canAccumulate {
					lineChars := 0
					for _, p := range result.Parts {
						lineChars += len(p.Text)
					}
					for _, p := range result.ToolDetectionThinkingParts {
						lineChars += len(p.Text)
					}
					if lineChars > 0 {
						if pending == nil {
							cp := result
							pending = &cp
						} else {
							pending.Parts = append(pending.Parts, result.Parts...)
							pending.ToolDetectionThinkingParts = append(pending.ToolDetectionThinkingParts, result.ToolDetectionThinkingParts...)
							pending.NextType = result.NextType
						}
						pendingChars += lineChars
						if pendingChars < minFlushChars {
							continue
						}
						if !flushPending() {
							return
						}
						continue
					}
				}

				if !flushPending() {
					return
				}
				if !sendResult(result) {
					return
				}
			}
		}
	}()
	return out, done
}
