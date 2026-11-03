package standoff

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/khunzzz/scope-watch/internal/model"
)

type pageClient interface {
	NextData(context.Context, string, url.Values, any) error
}

func (a *Adapter) list(ctx context.Context) ([]programData, error) {
	seen := map[string]bool{}
	out := []programData{}
	var total *int
	for n := 1; n <= 50; n++ {
		var p page
		if err := a.client.NextData(ctx, a.base+"/programs", url.Values{"page": {strconv.Itoa(n)}}, &p); err != nil {
			return nil, fmt.Errorf("list page %d: %w", n, err)
		}
		if p.Programs == nil {
			return nil, fmt.Errorf("missing programs on page %d", n)
		}
		if p.Total != nil {
			if *p.Total < 0 || (total != nil && *p.Total != *total) {
				return nil, fmt.Errorf("inconsistent page count on page %d", n)
			}
			total = p.Total
		}
		if len(p.Programs) == 0 {
			if total != nil && n <= *total {
				return nil, fmt.Errorf("empty page %d of %d", n, *total)
			}
			return out, nil
		}
		if total != nil && *total < n {
			return nil, fmt.Errorf("unexpected programs beyond page count on page %d", n)
		}
		for _, r := range p.Programs {
			if r.Slug == "" {
				return nil, fmt.Errorf("missing slug on page %d", n)
			}
			if seen[r.Slug] {
				return nil, fmt.Errorf("duplicate slug %q on page %d", r.Slug, n)
			}
			seen[r.Slug] = true
			if r.Visibility == "" || r.Visibility == "public" {
				out = append(out, r)
			}
		}
		if total != nil && n == *total {
			return out, nil
		}
	}
	return nil, fmt.Errorf("standoff pagination exceeds 50 pages")
}

func (a *Adapter) loadRewards(ctx context.Context, slug string) (map[string]rewardData, error) {
	var page struct{ Reward json.RawMessage }
	if err := a.client.NextData(ctx, a.base+"/programs/"+url.PathEscape(slug), nil, &page); err != nil {
		return nil, err
	}
	if len(page.Reward) == 0 {
		return nil, fmt.Errorf("missing reward field for %q", slug)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(page.Reward, &fields); err != nil {
		return nil, fmt.Errorf("rewards for %q: %w", slug, err)
	}
	if fields == nil {
		return nil, fmt.Errorf("null reward field for %q", slug)
	}
	result := make(map[string]rewardData)
	for _, severity := range model.Severities {
		if raw, ok := fields[severity]; ok {
			var reward rewardData
			if err := json.Unmarshal(raw, &reward); err != nil {
				return nil, fmt.Errorf("%s reward for %q: %w", severity, slug, err)
			}
			result[severity] = reward
		}
	}
	return result, nil
}
