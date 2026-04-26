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
	failures []string
	success  []string
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

func (a *failoverAuthStub) MarkAccountFailure(req *auth.RequestAuth) {
	if req != nil {
		a.failures = append(a.failures, req.AccountID)
	}
}

func (a *failoverAuthStub) MarkAccountSuccess(req *auth.RequestAuth) {
	if req != nil {
		a.success = append(a.success, req.AccountID)
	}
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

type emptyStreamRetryDSStub struct {
	callCount int
	accounts  []string
}

func (d *emptyStreamRetryDSStub) CreateSession(_ context.Context, a *auth.RequestAuth, _ int) (string, error) {
	return "session-" + a.AccountID, nil
}

func (d *emptyStreamRetryDSStub) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "pow", nil
}

func (d *emptyStreamRetryDSStub) UploadFile(_ context.Context, _ *auth.RequestAuth, _ deepseek.UploadFileRequest, _ int) (*deepseek.UploadFileResult, error) {
	return &deepseek.UploadFileResult{ID: "file-id", Filename: "file.txt", Bytes: 1, Status: "uploaded"}, nil
}

func (d *emptyStreamRetryDSStub) CallCompletion(_ context.Context, a *auth.RequestAuth, _ map[string]any, _ string, _ int) (*http.Response, error) {
	d.callCount++
	d.accounts = append(d.accounts, a.AccountID)
	if d.callCount == 1 {
		return makeSSEHTTPResponse(`data: [DONE]`), nil
	}
	return makeSSEHTTPResponse(`data: {"p":"response/content","v":"ok after retry"}`, `data: [DONE]`), nil
}

func (d *emptyStreamRetryDSStub) DeleteSessionForToken(_ context.Context, _ string, _ string) (*deepseek.DeleteSessionResult, error) {
	return &deepseek.DeleteSessionResult{Success: true}, nil
}

func (d *emptyStreamRetryDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

func TestChatCompletionsStreamRetriesEmptyOutputOnManagedAccount(t *testing.T) {
	authStub := &failoverAuthStub{}
	dsStub := &emptyStreamRetryDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{wideInput: true},
		Auth:  authStub,
		DS:    dsStub,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	req.Header.Set("Authorization", "Bearer ds2api-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after empty-output stream retry, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "upstream_empty_output") {
		t.Fatalf("expected empty output to be retried before reporting an error, got %s", body)
	}
	if !strings.Contains(body, "ok after retry") {
		t.Fatalf("expected successful retry content, got %s", body)
	}
	if got := strings.Join(dsStub.accounts, ","); got != "acct-1,acct-2" {
		t.Fatalf("expected stream attempts on acct-1 then acct-2, got %s", got)
	}
}

type protocolStreamRetryDSStub struct {
	callCount int
	accounts  []string
}

func (d *protocolStreamRetryDSStub) CreateSession(_ context.Context, a *auth.RequestAuth, _ int) (string, error) {
	return "session-" + a.AccountID, nil
}

func (d *protocolStreamRetryDSStub) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "pow", nil
}

func (d *protocolStreamRetryDSStub) UploadFile(_ context.Context, _ *auth.RequestAuth, _ deepseek.UploadFileRequest, _ int) (*deepseek.UploadFileResult, error) {
	return &deepseek.UploadFileResult{ID: "file-id", Filename: "file.txt", Bytes: 1, Status: "uploaded"}, nil
}

func (d *protocolStreamRetryDSStub) CallCompletion(_ context.Context, a *auth.RequestAuth, _ map[string]any, _ string, _ int) (*http.Response, error) {
	d.callCount++
	d.accounts = append(d.accounts, a.AccountID)
	if d.callCount == 1 {
		return makeSSEHTTPResponse(`data: {"p":"response/content","v":"Let me read the README first."}`, `data: [DONE]`), nil
	}
	return makeSSEHTTPResponse(`data: {"p":"response/content","v":"<tool_calls><tool_call><tool_name>read_file</tool_name><parameters>{\"path\":\"README.MD\"}</parameters></tool_call></tool_calls>"}`, `data: [DONE]`), nil
}

func (d *protocolStreamRetryDSStub) DeleteSessionForToken(_ context.Context, _ string, _ string) (*deepseek.DeleteSessionResult, error) {
	return &deepseek.DeleteSessionResult{Success: true}, nil
}

func (d *protocolStreamRetryDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

func TestChatCompletionsStreamRetriesMissingToolCallOnManagedAccount(t *testing.T) {
	authStub := &failoverAuthStub{}
	dsStub := &protocolStreamRetryDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{wideInput: true},
		Auth:  authStub,
		DS:    dsStub,
	}
	reqBody := `{
		"model":"deepseek-chat",
		"messages":[{"role":"user","content":"read README"}],
		"stream":true,
		"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer ds2api-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after protocol retry, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, upstreamMissingToolCallCode) {
		t.Fatalf("expected missing tool call to be retried before reporting an error, got %s", body)
	}
	if strings.Contains(body, "Let me read the README first") {
		t.Fatalf("did not expect rejected future-action text to leak before retry, got %s", body)
	}
	if !strings.Contains(body, `"name":"read_file"`) || !strings.Contains(body, `README.MD`) {
		t.Fatalf("expected retried tool call in stream, got %s", body)
	}
	if got := strings.Join(dsStub.accounts, ","); got != "acct-1,acct-2" {
		t.Fatalf("expected stream attempts on acct-1 then acct-2, got %s", got)
	}
	if got := strings.Join(authStub.failures, ","); got != "acct-1" {
		t.Fatalf("expected acct-1 marked failed, got %q", got)
	}
}
