//go:build unix

package e2e

import "testing"

func TestForumTopicRoutingE2E(t *testing.T) {
	for _, command := range []string{"test-notify", "seed", "run"} {
		t.Run(command, func(t *testing.T) {
			h := newHarness(t)
			h.setenv("TG_CHAT_IDS", "-1001234567890,-1009876543210")
			h.setenv("TG_MESSAGE_THREAD_ID", "4")
			h.fixture.mu.Lock()
			h.fixture.listFailures = nil
			h.fixture.mu.Unlock()
			args := []string{command}
			if command != "test-notify" {
				args = append(args, "--only", "standoff")
			}
			if command == "run" {
				h.fixture.mu.Lock()
				h.fixture.failedChat = "-1009876543210"
				h.fixture.mu.Unlock()
				h.run(1, args...)
				if h.status().Pending != 1 {
					t.Fatal("failed recipient not queued")
				}
				h.fixture.mu.Lock()
				h.fixture.failedChat = ""
				h.fixture.mu.Unlock()
			}
			h.run(0, args...)
			if command != "test-notify" && h.status().Pending != 0 {
				t.Fatal("delivered notification still pending")
			}
			h.fixture.mu.Lock()
			defer h.fixture.mu.Unlock()
			if len(h.fixture.deliveredTopics) != 2 {
				t.Fatalf("deliveries=%d want 2", len(h.fixture.deliveredTopics))
			}
			for i, topic := range h.fixture.deliveredTopics {
				if topic == nil || *topic != 4 {
					t.Fatalf("delivery %d topic=%v want 4", i, topic)
				}
				wantChat := []string{"-1001234567890", "-1009876543210"}[i]
				if h.fixture.deliveredChats[i] != wantChat {
					t.Fatalf("chat=%s want=%s", h.fixture.deliveredChats[i], wantChat)
				}
			}
		})
	}
}
