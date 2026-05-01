package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"ds2api/internal/auth"
)

func TestCallCompletionRetriesImmediateStillGeneratingSSE(t *testing.T) {
	calls := 0
	client := &Client{
		stream: doerFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			body := "data: {\"type\":\"text\",\"content\":\"ok\"}\n\ndata: [DONE]\n"
			if calls == 1 {
				body = "data: {\"type\":\"error\",\"content\":\"有消息正在生成，请稍后再试\"}\n"
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
		maxRetries: 1,
	}

	resp, err := client.CallCompletion(context.Background(), &auth.RequestAuth{AccountID: "acct-1", DeepSeekToken: "token"}, map[string]any{"chat_session_id": "s1"}, "pow", 1)
	if err != nil {
		t.Fatalf("CallCompletion returned error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if calls != 2 {
		t.Fatalf("expected retry after still-generating SSE, calls=%d", calls)
	}
	if !strings.Contains(string(body), `"content":"ok"`) {
		t.Fatalf("expected retried response body to be preserved, got %s", string(body))
	}
}

func TestCallCompletionPreservesInspectedNormalSSE(t *testing.T) {
	client := &Client{
		stream: doerFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("event: message\ndata: {\"content\":\"first\"}\n\ndata: [DONE]\n")),
				Request:    req,
			}, nil
		}),
		maxRetries: 1,
	}

	resp, err := client.CallCompletion(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, map[string]any{"chat_session_id": "s1"}, "pow", 1)
	if err != nil {
		t.Fatalf("CallCompletion returned error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(string(body), "event: message\ndata:") {
		t.Fatalf("expected inspected prefix to be replayed, got %s", string(body))
	}
}

func TestCallCompletionRetriesStillGeneratingStatusBody(t *testing.T) {
	calls := 0
	client := &Client{
		stream: doerFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"msg":"有消息正在生成，请稍后再试"}`)),
					Request:    req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("data: {\"content\":\"ok\"}\n")),
				Request:    req,
			}, nil
		}),
		maxRetries: 1,
	}

	resp, err := client.CallCompletion(context.Background(), &auth.RequestAuth{AccountID: "acct-1", DeepSeekToken: "token"}, map[string]any{"chat_session_id": "s1"}, "pow", 1)
	if err != nil {
		t.Fatalf("CallCompletion returned error: %v", err)
	}
	defer resp.Body.Close()
	if calls != 2 {
		t.Fatalf("expected status-body retry, calls=%d", calls)
	}
}

func TestCallCompletionMaxStillGeneratingRetriesReturnsError(t *testing.T) {
	calls := 0
	client := &Client{
		stream: doerFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"error\",\"content\":\"有消息正在生成，请稍后再试\"}\n",
				)),
				Request: req,
			}, nil
		}),
		maxRetries: 1,
	}

	_, err := client.CallCompletion(context.Background(), &auth.RequestAuth{AccountID: "acct-1", DeepSeekToken: "token"}, map[string]any{"chat_session_id": "s1"}, "pow", 1)
	if err == nil {
		t.Fatal("expected error after exhausting still-generating retries")
	}
	if !strings.Contains(err.Error(), "still generating") {
		t.Fatalf("expected 'still generating' error, got: %v", err)
	}
	// 1 initial + 5 retries = 6 calls
	if calls != 6 {
		t.Fatalf("expected 6 calls (1 initial + 5 retries), got %d", calls)
	}
}

func TestCallCompletionContextCancelledDuringStillGeneratingBackoff(t *testing.T) {
	firstCallDone := make(chan struct{})
	calls := 0

	client := &Client{
		stream: doerFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				close(firstCallDone)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(
						"data: {\"type\":\"error\",\"content\":\"有消息正在生成，请稍后再试\"}\n",
					)),
					Request: req,
				}, nil
			}
			// Subsequent calls — context should be cancelled.
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			default:
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("data: ok\n")),
				Request:    req,
			}, nil
		}),
		maxRetries: 1,
		fallbackS: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				default:
					return nil, errors.New("unexpected fallback call")
				}
			}),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-firstCallDone
		time.Sleep(5 * time.Millisecond) // let the first still-generating check finish
		cancel()
	}()

	_, err := client.CallCompletion(ctx, &auth.RequestAuth{AccountID: "acct-1", DeepSeekToken: "token"}, map[string]any{"chat_session_id": "s1"}, "pow", 1)
	if err == nil {
		t.Fatal("expected error after context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestCompletionErrorBodyStillGeneratingLeavesBodyReadable(t *testing.T) {
	originalBody := `{"msg":"some other error"}`
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(originalBody)),
	}

	// Should return false (not still generating)
	result := completionErrorBodyStillGenerating(resp)
	if result {
		t.Fatal("expected false for non-still-generating body")
	}

	// Body must still be fully readable with original content
	after, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("unexpected error reading reconstructed body: %v", err)
	}
	defer resp.Body.Close()
	if string(after) != originalBody {
		t.Fatalf("body content mismatch: got %q, want %q", string(after), originalBody)
	}
}

func TestCompletionErrorBodyStillGeneratingNilBody(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: nil}
	result := completionErrorBodyStillGenerating(resp)
	if result {
		t.Fatal("expected false for nil body")
	}
}

func TestIsCompletionStillGeneratingText(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Chinese matches
		{`有消息正在生成`, true},
		{`消息正在生成`, true},
		{`{"msg":"有消息正在生成，请稍后再试"}`, true},
		// English matches
		{`Message Is Being Generated`, true},
		{`message still generating`, true},
		{`still generating`, true},
		{`data: {"error":"message still generating"}`, true},
		// Non-matches
		{`data: {"type":"text","content":"hello"}`, false},
		{`event: message`, false},
		{`data: [DONE]`, false},
		{``, false},
		{`some random text`, false},
		// Near-miss: "generating" alone shouldn't match
		{`generating`, false},
		{`still`, false},
	}
	for _, tc := range tests {
		got := isCompletionStillGeneratingText(tc.input)
		if got != tc.want {
			t.Errorf("isCompletionStillGeneratingText(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestCompletionInspectStillGeneratingDecisionLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{": comment\n", false},
		{"event: message\n", false},
		{"\n", false},
		{"data: hello\n", true},
		{"data: [DONE]\n", true},
		// JSON object opening is a SSE data line → decision line
		{`{"key":"value"}`, true},
		{`{"key":"value"}` + "\n", true},
	}
	for _, tc := range tests {
		got := completionInspectSawDecisionLine([]byte(tc.line))
		if got != tc.want {
			t.Errorf("completionInspectSawDecisionLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestCompletionReplayBody(t *testing.T) {
	// Simulate an SSE body and a buffered reader that has read some prefix.
	fullBody := "event: message\ndata: {\"content\":\"hello\"}\n\ndata: [DONE]\n"
	raw := strings.NewReader(fullBody)
	reader := bufio.NewReader(raw)

	// Read a prefix (e.g., first event)
	var prefix bytes.Buffer
	for i := 0; i < 2; i++ {
		line, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			t.Fatalf("unexpected error: %v", err)
		}
		prefix.Write(line)
	}

	replay := completionReplayBody{
		Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), reader),
		closer: io.NopCloser(raw),
	}

	replayed, err := io.ReadAll(replay)
	if err != nil {
		t.Fatalf("unexpected error reading replay body: %v", err)
	}
	defer replay.Close()

	if string(replayed) != fullBody {
		t.Fatalf("replay body mismatch:\ngot:  %q\nwant: %q", string(replayed), fullBody)
	}
}
