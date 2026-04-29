package claude

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
	textclean "ds2api/internal/textclean"
	"ds2api/internal/toolcall"
)

type claudeStreamRuntime struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	canFlush bool

	model               string
	finalPrompt         string
	toolNames           []string
	toolSchemas         toolcall.ParameterSchemas
	allowMetaAgentTools bool
	messages            []any

	thinkingEnabled       bool
	searchEnabled         bool
	bufferToolContent     bool
	stripReferenceMarkers bool

	messageID       string
	outputSanitizer textclean.StreamSanitizer
	thinking        strings.Builder
	text            strings.Builder

	nextBlockIndex     int
	thinkingBlockOpen  bool
	thinkingBlockIndex int
	textBlockOpen      bool
	textBlockIndex     int
	ended              bool
	upstreamErr        string

	recoveryNeeded bool
	recoveryContext string
}

func newClaudeStreamRuntime(
	w http.ResponseWriter,
	rc *http.ResponseController,
	canFlush bool,
	model string,
	messages []any,
	thinkingEnabled bool,
	searchEnabled bool,
	stripReferenceMarkers bool,
	toolNames []string,
	toolSchemas toolcall.ParameterSchemas,
	finalPrompt string,
	allowMetaAgentTools bool,
) *claudeStreamRuntime {
	return &claudeStreamRuntime{
		w:                     w,
		rc:                    rc,
		canFlush:              canFlush,
		model:                 model,
		finalPrompt:           finalPrompt,
		messages:              messages,
		thinkingEnabled:       thinkingEnabled,
		searchEnabled:         searchEnabled,
		bufferToolContent:     len(toolNames) > 0,
		stripReferenceMarkers: stripReferenceMarkers,
		toolNames:             toolNames,
		toolSchemas:           toolSchemas,
		allowMetaAgentTools:   allowMetaAgentTools,
		messageID:             fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		thinkingBlockIndex:    -1,
		textBlockIndex:        -1,
	}
}

func (s *claudeStreamRuntime) onParsed(parsed sse.LineResult) streamengine.ParsedDecision {
	if !parsed.Parsed {
		return streamengine.ParsedDecision{}
	}
	if parsed.ErrorMessage != "" {
		s.upstreamErr = parsed.ErrorMessage
		return streamengine.ParsedDecision{Stop: true, StopReason: streamengine.StopReason("upstream_error")}
	}
	if parsed.Stop {
		if parsed.Finished && s.bufferToolContent && len(s.toolNames) > 0 {
			return streamengine.ParsedDecision{}
		}
		return streamengine.ParsedDecision{Stop: true}
	}

	contentSeen := false
	for _, p := range parsed.Parts {
		cleanedText := cleanVisibleOutput(s.outputSanitizer.Sanitize(p.Text), s.stripReferenceMarkers)
		if cleanedText == "" {
			continue
		}
		if p.Type != "thinking" && s.searchEnabled && sse.IsCitation(cleanedText) {
			continue
		}
		contentSeen = true

		if p.Type == "thinking" {
			if !s.thinkingEnabled {
				continue
			}
			trimmed := sse.TrimContinuationOverlap(s.thinking.String(), cleanedText)
			if trimmed == "" {
				continue
			}
			s.thinking.WriteString(trimmed)
			if s.bufferToolContent {
				continue
			}
			s.closeTextBlock()
			if !s.thinkingBlockOpen {
				s.thinkingBlockIndex = s.nextBlockIndex
				s.nextBlockIndex++
				s.send("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": s.thinkingBlockIndex,
					"content_block": map[string]any{
						"type":     "thinking",
						"thinking": "",
					},
				})
				s.thinkingBlockOpen = true
			}
			s.send("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": s.thinkingBlockIndex,
				"delta": map[string]any{
					"type":     "thinking_delta",
					"thinking": trimmed,
				},
			})
			continue
		}

		trimmed := sse.TrimContinuationOverlap(s.text.String(), cleanedText)
		if trimmed == "" {
			continue
		}
		s.text.WriteString(trimmed)
		if s.bufferToolContent {
			if hasUnclosedCodeFence(s.text.String()) {
				continue
			}
			continue
		}
		s.closeThinkingBlock()
		if !s.textBlockOpen {
			s.textBlockIndex = s.nextBlockIndex
			s.nextBlockIndex++
			s.send("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": s.textBlockIndex,
				"content_block": map[string]any{
					"type": "text",
					"text": "",
				},
			})
			s.textBlockOpen = true
		}
		s.send("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": s.textBlockIndex,
			"delta": map[string]any{
				"type": "text_delta",
				"text": trimmed,
			},
		})
	}

	return streamengine.ParsedDecision{ContentSeen: contentSeen}
}

func hasUnclosedCodeFence(text string) bool {
	return strings.Count(text, "```")%2 == 1
}
