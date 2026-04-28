package promptcache

import (
	"strings"
	"testing"
)

func TestStablePrefixUsesStaticSystemAndToolPrefix(t *testing.T) {
	prompt := "<｜begin▁of▁sentence｜><｜System｜>" + repeat("stable ", 80) + "<｜end▁of▁instructions｜><｜User｜>hello"
	got := StablePrefix(prompt)
	if got == "" || strings.Contains(got, "<｜User｜>") {
		t.Fatalf("expected stable prefix before first user marker, got %q", got)
	}
	if Fingerprint(prompt) == "" {
		t.Fatalf("expected non-empty fingerprint")
	}
}

func TestStablePrefixUsesConversationBeforeLatestUser(t *testing.T) {
	prompt := "<｜begin▁of▁sentence｜><｜System｜>" +
		repeat("stable ", 80) +
		"<｜end▁of▁instructions｜><｜User｜>first question<｜Assistant｜>first answer<｜User｜>latest question"
	got := StablePrefix(prompt)
	if !strings.Contains(got, "first question") || !strings.Contains(got, "first answer") {
		t.Fatalf("expected prior conversation in stable prefix, got %q", got)
	}
	if strings.Contains(got, "latest question") {
		t.Fatalf("expected latest user turn excluded from stable prefix, got %q", got)
	}
}

func TestLedgerReportsHitAfterFirstObservation(t *testing.T) {
	l := NewLedger()
	prompt := "<｜begin▁of▁sentence｜><｜System｜>" + repeat("stable ", 80) + "<｜end▁of▁instructions｜><｜User｜>hello"
	first := l.Observe(prompt)
	if first.HitTokens != 0 || first.MissTokens == 0 {
		t.Fatalf("first observation should miss cache, got %#v", first)
	}
	second := l.Observe(prompt)
	if second.Key == "" || second.HitTokens == 0 {
		t.Fatalf("second observation should hit cache, got %#v", second)
	}
	if second.HitTokens+second.MissTokens != first.MissTokens {
		t.Fatalf("token accounting mismatch: first=%#v second=%#v", first, second)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
