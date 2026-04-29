package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

const MaxManagedCompletionAccountAttempts = 3

type CompletionAttemptResult struct {
	SessionID string
	Pow       string
	Payload   map[string]any
	Response  *http.Response
	Stage     string
	Attempts  int
}

type AccountSwitcher interface {
	SwitchAccount(ctx context.Context, a *auth.RequestAuth) bool
}

type AccountHealthReporter interface {
	MarkAccountFailure(a *auth.RequestAuth)
	MarkAccountSuccess(a *auth.RequestAuth)
}

func CallCompletionWithManagedFailover(ctx context.Context, resolver AuthResolver, ds DeepSeekCaller, store ConfigReader, a *auth.RequestAuth, req promptcompat.StandardRequest) (CompletionAttemptResult, error) {
	if ds == nil {
		return CompletionAttemptResult{Stage: "completion"}, fmt.Errorf("deepseek caller is nil")
	}
	attemptLimit := 1
	if canManagedFailover(resolver, a, req) {
		attemptLimit = MaxManagedCompletionAccountAttempts
	}

	var last CompletionAttemptResult
	var lastErr error
	for attempt := 1; attempt <= attemptLimit; attempt++ {
		last.Attempts = attempt
		sessionID, err := ds.CreateSession(ctx, a, 3)
		if err != nil {
			last.Stage = "session"
			lastErr = err
			markManagedAccountFailure(resolver, a)
			if !switchAfterAttempt(ctx, resolver, a, req, attempt, attemptLimit, last.Stage, err) {
				break
			}
			continue
		}
		last.SessionID = sessionID

		pow, err := ds.GetPow(ctx, a, 3)
		if err != nil {
			last.Stage = "pow"
			lastErr = err
			deleteFailedAttemptSession(ctx, ds, store, a, sessionID)
			markManagedAccountFailure(resolver, a)
			if !switchAfterAttempt(ctx, resolver, a, req, attempt, attemptLimit, last.Stage, err) {
				break
			}
			continue
		}
		last.Pow = pow

		payload := req.CompletionPayload(sessionID)
		resp, err := ds.CallCompletion(ctx, a, payload, pow, 3)
		if err != nil {
			last.Stage = "completion"
			lastErr = err
			deleteFailedAttemptSession(ctx, ds, store, a, sessionID)
			markManagedAccountFailure(resolver, a)
			if !switchAfterAttempt(ctx, resolver, a, req, attempt, attemptLimit, last.Stage, err) {
				break
			}
			continue
		}
		if resp != nil && shouldFailoverCompletionStatus(resp.StatusCode) && canManagedFailover(resolver, a, req) {
			last.Stage = "completion_status"
			lastErr = fmt.Errorf("completion returned status %d", resp.StatusCode)
			_ = resp.Body.Close()
			deleteFailedAttemptSession(ctx, ds, store, a, sessionID)
			markManagedAccountFailure(resolver, a)
			if !switchAfterAttempt(ctx, resolver, a, req, attempt, attemptLimit, last.Stage, lastErr) {
				break
			}
			continue
		}
		markManagedAccountSuccess(resolver, a)
		recordTokenUsage(resolver, a, resp)
		return CompletionAttemptResult{
			SessionID: sessionID,
			Pow:       pow,
			Payload:   payload,
			Response:  resp,
			Attempts:  attempt,
		}, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("completion failed")
	}
	return last, lastErr
}

func canManagedFailover(resolver AuthResolver, a *auth.RequestAuth, req promptcompat.StandardRequest) bool {
	if resolver == nil || a == nil || !a.UseConfigToken {
		return false
	}
	if _, ok := resolver.(AccountSwitcher); !ok {
		return false
	}
	// ref_file_ids are account-scoped DeepSeek resources. Until we can replay
	// their source bytes for each new account, cross-account failover would
	// risk sending an invalid or wrong-context file reference.
	return len(req.RefFileIDs) == 0
}

