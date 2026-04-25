package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/auth"
	"ds2api/internal/deepseek"
)

type failoverAuthStub struct {
	switched bool
}

func (a *failoverAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "token-1",
		CallerID:       "caller:test",
		AccountID:      "acct-1",
		TriedAccounts:  map[string]bool{},
	}, nil
}

func (a *failoverAuthStub) DetermineCaller(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{CallerID: "caller:test", TriedAccounts: map[string]bool{}}, nil
}

func (a *failoverAuthStub) Release(_ *auth.RequestAuth) {}

func (a *failoverAuthStub) SwitchAccount(_ context.Context, req *auth.RequestAuth) bool {
	if a.switched {
		return false
	}
	a.switched = true
	req.AccountID = "acct-2"
	req.DeepSeekToken = "token-2"
	return true
}

type failoverDSStub struct {
	callCount int
	accounts  []string
	statuses  []int
}

func (d *failoverDSStub) CreateSession(_ context.Context, a *auth.RequestAuth, _ int) (string, error) {
	return "session-" + a.AccountID, nil
}

func (d *failoverDSStub) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "pow", nil
}

func (d *failoverDSStub) UploadFile(_ context.Context, _ *auth.RequestAuth, _ deepseek.UploadFileRequest, _ int) (*deepseek.UploadFileResult, error) {
	return &deepseek.UploadFileResult{ID: "file-id", Filename: "file.txt", Bytes: 1, Status: "uploaded"}, nil
}

func (d *failoverDSStub) CallCompletion(_ context.Context, a *auth.RequestAuth, _ map[string]any, _ string, _ int) (*http.Response, error) {
	d.callCount++
	d.accounts = append(d.accounts, a.AccountID)
	if len(d.statuses) >= d.callCount && d.statuses[d.callCount-1] != 0 {
		return &http.Response{
			StatusCode: d.statuses[d.callCount-1],
			Body:       io.NopCloser(strings.NewReader("rate limited")),
		}, nil
	}
	if d.callCount == 1 {
		return nil, errors.New("socket closed")
	}
	return makeSSEHTTPResponse(`data: {"p":"response/content","v":"ok"}`, `data: [DONE]`), nil
}

func (d *failoverDSStub) DeleteSessionForToken(_ context.Context, _ string, _ string) (*deepseek.DeleteSessionResult, error) {
	return &deepseek.DeleteSessionResult{Success: true}, nil
}

func (d *failoverDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error { return nil }

func TestChatCompletionsFailsOverToNextManagedAccount(t *testing.T) {
	authStub := &failoverAuthStub{}
	dsStub := &failoverDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{wideInput: true},
		Auth:  authStub,
		DS:    dsStub,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer ds2api-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after failover, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !authStub.switched {
		t.Fatalf("expected managed account switch")
	}
	if got := strings.Join(dsStub.accounts, ","); got != "acct-1,acct-2" {
		t.Fatalf("expected completion attempts on acct-1 then acct-2, got %s", got)
	}
	if !strings.Contains(rec.Body.String(), `"content":"ok"`) {
		t.Fatalf("expected successful completion body, got %s", rec.Body.String())
	}
}

func TestChatCompletionsFailsOverOnRetriableCompletionStatus(t *testing.T) {
	authStub := &failoverAuthStub{}
	dsStub := &failoverDSStub{statuses: []int{http.StatusTooManyRequests, 0}}
	h := &Handler{
		Store: mockOpenAIConfig{wideInput: true},
		Auth:  authStub,
		DS:    dsStub,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer ds2api-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after status failover, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.Join(dsStub.accounts, ","); got != "acct-1,acct-2" {
		t.Fatalf("expected completion attempts on acct-1 then acct-2, got %s", got)
	}
}
