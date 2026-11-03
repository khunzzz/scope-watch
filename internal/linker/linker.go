// Package linker matches companies across platforms by weighted shared assets.
package linker

import (
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/khunzzz/scope-watch/internal/assets"
	"github.com/khunzzz/scope-watch/internal/model"
)

var slugParts = regexp.MustCompile(`[^a-z0-9]+`)

func SlugKey(slug string) string {
	parts := []string{}
	noise := strings.Fields("vk gov fgis gis bug bounty bb new ru program")
	for _, p := range slugParts.Split(strings.ToLower(slug), -1) {
		if p != "" && !slices.Contains(noise, p) {
			parts = append(parts, p)
		}
	}
	key := strings.Join(parts, "")
	if strings.HasPrefix(key, "vk") && len(key) > 4 {
		return key[2:]
	}
	return key
}

type Link struct {
	A, B   string
	Score  float64
	Shared []string
}
type Linker struct {
	ByKey  map[string]model.Program
	Links  []Link
	peers  map[string][]string
	tokens map[string]map[string]bool
	weight map[string]float64
}

func New(programs []model.Program) *Linker {
	l := &Linker{ByKey: map[string]model.Program{}, peers: map[string][]string{}, tokens: map[string]map[string]bool{}, weight: map[string]float64{}}
	df := map[string]int{}
	platforms := []string{}
	programs = append([]model.Program{}, programs...)
	sort.SliceStable(programs, func(i, j int) bool { return programs[i].Key() < programs[j].Key() })
	for _, p := range programs {
		l.ByKey[p.Key()] = p
		l.tokens[p.Key()] = assets.LinkTokens(p.Assets)
		platforms = append(platforms, p.Platform)
		for t := range l.tokens[p.Key()] {
			df[t]++
		}
	}
	for t, n := range df {
		l.weight[t] = 1 / float64(n)
	}
	slices.Sort(platforms)
	platforms = slices.Compact(platforms)
	for i, pa := range platforms {
		for _, pb := range platforms[i+1:] {
			a, b := []model.Program{}, []model.Program{}
			for _, p := range programs {
				if p.Platform == pa {
					a = append(a, p)
				}
				if p.Platform == pb {
					b = append(b, p)
				}
			}
			scores := map[[2]string]Link{}
			bestA, bestB := map[string]string{}, map[string]string{}
			for _, x := range a {
				best := -1.0
				for _, y := range b {
					s, shared := l.Score(x, y)
					scores[[2]string{x.Key(), y.Key()}] = Link{x.Key(), y.Key(), s, shared}
					if s > best {
						best = s
						bestA[x.Key()] = y.Key()
					}
				}
			}
			for _, y := range b {
				best := -1.0
				for _, x := range a {
					if s := scores[[2]string{x.Key(), y.Key()}].Score; s > best {
						best = s
						bestB[y.Key()] = x.Key()
					}
				}
			}
			for _, x := range a {
				bk := bestA[x.Key()]
				link := scores[[2]string{x.Key(), bk}]
				if link.Score >= .6 && bestB[bk] == x.Key() {
					link.Score = math.Round(link.Score*1000) / 1000
					l.Links = append(l.Links, link)
					l.peers[link.A] = append(l.peers[link.A], link.B)
					l.peers[link.B] = append(l.peers[link.B], link.A)
				}
			}
		}
	}
	return l
}
func (l *Linker) Score(a, b model.Program) (float64, []string) {
	shared := []string{}
	for t := range l.tokens[a.Key()] {
		if l.tokens[b.Key()][t] {
			shared = append(shared, t)
		}
	}
	sort.Slice(shared, func(i, j int) bool {
		if l.weight[shared[i]] == l.weight[shared[j]] {
			return shared[i] < shared[j]
		}
		return l.weight[shared[i]] > l.weight[shared[j]]
	})
	score := 0.0
	for _, t := range shared {
		score += l.weight[t]
	}
	if ka := SlugKey(a.Slug); ka != "" && ka == SlugKey(b.Slug) {
		score++
	}
	return score, shared
}
func (l *Linker) Peers(p model.Program) []model.Program {
	out := []model.Program{}
	for _, key := range l.peers[p.Key()] {
		out = append(out, l.ByKey[key])
	}
	return out
}
func (l *Linker) GroupKey(p model.Program) string {
	key := p.Key()
	for _, s := range l.peers[key] {
		if s < key {
			key = s
		}
	}
	return key
}
