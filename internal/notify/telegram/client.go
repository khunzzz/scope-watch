// Package telegram formats and delivers notifications.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var tokenRE = regexp.MustCompile(`[0-9]{6,}:[A-Za-z0-9_-]{20,}`)
var tagsRE = regexp.MustCompile(`<[^>]*>`)

func Redact(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "***")
	}
	return tokenRE.ReplaceAllString(s, "***")
}

type RedactingWriter struct {
	Writer io.Writer
	Token  string
}

func (w RedactingWriter) Write(p []byte) (int, error) {
	_, err := io.WriteString(w.Writer, Redact(string(p), w.Token))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

type Stdout struct{ Writer io.Writer }

func (s Stdout) Send(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(s.Writer, strings.Repeat("─", 60)+"\n"+text)
	return err
}

type Client struct {
	http                *http.Client
	token, chatID, base string
	mu                  sync.Mutex
	last                time.Time
	pause               time.Duration
}

func NewClient(token, chatID string) *Client {
	return &Client{
		http:   &http.Client{Timeout: 30 * time.Second},
		token:  token,
		chatID: chatID,
		base:   "https://api.telegram.org",
		pause:  time.Second,
	}
}

type response struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
	Result json.RawMessage `json:"result"`
}

func (t *Client) request(ctx context.Context, method string, payload any) (response, int, error) {
	var body io.Reader
	verb := http.MethodGet
	if payload != nil {
		raw, e := json.Marshal(payload)
		if e != nil {
			return response{}, 0, e
		}
		body = bytes.NewReader(raw)
		verb = http.MethodPost
	}
	req, e := http.NewRequestWithContext(ctx, verb, t.base+"/bot"+t.token+"/"+method, body)
	if e != nil {
		return response{}, 0, fmt.Errorf("telegram request: %s", Redact(e.Error(), t.token))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	r, e := t.http.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return response{}, 0, ctx.Err()
		}
		return response{}, 0, fmt.Errorf("telegram request: %s", Redact(e.Error(), t.token))
	}
	defer r.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if e != nil {
		return response{}, r.StatusCode, fmt.Errorf("telegram response: %s", Redact(e.Error(), t.token))
	}
	var out response
	if e = json.Unmarshal(raw, &out); e != nil {
		return response{}, r.StatusCode, fmt.Errorf("telegram HTTP %d: invalid JSON", r.StatusCode)
	}
	out.Description = Redact(out.Description, t.token)
	return out, r.StatusCode, nil
}

func (t *Client) Send(ctx context.Context, text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	mode := "HTML"
	if Units(text) > 4096 {
		mode = ""
		text = html.UnescapeString(tagsRE.ReplaceAllString(text, ""))
		units, end := 0, 0
		for i, r := range text {
			width := 1
			if r > 0xffff {
				width = 2
			}
			if units+width > 4000 {
				break
			}
			units += width
			end = i + len(string(r))
		}
		text = text[:end]
		text += "\n…(обрезано)"
	}
	if err := wait(ctx, time.Until(t.last.Add(t.pause))); err != nil {
		return err
	}
	for attempt := 0; attempt < 5; attempt++ {
		payload := map[string]any{"chat_id": t.chatID, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}
		if mode != "" {
			payload["parse_mode"] = mode
		}
		r, status, err := t.request(ctx, "sendMessage", payload)
		t.last = time.Now()
		if err != nil {
			return err
		}
		if status == 429 {
			if err := wait(ctx, time.Duration(max(1, r.Parameters.RetryAfter))*time.Second); err != nil {
				return err
			}
			continue
		}
		if status == 400 && strings.Contains(r.Description, "can't parse entities") && mode != "" {
			mode = ""
			text = html.UnescapeString(tagsRE.ReplaceAllString(text, ""))
			continue
		}
		if status < 200 || status >= 300 || !r.OK {
			return fmt.Errorf("telegram HTTP %d: %s", status, Clip(r.Description, 200))
		}
		return nil
	}
	return fmt.Errorf("telegram retries exhausted")
}

func (t *Client) ChatIDs(ctx context.Context) (string, error) {
	r, status, err := t.request(ctx, "getUpdates", nil)
	if err != nil {
		return "", err
	}
	if status != 200 || !r.OK {
		return "", fmt.Errorf("telegram HTTP %d: %s", status, r.Description)
	}
	var updates []struct {
		Message *struct {
			Chat struct {
				ID       int64
				Username string
			}
		}
	}
	if err = json.Unmarshal(r.Result, &updates); err != nil {
		return "", fmt.Errorf("decode Telegram updates: %w", err)
	}
	chats := map[int64]string{}
	for _, u := range updates {
		if u.Message != nil {
			chats[u.Message.Chat.ID] = u.Message.Chat.Username
		}
	}
	ids := []int64{}
	for id := range chats {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parts := []string{}
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%d (@%s)", id, chats[id]))
	}
	if len(parts) == 0 {
		return "нет апдейтов — напиши боту /start", nil
	}
	return strings.Join(parts, ", "), nil
}

func wait(ctx context.Context, d time.Duration) error {
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
