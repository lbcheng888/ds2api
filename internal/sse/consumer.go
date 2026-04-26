package sse

import (
	"net/http"
	"strings"

	"ds2api/internal/deepseek"
)

// CollectResult holds the aggregated text and thinking content from a
// DeepSeek SSE stream, consumed to completion (non-streaming use case).
type CollectResult struct {
	Text          string
	Thinking      string
	ContentFilter bool
	ErrorMessage  string
	ErrorCode     string
	CitationLinks map[int]string
}

// CollectStream fully consumes a DeepSeek SSE response and separates
// thinking content from text content. This replaces the duplicated
// stream-collection logic in openai.handleNonStream, claude.collectDeepSeek,
// and admin.testAccount.
//
// The caller is responsible for closing resp.Body unless closeBody is true.
func CollectStream(resp *http.Response, thinkingEnabled bool, closeBody bool) CollectResult {
	if closeBody {
		defer func() { _ = resp.Body.Close() }()
	}
	text := strings.Builder{}
	thinking := strings.Builder{}
	contentFilter := false
	errorMessage := ""
	errorCode := ""
	stopped := false
	collector := newCitationLinkCollector()
	currentType := "text"
	if thinkingEnabled {
		currentType = "thinking"
	}
	eventName := ""
	_ = deepseek.ScanSSELines(resp, func(line []byte) bool {
		trimmedLine := strings.TrimSpace(string(line))
		if strings.HasPrefix(trimmedLine, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(trimmedLine, "event:"))
			return true
		}
		chunk, done, parsed := ParseDeepSeekSSELine(line)
		if parsed && !done {
			collector.ingestChunk(chunk)
		}
		if done {
			return false
		}
		result := ParseDeepSeekContentLineWithEvent(line, eventName, thinkingEnabled, currentType)
		if result.Parsed {
			eventName = ""
		}
		currentType = result.NextType
		if !result.Parsed {
			return true
		}
		if stopped {
			if result.LateToolTitle {
				appendCollectParts(&text, &thinking, result.Parts)
			}
			return true
		}
		if result.Stop {
			if result.ContentFilter {
				contentFilter = true
			}
			if result.ErrorMessage != "" {
				errorMessage = result.ErrorMessage
				errorCode = result.ErrorCode
			}
			// Keep scanning to collect late-arriving citation metadata lines
			// that can appear after response/status=FINISHED, but stop as soon
			// as [DONE] arrives.
			stopped = true
			return true
		}
		for _, p := range result.Parts {
			appendCollectPart(&text, &thinking, p)
		}
		return true
	})
	return CollectResult{
		Text:          text.String(),
		Thinking:      thinking.String(),
		ContentFilter: contentFilter,
		ErrorMessage:  errorMessage,
		ErrorCode:     errorCode,
		CitationLinks: collector.build(),
	}
}

func appendCollectParts(text, thinking *strings.Builder, parts []ContentPart) {
	for _, p := range parts {
		appendCollectPart(text, thinking, p)
	}
}

func appendCollectPart(text, thinking *strings.Builder, p ContentPart) {
	if p.Type == "thinking" {
		trimmed := TrimContinuationOverlap(thinking.String(), p.Text)
		thinking.WriteString(trimmed)
		return
	}
	trimmed := TrimContinuationOverlap(text.String(), p.Text)
	text.WriteString(trimmed)
}
