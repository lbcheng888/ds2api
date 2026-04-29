//go:build legacy_openai_adapter

package openai

import (
	"net/http"
	"strconv"

	"ds2api/internal/promptcache"
	"ds2api/internal/util"
)

func setPromptCacheHeaders(w http.ResponseWriter, finalPrompt string) {
	if w == nil {
		return
	}
	key := promptcache.Fingerprint(finalPrompt)
	if key == "" {
		return
	}
	prefixTokens := util.EstimateTokens(promptcache.StablePrefix(finalPrompt))
	w.Header().Set("X-Ds2-Prompt-Cache-Key", key)
	w.Header().Set("X-Ds2-Prompt-Cache-Prefix-Tokens", strconv.Itoa(prefixTokens))
	w.Header().Set("X-Ds2-Prompt-Cache-Policy", "stable-prefix-before-first-user")
}
