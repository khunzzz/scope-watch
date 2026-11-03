package app

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/khunzzz/scope-watch/internal/notify/telegram"
	"github.com/khunzzz/scope-watch/internal/scope"
	"github.com/khunzzz/scope-watch/internal/storage"
)

// Adapter returns normalized public programs without modifying the previous snapshot.
// A list/scope failure must return an error; isolated detail failures belong in Failed.
type Adapter interface {
	Name() string
	Title() string
	Fetch(context.Context, map[string]scope.Program) (scope.Fetched, error)
}

type Notifier interface {
	Send(context.Context, string) error
}

type Runner struct {
	settings Settings
	adapters map[string]Adapter
	names    []string
	render   *telegram.Renderer
	log      *slog.Logger
}

func newRunner(settings Settings, adapters []Adapter, log *slog.Logger) (*Runner, error) {
	r := &Runner{settings: settings, adapters: map[string]Adapter{}, log: log}
	if log == nil {
		r.log = slog.Default()
	}
	titles := map[string]string{}
	for _, a := range adapters {
		if a == nil || a.Name() == "" {
			return nil, fmt.Errorf("adapter requires a name")
		}
		if _, ok := r.adapters[a.Name()]; ok {
			return nil, fmt.Errorf("duplicate adapter %q", a.Name())
		}
		r.names = append(r.names, a.Name())
		r.adapters[a.Name()] = a
		titles[a.Name()] = a.Title()
	}
	r.render = telegram.NewRenderer(titles)
	return r, nil
}

func (r *Runner) Names() []string { return append([]string{}, r.names...) }

func (r *Runner) Titles() map[string]string {
	out := map[string]string{}
	for n, a := range r.adapters {
		out[n] = a.Title()
	}
	return out
}

type Options struct {
	Platforms    []string
	Seed, DryRun bool
}

type Result struct {
	Messages        []string
	Changes         []scope.Change
	Seeded          []string
	Unsent          int
	FailedPlatforms []string
}

type collected struct {
	prev    map[string]scope.Program
	current []scope.Program
	err     error
}

