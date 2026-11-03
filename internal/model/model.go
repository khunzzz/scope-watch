// Package model defines the platform-independent snapshots stored by ScopeWatch.
package model

import "encoding/json"

const NoBounty = " (без выплат)"

var Severities = []string{"low", "medium", "high", "critical"}

type Program struct {
	Platform string `json:"platform"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Active   bool   `json:"active"`
	Status   string `json:"status"`
	// Rewards is nil until loaded; a non-nil empty map means no payouts.
	Rewards      map[string][]int `json:"rewards"`
	Assets       []string         `json:"assets"`
	OutOfScope   []string         `json:"out_of_scope"`
	AssetsSource string           `json:"assets_source"`
	ScopeMD      string           `json:"scope_md"`
	Versions     []string         `json:"versions"`
	UpdatedAt    string           `json:"updated_at"`
	Meta         map[string]any   `json:"meta"`
}

func (p Program) Key() string { return p.Platform + "/" + p.Slug }
func (p Program) MaxReward() int {
	n := 0
	for _, r := range p.Rewards {
		if len(r) == 2 && r[1] > n {
			n = r[1]
		}
	}
	return n
}
func (p Program) Clone() Program {
	p.Assets = append([]string{}, p.Assets...)
	p.OutOfScope = append([]string{}, p.OutOfScope...)
	p.Versions = append([]string{}, p.Versions...)
	if p.Rewards != nil {
		rewards := make(map[string][]int, len(p.Rewards))
		for k, v := range p.Rewards {
			rewards[k] = append([]int{}, v...)
		}
		p.Rewards = rewards
	}
	meta := make(map[string]any, len(p.Meta))
	for k, v := range p.Meta {
		meta[k] = v
	}
	p.Meta = meta
	return p
}
func (p Program) MetaNumber(key string) float64 {
	switch v := p.Meta[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	}
	return 0
}

type Fetched struct {
	Programs []Program
	Total    int
	Failed   []string
}

type RewardDelta struct {
	Severity string `json:"severity"`
	Before   []int  `json:"before"`
	After    []int  `json:"after"`
}

type Change struct {
	Kind          string        `json:"kind"`
	Program       Program       `json:"-"`
	Old           *Program      `json:"-"`
	Activated     *bool         `json:"activated"`
	RenamedFrom   string        `json:"renamed_from"`
	AssetsAdded   []string      `json:"assets_added"`
	AssetsRemoved []string      `json:"assets_removed"`
	BountyLost    []string      `json:"bounty_lost"`
	BountyGained  []string      `json:"bounty_gained"`
	OOSAdded      []string      `json:"oos_added"`
	OOSRemoved    []string      `json:"oos_removed"`
	Rewards       []RewardDelta `json:"rewards"`
	NewVersions   []string      `json:"new_versions"`
	TextDiff      string        `json:"text_diff"`
}

func (c Change) Empty() bool {
	return c.Kind == "updated" && c.Activated == nil && c.RenamedFrom == "" && len(c.AssetsAdded)+len(c.AssetsRemoved)+len(c.BountyLost)+len(c.BountyGained)+len(c.OOSAdded)+len(c.OOSRemoved)+len(c.Rewards)+len(c.NewVersions) == 0 && c.TextDiff == ""
}

type Health struct {
	Fails     int
	Alerted   bool
	LastError string
}
