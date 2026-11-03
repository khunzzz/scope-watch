package standoff

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

type pageClient interface {
	NextData(context.Context, string, url.Values, any) error
}
type page struct {
	Programs []programData
	Total    *int
}

func (a *Adapter) list(ctx context.Context) ([]programData, error) {
	seen := map[string]bool{}
	out := []programData{}
	total := 0
	for n := 1; n <= 50; n++ {
		var p page
		if err := a.client.NextData(ctx, a.base+"/programs", url.Values{"page": {strconv.Itoa(n)}}, &p); err != nil {
			return nil, fmt.Errorf("list page %d: %w", n, err)
		}
		if p.Total != nil && total == 0 {
			total = *p.Total
		}
		if len(p.Programs) == 0 {
			if total > 0 && n <= total {
				return nil, fmt.Errorf("empty page %d of %d", n, total)
			}
			return out, nil
		}
		for _, r := range p.Programs {
			if r.Slug == "" {
				return nil, fmt.Errorf("missing slug on page %d", n)
			}
			if (r.Visibility == "" || r.Visibility == "public") && !seen[r.Slug] {
				seen[r.Slug] = true
				out = append(out, r)
			}
		}
		if total > 0 && n >= total {
			return out, nil
		}
	}
	return nil, fmt.Errorf("standoff pagination exceeds 50 pages")
}