func switchAfterAttempt(ctx context.Context, resolver AuthResolver, a *auth.RequestAuth, req promptcompat.StandardRequest, attempt, attemptLimit int, stage string, err error) bool {
	if attempt >= attemptLimit || !canManagedFailover(resolver, a, req) {
		return false
	}
	switcher, _ := resolver.(AccountSwitcher)
	if !switcher.SwitchAccount(ctx, a) {
		return false
	}
	config.Logger.Warn("[openai_completion] switched managed account after failed attempt", "attempt", attempt, "stage", stage, "account", accountIDForLog(a), "error", err)
	return true
}

func markManagedAccountFailure(resolver AuthResolver, a *auth.RequestAuth) {
	reporter, ok := resolver.(AccountHealthReporter)
	if !ok {
		return
	}
	reporter.MarkAccountFailure(a)
}

func markManagedAccountSuccess(resolver AuthResolver, a *auth.RequestAuth) {
	reporter, ok := resolver.(AccountHealthReporter)
	if !ok {
		return
	}
	reporter.MarkAccountSuccess(a)
}

func deleteFailedAttemptSession(ctx context.Context, ds DeepSeekCaller, store ConfigReader, a *auth.RequestAuth, sessionID string) {
	if ds == nil || store == nil || a == nil || strings.TrimSpace(a.DeepSeekToken) == "" || strings.TrimSpace(sessionID) == "" {
		return
	}
	if store.AutoDeleteMode() != "single" {
		return
	}
	if _, err := ds.DeleteSessionForToken(context.WithoutCancel(ctx), a.DeepSeekToken, sessionID); err != nil {
		config.Logger.Warn("[openai_completion] failed to delete failed attempt session", "account", accountIDForLog(a), "session_id", sessionID, "error", err)
	}
}

func shouldFailoverCompletionStatus(status int) bool {
	if status == http.StatusTooManyRequests || status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusRequestTimeout {
		return true
	}
	return status >= 500 && status <= 599
}

func accountIDForLog(a *auth.RequestAuth) string {
	if a == nil {
		return ""
	}
	if strings.TrimSpace(a.AccountID) != "" {
		return strings.TrimSpace(a.AccountID)
	}
	return strings.TrimSpace(a.CallerID)
}

func CompletionAttemptErrorDetail(a *auth.RequestAuth, stage string) (int, string, string) {
	switch stage {
	case "session":
		if a != nil && !a.UseConfigToken {
			return http.StatusUnauthorized, "Invalid token. If this should be a DS2API key, add it to config.keys first.", "authentication_failed"
		}
		return http.StatusBadGateway, "Failed to create DeepSeek session after managed-account failover.", "upstream_session_failed"
	case "pow":
		return http.StatusBadGateway, "Failed to get DeepSeek PoW after managed-account failover.", "upstream_pow_failed"
	case "completion_status":
		return http.StatusBadGateway, "DeepSeek completion returned a retriable error after managed-account failover.", "upstream_completion_status_failed"
	default:
		return http.StatusBadGateway, "Failed to get completion after managed-account failover.", "upstream_completion_failed"
	}
}

func recordTokenUsage(resolver AuthResolver, a *auth.RequestAuth, resp *http.Response) {
	if resolver == nil || a == nil || resp == nil {
		return
	}
	tracker, ok := resolver.(*auth.Resolver)
	if !ok || tracker == nil || tracker.TokenTracker == nil {
		return
	}
	if a.AccountID == "" {
		return
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage == nil {
		return
	}
	totalTokens, ok := usage["total_tokens"].(float64)
	if !ok || totalTokens <= 0 {
		return
	}
	tracker.TokenTracker.RecordUsage(a.AccountID, int64(totalTokens))
	if tracker.TokenTracker.IsNearLimit(a.AccountID) {
		config.Logger.Info("[token_tracker] account nearing token limit, pre-switching", "account", accountIDForLog(a), "total_tokens", int64(totalTokens))
	}
	// Replace the body so downstream consumers can still read it.
	resp.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
}
