package app

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// platformTransport applies the collection policy to GET requests.
// Response decoding belongs to each platform adapter.
type platformTransport struct {
	base  *http.Transport
	delay time.Duration
	mu    sync.Mutex
	last  time.Time
}

func newPlatformHTTPClient(delay time.Duration) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &platformTransport{
			base:  http.DefaultTransport.(*http.Transport).Clone(),
			delay: delay,
		},
	}
}

func (t *platformTransport) CloseIdleConnections() {
	t.base.CloseIdleConnections()
}

func (t *platformTransport) pace(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := wait(ctx, time.Until(t.last.Add(t.delay))); err != nil {
		return err
	}
	t.last = time.Now()
	return nil
}

func (t *platformTransport) RoundTrip(original *http.Request) (*http.Response, error) {
	if original.Method != http.MethodGet {
		return t.base.RoundTrip(original)
	}
	req := original.Clone(original.Context())
	req.Header.Set("User-Agent", "scope-watch/1.0 (+https://github.com/khunzzz/scope-watch)")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
	req.Header.Set("X-Bug-Bounty", "sayfu")
	for attempt := 0; ; attempt++ {
		if err := t.pace(req.Context()); err != nil {
			return nil, err
		}
		resp, err := t.base.RoundTrip(req)
		if req.Context().Err() != nil {
			if resp != nil {
				resp.Body.Close()
			}
			return nil, req.Context().Err()
		}
		if attempt == 3 || (err == nil && !retryableStatus(resp.StatusCode)) {
			return resp, err
		}
		delay := time.Second * time.Duration(1<<attempt)
		if resp != nil {
			delay = max(delay, retryAfter(resp.Header.Get("Retry-After"), time.Now()))
			_, _ = io.CopyN(io.Discard, resp.Body, 64<<10)
			resp.Body.Close()
		}
		if err := wait(req.Context(), delay); err != nil {
			return nil, err
		}
	}
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		// Waiting longer cannot succeed within the client's request deadline.
		return time.Duration(min(seconds, 30)) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil {
		return max(time.Duration(0), deadline.Sub(now))
	}
	return 0
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
