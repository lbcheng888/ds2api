package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"ds2api/internal/account"
	"ds2api/internal/config"
)

type ctxKey string

const authCtxKey ctxKey = "auth_context"

var (
	ErrUnauthorized = errors.New("unauthorized: missing auth token")
	ErrNoAccount    = errors.New("no accounts configured or all accounts are busy")
)

type RequestAuth struct {
	UseConfigToken bool
	DeepSeekToken  string
	CallerID       string
	AccountID      string
	Account        config.Account
	TriedAccounts  map[string]bool
	AffinityKey    string
	resolver       *Resolver
}

type LoginFunc func(ctx context.Context, acc config.Account) (string, error)

type AccountHealth struct {
	AccountID                string `json:"account_id"`
	Status                   string `json:"status"`
	QualityScore             int64  `json:"quality_score"`
	CooldownUntilUnix        int64  `json:"cooldown_until_unix,omitempty"`
	CooldownRemainingSeconds int    `json:"cooldown_remaining_seconds,omitempty"`
	SuccessCount             int64  `json:"success_count,omitempty"`
	FailureCount             int64  `json:"failure_count,omitempty"`
	ConsecutiveFailures      int64  `json:"consecutive_failures,omitempty"`
	LastFailureReason        string `json:"last_failure_reason,omitempty"`
	LastSuccessUnix          int64  `json:"last_success_unix,omitempty"`
	LastFailureUnix          int64  `json:"last_failure_unix,omitempty"`
}

type accountHealthStats struct {
	successCount        int64
	failureCount        int64
	consecutiveFailures int64
	lastSuccess         time.Time
	lastFailure         time.Time
	lastFailureReason   string
}

type rankedAccount struct {
	id    string
	index int
	score int64
}

type accountAffinity struct {
	AccountID string
	Expires   time.Time
}

type Resolver struct {
	Store *config.Store
	Pool  *account.Pool
	Login LoginFunc

	TokenTracker *TokenTracker
	mu                sync.Mutex
	tokenRefreshedAt  map[string]time.Time
	accountCooldowns  map[string]time.Time
	accountStats      map[string]accountHealthStats
	accountAffinities map[string]accountAffinity
}

func NewResolver(store *config.Store, pool *account.Pool, login LoginFunc) *Resolver {
	return &Resolver{
		Store:             store,
		Pool:              pool,
		Login:             login,
		TokenTracker:      NewTokenTrackerFromEnv(),
		tokenRefreshedAt:  map[string]time.Time{},
		accountCooldowns:  map[string]time.Time{},
		accountStats:      map[string]accountHealthStats{},
		accountAffinities: map[string]accountAffinity{},
	}
}

func (r *Resolver) Determine(req *http.Request) (*RequestAuth, error) {
	return r.DetermineWithAffinity(req, "")
}

func (r *Resolver) DetermineWithAffinity(req *http.Request, affinityKey string) (*RequestAuth, error) {
	callerKey := r.selectCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	callerID := callerTokenID(callerKey)
	ctx := req.Context()
	if !r.Store.HasAPIKey(callerKey) {
		return &RequestAuth{
			UseConfigToken: false,
			DeepSeekToken:  callerKey,
			CallerID:       callerID,
			resolver:       r,
			TriedAccounts:  map[string]bool{},
		}, nil
	}
	target := strings.TrimSpace(req.Header.Get("X-Ds2-Target-Account"))
	a, err := r.acquireManagedRequestAuth(ctx, callerID, target, affinityKey)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *Resolver) acquireManagedRequestAuth(ctx context.Context, callerID, target, affinityKey string) (*RequestAuth, error) {
	tried := map[string]bool{}
	var lastEnsureErr error
	affinityKey = normalizeAffinityKey(callerID, affinityKey)
	for {
		r.applyAccountCooldowns(tried)
		if target == "" && len(tried) >= len(r.Store.Accounts()) {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}
		acquireTarget := target
		if acquireTarget == "" {
			acquireTarget = r.affinityAccountTarget(affinityKey, tried)
		}
		acc, ok := r.Pool.AcquireWaitPreferred(ctx, acquireTarget, tried, r.preferredAccountOrder())
		if !ok {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}

		a := &RequestAuth{
			UseConfigToken: true,
			CallerID:       callerID,
			AccountID:      acc.Identifier(),
			Account:        acc,
			TriedAccounts:  tried,
			AffinityKey:    affinityKey,
			resolver:       r,
		}

		if err := r.ensureManagedToken(ctx, a); err != nil {
			lastEnsureErr = err
			tried[a.AccountID] = true
			r.markAccountFailure(a, "login")
			r.Pool.Release(a.AccountID)
			if target != "" {
				return nil, err
			}
			continue
		}
		r.rememberAffinity(affinityKey, a.AccountID)
		return a, nil
	}
}

