package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/deepseek"
	"ds2api/internal/util"
)

const maxManagedCompletionAccountAttempts = 3

type accountSwitcher interface {
	SwitchAccount(ctx context.Context, a *auth.RequestAuth) bool
}

type accountHealthReporter interface {
	MarkAccountFailure(a *auth.RequestAuth)
	MarkAccountSuccess(a *auth.RequestAuth)
}

func (h *Handler) callCompletionWithFailover(ctx context.Context, a *auth.RequestAuth, stdReq util.StandardRequest) (string, *http.Response, string, error) {
	attemptLimit := 1
	if a != nil && a.UseConfigToken {
		attemptLimit = maxManagedCompletionAccountAttempts
	}
	var lastStage string
	var lastErr error
	attemptReq := stdReq
	for attempt := 1; attempt <= attemptLimit; attempt++ {
		refreshedReq, refreshErr := h.ensureHistoryAttachmentForCurrentAccount(ctx, a, attemptReq)
		if refreshErr != nil {
			lastStage = "history_upload"
			lastErr = refreshErr
			h.markManagedAccountFailure(a)
			config.Logger.Warn("[openai_completion] attempt failed", "stage", lastStage, "attempt", attempt, "account", accountIDForLog(a), "error", refreshErr)
			if attempt >= attemptLimit || !h.switchManagedAccount(ctx, a) {
				break
			}
			continue
		}
		attemptReq = refreshedReq
		sessionID, resp, stage, err := h.callCompletionOnce(ctx, a, attemptReq)
		if err == nil {
			if resp != nil && shouldFailoverCompletionStatus(resp.StatusCode) && a != nil && a.UseConfigToken {
				lastStage = "completion_status"
				lastErr = fmt.Errorf("completion returned status %d", resp.StatusCode)
				_ = resp.Body.Close()
				if sessionID != "" {
					h.autoDeleteRemoteSession(ctx, a, sessionID)
				}
				h.markManagedAccountFailure(a)
				config.Logger.Warn("[openai_completion] attempt failed", "stage", lastStage, "attempt", attempt, "account", accountIDForLog(a), "status", resp.StatusCode)
				if attempt >= attemptLimit || !h.switchManagedAccount(ctx, a) {
					break
				}
				continue
			}
			h.markManagedAccountSuccess(a)
			return sessionID, resp, "", nil
		}
		lastStage = stage
		lastErr = err
		if sessionID != "" {
			h.autoDeleteRemoteSession(ctx, a, sessionID)
		}
		h.markManagedAccountFailure(a)
		config.Logger.Warn("[openai_completion] attempt failed", "stage", stage, "attempt", attempt, "account", accountIDForLog(a), "error", err)
		if attempt >= attemptLimit || !h.switchManagedAccount(ctx, a) {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("completion failed")
	}
	return "", nil, lastStage, lastErr
}

func (h *Handler) callCompletionOnce(ctx context.Context, a *auth.RequestAuth, stdReq util.StandardRequest) (string, *http.Response, string, error) {
	sessionID, err := h.DS.CreateSession(ctx, a, 3)
	if err != nil {
		return "", nil, "session", err
	}
	pow, err := h.DS.GetPow(ctx, a, 3)
	if err != nil {
		return sessionID, nil, "pow", err
	}
	resp, err := h.DS.CallCompletion(ctx, a, stdReq.CompletionPayload(sessionID), pow, 3)
	if err != nil {
		return sessionID, nil, "completion", err
	}
	return sessionID, resp, "", nil
}

func (h *Handler) ensureHistoryAttachmentForCurrentAccount(ctx context.Context, a *auth.RequestAuth, stdReq util.StandardRequest) (util.StandardRequest, error) {
	if strings.TrimSpace(stdReq.HistoryText) == "" {
		return stdReq, nil
	}
	currentAccount := accountIDForLog(a)
	if strings.TrimSpace(stdReq.HistoryRefFileID) != "" && strings.EqualFold(strings.TrimSpace(stdReq.HistoryRefAccountID), currentAccount) {
		return stdReq, nil
	}
	if h == nil || h.DS == nil {
		return stdReq, errors.New("history attachment upload requires DeepSeek client")
	}
	result, err := h.DS.UploadFile(ctx, a, deepseek.UploadFileRequest{
		Filename:    historySplitFilename,
		ContentType: historySplitContentType,
		Purpose:     historySplitPurpose,
		Data:        []byte(stdReq.HistoryText),
	}, 3)
	if err != nil {
		return stdReq, fmt.Errorf("upload history file for current account: %w", err)
	}
	fileID := strings.TrimSpace(result.ID)
	if fileID == "" {
		return stdReq, errors.New("upload history file for current account returned empty file id")
	}
	stdReq.RefFileIDs = replaceHistoryRefFileID(stdReq.RefFileIDs, stdReq.HistoryRefFileID, fileID)
	stdReq.HistoryRefFileID = fileID
	stdReq.HistoryRefAccountID = currentAccount
	return stdReq, nil
}

func replaceHistoryRefFileID(existing []string, oldID, newID string) []string {
	newID = strings.TrimSpace(newID)
	if newID == "" {
		return existing
	}
	oldID = strings.TrimSpace(oldID)
	out := make([]string, 0, len(existing)+1)
	seen := map[string]struct{}{strings.ToLower(newID): {}}
	out = append(out, newID)
	for _, id := range existing {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || strings.EqualFold(trimmed, newID) {
			continue
		}
		if oldID != "" && strings.EqualFold(trimmed, oldID) {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func shouldFailoverCompletionStatus(status int) bool {
	if status == http.StatusTooManyRequests || status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusRequestTimeout {
		return true
	}
	return status >= 500 && status <= 599
}

func (h *Handler) switchManagedAccount(ctx context.Context, a *auth.RequestAuth) bool {
	switcher, ok := h.Auth.(accountSwitcher)
	if !ok {
		return false
	}
	if !switcher.SwitchAccount(ctx, a) {
		return false
	}
	config.Logger.Info("[openai_completion] switched managed account", "account", accountIDForLog(a))
	return true
}

func (h *Handler) markManagedAccountFailure(a *auth.RequestAuth) {
	reporter, ok := h.Auth.(accountHealthReporter)
	if !ok {
		return
	}
	reporter.MarkAccountFailure(a)
}

func (h *Handler) markManagedAccountSuccess(a *auth.RequestAuth) {
	reporter, ok := h.Auth.(accountHealthReporter)
	if !ok {
		return
	}
	reporter.MarkAccountSuccess(a)
}

func accountIDForLog(a *auth.RequestAuth) string {
	if a == nil {
		return ""
	}
	if a.AccountID != "" {
		return a.AccountID
	}
	return a.CallerID
}

func (h *Handler) writeChatCompletionAttemptError(w http.ResponseWriter, a *auth.RequestAuth, stage string, historySession *chatHistorySession) {
	status, message, code := completionAttemptErrorDetail(a, stage)
	if historySession != nil {
		historySession.error(status, message, code, "", "")
	}
	writeOpenAIErrorWithCode(w, status, message, code)
}

func (h *Handler) writeCompletionAttemptError(w http.ResponseWriter, a *auth.RequestAuth, stage string) {
	status, message, code := completionAttemptErrorDetail(a, stage)
	writeOpenAIErrorWithCode(w, status, message, code)
}

func completionAttemptErrorDetail(a *auth.RequestAuth, stage string) (int, string, string) {
	switch stage {
	case "session":
		if a != nil && !a.UseConfigToken {
			return http.StatusUnauthorized, "Invalid token. If this should be a DS2API key, add it to config.keys first.", "authentication_failed"
		}
		return http.StatusBadGateway, "Failed to create DeepSeek session after managed-account failover.", "upstream_session_failed"
	case "pow":
		return http.StatusBadGateway, "Failed to get DeepSeek PoW after managed-account failover.", "upstream_pow_failed"
	case "history_upload":
		return http.StatusBadGateway, "Failed to upload HISTORY.txt after managed-account failover.", "upstream_history_upload_failed"
	case "completion_status":
		return http.StatusBadGateway, "DeepSeek completion returned a retriable error after managed-account failover.", "upstream_completion_status_failed"
	default:
		return http.StatusBadGateway, "Failed to get completion after managed-account failover.", "upstream_completion_failed"
	}
}
