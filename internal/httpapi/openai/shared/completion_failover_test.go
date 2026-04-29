package shared

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"ds2api/internal/auth"
	dsclient "ds2api/internal/deepseek/client"
	"ds2api/internal/promptcompat"
)

type failoverAuthStub struct {
	failures []string
	success  []string
	switches int
}

func (f *failoverAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return nil, nil
}

func (f *failoverAuthStub) DetermineCaller(_ *http.Request) (*auth.RequestAuth, error) {
	return nil, nil
}

func (f *failoverAuthStub) Release(_ *auth.RequestAuth) {}

func (f *failoverAuthStub) SwitchAccount(_ context.Context, a *auth.RequestAuth) bool {
	f.switches++
	a.AccountID = fmt.Sprintf("acct-%d", f.switches+1)
	a.DeepSeekToken = fmt.Sprintf("token-%d", f.switches+1)
	return true
}

func (f *failoverAuthStub) MarkAccountFailure(a *auth.RequestAuth) {
	f.failures = append(f.failures, a.AccountID)
}

func (f *failoverAuthStub) MarkAccountSuccess(a *auth.RequestAuth) {
	f.success = append(f.success, a.AccountID)
}

type failoverDSStub struct {
	statuses []int
	payloads []map[string]any
	accounts []string
}

func (d *failoverDSStub) CreateSession(_ context.Context, a *auth.RequestAuth, _ int) (string, error) {
	return "session-" + a.AccountID, nil
}

func (d *failoverDSStub) GetPow(_ context.Context, a *auth.RequestAuth, _ int) (string, error) {
	return "pow-" + a.AccountID, nil
}

func (d *failoverDSStub) UploadFile(_ context.Context, _ *auth.RequestAuth, _ dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	return nil, nil
}

func (d *failoverDSStub) CallCompletion(_ context.Context, a *auth.RequestAuth, payload map[string]any, _ string, _ int) (*http.Response, error) {
	d.accounts = append(d.accounts, a.AccountID)
	d.payloads = append(d.payloads, payload)
	status := http.StatusOK
	if len(d.statuses) > 0 {
		status = d.statuses[0]
		d.statuses = d.statuses[1:]
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func (d *failoverDSStub) DeleteSessionForToken(_ context.Context, _ string, _ string) (*dsclient.DeleteSessionResult, error) {
	return &dsclient.DeleteSessionResult{Success: true}, nil
}

func (d *failoverDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

func TestCallCompletionWithManagedFailoverSwitchesAccountsAndRebuildsPayload(t *testing.T) {
	authStub := &failoverAuthStub{}
	ds := &failoverDSStub{statuses: []int{http.StatusTooManyRequests, http.StatusOK}}
	a := &auth.RequestAuth{
		UseConfigToken: true,
		AccountID:      "acct-1",
		DeepSeekToken:  "token-1",
		TriedAccounts:  map[string]bool{},
	}
	req := promptcompat.StandardRequest{
		RequestedModel: "deepseek-v4-pro",
		ResolvedModel:  "deepseek-v4-pro",
		FinalPrompt:    "hello",
	}

	got, err := CallCompletionWithManagedFailover(context.Background(), authStub, ds, nil, a, req)
	if err != nil {
		t.Fatalf("expected failover success, got error: %v", err)
	}
	if got.Response == nil || got.Response.StatusCode != http.StatusOK {
		t.Fatalf("expected final OK response, got %#v", got.Response)
	}
	if strings.Join(ds.accounts, ",") != "acct-1,acct-2" {
		t.Fatalf("expected completion attempts on two accounts, got %v", ds.accounts)
	}
	if ds.payloads[0]["chat_session_id"] != "session-acct-1" || ds.payloads[1]["chat_session_id"] != "session-acct-2" {
		t.Fatalf("expected payload rebuilt with new session IDs, got %#v", ds.payloads)
	}
	if ds.payloads[1]["parent_message_id"] != nil {
		t.Fatalf("expected cross-account failover parent_message_id to stay nil, got %#v", ds.payloads[1]["parent_message_id"])
	}
	if strings.Join(authStub.failures, ",") != "acct-1" || strings.Join(authStub.success, ",") != "acct-2" {
		t.Fatalf("unexpected health marks failures=%v success=%v", authStub.failures, authStub.success)
	}
}

func TestCallCompletionWithManagedFailoverDoesNotSwitchWithAccountScopedFiles(t *testing.T) {
	authStub := &failoverAuthStub{}
	ds := &failoverDSStub{statuses: []int{http.StatusTooManyRequests}}
	a := &auth.RequestAuth{
		UseConfigToken: true,
		AccountID:      "acct-1",
		DeepSeekToken:  "token-1",
		TriedAccounts:  map[string]bool{},
	}
	req := promptcompat.StandardRequest{
		RequestedModel: "deepseek-v4-pro",
		ResolvedModel:  "deepseek-v4-pro",
		FinalPrompt:    "hello",
		RefFileIDs:     []string{"file-from-acct-1"},
	}

	got, err := CallCompletionWithManagedFailover(context.Background(), authStub, ds, nil, a, req)
	if err != nil {
		t.Fatalf("expected retriable status to be returned without cross-account failover, got error: %v", err)
	}
	if got.Response == nil || got.Response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected original 429 response, got %#v", got.Response)
	}
	if authStub.switches != 0 || len(ds.accounts) != 1 {
		t.Fatalf("expected no failover for account-scoped file refs, switches=%d accounts=%v", authStub.switches, ds.accounts)
	}
}
