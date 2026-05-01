package client

import (
	"bufio"
	"bytes"
	"context"
	dsprotocol "ds2api/internal/deepseek/protocol"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	trans "ds2api/internal/deepseek/transport"
)

func (c *Client) CallCompletion(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string, maxAttempts int) (*http.Response, error) {
	if maxAttempts <= 0 {
		maxAttempts = c.maxRetries
	}
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken)
	headers["x-ds-pow-response"] = powResp
	captureSession := c.capture.Start("deepseek_completion", dsprotocol.DeepSeekCompletionURL, a.AccountID, payload)
	attempts := 0
	stillGeneratingRetries := 0
	var lastErr error
	for attempts < maxAttempts {
		resp, err := c.streamPost(ctx, clients.stream, dsprotocol.DeepSeekCompletionURL, headers, payload)
		if err != nil {
			lastErr = err
			attempts++
			time.Sleep(time.Second)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			stillGenerating, inspectedResp := inspectCompletionStillGenerating(resp)
			if stillGenerating {
				stillGeneratingRetries++
				_ = resp.Body.Close()
				if stillGeneratingRetries <= maxCompletionStillGeneratingRetries {
					config.Logger.Warn("[completion] upstream still generating; retrying", "retry", stillGeneratingRetries, "account", completionAccountID(a))
					sleepCompletionStillGenerating(ctx, stillGeneratingRetries)
					continue
				}
				return nil, errors.New("completion still generating")
			}
			resp = inspectedResp
			if captureSession != nil {
				resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
			}
			resp = c.wrapCompletionWithAutoContinue(ctx, a, payload, powResp, resp)
			return resp, nil
		}
		if completionErrorBodyStillGenerating(resp) {
			stillGeneratingRetries++
			if stillGeneratingRetries <= maxCompletionStillGeneratingRetries {
				config.Logger.Warn("[completion] upstream still generating status; retrying", "retry", stillGeneratingRetries, "status", resp.StatusCode, "account", completionAccountID(a))
				sleepCompletionStillGenerating(ctx, stillGeneratingRetries)
				continue
			}
			return nil, errors.New("completion still generating")
		}
		if captureSession != nil {
			resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
		}
		_ = resp.Body.Close()
		lastErr = &completionHTTPError{status: resp.StatusCode}
		attempts++
		time.Sleep(time.Second)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("completion failed: %w", lastErr)
	}
	return nil, errors.New("completion failed")
}

const (
	maxCompletionInspectLines            = 8
	maxCompletionInspectBytes            = 16 * 1024
	maxCompletionStillGeneratingRetries  = 5
	completionStillGeneratingBaseBackoff = 500 * time.Millisecond
)

type completionReplayBody struct {
	io.Reader
	closer io.Closer
}

func (b completionReplayBody) Close() error {
	if b.closer == nil {
		return nil
	}
	return b.closer.Close()
}

func inspectCompletionStillGenerating(resp *http.Response) (bool, *http.Response) {
	if resp == nil || resp.Body == nil {
		return false, resp
	}
	reader := bufio.NewReader(resp.Body)
	var prefix bytes.Buffer
	for lines := 0; lines < maxCompletionInspectLines && prefix.Len() < maxCompletionInspectBytes; lines++ {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			prefix.Write(line)
			if isCompletionStillGeneratingText(prefix.String()) {
				return true, resp
			}
			if completionInspectSawDecisionLine(line) {
				break
			}
		}
		if err != nil {
			break
		}
	}
	resp.Body = completionReplayBody{
		Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), reader),
		closer: resp.Body,
	}
	return false, resp
}

func completionInspectSawDecisionLine(line []byte) bool {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || strings.HasPrefix(trimmed, ":") || strings.HasPrefix(strings.ToLower(trimmed), "event:") {
		return false
	}
	return strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, "{")
}

func completionErrorBodyStillGenerating(resp *http.Response) bool {
	if resp == nil || resp.Body == nil {
		return false
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return false
	}
	// Replace the closed body with a fresh reader so the caller (capture session,
	// WrapBody, etc.) still sees a valid, open body.
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return isCompletionStillGeneratingText(string(body))
}

func isCompletionStillGeneratingText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(text, "有消息正在生成") ||
		strings.Contains(text, "消息正在生成") ||
		strings.Contains(lower, "message is being generated") ||
		strings.Contains(lower, "message still generating") ||
		strings.Contains(lower, "still generating")
}

type completionHTTPError struct {
	status int
}

func (e *completionHTTPError) Error() string {
	return fmt.Sprintf("upstream returned status %d", e.status)
}

func sleepCompletionStillGenerating(ctx context.Context, retry int) {
	if retry < 1 {
		retry = 1
	}
	delay := time.Duration(retry) * completionStillGeneratingBaseBackoff
	if delay > 3*time.Second {
		delay = 3 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func completionAccountID(a *auth.RequestAuth) string {
	if a == nil {
		return ""
	}
	if strings.TrimSpace(a.AccountID) != "" {
		return strings.TrimSpace(a.AccountID)
	}
	return strings.TrimSpace(a.CallerID)
}

func (c *Client) streamPost(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (*http.Response, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	headers = c.jsonHeaders(headers)
	clients := c.requestClientsFromContext(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := doer.Do(req)
	if err != nil {
		config.Logger.Warn("[deepseek] fingerprint stream request failed, fallback to std transport", "url", url, "error", err)
		req2, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
		if reqErr != nil {
			return nil, reqErr
		}
		for k, v := range headers {
			req2.Header.Set(k, v)
		}
		return clients.fallbackS.Do(req2)
	}
	return resp, nil
}
