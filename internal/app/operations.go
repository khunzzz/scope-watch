package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/khunzzz/scope-watch/internal/scope"
	"github.com/khunzzz/scope-watch/internal/storage"
)

func loop(ctx context.Context, s Settings, r *Runner, n Notifier, only []string, log *slog.Logger) error {
	for {
		started := time.Now()
		res, err := r.Run(ctx, n, Options{Platforms: only})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			log.Error("run failed", "error", err)
		} else if s.Heartbeat != "" && res.Unsent == 0 {
			if err = os.WriteFile(s.Heartbeat, nil, 0600); err != nil {
				log.Error("heartbeat failed", "error", err)
			}
		}
		delay := max(30*time.Second, s.Interval-time.Since(started)) + time.Duration(rand.Int64N(int64(30*time.Second)))
		if err = wait(ctx, delay); err != nil {
			return err
		}
	}
}

func healthcheck() error {
	path := os.Getenv("SCOPEWATCH_HEARTBEAT")
	if path == "" {
		path = "/tmp/heartbeat"
	}
	seconds := 900.0
	if v := os.Getenv("SCOPEWATCH_INTERVAL"); v != "" {
		n, e := strconv.ParseFloat(v, 64)
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 86400*365 {
			return fmt.Errorf("invalid SCOPEWATCH_INTERVAL")
		}
		seconds = n
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if time.Since(info.ModTime()) > time.Duration((2*seconds+300)*float64(time.Second)) {
		return fmt.Errorf("heartbeat is stale")
	}
	return nil
}

func inspect(ctx context.Context, s Settings, r *Runner, command string, limit int, slug, output string, out io.Writer) (err error) {
	store, err := storage.Open(ctx, s.DataDir)
	if err != nil {
		return err
	}
	defer func() {
		if e := store.Close(); err == nil {
			err = e
		}
	}()
	titles := r.Titles()
	if command == "history" {
		rows, e := store.History(ctx, limit, slug)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(out, "изменений пока не было")
			return err
		}
		for i := len(rows) - 1; i >= 0; i-- {
			row := rows[i]
			ts, _ := row["ts"].(float64)
			platform, _ := row["platform"].(string)
			title := titles[platform]
			if title == "" {
				title = platform
			}
			details := []string{}
			if k, _ := row["kind"].(string); k != "updated" {
				details = append(details, k)
			}
			if active, ok := row["activated"].(bool); ok {
				if active {
					details = append(details, "reopened")
				} else {
					details = append(details, "closed")
				}
			}
			for key, label := range map[string]string{
				"assets_added":   "новые ассеты",
				"assets_removed": "убраны ассеты",
				"bounty_lost":    "без выплат",
				"bounty_gained":  "с выплатами",
				"rewards":        "выплаты",
				"new_versions":   "версии",
			} {
				if xs, ok := row[key].([]any); ok && len(xs) > 0 {
					details = append(details, fmt.Sprintf("%s: %d", label, len(xs)))
				}
			}
			if text, _ := row["text_diff"].(string); text != "" {
				details = append(details, "текст")
			}
			slices.Sort(details)
			_, err = fmt.Fprintf(out, "%s  %-12s %-28v %s\n",
				time.Unix(int64(ts), 0).Format("2006-01-02 15:04"),
				title, row["name"], strings.Join(details, "; "),
			)
			if err != nil {
				return err
			}
		}
		return nil
	}
	programs := []scope.Program{}
	for _, name := range r.Names() {
		ps, e := store.Load(ctx, name)
		if e != nil {
			return e
		}
		for _, p := range ps {
			if p.Active {
				programs = append(programs, p)
			}
		}
	}
	lk := scope.LinkPrograms(programs)
	if command == "links" {
		sort.SliceStable(lk.Links, func(i, j int) bool { return lk.Links[i].Score > lk.Links[j].Score })
		for _, ln := range lk.Links {
			if _, err = fmt.Fprintf(out, "%5.2f %-36s %-44s %s\n", ln.Score, ln.A, ln.B, strings.Join(ln.Shared[:min(3, len(ln.Shared))], ", ")); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(out, "\nсклеено пар: %d\n", len(lk.Links))
		return err
	}
	groups := map[string][]scope.Program{}
	for _, p := range programs {
		k := lk.GroupKey(p)
		groups[k] = append(groups[k], p)
	}
	type listed struct {
		Platform  string `json:"platform"`
		Slug      string `json:"slug"`
		URL       string `json:"url"`
		MaxReward int    `json:"max_reward"`
	}
	type company struct {
		Company    string   `json:"company"`
		Programs   []listed `json:"programs"`
		MaxReward  int      `json:"max_reward"`
		Assets     []string `json:"assets"`
		OutOfScope []string `json:"out_of_scope"`
	}
	companies := []company{}
	for _, ps := range groups {
		sort.Slice(ps, func(i, j int) bool { return ps[i].Platform < ps[j].Platform })
		c := company{Company: ps[0].Name, Assets: []string{}, OutOfScope: []string{}}
		for _, p := range ps {
			c.Programs = append(c.Programs, listed{titles[p.Platform], p.Slug, p.URL, p.MaxReward()})
			c.MaxReward = max(c.MaxReward, p.MaxReward())
			c.Assets = append(c.Assets, p.Assets...)
			c.OutOfScope = append(c.OutOfScope, p.OutOfScope...)
		}
		slices.Sort(c.Assets)
		c.Assets = slices.Compact(c.Assets)
		slices.Sort(c.OutOfScope)
		c.OutOfScope = slices.Compact(c.OutOfScope)
		companies = append(companies, c)
	}
	sort.Slice(companies, func(i, j int) bool {
		if companies[i].MaxReward == companies[j].MaxReward {
			return companies[i].Company < companies[j].Company
		}
		return companies[i].MaxReward > companies[j].MaxReward
	})
	data, err := json.MarshalIndent(companies, "", " ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if output == "" {
		_, err = out.Write(data)
		return err
	}
	if err = atomicWrite(output, data); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%d компаний → %s\n", len(companies), output)
	return err
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".scope-watch-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func showStatus(ctx context.Context, s Settings, names []string, out io.Writer, asJSON bool) (err error) {
	store, err := storage.Open(ctx, s.DataDir)
	if err != nil {
		return err
	}
	defer func() {
		if e := store.Close(); err == nil {
			err = e
		}
	}()
	platforms, err := store.Status(ctx, names)
	if err != nil {
		return err
	}
	queue, err := store.Outbox(ctx)
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(out).Encode(struct {
			Platforms []storage.PlatformStatus `json:"platforms"`
			Pending   int                      `json:"pending_notifications"`
		}{platforms, len(queue)})
	}
	for _, p := range platforms {
		fresh := "успешных сборов ещё не было"
		if p.LastSuccess != nil {
			fresh = p.LastSuccess.Format(time.RFC3339)
		}
		_, err = fmt.Fprintf(out,
			"%s: программ %d, активных %d, сбоев подряд %d\n  Последний успешный сбор: %s\n",
			p.Platform, p.Programs, p.Active, p.Failures, fresh,
		)
		if err != nil {
			return err
		}
		if p.LastError != "" {
			if _, err = fmt.Fprintln(out, "  Ошибка:", p.LastError); err != nil {
				return err
			}
		}
	}
	_, err = fmt.Fprintf(out, "В очереди Telegram: %d\n", len(queue))
	return err
}
