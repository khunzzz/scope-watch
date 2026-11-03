package bizone

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/khunzzz/scope-watch/internal/assets"
	"github.com/khunzzz/scope-watch/internal/model"
)

const Name = "bizone"
const Title = "BI.ZONE"
const baseURL = "https://bugbounty.bi.zone"

type programData struct {
	Slug                  string `json:"slug"`
	Name                  string `json:"name"`
	Description           string `json:"description"`
	Rules                 string `json:"rules"`
	Public                *bool  `json:"public"`
	IsActive              *bool  `json:"isActive"`
	MinLowPrice           int    `json:"minLowPrice"`
	MaxLowPrice           int    `json:"maxLowPrice"`
	MinMediumPrice        int    `json:"minMediumPrice"`
	MaxMediumPrice        int    `json:"maxMediumPrice"`
	MinHighPrice          int    `json:"minHighPrice"`
	MaxHighPrice          int    `json:"maxHighPrice"`
	MinCriticalPrice      int    `json:"minCriticalPrice"`
	MaxCriticalPrice      int    `json:"maxCriticalPrice"`
	ModificationDateRules string `json:"modificationDateRules"`
	ModificationDatePrice string `json:"modificationDatePrice"`
}
type taskData struct {
	ID          int    `json:"id"`
	TextTask    string `json:"textTask"`
	Description string `json:"description"`
	InScope     *bool  `json:"inScope"`
	Eligibility *bool  `json:"eligibility"`
	InArchive   bool   `json:"inArchive"`
	Company     *struct {
		Slug string `json:"slug"`
	} `json:"company"`
}
type versionData struct {
	Type    string `json:"type"`
	Version any    `json:"version"`
}

func defaultTrue(b *bool) bool { return b == nil || *b }
func taskAssets(t taskData) []string {
	title := strings.Join(strings.Fields(t.TextTask), " ")
	found := assets.Extract(title+"\n"+t.Description, nil)
	if len(found) == 0 && title != "" {
		r := []rune(title)
		found = []string{string(r[:min(120, len(r))])}
	}
	if defaultTrue(t.InScope) && !defaultTrue(t.Eligibility) {
		for i := range found {
			found[i] += model.NoBounty
		}
	}
	return found
}
func unique(items []string) []string { slices.Sort(items); return slices.Compact(items) }
func parse(d programData, versions []versionData, tasks []taskData) (model.Program, error) {
	if d.Slug == "" {
		return model.Program{}, fmt.Errorf("BI.ZONE program has no slug")
	}
	live := []taskData{}
	for _, t := range tasks {
		if !t.InArchive {
			live = append(live, t)
		}
	}
	sort.SliceStable(live, func(i, j int) bool {
		if defaultTrue(live[i].InScope) != defaultTrue(live[j].InScope) {
			return defaultTrue(live[i].InScope)
		}
		return live[i].ID < live[j].ID
	})
	prose := []string{}
	for _, s := range []string{d.Description, d.Rules} {
		if s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")); s != "" {
			prose = append(prose, s)
		}
	}
	scope := strings.Join(prose, "\n\n")
	inside, outside := []string{}, []string{}
	source := "text"
	blocks := []string{}
	for _, t := range live {
		flag := ""
		if defaultTrue(t.InScope) {
			inside = append(inside, taskAssets(t)...)
		} else {
			flag = " [out of scope]"
			outside = append(outside, taskAssets(t)...)
		}
		desc := strings.TrimSpace(strings.ReplaceAll(t.Description, "\r\n", "\n"))
		blocks = append(blocks, strings.TrimRight("### "+strings.TrimSpace(t.TextTask)+flag+"\n"+desc, " \n\r\t"))
	}
	if len(live) > 0 {
		source = "structured"
		inside = unique(inside)
		outside = unique(outside)
		taskMD := "## Скоуп (задачи BI.ZONE)\n\n" + strings.Join(blocks, "\n\n")
		if scope != "" {
			scope = taskMD + "\n\n" + scope
		} else {
			scope = taskMD
		}
	} else {
		exclude := []string{"bi.zone"}
		if d.Slug == "bi-zone-bug-bounty" {
			exclude = nil
		}
		inside, outside = assets.SplitScope(scope, exclude)
	}
	rs := map[string][]int{}
	for s, r := range map[string][]int{
		"low":      {d.MinLowPrice, d.MaxLowPrice},
		"medium":   {d.MinMediumPrice, d.MaxMediumPrice},
		"high":     {d.MinHighPrice, d.MaxHighPrice},
		"critical": {d.MinCriticalPrice, d.MaxCriticalPrice},
	} {
		if r[1] != 0 {
			rs[s] = r
		}
	}
	vers := []string{}
	for _, v := range versions {
		vers = append(vers, fmt.Sprint(v.Type, ":", v.Version))
	}
	vers = unique(vers)
	sort.SliceStable(vers, func(i, j int) bool {
		a, an, _ := strings.Cut(vers[i], ":")
		b, bn, _ := strings.Cut(vers[j], ":")
		if a != b {
			return a < b
		}
		x, _ := strconv.Atoi(an)
		y, _ := strconv.Atoi(bn)
		return x < y
	})
	name := d.Name
	if name == "" {
		name = d.Slug
	}
	active := defaultTrue(d.IsActive)
	status := "inactive"
	if active {
		status = "active"
	}
	return model.Program{
		Platform:     Name,
		Slug:         d.Slug,
		Name:         name,
		URL:          baseURL + "/companies/" + url.PathEscape(d.Slug),
		Active:       active,
		Status:       status,
		Rewards:      rs,
		Assets:       inside,
		OutOfScope:   outside,
		AssetsSource: source,
		ScopeMD:      scope,
		Versions:     vers,
		UpdatedAt:    max(d.ModificationDateRules, d.ModificationDatePrice),
		Meta:         map[string]any{"extractor": assets.Version},
	}, nil
}
