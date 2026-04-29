//go:build legacy_openai_adapter

package openai

import (
	openaifmt "ds2api/internal/format/openai"
	"strings"

	"github.com/google/uuid"
)

func (s *responsesStreamRuntime) ensureReasoningItemID() string {
	if strings.TrimSpace(s.reasoningItemID) != "" {
		return s.reasoningItemID
	}
	s.reasoningItemID = "rs_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return s.reasoningItemID
}

func (s *responsesStreamRuntime) ensureReasoningOutputIndex() int {
	if s.reasoningOutputID >= 0 {
		return s.reasoningOutputID
	}
	s.reasoningOutputID = s.allocateOutputIndex()
	return s.reasoningOutputID
}

func (s *responsesStreamRuntime) ensureReasoningItemAdded() {
	if s.reasoningAdded {
		return
	}
	itemID := s.ensureReasoningItemID()
	item := map[string]any{
		"id":      itemID,
		"type":    "reasoning",
		"status":  "in_progress",
		"summary": []any{},
	}
	s.sendEvent(
		"response.output_item.added",
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, itemID, s.ensureReasoningOutputIndex(), item),
	)
	s.reasoningAdded = true
}

func (s *responsesStreamRuntime) closeReasoningItem() {
	if !s.reasoningAdded || s.reasoningDone {
		return
	}
	itemID := s.ensureReasoningItemID()
	item := map[string]any{
		"id":      itemID,
		"type":    "reasoning",
		"status":  "completed",
		"summary": []any{},
	}
	s.sendEvent(
		"response.output_item.done",
		openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, s.ensureReasoningOutputIndex(), item),
	)
	s.reasoningDone = true
}