// DetermineCaller resolves caller identity without acquiring any pooled account.
// Use this for local-cache lookup routes that only need tenant isolation.
func (r *Resolver) DetermineCaller(req *http.Request) (*RequestAuth, error) {
	callerKey := r.selectCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	callerID := callerTokenID(callerKey)
	a := &RequestAuth{
		UseConfigToken: false,
		CallerID:       callerID,
		resolver:       r,
		TriedAccounts:  map[string]bool{},
	}
	if r == nil || r.Store == nil || !r.Store.HasAPIKey(callerKey) {
		a.DeepSeekToken = callerKey
	}
	return a, nil
}

func WithAuth(ctx context.Context, a *RequestAuth) context.Context {
	return context.WithValue(ctx, authCtxKey, a)
}

func FromContext(ctx context.Context) (*RequestAuth, bool) {
	v := ctx.Value(authCtxKey)
	a, ok := v.(*RequestAuth)
	return a, ok
}

func (r *Resolver) loginAndPersist(ctx context.Context, a *RequestAuth) error {
	token, err := r.Login(ctx, a.Account)
	if err != nil {
		return err
	}
	a.Account.Token = token
	a.DeepSeekToken = token
	r.markTokenRefreshedNow(a.AccountID)
	return r.Store.UpdateAccountToken(a.AccountID, token)
}

func (r *Resolver) RefreshToken(ctx context.Context, a *RequestAuth) bool {
	if !a.UseConfigToken || a.AccountID == "" {
		return false
	}
	_ = r.Store.UpdateAccountToken(a.AccountID, "")
	a.Account.Token = ""
	if err := r.loginAndPersist(ctx, a); err != nil {
		config.Logger.Error("[refresh_token] failed", "account", a.AccountID, "error", err)
		return false
	}
	return true
}

func (r *Resolver) MarkTokenInvalid(a *RequestAuth) {
	if !a.UseConfigToken || a.AccountID == "" {
		return
	}
	a.Account.Token = ""
	a.DeepSeekToken = ""
	r.clearTokenRefreshMark(a.AccountID)
	_ = r.Store.UpdateAccountToken(a.AccountID, "")
}

func (r *Resolver) SwitchAccount(ctx context.Context, a *RequestAuth) bool {
	if !a.UseConfigToken {
		return false
	}
	if a.TriedAccounts == nil {
		a.TriedAccounts = map[string]bool{}
	}
	if a.AccountID != "" {
		a.TriedAccounts[a.AccountID] = true
		r.clearAffinityForAccount(a.AffinityKey, a.AccountID)
		r.Pool.Release(a.AccountID)
	}
	for {
		r.applyAccountCooldowns(a.TriedAccounts)
		acc, ok := r.Pool.AcquirePreferred("", a.TriedAccounts, r.preferredAccountOrder())
		if !ok {
			return false
		}
		a.Account = acc
		a.AccountID = acc.Identifier()
		if err := r.ensureManagedToken(ctx, a); err != nil {
			a.TriedAccounts[a.AccountID] = true
			r.markAccountFailure(a, "login")
			r.Pool.Release(a.AccountID)
			continue
		}
		r.rememberAffinity(a.AffinityKey, a.AccountID)
		return true
	}
}

func (r *Resolver) Release(a *RequestAuth) {
	if a == nil || !a.UseConfigToken || a.AccountID == "" {
		return
	}
	r.Pool.Release(a.AccountID)
}

func (r *Resolver) MarkAccountFailure(a *RequestAuth) {
	r.markAccountFailure(a, "request")
}

func (r *Resolver) markAccountFailure(a *RequestAuth, reason string) {
	if r == nil || a == nil || !a.UseConfigToken {
		return
	}
	accountID := strings.TrimSpace(a.AccountID)
	if accountID == "" {
		return
	}
	now := time.Now()
	cooldown := time.Duration(r.accountFailureCooldownSeconds()) * time.Second
	until := time.Time{}
	if cooldown > 0 {
		until = now.Add(cooldown)
	}
	r.mu.Lock()
	if cooldown > 0 && r.accountCooldowns == nil {
		r.accountCooldowns = map[string]time.Time{}
	}
	if r.accountStats == nil {
		r.accountStats = map[string]accountHealthStats{}
	}
	if cooldown > 0 {
		r.accountCooldowns[accountID] = until
	}
	r.clearAffinityForAccountLocked(a.AffinityKey, accountID)
	stats := r.accountStats[accountID]
	stats.failureCount++
	stats.consecutiveFailures++
	stats.lastFailure = now
	stats.lastFailureReason = strings.TrimSpace(reason)
	r.accountStats[accountID] = stats
	r.mu.Unlock()
	if cooldown > 0 {
		config.Logger.Warn("[account_health] account cooldown started", "account", accountID, "cooldown_seconds", int(cooldown.Seconds()))
	}
}

