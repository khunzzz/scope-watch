// Package httpclient provides bounded, cancellable GET requests with retry and pacing.
package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const maxBody = 8 << 20

var nextData = regexp.MustCompile(`(?s)<script\b[^>]*\bid=["']__NEXT_DATA__["'][^>]*>(.*?)</script>`)

type Client struct {
	HTTP  *http.Client
	Delay time.Duration
	mu    sync.Mutex
	last  time.Time
}

func New(delay time.Duration) *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, Delay: delay}
}
func Wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (c *Client) pace(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := Wait(ctx, time.Until(c.last.Add(c.Delay))); err != nil {
		return err
	}
	c.last = time.Now()
	return nil
}
func RetryAfter(value string, now time.Time) time.Duration {
	if n, e := strconv.Atoi(value); e == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, e := http.ParseTime(value); e == nil {
		return max(time.Duration(0), t.Sub(now))
	}
	return 0
}
func (c *Client) Get(ctx context.Context, address string, params url.Values) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("request URL: %w", err)
	}
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	for attempt := 0; attempt < 4; attempt++ {
		if err := c.pace(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "scope-watch/1.0 (+https://github.com/khunzzz/scope-watch)")
		req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
		req.Header.Set("X-Bug-Bounty", "sayfu")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == 3 {
				return nil, fmt.Errorf("GET %s: %w", u.Redacted(), err)
			}
			if e := Wait(ctx, time.Second*time.Duration(1<<attempt)); e != nil {
				return nil, e
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", u.Redacted(), readErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(body) > maxBody {
			return nil, fmt.Errorf("response from %s exceeds %d bytes", u.Redacted(), maxBody)
		}
		retry := resp.StatusCode == 429 || resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504
		if retry && attempt < 3 {
			delay := max(time.Second*time.Duration(1<<attempt), RetryAfter(resp.Header.Get("Retry-After"), time.Now()))
			if err := Wait(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("GET %s: HTTP %d", u.Redacted(), resp.StatusCode)
		}
		return body, nil
	}
	return nil, fmt.Errorf("GET %s: retries exhausted", u.Redacted())
}
func (c *Client) JSON(ctx context.Context, u string, q url.Values, out any) error {
	b, e := c.Get(ctx, u, q)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, out); e != nil {
		return fmt.Errorf("decode %s: %w", u, e)
	}
	return nil
}
func (c *Client) NextData(ctx context.Context, u string, q url.Values, out any) error {
	b, e := c.Get(ctx, u, q)
	if e != nil {
		return e
	}
	m := nextData.FindSubmatch(b)
	if m == nil {
		return fmt.Errorf("no __NEXT_DATA__ at %s", u)
	}
	var envelope struct {
		Props struct {
			PageProps json.RawMessage `json:"pageProps"`
		} `json:"props"`
	}
	if e = json.Unmarshal(m[1], &envelope); e != nil {
		return fmt.Errorf("decode Next.js envelope: %w", e)
	}
	if len(envelope.Props.PageProps) == 0 {
		return fmt.Errorf("missing pageProps at %s", u)
	}
	return json.Unmarshal(envelope.Props.PageProps, out)
}
