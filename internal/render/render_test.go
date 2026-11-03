package render

import (
	"github.com/khunzzz/scope-watch/internal/diff"
	"github.com/khunzzz/scope-watch/internal/linker"
	"github.com/khunzzz/scope-watch/internal/model"
	"github.com/khunzzz/scope-watch/internal/testutil"
	"strings"
	"testing"
)

func renderer() *Renderer {
	return New(map[string]string{"standoff": "Standoff 365", "bizone": "BI.ZONE"})
}
func TestLinkedChanges(t *testing.T) {
	ps := testutil.Programs(t)
	a, b := ps["standoff/tbank"], ps["bizone/tbank"]
	an, bn := a.Clone(), b.Clone()
	an.Assets = append(an.Assets, "fresh.tinkoff.ru")
	bn.Assets = append(bn.Assets, "fresh.tinkoff.ru")
	lk := linker.New([]model.Program{an, bn})
	if len(lk.Peers(an)) != 1 {
		t.Fatal("not linked")
	}
	changes := []model.Change{diff.Compare(a, an), diff.Compare(b, bn)}
	msgs := renderer().Render(changes, lk)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "Standoff 365") || !strings.Contains(msgs[0], "BI.ZONE") {
		t.Fatal(msgs)
	}
	if Units(msgs[0]) > MessageMax {
		t.Fatal("oversized")
	}
}
func TestPeerUsesPreviousState(t *testing.T) {
	ps := testutil.Programs(t)
	a, b := ps["standoff/tbank"], ps["bizone/tbank"]
	a.Assets = []string{"stable.corp.ru"}
	b.Assets = []string{"stable.corp.ru"}
	an, bn := a.Clone(), b.Clone()
	an.Assets = append(an.Assets, "fresh.corp.ru")
	bn.Assets = append(bn.Assets, "fresh.corp.ru")
	msgs := renderer().Render([]model.Change{diff.Compare(a, an), diff.Compare(b, bn)}, linker.New([]model.Program{an, bn}))
	if strings.Contains(strings.Join(msgs, ""), "уже были") {
		t.Fatal("same-run additions marked as known")
	}
}
func TestReopened(t *testing.T) {
	p := testutil.Programs(t)["standoff/flowwow"]
	p.Active = false
	p.Rewards = map[string][]int{"critical": {50000, 70000}}
	n := p.Clone()
	n.Active = true
	n.Assets = append(n.Assets, "*.fresh.flowwow.com")
	msgs := renderer().Render([]model.Change{diff.Compare(p, n)}, linker.New([]model.Program{n}))
	text := strings.Join(msgs, "")
	for _, s := range []string{"♻️", "crit 50 000–70 000", "*.fresh.flowwow.com"} {
		if !strings.Contains(text, s) {
			t.Error(s)
		}
	}
	if strings.Contains(text, "Новые ассеты") {
		t.Fatal("duplicated assets")
	}
}
func TestEscapingAndBounds(t *testing.T) {
	p := testutil.Programs(t)["standoff/flowwow"]
	p.Name = "<bad>&"
	p.Assets = []string{strings.Repeat("<&😀", 3000)}
	old := p.Clone()
	p.ScopeMD = strings.Repeat("<&😀\n", 1000)
	c := diff.Compare(old, p)
	c.AssetsAdded = []string{strings.Repeat("<&😀", 2000)}
	msgs := renderer().Render([]model.Change{c}, linker.New([]model.Program{p}))
	for _, m := range msgs {
		if Units(m) > MessageMax || strings.Count(m, "<pre>") != strings.Count(m, "</pre>") || strings.Contains(m, "<bad>") {
			t.Fatalf("invalid message: units=%d", Units(m))
		}
	}
}
