package diff

import (
	"github.com/khunzzz/scope-watch/internal/model"
	"github.com/khunzzz/scope-watch/internal/testutil"
	"reflect"
	"strings"
	"testing"
)

func TestChanges(t *testing.T) {
	base := testutil.Programs(t)["standoff/flowwow"]
	tests := []struct {
		name  string
		edit  func(*model.Program)
		check func(*testing.T, model.Change)
	}{
		{"unchanged", func(p *model.Program) {}, func(t *testing.T, c model.Change) {
			if !c.Empty() {
				t.Fatal(c)
			}
		}},
		{"assets", func(p *model.Program) { p.Assets = append(p.Assets, "*.new.flowwow.com") }, func(t *testing.T, c model.Change) {
			if !reflect.DeepEqual(c.AssetsAdded, []string{"*.new.flowwow.com"}) {
				t.Fatal(c)
			}
		}},
		{"source-switch", func(p *model.Program) { p.Assets = []string{"other.corp.ru"}; p.AssetsSource = "structured" }, func(t *testing.T, c model.Change) {
			if !c.Empty() {
				t.Fatal(c)
			}
		}},
		{"extractor-upgrade", func(p *model.Program) { p.Assets = []string{"other.corp.ru"}; p.Meta["extractor"] = 999 }, func(t *testing.T, c model.Change) {
			if !c.Empty() {
				t.Fatal(c)
			}
		}},
		{"closed", func(p *model.Program) { p.Active = false }, func(t *testing.T, c model.Change) {
			if c.Activated == nil || *c.Activated {
				t.Fatal(c)
			}
		}},
		{"renamed", func(p *model.Program) { p.Name = "new" }, func(t *testing.T, c model.Change) {
			if c.RenamedFrom == "" {
				t.Fatal(c)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { p := base.Clone(); tt.edit(&p); tt.check(t, Compare(base, p)) })
	}
}
func TestRewardsAndBounty(t *testing.T) {
	old := testutil.Programs(t)["standoff/flowwow"]
	old.Rewards = map[string][]int{"critical": {50000, 70000}}
	old.Assets = []string{"a.corp.ru", "b.corp.ru" + model.NoBounty, "c.corp.ru"}
	p := old.Clone()
	p.Rewards["critical"] = []int{50000, 150000}
	p.Assets = []string{"a.corp.ru" + model.NoBounty, "b.corp.ru", "d.corp.ru"}
	c := Compare(old, p)
	if !reflect.DeepEqual(c.BountyLost, []string{"a.corp.ru"}) || !reflect.DeepEqual(c.BountyGained, []string{"b.corp.ru"}) || !reflect.DeepEqual(c.AssetsAdded, []string{"d.corp.ru"}) || len(c.Rewards) != 1 {
		t.Fatal(c)
	}
	p = old.Clone()
	p.Rewards = nil
	if !Compare(old, p).Empty() {
		t.Fatal("unloaded rewards changed")
	}
}
func TestMissing(t *testing.T) {
	p := testutil.Programs(t)["standoff/flowwow"]
	old := map[string]model.Program{p.Slug: p}
	carried := CarryMissing(old, nil, 2)
	if len(carried) != 1 || carried[0].MetaNumber("missing") != 1 || p.MetaNumber("missing") != 0 {
		t.Fatal(carried)
	}
	if len(Platform(old, carried)) != 0 {
		t.Fatal("early removal")
	}
	if len(CarryMissing(map[string]model.Program{p.Slug: carried[0]}, nil, 2)) != 0 {
		t.Fatal("not removed")
	}
	changes := Platform(old, nil)
	if len(changes) != 1 || changes[0].Kind != "removed" {
		t.Fatal(changes)
	}
}
func TestTextDiff(t *testing.T) {
	for _, tt := range []struct{ old, new, want string }{{"-- x", "++ y", "--- x\n+++ y"}, {"a\nb", "a\nc", "-b\n+c"}, {"a\n\n", "a\n", ""}} {
		if got := Text(tt.old, tt.new, 1500); got != tt.want {
			t.Errorf("%q != %q", got, tt.want)
		}
	}
	if got := Text(strings.Repeat("old\n", 100), strings.Repeat("new\n", 100), 50); !strings.Contains(got, "ещё") {
		t.Fatal(got)
	}
}
