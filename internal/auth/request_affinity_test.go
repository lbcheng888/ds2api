package auth

import (
	"context"
	"net/http/httptest"
	"testing"

	"ds2api/internal/account"
	"ds2api/internal/config"
)

func newAffinityTestResolver(t *testing.T) *Resolver {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"one@example.com","token":"token-1"},
			{"email":"two@example.com","token":"token-2"}
		],
		"runtime":{"account_max_inflight":1,"account_max_queue":10,"account_affinity_ttl_seconds":3600}
	}`)
	store := config.LoadStore()
	return NewResolver(store, account.NewPool(store), func(_ context.Context, _ config.Account) (string, error) {
		return "unused", nil
	})
}

func TestDetermineWithAffinityKeepsConversationOnSameAccount(t *testing.T) {
	resolver := newAffinityTestResolver(t)
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer managed-key")

	first, err := resolver.DetermineWithAffinity(req, "thread-a")
	if err != nil {
		t.Fatalf("first determine failed: %v", err)
	}
	firstAccount := first.AccountID
	resolver.Release(first)

	other, err := resolver.DetermineWithAffinity(req, "thread-b")
	if err != nil {
		t.Fatalf("other determine failed: %v", err)
	}
	if other.AccountID == firstAccount {
		t.Fatalf("expected different affinity to follow pool rotation, got same account %q", other.AccountID)
	}
	resolver.Release(other)

	second, err := resolver.DetermineWithAffinity(req, "thread-a")
	if err != nil {
		t.Fatalf("second determine failed: %v", err)
	}
	if second.AccountID != firstAccount {
		t.Fatalf("expected thread-a to stay on %q, got %q", firstAccount, second.AccountID)
	}
	resolver.Release(second)
}

func TestSwitchAccountMovesAffinityToNewAccount(t *testing.T) {
	resolver := newAffinityTestResolver(t)
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer managed-key")

	first, err := resolver.DetermineWithAffinity(req, "thread-a")
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	oldAccount := first.AccountID
	if !resolver.SwitchAccount(context.Background(), first) {
		t.Fatalf("expected switch account to succeed")
	}
	newAccount := first.AccountID
	if newAccount == oldAccount {
		t.Fatalf("expected account to change")
	}
	resolver.Release(first)

	next, err := resolver.DetermineWithAffinity(req, "thread-a")
	if err != nil {
		t.Fatalf("determine after switch failed: %v", err)
	}
	if next.AccountID != newAccount {
		t.Fatalf("expected affinity to move to %q, got %q", newAccount, next.AccountID)
	}
	resolver.Release(next)
}
