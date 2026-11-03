package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "1234567890:AAH-abcdefghijklmnopqrstuvwxyz012345"

func TestSendFallbackAndRedaction(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var p map[string]any
		if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
			t.Error(e)
		}
		if calls == 1 {
			if p["parse_mode"] != "HTML" {
				t.Error(p)
			}
			w.WriteHeader(400)
			fmt.Fprint(w, `{"ok":false,"description":"can't parse entities"}`)
		} else {
			if _, ok := p["parse_mode"]; ok {
				t.Error("HTML mode retained")
			}
			if p["text"] != "hello <world>" {
				t.Error(p)
			}
			fmt.Fprint(w, `{"ok":true}`)
		}
	}))
	defer s.Close()
	tg := NewTelegram(testToken, "1")
	tg.base = s.URL
	tg.pause = 0
	if e := tg.Send(context.Background(), "<b>hello</b> &lt;world&gt;"); e != nil || calls != 2 {
		t.Fatal(e, calls)
	}
	s.Close()
	e := tg.Send(context.Background(), "x")
	if e == nil || strings.Contains(e.Error(), testToken) {
		t.Fatal(e)
	}
	if strings.Contains(Redact("other 9876543210:BBBbbbbbbbbbbbbbbbbbbbbbbbbbbbb", testToken), "9876543210") {
		t.Fatal("foreign token leaked")
	}
}
func Test429Cancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"ok":false,"parameters":{"retry_after":1000}}`)
	}))
	defer s.Close()
	tg := NewTelegram(testToken, "1")
	tg.base = s.URL
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e := tg.Send(ctx, "x"); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestAPIErrorEvenOnHTTP200(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": testToken})
	}))
	defer s.Close()
	tg := NewTelegram(testToken, "1")
	tg.base = s.URL
	e := tg.Send(context.Background(), "x")
	if e == nil || strings.Contains(e.Error(), testToken) {
		t.Fatal(e)
	}
}
