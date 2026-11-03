// Package standoff adapts public Standoff 365 pages to program snapshots.
package standoff

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/khunzzz/scope-watch/internal/scope"
)

const rewardTTL = 6 * time.Hour

type Adapter struct {
	client *http.Client
	base   string
	now    func() time.Time
}

func New(client *http.Client) *Adapter { return &Adapter{client: client, base: baseURL, now: time.Now} }

func (*Adapter) Name() string { return Name }

func (*Adapter) Title() string { return Title }

func (a *Adapter) Fetch(ctx context.Context, prev map[string]scope.Program) (scope.Fetched, error) {
	items, err := a.list(ctx)
	if err != nil {
		return scope.Fetched{}, err
	}
	out := scope.Fetched{Total: len(items), Programs: []scope.Program{}, Failed: []string{}}
	now := float64(a.now().UnixNano()) / 1e9
	for _, raw := range items {
		if err := ctx.Err(); err != nil {
			return scope.Fetched{}, err
		}
		p, err := parse(raw)
		if err != nil {
			return scope.Fetched{}, err
		}
		old, exists := prev[p.Slug]
		stale := !exists || old.Rewards == nil || old.UpdatedAt != p.UpdatedAt || now-old.MetaNumber("rewards_at") > rewardTTL.Seconds()
		loaded := false
		if stale && p.Active {
			reward, err := a.loadRewards(ctx, p.Slug)
			if err == nil {
				p.Rewards = rewards(reward)
				p.Meta["rewards_at"] = now
				loaded = true
			} else {
				if ctx.Err() != nil {
					return scope.Fetched{}, ctx.Err()
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
