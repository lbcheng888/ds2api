package promptcompat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

const defaultPromptCompatCacheTTL = 5 * time.Minute

type cacheEntry struct {
	Response  map[string]any
	ExpiresAt time.Time
}

type promptCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]*cacheEntry
}

var globalCache = newPromptCache(defaultPromptCompatCacheTTL)

func newPromptCache(ttl time.Duration) *promptCache {
	if ttl <= 0 {
		ttl = defaultPromptCompatCacheTTL
	}
	return &promptCache{
		ttl:   ttl,
		items: make(map[string]*cacheEntry),
	}
}

func cacheKey(model string, messages []any, tools any) string {
	h := sha256.New()
	h.Write([]byte(model))
	h.Write([]byte{0})
	msgJSON, _ := json.Marshal(messages)
	h.Write(msgJSON)
	h.Write([]byte{0})
	if tools != nil {
		toolJSON, _ := json.Marshal(tools)
		h.Write(toolJSON)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *promptCache) get(key string) (map[string]any, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.items[key]
	if !ok || time.Now().After(entry.ExpiresAt) {
		return nil, false
	}
	return cloneResponse(entry.Response), true
}

func (c *promptCache) setByKey(key string, response map[string]any) {
	if c == nil || key == "" || response == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = &cacheEntry{
		Response:  cloneResponse(response),
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

func (c *promptCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, entry := range c.items {
		if now.After(entry.ExpiresAt) {
			delete(c.items, key)
		}
	}
}

func cloneResponse(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	if raw, err := json.Marshal(src); err == nil {
		var dst map[string]any
		if err := json.Unmarshal(raw, &dst); err == nil {
			return dst
		}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func GetCachedResponse(model string, messages []any, tools any, thinking bool) (map[string]any, bool) {
	if thinking {
		return nil, false
	}
	key := cacheKey(model, messages, tools)
	return globalCache.get(key)
}

func SaveCachedResponse(model string, messages []any, tools any, response map[string]any) {
	globalCache.setForRequest(model, messages, tools, response)
}

func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			globalCache.cleanup()
		}
	}()
}

func (c *promptCache) setForRequest(model string, messages []any, tools any, response map[string]any) {
	key := cacheKey(model, messages, tools)
	c.setByKey(key, response)
}
