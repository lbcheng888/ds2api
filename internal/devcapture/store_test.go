package devcapture

import (
	"io"
	"strings"
	"testing"
)

func TestNewFromEnvDefaults(t *testing.T) {
	t.Setenv("DS2API_DEV_PACKET_CAPTURE_LIMIT", "")
	t.Setenv("DS2API_DEV_PACKET_CAPTURE_MAX_BODY_BYTES", "")
	t.Setenv("VERCEL", "")
	t.Setenv("NOW_REGION", "")

	s := NewFromEnv()
	if s.Limit() != 20 {
		t.Fatalf("expected default limit 20, got %d", s.Limit())
	}
	if s.MaxBodyBytes() != 5*1024*1024 {
		t.Fatalf("expected default max body bytes 5MB, got %d", s.MaxBodyBytes())
	}
}

func TestNewFromEnvHonorsOverrides(t *testing.T) {
	t.Setenv("DS2API_DEV_PACKET_CAPTURE_LIMIT", "7")
	t.Setenv("DS2API_DEV_PACKET_CAPTURE_MAX_BODY_BYTES", "8192")
	t.Setenv("VERCEL", "")
	t.Setenv("NOW_REGION", "")
	s := NewFromEnv()
	if s.Limit() != 7 {
		t.Fatalf("expected override limit 7, got %d", s.Limit())
	}
	if s.MaxBodyBytes() != 8192 {
		t.Fatalf("expected override max body bytes 8192, got %d", s.MaxBodyBytes())
	}
}

func TestStorePushKeepsNewestWithinLimit(t *testing.T) {
	s := &Store{enabled: true, limit: 2, maxBodyBytes: 1024}
	for i := 0; i < 3; i++ {
		session := s.Start("test", "http://x", "", map[string]any{"seq": i})
		if session == nil {
			t.Fatal("expected session")
		}
		rc := session.WrapBody(io.NopCloser(strings.NewReader("ok")), 200)
		_, _ = io.ReadAll(rc)
		_ = rc.Close()
	}
	items := s.Snapshot()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !strings.Contains(items[0].RequestBody, `"seq":2`) {
		t.Fatalf("expected newest first, got %#v", items[0].RequestBody)
	}
	if !strings.Contains(items[1].RequestBody, `"seq":1`) {
		t.Fatalf("expected second newest, got %#v", items[1].RequestBody)
	}
}

func TestWrapBodyTruncatesByLimit(t *testing.T) {
	s := &Store{enabled: true, limit: 5, maxBodyBytes: 4}
	session := s.Start("test", "http://x", "acc1", map[string]any{"x": 1})
	if session == nil {
		t.Fatal("expected session")
	}
	rc := session.WrapBody(io.NopCloser(strings.NewReader("abcdef")), 200)
	_, _ = io.ReadAll(rc)
	_ = rc.Close()

	items := s.Snapshot()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ResponseBody != "abcd" {
		t.Fatalf("expected truncated body, got %q", items[0].ResponseBody)
	}
	if !items[0].ResponseTruncated {
		t.Fatal("expected truncated flag true")
	}
	if items[0].AccountID != "acc1" {
		t.Fatalf("expected account id, got %q", items[0].AccountID)
	}
}

func TestLatestChainBySessionReturnsOrderedEntries(t *testing.T) {
	s := &Store{enabled: true, limit: 5, maxBodyBytes: 1024}

	first := s.Start("deepseek_completion", "http://x/completion", "acc1", map[string]any{"chat_session_id": "session-1"})
	firstBody := first.WrapBody(io.NopCloser(strings.NewReader("first")), 200)
	_, _ = io.ReadAll(firstBody)
	_ = firstBody.Close()

	other := s.Start("deepseek_completion", "http://x/completion", "acc2", map[string]any{"chat_session_id": "session-2"})
	otherBody := other.WrapBody(io.NopCloser(strings.NewReader("other")), 200)
	_, _ = io.ReadAll(otherBody)
	_ = otherBody.Close()

	second := s.Start("deepseek_continue", "http://x/continue", "acc1", map[string]any{"chat_session_id": "session-1"})
	secondBody := second.WrapBody(io.NopCloser(strings.NewReader("second")), 200)
	_, _ = io.ReadAll(secondBody)
	_ = secondBody.Close()

	chain, ok := s.LatestChainBySession("session-1")
	if !ok {
		t.Fatal("expected chain")
	}
	if chain.Key != "session:session-1" {
		t.Fatalf("unexpected chain key: %s", chain.Key)
	}
	if len(chain.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(chain.Entries))
	}
	if chain.Entries[0].ResponseBody != "first" || chain.Entries[1].ResponseBody != "second" {
		t.Fatalf("expected ordered matching entries, got %#v", chain.Entries)
	}
	if len(chain.IDs()) != 2 {
		t.Fatalf("expected 2 ids, got %#v", chain.IDs())
	}
}
