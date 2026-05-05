package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	dsclient "ds2api/internal/deepseek/client"
	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/promptcompat"
)

const (
	currentInputFilename    = promptcompat.CurrentInputContextFilename
	currentInputContentType = "text/plain; charset=utf-8"
	currentInputPurpose     = "assistants"
	hotContextMaxMessages   = 8
	hotContextMaxRunes      = 8000
	executionLedgerMaxItems = 8
	uploadCacheMaxEntries   = 256
	uploadCacheTTL          = 30 * time.Minute
)

type CurrentInputConfigReader interface {
	CurrentInputFileEnabled() bool
	CurrentInputFileMinChars() int
}

type CurrentInputUploader interface {
	UploadFile(ctx context.Context, a *auth.RequestAuth, req dsclient.UploadFileRequest, maxAttempts int) (*dsclient.UploadFileResult, error)
}

type Service struct {
	Store CurrentInputConfigReader
	DS    CurrentInputUploader
}

func (s Service) ApplyCurrentInputFile(ctx context.Context, a *auth.RequestAuth, stdReq promptcompat.StandardRequest) (promptcompat.StandardRequest, error) {
	if stdReq.CurrentInputFileApplied || s.DS == nil || s.Store == nil || a == nil || !s.Store.CurrentInputFileEnabled() {
		return stdReq, nil
	}
	threshold := s.Store.CurrentInputFileMinChars()

	index, text := latestUserInputForFile(stdReq.Messages)
	if index < 0 {
		return stdReq, nil
	}
	if len([]rune(text)) < threshold {
		return stdReq, nil
	}
	fileText := promptcompat.BuildOpenAICurrentInputContextTranscript(stdReq.Messages)
	if strings.TrimSpace(fileText) == "" {
		return stdReq, errors.New("current user input file produced empty transcript")
	}
	modelType := "default"
	if resolvedType, ok := config.GetModelType(stdReq.ResolvedModel); ok {
		modelType = resolvedType
	}
	fileID, err := s.uploadCurrentInputFile(ctx, a, modelType, fileText)
	if err != nil {
		return stdReq, fmt.Errorf("upload current user input file: %w", err)
	}
	if fileID == "" {
		return stdReq, errors.New("upload current user input file returned empty file id")
	}

	liveContext := buildCurrentInputLiveContext(stdReq.Messages, index)
	messages := []any{
		map[string]any{
			"role":    "user",
			"content": currentInputFilePrompt(latestUserRequestForLivePrompt(text, s.Store), liveContext),
		},
	}

	stdReq.Messages = messages
	stdReq.HistoryText = fileText
	stdReq.CurrentInputFileApplied = true
	stdReq.RefFileIDs = prependUniqueRefFileID(stdReq.RefFileIDs, fileID)
	stdReq.FinalPrompt, stdReq.ToolNames = promptcompat.BuildOpenAIPrompt(messages, stdReq.ToolsRaw, "", stdReq.ToolChoice, stdReq.Thinking)
	// Token accounting must reflect the actual downstream context:
	// the uploaded DS2API_HISTORY.txt file content + the continuation live prompt.
	stdReq.PromptTokenText = fileText + "\n" + stdReq.FinalPrompt
	return stdReq, nil
}

func (s Service) uploadCurrentInputFile(ctx context.Context, a *auth.RequestAuth, modelType, fileText string) (string, error) {
	key := currentInputUploadCacheKey(s.DS, a, modelType, fileText)
	if key != "" {
		if fileID, ok := defaultCurrentInputUploadCache.Get(key); ok {
			return fileID, nil
		}
	}
	result, err := s.DS.UploadFile(ctx, a, dsclient.UploadFileRequest{
		Filename:    currentInputFilename,
		ContentType: currentInputContentType,
		Purpose:     currentInputPurpose,
		ModelType:   modelType,
		Data:        []byte(fileText),
	}, 3)
	if err != nil {
		return "", err
	}
	fileID := ""
	if result != nil {
		fileID = strings.TrimSpace(result.ID)
	}
	if key != "" && fileID != "" {
		defaultCurrentInputUploadCache.Put(key, fileID)
	}
	return fileID, nil
}

