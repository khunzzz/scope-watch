// Package diff compares snapshots without performing I/O.
package diff

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/khunzzz/scope-watch/internal/model"
	"github.com/pmezard/go-difflib/difflib"
)

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
func Text(old, new string, budget int) string {
	lines := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n"), "\n")
	}
	a, b := lines(old), lines(new)
	m := difflib.NewMatcher(a, b)
	changes := []string{}
	for _, op := range m.GetOpCodes() {
		if op.Tag == 'e' {
			continue
		}
		if op.Tag == 'd' || op.Tag == 'r' {
			for _, s := range a[op.I1:op.I2] {
				if strings.TrimSpace(s) != "" {
					changes = append(changes, truncate("-"+s, 220))
				}
			}
		}
		if op.Tag == 'i' || op.Tag == 'r' {
			for _, s := range b[op.J1:op.J2] {
				if strings.TrimSpace(s) != "" {
					changes = append(changes, truncate("+"+s, 220))
				}
			}
		}
	}
	out := []string{}
	used := 0
	for i, s := range changes {
		n := len([]rune(s)) + 1
		if used+n > budget {
			out = append(out, fmt.Sprintf("…ещё %d строк", len(changes)-i))
			break
		}
		out = append(out, s)
		used += n
	}
	return strings.Join(out, "\n")
}
func difference(a, b []string) []string {
	other := map[string]bool{}
	for _, s := range b {
		other[s] = true
	}
	set := map[string]bool{}
	for _, s := range a {
		if !other[s] {
			set[s] = true
		}
	}
	out := []string{}
	for s := range set {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}
func Compare(old, new model.Program) model.Change {
	ch := model.Change{Kind: "updated", Program: new, Old: &old}
	if old.Active != new.Active {
		v := new.Active
		ch.Activated = &v
	}
	if old.Name != new.Name {
		ch.RenamedFrom = old.Name
	}
	if old.AssetsSource == new.AssetsSource && old.MetaNumber("extractor") == new.MetaNumber("extractor") {
		added, removed := difference(new.Assets, old.Assets), difference(old.Assets, new.Assets)
		skipAdded, skipRemoved := map[string]bool{}, map[string]bool{}
		for _, a := range removed {
			if slices.Contains(added, a+model.NoBounty) {
				ch.BountyLost = append(ch.BountyLost, a)
				skipAdded[a+model.NoBounty] = true
				skipRemoved[a] = true
			}
			if strings.HasSuffix(a, model.NoBounty) {
				bare := strings.TrimSuffix(a, model.NoBounty)
				if slices.Contains(added, bare) {
					ch.BountyGained = append(ch.BountyGained, bare)
					skipAdded[bare] = true
					skipRemoved[a] = true
				}
			}
		}
		for _, a := range added {
			if !skipAdded[a] {
				ch.AssetsAdded = append(ch.AssetsAdded, a)
			}
		}
		for _, a := range removed {
			if !skipRemoved[a] {
				ch.AssetsRemoved = append(ch.AssetsRemoved, a)
			}
		}
		ch.OOSAdded = difference(new.OutOfScope, old.OutOfScope)
		ch.OOSRemoved = difference(old.OutOfScope, new.OutOfScope)
	}
	if len(old.Rewards) > 0 && len(new.Rewards) > 0 {
		for _, s := range model.Severities {
			if !reflect.DeepEqual(old.Rewards[s], new.Rewards[s]) {
				ch.Rewards = append(ch.Rewards, model.RewardDelta{Severity: s, Before: old.Rewards[s], After: new.Rewards[s]})
			}
		}
	}
	if len(old.Versions) > 0 {
		for _, v := range new.Versions {
			if !slices.Contains(old.Versions, v) {
				ch.NewVersions = append(ch.NewVersions, v)
			}
		}
	}
	if old.ScopeMD != new.ScopeMD {
		ch.TextDiff = Text(old.ScopeMD, new.ScopeMD, 1500)
	}
	return ch
}
func CarryMissing(old map[string]model.Program, current []model.Program, runs int) []model.Program {
	present := map[string]bool{}
	out := append([]model.Program{}, current...)
	for _, p := range current {
		present[p.Slug] = true
	}
	keys := []string{}
	for s := range old {
		keys = append(keys, s)
	}
	slices.Sort(keys)
	for _, s := range keys {
		p := old[s]
		if present[s] {
			continue
		}
		misses := int(p.MetaNumber("missing")) + 1
		if misses < runs {
			p = p.Clone()
			p.Meta["missing"] = misses
			out = append(out, p)
		}
	}
	return out
}
func Platform(old map[string]model.Program, current []model.Program) []model.Change {
	out := []model.Change{}
	seen := map[string]bool{}
	for _, p := range current {
		seen[p.Slug] = true
		if o, ok := old[p.Slug]; !ok {
			if p.Active {
				out = append(out, model.Change{Kind: "new", Program: p})
			}
		} else {
			ch := Compare(o, p)
			if !ch.Empty() {
				out = append(out, ch)
			}
		}
	}
	keys := []string{}
	for s := range old {
		keys = append(keys, s)
	}
	slices.Sort(keys)
	for _, s := range keys {
		p := old[s]
		if !seen[s] && p.Active {
			out = append(out, model.Change{Kind: "removed", Program: p, Old: &p})
		}
	}
	return out
}
