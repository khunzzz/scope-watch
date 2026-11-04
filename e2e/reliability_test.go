//go:build unix

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khunzzz/scope-watch/internal/storage"
)

func TestPartialCollectionAndRewardRetryE2E(t *testing.T) {
	h := newHarness(t)
	h.fixture.mu.Lock()
	h.fixture.extraPrograms = 4
	h.fixture.listFailures = nil
	h.fixture.mu.Unlock()
	h.run(0, "run", "--only", "standoff")
	initial := h.status().Platforms[0].LastSuccess
	h.fixture.mu.Lock()
	h.fixture.rewardsChanged = true
	h.fixture.rewardsDown = true
	h.fixture.mu.Unlock()
	for attempt := 1; attempt <= 2; attempt++ {
		h.run(1, "run", "--only", "standoff")
		p := h.status().Platforms[0]
		if p.Programs != 5 || p.Failures != attempt || len(p.FailedPrograms) != 1 || p.FailedPrograms[0].Slug != "flowwow" || p.FailedPrograms[0].Failures != attempt {
			t.Fatalf("partial failure not tracked: %+v", p)
		}
		if p.LastSuccess == nil || initial == nil || !p.LastSuccess.Equal(*initial) || p.FailedPrograms[0].LastSuccess == nil {
			t.Fatalf("partial run advanced success or lost program freshness: %+v", p)
		}
	}
	h.fixture.mu.Lock()
	h.fixture.rewardsDown = false
	h.fixture.mu.Unlock()
	h.run(0, "run", "--only", "standoff")
	p := h.status().Platforms[0]
	if p.Failures != 0 || len(p.FailedPrograms) != 0 || !p.LastSuccess.After(*initial) {
		t.Fatalf("recovery not tracked: %+v", p)
	}
	if !strings.Contains(h.fixture.lastMessage(), "140 000") {
		t.Fatal("reward update was hidden by cache", h.fixture.lastMessage())
	}
	if text := h.run(0, "history", "--program", "flowwow"); !strings.Contains(text, "выплаты") {
		t.Fatal("reward delta not recorded", text)
	}
}

func TestRejectedMessageDoesNotBlockQueueE2E(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s, err := storage.Open(ctx, filepath.Join(h.dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	messages := []string{"reject-this", "deliver-this"}
	if err = s.CommitRun(ctx, nil, nil, nil, messages, storage.CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	h.fixture.mu.Lock()
	h.fixture.listFailures = nil
	h.fixture.rejectText = "reject-this"
	h.fixture.rejectDescription = "Bad Request: can't parse entities: unsupported start tag"
	h.fixture.mu.Unlock()
	h.run(1, "run", "--only", "standoff")
	status := h.status()
	if status.Pending != 1 || status.Quarantined != 1 {
		t.Fatalf("rejected message not retained: %+v", status)
	}
	var failures []storage.DeliveryFailure
	if err = json.Unmarshal([]byte(h.run(0, "notifications")), &failures); err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].HTML != "reject-this" || failures[0].Attempts != 1 || !failures[0].Quarantined {
		t.Fatalf("failure details lost: %+v", failures)
	}
	h.fixture.mu.Lock()
	delivered := strings.Join(h.fixture.messages, "\n")
	h.fixture.rejectText = ""
	h.fixture.mu.Unlock()
	if !strings.Contains(delivered, "deliver-this") {
		t.Fatal("rejected message blocked later delivery")
	}
	// A quarantined message is not retried implicitly.
	h.run(1, "run", "--only", "standoff")
	if text := h.run(0, "notifications"); !strings.Contains(text, `"attempts":1`) {
		t.Fatal(text)
	}
	h.run(0, "notifications", "retry", "--id", fmt.Sprint(failures[0].ID))
	h.run(0, "run", "--only", "standoff")
	if status := h.status(); status.Pending != 0 || status.Quarantined != 0 {
		t.Fatal("explicit retry did not recover", status)
	}
	if h.run(0, "notifications") != "[]\n" {
		t.Fatal("acknowledged failure retained")
	}
	h.run(1, "notifications", "retry", "--id", fmt.Sprint(failures[0].ID))
	h.run(2, "notifications", "retry", "--id", "0")
}
