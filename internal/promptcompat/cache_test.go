package promptcompat

import (
	"testing"
	"time"
)

func TestPromptCacheRoundTripClonesResponses(t *testing.T) {
	cache := newPromptCache(time.Minute)
	messages := []any{map[string]any{"role": "user", "content": "hello"}}
	tools := []any{map[string]any{"type": "function", "name": "Read"}}

	cache.setForRequest("deepseek-v4-pro", messages, tools, map[string]any{
		"output": []any{"first"},
	})

	first, ok := cache.get(cacheKey("deepseek-v4-pro", messages, tools))
	if !ok {
		t.Fatalf("expected cache hit")
	}
	first["output"] = []any{"mutated"}

	second, ok := cache.get(cacheKey("deepseek-v4-pro", messages, tools))
	if !ok {
		t.Fatalf("expected second cache hit")
	}
	output, _ := second["output"].([]any)
	if len(output) != 1 || output[0] != "first" {
		t.Fatalf("expected cached response clone to remain unchanged, got %#v", second)
	}
}

func TestGetCachedResponseSkipsThinkingRequests(t *testing.T) {
	old := globalCache
	t.Cleanup(func() { globalCache = old })
	globalCache = newPromptCache(time.Minute)

	messages := []any{map[string]any{"role": "user", "content": "hello"}}
	SaveCachedResponse("deepseek-v4-pro", messages, nil, map[string]any{"id": "cached"})

	if _, ok := GetCachedResponse("deepseek-v4-pro", messages, nil, true); ok {
		t.Fatalf("expected thinking requests to bypass response cache")
	}
	if got, ok := GetCachedResponse("deepseek-v4-pro", messages, nil, false); !ok || got["id"] != "cached" {
		t.Fatalf("expected non-thinking request cache hit, got %#v ok=%v", got, ok)
	}
}
