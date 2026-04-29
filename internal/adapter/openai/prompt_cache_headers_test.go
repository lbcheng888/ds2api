//go:build legacy_openai_adapter

package openai

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetPromptCacheHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	prompt := "<｜begin▁of▁sentence｜><｜System｜>" + strings.Repeat("stable ", 80) + "<｜end▁of▁instructions｜><｜User｜>hello"

	setPromptCacheHeaders(rec, prompt)

	if rec.Header().Get("X-Ds2-Prompt-Cache-Key") == "" {
		t.Fatalf("expected prompt cache key header")
	}
	if rec.Header().Get("X-Ds2-Prompt-Cache-Prefix-Tokens") == "" {
		t.Fatalf("expected prompt cache prefix token header")
	}
}
