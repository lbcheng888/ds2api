package auth

import (
	"os"
	"strconv"
	"sync"
)

const defaultTokenThreshold int64 = 950_000

type TokenTracker struct {
	mu        sync.Mutex
	threshold int64
	usage     map[string]int64
}

func NewTokenTracker(threshold int64) *TokenTracker {
	if threshold <= 0 {
		threshold = defaultTokenThreshold
	}
	return &TokenTracker{
		threshold: threshold,
		usage:     make(map[string]int64),
	}
}

func NewTokenTrackerFromEnv() *TokenTracker {
	threshold := defaultTokenThreshold
	if raw := os.Getenv("DS2API_ACCOUNT_TOKEN_THRESHOLD"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
			threshold = n
		}
	}
	return NewTokenTracker(threshold)
}

func (t *TokenTracker) RecordUsage(accountID string, totalTokens int64) {
	if t == nil || accountID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.usage[accountID] = totalTokens
}

func (t *TokenTracker) IsNearLimit(accountID string) bool {
	if t == nil || accountID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage[accountID] >= t.threshold
}