func sortedPrograms(m map[string]scope.Program) []scope.Program {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := []scope.Program{}
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

func (r *Runner) Run(ctx context.Context, n Notifier, opt Options) (result Result, err error) {
	platforms := opt.Platforms
	if len(platforms) == 0 {
		platforms = r.names
	}
	seen := map[string]bool{}
	names := []string{}
	for _, name := range platforms {
		if _, ok := r.adapters[name]; !ok {
			return result, fmt.Errorf("unknown platform %q", name)
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if !opt.DryRun {
		lock, e := acquireRunLock(r.settings.DataDir)
		if e != nil {
			return result, e
		}
		defer func() {
			if e := lock.Close(); err == nil {
				err = e
			}
		}()
	}
	store, err := storage.Open(ctx, r.settings.DataDir)
	if err != nil {
		return result, err
	}
	defer func() {
		if e := store.Close(); err == nil {
			err = e
		}
	}()
	all := map[string]map[string]scope.Program{}
	for _, name := range r.names {
		all[name], err = store.Load(ctx, name)
		if err != nil {
			return result, err
		}
	}
	collectedBy := map[string]collected{}
	seeding := map[string]bool{}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		c := collected{prev: all[name]}
		f, e := r.adapters[name].Fetch(ctx, c.prev)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if e == nil {
			if f.Total < len(f.Programs) || f.Total < 0 {
				e = fmt.Errorf("invalid program count: %d", f.Total)
			} else if len(f.Programs) == 0 {
				e = fmt.Errorf("пустой список программ")
			} else if len(f.Failed) > 0 && float64(len(f.Failed)) > r.settings.MaxFailShare*float64(f.Total) {
				e = fmt.Errorf("не загрузились %d из %d программ: %s", len(f.Failed), f.Total, strings.Join(f.Failed[:min(5, len(f.Failed))], ", "))
			} else if len(c.prev) > 0 &&
				float64(len(f.Programs)) < r.settings.MinRatio*float64(len(c.prev)) &&
				len(c.prev)-len(f.Programs) > r.settings.MinDrop {
				e = fmt.Errorf("подозрительно мало программ: %d против %d", len(f.Programs), len(c.prev))
			}
		}
		if e == nil {
			slugs := map[string]bool{}
			for _, p := range f.Programs {
				if p.Platform != name || p.Slug == "" || slugs[p.Slug] {
					e = fmt.Errorf("invalid or duplicate program identity for %s", name)
					break
				}
				slugs[p.Slug] = true
			}
		}
		c.err = e
		if e != nil {
			c.current = sortedPrograms(c.prev)
			result.FailedPlatforms = append(result.FailedPlatforms, name)
			r.log.Error("platform collection failed", "platform", name, "error", e)
		} else {
			c.current = f.Programs
			seeding[name] = opt.Seed || len(c.prev) == 0
			if !seeding[name] {
				c.current = scope.CarryMissing(c.prev, c.current, r.settings.MissingRuns)
			}
			r.log.Info("platform collected", "platform", name, "programs", len(f.Programs), "failed", len(f.Failed))
		}
		collectedBy[name] = c
	}
	everything := []scope.Program{}
	for _, name := range r.names {
		ps := sortedPrograms(all[name])
		if c, ok := collectedBy[name]; ok {
			ps = c.current
		}
		for _, p := range ps {
			if p.Active {
				everything = append(everything, p)
			}
		}
	}
	lk := scope.LinkPrograms(everything)
	perCompany := []scope.Change{}
	mass := []string{}
	snapshots := map[string][]scope.Program{}
	for _, name := range names {
		c := collectedBy[name]
		if c.err != nil {
			continue
		}
		snapshots[name] = c.current
		if seeding[name] {
			active := 0
			for _, p := range c.current {
				if p.Active {
					active++
				}
			}
			result.Seeded = append(result.Seeded, fmt.Sprintf("%s: %d программ (%d активных)", r.render.Title(name), len(c.current), active))
			continue
		}
		changes := scope.ComparePlatform(c.prev, c.current)
		result.Changes = append(result.Changes, changes...)
		if float64(len(changes)) > max(float64(r.settings.MassMin), r.settings.MassRatio*float64(len(c.prev))) {
			mass = append(mass, r.render.MassChange(name, changes))
		} else {
			perCompany = append(perCompany, changes...)
		}
	}
	result.Messages = append(r.render.Render(perCompany, lk), mass...)
	if len(result.Seeded) > 0 {
		result.Messages = append(result.Messages, telegram.Seeded(result.Seeded))
	}
	if opt.DryRun {
		for _, m := range result.Messages {
			if err = n.Send(ctx, m); err != nil {
				return result, err
			}
		}
		return result, nil
	}
	health, err := store.Health(ctx)
	if err != nil {
		return result, err
	}
	alerts := []string{}
	for _, name := range names {
		h := health[name]
		c := collectedBy[name]
		if c.err != nil {
			h.Fails++
			h.LastError = telegram.Clip(c.err.Error(), 300)
			if h.Fails >= r.settings.FailAlertAfter && !h.Alerted {
				alerts = append(alerts, r.render.HealthAlert(name, h.Fails, c.err.Error()))
				h.Alerted = true
			}
		} else {
			if h.Alerted {
				alerts = append(alerts, r.render.HealthOK(name))
			}
			h = scope.Health{}
		}
		health[name] = h
	}
	result.Messages = append(alerts, result.Messages...)
	if err = store.CommitRun(ctx, snapshots, health, result.Changes, result.Messages, names...); err != nil {
		return result, fmt.Errorf("commit run: %w", err)
	}
	result.Unsent, err = Flush(ctx, store, n, r.log)
	return result, err
}

func Flush(ctx context.Context, s *storage.Store, n Notifier, log *slog.Logger) (int, error) {
	pending, err := s.Outbox(ctx)
	if err != nil {
		return 0, err
	}
	for i, m := range pending {
		if err = n.Send(ctx, m.HTML); err != nil {
			if ctx.Err() != nil {
				return len(pending) - i, ctx.Err()
			}
			log.Error("notification delivery failed", "remaining", len(pending)-i, "error", err)
			return len(pending) - i, nil
		}
		if err = s.OutboxDone(ctx, m.ID); err != nil {
			return len(pending) - i, fmt.Errorf("acknowledge message: %w", err)
		}
	}
	return 0, nil
}
