//go:build unix

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMixedHTMLRulesE2E(t *testing.T) {
	h := newHarness(t)
	setRules := func(text string) {
		t.Helper()
		h.fixture.mu.Lock()
		defer h.fixture.mu.Unlock()
		var p map[string]any
		if err := json.Unmarshal(h.fixture.standoff["flowwow"], &p); err != nil {
			t.Fatal(err)
		}
		p["description"] = text
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		h.fixture.standoff["flowwow"] = raw
	}
	old := "# Rules\n<style>.policy {color: red}</style><table style=\"" + strings.Repeat("padding: 1px;", 80) + "\">" + strings.Repeat("<tr><td>Manual eligibility</td><td>Allowed</td></tr>", 80) + "</table>"
	setRules(old)
	h.run(0, "run", "--only", "standoff")
	before := h.fixture.messageCount()
	setRules(strings.ReplaceAll(old, "color: red", "color: blue"))
	h.run(0, "run", "--only", "standoff")
	if h.fixture.messageCount() != before {
		t.Fatal("formatting-only edit notified", h.fixture.lastMessage())
	}
	setRules("# Rules\n<p>Only assets in Tasks are eligible.</p><p>Maximum <b>10</b> requests &amp; no destructive testing.</p>")
	h.run(0, "run", "--only", "standoff")
	if h.fixture.messageCount() != before+1 {
		t.Fatalf("want one change notification, got %d", h.fixture.messageCount()-before)
	}
	got := h.fixture.lastMessage()
	for _, want := range []string{"-Manual eligibility | Allowed", "+Only assets in Tasks are eligible.", "+Maximum 10 requests &amp; no destructive testing."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	for _, bad := range []string{"padding:", "color:", "&lt;table", "&lt;td", "&lt;p", "Новые ассеты", "Убраны ассеты"} {
		if strings.Contains(got, bad) {
			t.Errorf("unexpected %q in %s", bad, got)
		}
	}
	h.run(0, "run", "--only", "standoff")
	if h.fixture.messageCount() != before+1 {
		t.Fatal("unchanged rules notified twice")
	}
}