func latestUserInputForFile(messages []any) (int, string) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(shared.AsString(msg["role"])))
		if role != "user" {
			continue
		}
		text := promptcompat.NormalizeOpenAIContentForPrompt(msg["content"])
		if strings.TrimSpace(text) == "" {
			return -1, ""
		}
		return i, text
	}
	return -1, ""
}

type currentInputLiveContext struct {
	HotTranscript   string
	ExecutionLedger string
}

func currentInputFilePrompt(latestUserText string, live currentInputLiveContext) string {
	latestUserText = strings.TrimSpace(latestUserText)
	prompt := "Continue from the latest state in the attached DS2API_HISTORY.txt context. Treat it as the current working state and answer the latest user request directly. Do not repeat an earlier assistant answer unless the latest user request explicitly asks for that."
	if latestUserText != "" {
		prompt += "\n\nLatest user request, authoritative:\n" + latestUserText
	}
	if strings.TrimSpace(live.ExecutionLedger) != "" {
		prompt += "\n\nRecent execution ledger, authoritative hot state:\n" + strings.TrimSpace(live.ExecutionLedger)
	}
	if strings.TrimSpace(live.HotTranscript) != "" {
		prompt += "\n\nRecent hot context copied inline from DS2API_HISTORY.txt:\n" + strings.TrimSpace(live.HotTranscript)
	}
	return prompt
}

func latestUserRequestForLivePrompt(text string, store CurrentInputConfigReader) string {
	out := strings.TrimSpace(text)
	candidates := []string{}
	if promptReader, ok := store.(interface{ ThinkingInjectionPrompt() string }); ok {
		if prompt := strings.TrimSpace(promptReader.ThinkingInjectionPrompt()); prompt != "" {
			candidates = append(candidates, prompt)
		}
	}
	candidates = append(candidates, promptcompat.DefaultThinkingInjectionPrompt)
	for _, candidate := range candidates {
		if candidate == "" || !strings.HasSuffix(out, candidate) {
			continue
		}
		out = strings.TrimSpace(strings.TrimSuffix(out, candidate))
	}
	if idx := strings.Index(out, "\n\n"+promptcompat.ThinkingInjectionMarker); idx >= 0 {
		out = strings.TrimSpace(out[:idx])
	}
	return out
}

func buildCurrentInputLiveContext(messages []any, latestUserIndex int) currentInputLiveContext {
	hotMessages := currentInputHotMessages(messages, latestUserIndex)
	if len(hotMessages) == 0 {
		return currentInputLiveContext{}
	}
	return currentInputLiveContext{
		HotTranscript:   truncateRunes(promptcompat.BuildOpenAIHistoryTranscript(hotMessages), hotContextMaxRunes),
		ExecutionLedger: buildExecutionLedger(hotMessages),
	}
}

func currentInputHotMessages(messages []any, latestUserIndex int) []any {
	if latestUserIndex <= 0 || latestUserIndex > len(messages) {
		return nil
	}
	start := 0
	for i := latestUserIndex - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(shared.AsString(msg["role"])), "user") {
			start = i + 1
			break
		}
	}
	if start >= latestUserIndex {
		return nil
	}
	if count := latestUserIndex - start; count > hotContextMaxMessages {
		start = latestUserIndex - hotContextMaxMessages
	}
	out := make([]any, 0, latestUserIndex-start)
	out = append(out, messages[start:latestUserIndex]...)
	return out
}

func buildExecutionLedger(messages []any) string {
	items := []string{}
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(shared.AsString(msg["role"])))
		if role != "tool" && role != "function" {
			continue
		}
		name := strings.TrimSpace(shared.AsString(msg["name"]))
		if name == "" {
			name = role
		}
		callID := strings.TrimSpace(shared.AsString(msg["tool_call_id"]))
		summary := summarizeLedgerContent(promptcompat.NormalizeOpenAIContentForPrompt(msg["content"]))
		item := "- " + name
		if callID != "" {
			item += " " + callID
		}
		item += ": " + summary
		items = append(items, item)
	}
	if len(items) == 0 {
		return ""
	}
	if len(items) > executionLedgerMaxItems {
		items = items[len(items)-executionLedgerMaxItems:]
	}
	return strings.Join(items, "\n")
}