func (r *Resolver) MarkAccountSuccess(a *RequestAuth) {
	if r == nil || a == nil || !a.UseConfigToken {
		return
	}
	accountID := strings.TrimSpace(a.AccountID)
	if accountID == "" {
		return
	}
	r.mu.Lock()
	delete(r.accountCooldowns, accountID)
	if r.accountStats == nil {
		r.accountStats = map[string]accountHealthStats{}
	}
	stats := r.accountStats[accountID]
	stats.successCount++
	stats.consecutiveFailures = 0
	stats.lastFailureReason = ""
	stats.lastSuccess = time.Now()
	r.accountStats[accountID] = stats
	r.rememberAffinityLocked(a.AffinityKey, accountID, time.Now())
	r.mu.Unlock()
}

func (r *Resolver) AccountHealthStatus() []AccountHealth {
	if r == nil {
		return nil
	}
	now := time.Now()
	out := []AccountHealth{}
	configuredAccountIDs := []string{}
	if r.Store != nil {
		for _, acc := range r.Store.Accounts() {
			if id := strings.TrimSpace(acc.Identifier()); id != "" {
				configuredAccountIDs = append(configuredAccountIDs, id)
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	accountIDs := map[string]struct{}{}
	for _, id := range configuredAccountIDs {
		accountIDs[id] = struct{}{}
	}
	for accountID := range r.accountStats {
		accountIDs[accountID] = struct{}{}
	}
	for accountID := range r.accountCooldowns {
		accountIDs[accountID] = struct{}{}
	}
	for accountID := range accountIDs {
		until := r.accountCooldowns[accountID]
		stats := r.accountStats[accountID]
		status := "healthy"
		health := AccountHealth{
			AccountID:           accountID,
			Status:              status,
			QualityScore:        accountQualityScore(stats),
			SuccessCount:        stats.successCount,
			FailureCount:        stats.failureCount,
			ConsecutiveFailures: stats.consecutiveFailures,
			LastFailureReason:   stats.lastFailureReason,
			LastSuccessUnix:     unixIfSet(stats.lastSuccess),
			LastFailureUnix:     unixIfSet(stats.lastFailure),
		}
		if !now.Before(until) {
			delete(r.accountCooldowns, accountID)
			out = append(out, health)
			continue
		}
		remaining := int(time.Until(until).Seconds())
		if remaining < 1 {
			remaining = 1
		}
		health.Status = "cooldown"
		health.CooldownUntilUnix = until.Unix()
		health.CooldownRemainingSeconds = remaining
		out = append(out, health)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AccountID < out[j].AccountID
	})
	return out
}

func (r *Resolver) preferredAccountOrder() []string {
	if r == nil || r.Store == nil {
		return nil
	}
	accounts := r.Store.Accounts()
	if len(accounts) == 0 {
		return nil
	}
	ranked := make([]rankedAccount, 0, len(accounts))
	r.mu.Lock()
	for i, acc := range accounts {
		id := strings.TrimSpace(acc.Identifier())
		if id == "" {
			continue
		}
		ranked = append(ranked, rankedAccount{
			id:    id,
			index: i,
			score: accountQualityScore(r.accountStats[id]),
		})
	}
	r.mu.Unlock()
	if len(ranked) == 0 || allAccountScoresEqual(ranked) {
		return nil
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].index < ranked[j].index
		}
		return ranked[i].score > ranked[j].score
	})
	out := make([]string, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.id)
	}
	return out
}

func normalizeAffinityKey(callerID, raw string) string {
	raw = strings.TrimSpace(raw)
	if callerID == "" || raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return callerID + "\x00" + hex.EncodeToString(sum[:8])
}

