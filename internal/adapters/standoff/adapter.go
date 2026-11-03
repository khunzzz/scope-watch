// Package standoff adapts public Standoff 365 Next.js pages to scope snapshots.
package standoff

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/khunzzz/scope-watch/internal/model"
)

const rewardTTL = 6 * time.Hour

type Adapter struct {
	client pageClient
	base   string
	now    func() time.Time
}

func New(client pageClient) *Adapter { return &Adapter{client: client, base: baseURL, now: time.Now} }
func (*Adapter) Name() string        { return Name }
func (*Adapter) Title() string       { return Title }
func (a *Adapter) Fetch(ctx context.Context, prev map[string]model.Program) (model.Fetched, error) {
	items, err := a.list(ctx)
	if err != nil {
		return model.Fetched{}, err
	}
	out := model.Fetched{Total: len(items), Programs: []model.Program{}, Failed: []string{}}
	now := float64(a.now().UnixNano()) / 1e9
	for _, raw := range items {
		if err := ctx.Err(); err != nil {
			return model.Fetched{}, err
		}
		p, err := parse(raw)
		if err != nil {
			return model.Fetched{}, err
		}
		old, exists := prev[p.Slug]
		stale := !exists || old.UpdatedAt != p.UpdatedAt || now-old.MetaNumber("rewards_at") > rewardTTL.Seconds()
		loaded := false
		if stale && p.Active {
			var page struct{ Reward map[string]rewardData }
			err := a.client.NextData(ctx, a.base+"/programs/"+url.PathEscape(p.Slug), nil, &page)
			if err == nil {
				p.Rewards = rewards(page.Reward)
				p.Meta["rewards_at"] = now
				loaded = true
			} else {
				if ctx.Err() != nil {
					return model.Fetched{}, ctx.Err()
				}
				out.Failed = append(out.Failed, p.Slug)
			}
		}
		if !loaded && exists {
			p.Rewards = old.Clone().Rewards
			p.Meta["rewards_at"] = old.MetaNumber("rewards_at")
		}
		out.Programs = append(out.Programs, p)
	}
	if len(items) == 0 {
		return out, fmt.Errorf("standoff returned no public programs")
	}
	return out, nil
}