func summarizeLedgerContent(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "(empty output)"
	}
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return truncateRunes(strings.Join(strings.Fields(line), " "), 240)
		}
	}
	return "(empty output)"
}

func truncateRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "\n...[truncated]"
}

func prependUniqueRefFileID(existing []string, fileID string) []string {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return existing
	}
	out := make([]string, 0, len(existing)+1)
	out = append(out, fileID)
	for _, id := range existing {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || strings.EqualFold(trimmed, fileID) {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

type currentInputUploadCache struct {
	mu      sync.Mutex
	entries map[string]currentInputUploadCacheEntry
	order   []string
}

type currentInputUploadCacheEntry struct {
	FileID    string
	StoredAt  time.Time
	LastUseAt time.Time
}

var defaultCurrentInputUploadCache = newCurrentInputUploadCache()

func newCurrentInputUploadCache() *currentInputUploadCache {
	return &currentInputUploadCache{
		entries: map[string]currentInputUploadCacheEntry{},
	}
}

func (c *currentInputUploadCache) Get(key string) (string, bool) {
	if c == nil || strings.TrimSpace(key) == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return "", false
	}
	now := time.Now()
	if now.Sub(entry.StoredAt) > uploadCacheTTL {
		delete(c.entries, key)
		return "", false
	}
	entry.LastUseAt = now
	c.entries[key] = entry
	return entry.FileID, true
}

func (c *currentInputUploadCache) Put(key, fileID string) {
	if c == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(fileID) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if _, exists := c.entries[key]; !exists {
		c.order = append(c.order, key)
	}
	c.entries[key] = currentInputUploadCacheEntry{
		FileID:    strings.TrimSpace(fileID),
		StoredAt:  now,
		LastUseAt: now,
	}
	c.pruneLocked()
}

func (c *currentInputUploadCache) pruneLocked() {
	now := time.Now()
	nextOrder := c.order[:0]
	for _, key := range c.order {
		entry, ok := c.entries[key]
		if !ok {
			continue
		}
		if now.Sub(entry.StoredAt) > uploadCacheTTL {
			delete(c.entries, key)
			continue
		}
		nextOrder = append(nextOrder, key)
	}
	c.order = nextOrder
	for len(c.order) > uploadCacheMaxEntries {
		key := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, key)
	}
}

func currentInputUploadCacheKey(ds CurrentInputUploader, a *auth.RequestAuth, modelType, fileText string) string {
	if ds == nil || a == nil || strings.TrimSpace(fileText) == "" {
		return ""
	}
	principal := currentInputPrincipalKey(a)
	if principal == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fileText))
	return strings.Join([]string{
		uploaderIdentity(ds),
		principal,
		strings.TrimSpace(modelType),
		hex.EncodeToString(sum[:]),
	}, "|")
}

func currentInputPrincipalKey(a *auth.RequestAuth) string {
	if a == nil {
		return ""
	}
	if accountID := strings.TrimSpace(a.AccountID); accountID != "" {
		return "account:" + accountID
	}
	if token := strings.TrimSpace(a.DeepSeekToken); token != "" {
		sum := sha256.Sum256([]byte(token))
		return "token:" + hex.EncodeToString(sum[:])
	}
	if callerID := strings.TrimSpace(a.CallerID); callerID != "" {
		return "caller:" + callerID
	}
	return ""
}

func uploaderIdentity(ds CurrentInputUploader) string {
	value := reflect.ValueOf(ds)
	if value.IsValid() {
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Map, reflect.Ptr, reflect.Slice, reflect.UnsafePointer:
			if !value.IsNil() {
				return fmt.Sprintf("%T:%x", ds, value.Pointer())
			}
		}
	}
	return fmt.Sprintf("%T", ds)
}