func (r *Resolver) affinityAccountTarget(key string, tried map[string]bool) string {
	if r == nil || strings.TrimSpace(key) == "" {
		return ""
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	aff, ok := r.accountAffinities[key]
	if !ok {
		return ""
	}
	if now.After(aff.Expires) {
		delete(r.accountAffinities, key)
		return ""
	}
	accountID := strings.TrimSpace(aff.AccountID)
	if accountID == "" || tried[accountID] {
		return ""
	}
	if until := r.accountCooldowns[accountID]; now.Before(until) {
		delete(r.accountAffinities, key)
		return ""
	}
	return accountID
}

func (r *Resolver) rememberAffinity(key, accountID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rememberAffinityLocked(key, accountID, time.Now())
}

func (r *Resolver) rememberAffinityLocked(key, accountID string, now time.Time) {
	key = strings.TrimSpace(key)
	accountID = strings.TrimSpace(accountID)
	if key == "" || accountID == "" {
		return
	}
	if r.accountAffinities == nil {
		r.accountAffinities = map[string]accountAffinity{}
	}
	r.accountAffinities[key] = accountAffinity{
		AccountID: accountID,
		Expires:   now.Add(time.Duration(r.accountAffinityTTLSeconds()) * time.Second),
	}
}

func (r *Resolver) clearAffinityForAccount(key, accountID string) {
	if r == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(accountID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearAffinityForAccountLocked(key, accountID)
}

func (r *Resolver) clearAffinityForAccountLocked(key, accountID string) {
	aff, ok := r.accountAffinities[strings.TrimSpace(key)]
	if !ok {
		return
	}
	if strings.EqualFold(strings.TrimSpace(aff.AccountID), strings.TrimSpace(accountID)) {
		delete(r.accountAffinities, strings.TrimSpace(key))
	}
}

func allAccountScoresEqual(accounts []rankedAccount) bool {
	if len(accounts) < 2 {
		return true
	}
	first := accounts[0].score
	for _, item := range accounts[1:] {
		if item.score != first {
			return false
		}
	}
	return true
}

func accountQualityScore(stats accountHealthStats) int64 {
	return stats.successCount*4 - stats.failureCount*3 - stats.consecutiveFailures*10
}

func unixIfSet(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func extractCallerToken(req *http.Request) string {
	authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token := strings.TrimSpace(authHeader[7:])
		if token != "" {
			return token
		}
	}
	if key := strings.TrimSpace(req.Header.Get("x-api-key")); key != "" {
		return key
	}
	// Gemini/Google clients commonly send API key via x-goog-api-key.
	if key := strings.TrimSpace(req.Header.Get("x-goog-api-key")); key != "" {
		return key
	}
	// Gemini AI Studio compatibility: allow query key fallback only when no
	// header-based credential is present.
	if key := strings.TrimSpace(req.URL.Query().Get("key")); key != "" {
		return key
	}
	return strings.TrimSpace(req.URL.Query().Get("api_key"))
}

func (r *Resolver) selectCallerToken(req *http.Request) string {
	if r != nil && r.Store != nil {
		for _, token := range callerTokenCandidates(req) {
			if token != "" && r.Store.HasAPIKey(token) {
				return token
			}
		}
	}
	return extractCallerToken(req)
}

func callerTokenCandidates(req *http.Request) []string {
	if req == nil {
		return nil
	}
	out := make([]string, 0, 5)
	authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		out = append(out, strings.TrimSpace(authHeader[7:]))
	}
	out = append(out,
		strings.TrimSpace(req.Header.Get("x-api-key")),
		strings.TrimSpace(req.Header.Get("x-goog-api-key")),
		strings.TrimSpace(req.URL.Query().Get("key")),
		strings.TrimSpace(req.URL.Query().Get("api_key")),
	)
	return out
}

func callerTokenID(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "caller:" + hex.EncodeToString(sum[:8])
}

func (r *Resolver) ensureManagedToken(ctx context.Context, a *RequestAuth) error {
	if strings.TrimSpace(a.Account.Token) == "" {
		return r.loginAndPersist(ctx, a)
	}
	if r.shouldForceRefresh(a.AccountID) {
		if err := r.loginAndPersist(ctx, a); err != nil {
			return err
		}
		return nil
	}
	a.DeepSeekToken = a.Account.Token
	return nil
}

func (r *Resolver) shouldForceRefresh(accountID string) bool {
	if r == nil || r.Store == nil {
		return false
	}
	if strings.TrimSpace(accountID) == "" {
		return false
	}
	intervalHours := r.Store.RuntimeTokenRefreshIntervalHours()
	if intervalHours <= 0 {
		return false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	last, ok := r.tokenRefreshedAt[accountID]
	if !ok || last.IsZero() {
		r.tokenRefreshedAt[accountID] = now
		return false
	}
	return now.Sub(last) >= time.Duration(intervalHours)*time.Hour
}

func (r *Resolver) markTokenRefreshedNow(accountID string) {
	if strings.TrimSpace(accountID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokenRefreshedAt[accountID] = time.Now()
}

func (r *Resolver) clearTokenRefreshMark(accountID string) {
	if strings.TrimSpace(accountID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tokenRefreshedAt, accountID)
}

func (r *Resolver) applyAccountCooldowns(exclude map[string]bool) {
	if r == nil || exclude == nil {
		return
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for accountID, until := range r.accountCooldowns {
		if now.Before(until) {
			exclude[accountID] = true
			continue
		}
		delete(r.accountCooldowns, accountID)
	}
}

func (r *Resolver) accountFailureCooldownSeconds() int {
	if r == nil || r.Store == nil {
		return 120
	}
	return r.Store.RuntimeAccountFailureCooldownSeconds()
}

func (r *Resolver) accountAffinityTTLSeconds() int {
	if r == nil || r.Store == nil {
		return 3600
	}
	return r.Store.RuntimeAccountAffinityTTLSeconds()
}
