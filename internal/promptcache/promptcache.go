package promptcache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"ds2api/internal/util"
)

const (
	userMarker      = "<｜User｜>"
	minPrefixTokens = 32
	maxEntries      = 256
)

type Stats struct {
	Key        string
	HitTokens  int
	MissTokens int
}

type Ledger struct {
	mu    sync.Mutex
	seen  map[string]int
	order []string
}

var DefaultLedger = NewLedger()

func NewLedger() *Ledger {
	return &Ledger{seen: map[string]int{}}
}

func Fingerprint(prompt string) string {
	prefix := StablePrefix(prompt)
	if strings.TrimSpace(prefix) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(prefix))
	return "pc_" + hex.EncodeToString(sum[:8])
}

func StablePrefix(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if idx := strings.LastIndex(prompt, userMarker); idx > 0 {
		return strings.TrimSpace(prompt[:idx])
	}
	return prompt
}

func (l *Ledger) Observe(prompt string) Stats {
	totalTokens := util.EstimateTokens(prompt)
	prefix := StablePrefix(prompt)
	prefixTokens := util.EstimateTokens(prefix)
	if prefixTokens < minPrefixTokens {
		return Stats{MissTokens: totalTokens}
	}
	key := Fingerprint(prompt)
	if key == "" {
		return Stats{MissTokens: totalTokens}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if cachedTokens := l.seen[key]; cachedTokens > 0 {
		if cachedTokens > totalTokens {
			cachedTokens = totalTokens
		}
		return Stats{Key: key, HitTokens: cachedTokens, MissTokens: totalTokens - cachedTokens}
	}
	l.rememberLocked(key, prefixTokens)
	return Stats{Key: key, MissTokens: totalTokens}
}

func (l *Ledger) rememberLocked(key string, tokens int) {
	if l.seen == nil {
		l.seen = map[string]int{}
	}
	if _, exists := l.seen[key]; !exists {
		l.order = append(l.order, key)
	}
	l.seen[key] = tokens
	for len(l.order) > maxEntries {
		oldest := l.order[0]
		l.order = l.order[1:]
		delete(l.seen, oldest)
	}
}
