package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMessageThreadRouting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		topic []int
	}{
		{name: "legacy"}, {name: "disabled", topic: []int{0}}, {name: "forum", topic: []int{4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var client *Client
			if len(tc.topic) == 0 {
				client = NewClient("test-token", []string{"-1001", "-1002"})
			} else {
				client = NewClient("test-token", []string{"-1001", "-1002"}, tc.topic...)
			}
			var bodies []map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				bodies = append(bodies, body)
				if r.Method != http.MethodPost || r.URL.Path != "/bottest-token/sendMessage" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if len(bodies) == 1 {
					w.WriteHeader(400)
					_, _ = io.WriteString(w, `{"ok":false,"description":"can't parse entities"}`)
					return
				}
				_, _ = io.WriteString(w, `{"ok":true}`)
			}))
			defer server.Close()
			client.base, client.http, client.pause = server.URL, server.Client(), 0
			if err := client.Send(context.Background(), "<b>test</b>"); err != nil {
				t.Fatal(err)
			}
			if err := client.SendTo(context.Background(), "-1003", "direct"); err != nil {
				t.Fatal(err)
			}
			if len(bodies) != 4 {
				t.Fatalf("requests=%d want 4", len(bodies))
			}
			for i, body := range bodies {
				wantChat := []string{"-1001", "-1001", "-1002", "-1003"}[i]
				if body["chat_id"] != wantChat {
					t.Fatalf("chat=%v want=%s", body["chat_id"], wantChat)
				}
				topic, exists := body["message_thread_id"]
				if len(tc.topic) > 0 && tc.topic[0] > 0 {
					if topic != float64(tc.topic[0]) {
						t.Fatalf("request %d topic=%v want=%d", i, topic, tc.topic[0])
					}
				} else if exists {
					t.Fatalf("unconfigured topic must be omitted: %v", topic)
				}
			}
		})
	}
}
