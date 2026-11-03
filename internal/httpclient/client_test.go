package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestJSONRetryAndQuery(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "2" {
			t.Error("missing params")
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"value":42}`)
	}))
	defer s.Close()
	c := New(0)
	var got struct{ Value int }
	if e := c.JSON(context.Background(), s.URL, url.Values{"page": {"2"}}, &got); e != nil || got.Value != 42 || calls.Load() != 2 {
		t.Fatal(got, e)
	}
}
func TestNextData(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><script type="application/json" id="__NEXT_DATA__">{"props":{"pageProps":{"total":2}}}</script></html>`)
	}))
	defer s.Close()
	var got struct{ Total int }
	if e := New(0).NextData(context.Background(), s.URL, nil, &got); e != nil || got.Total != 2 {
		t.Fatal(got, e)
	}
}
func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := New(time.Hour)
	c.last = time.Now()
	if _, e := c.Get(ctx, "https://example.invalid", nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := New(0).Get(ctx, s.URL, nil); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestInvalidAndOversizedResponse(t *testing.T) {
	for _, body := range []string{"not JSON", strings.Repeat("x", maxBody+1)} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		var out any
		e := New(0).JSON(context.Background(), s.URL, nil, &out)
		s.Close()
		if e == nil {
			t.Fatal("invalid response accepted")
		}
	}
}
func TestRetryAfter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	if RetryAfter("5", now) != 5*time.Second || RetryAfter(now.Add(3*time.Second).Format(http.TimeFormat), now) != 3*time.Second || RetryAfter("bad", now) != 0 {
		t.Fatal("retry-after")
	}
}
