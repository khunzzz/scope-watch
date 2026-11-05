//go:build unix

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStandoffUnpublishedRewardsE2E(t *testing.T) {
	h := newHarness(t)
	h.fixture.mu.Lock()
	h.fixture.listFailures = nil
	h.fixture.rewards = json.RawMessage(`{"reward":null}`)
	h.fixture.mu.Unlock()

	h.run(0, "run", "--only", "standoff")
	assertHealthy := func() {
		t.Helper()
		p := h.status().Platforms[0]
		if p.Failures != 0 || len(p.FailedPrograms) != 0 || p.LastSuccess == nil {
			t.Fatalf("unpublished rewards marked as collection failure: %+v", p)
		}
	}
	assertHealthy()
	// An unchanged unpublished reward must not produce repeated notifications.
	before := h.fixture.messageCount()
	h.run(0, "run", "--only", "standoff")
	if h.fixture.messageCount() != before {
		t.Fatal("unchanged unpublished rewards notified again")
	}

	// Publication is detected normally when the program changes.
	h.fixture.mu.Lock()
	h.fixture.rewardsChanged = true
	h.fixture.mu.Unlock()
	h.run(0, "run", "--only", "standoff")
	assertHealthy()
	if !strings.Contains(h.fixture.lastMessage(), "140 000") {
		t.Fatal("published rewards were not detected", h.fixture.lastMessage())
	}

	// Returning to null is not proof that previously published amounts became zero.
	h.fixture.mu.Lock()
	h.fixture.rewardsChanged = false
	h.fixture.mu.Unlock()
	before = h.fixture.messageCount()
	h.run(0, "run", "--only", "standoff")
	assertHealthy()
	if h.fixture.messageCount() != before {
		t.Fatal("null erased known rewards or generated a false change", h.fixture.lastMessage())
	}
	if text := h.run(0, "export"); !strings.Contains(text, "140000") {
		t.Fatal("null erased previously known rewards", text)
	}
}
