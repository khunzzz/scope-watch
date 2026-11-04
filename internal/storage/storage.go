// Package storage persists snapshots, history and the transactional outbox.
package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	sq "github.com/Masterminds/squirrel"
	_ "modernc.org/sqlite"

	"github.com/khunzzz/scope-watch/internal/scope"
)

//go:embed schema.sql
var schema string

type Store struct{ db *sql.DB }

func Open(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "scope-watch.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Store, error) {
		db.Close()
		return nil, err
	}
	if _, err = db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return fail(err)
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		return fail(fmt.Errorf("initialize SQLite: %w", err))
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Load(ctx context.Context, platform string) (map[string]scope.Program, error) {
	rows, err := sq.Select("data").
		From("programs").
		Where(sq.Eq{"platform": platform}).
		OrderBy("slug").
		RunWith(s.db).
		QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]scope.Program{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p scope.Program
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decode %s snapshot: %w", platform, err)
		}
		if p.Meta == nil {
			p.Meta = map[string]any{}
		}
		out[p.Slug] = p
	}
	return out, rows.Err()
}

func (s *Store) Health(ctx context.Context) (map[string]scope.Health, error) {
	rows, err := sq.Select("platform", "fails", "alerted", "last_error").
		From("health").
		RunWith(s.db).
		QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]scope.Health{}
	for rows.Next() {
		var p string
		var h scope.Health
		if err = rows.Scan(&p, &h.Fails, &h.Alerted, &h.LastError); err != nil {
			return nil, err
		}
		out[p] = h
	}
	return out, rows.Err()
}

type CommitOptions struct {
	Attempted      []string
	FailedPrograms map[string][]string
	HistoryDays    int
}

func (s *Store) CommitRun(
	ctx context.Context,
	snapshots map[string][]scope.Program,
	health map[string]scope.Health,
	changes []scope.Change,
	messages []string,
	options CommitOptions,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	keys := []string{}
	for k := range snapshots {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, name := range keys {
		_, err = sq.Delete("programs").
			Where(sq.Eq{"platform": name}).
			RunWith(tx).
			ExecContext(ctx)
		if err != nil {
			return err
		}
		for _, p := range snapshots[name] {
			if p.Platform != name {
				return fmt.Errorf("snapshot platform mismatch: %s != %s", p.Platform, name)
			}
			raw, e := json.Marshal(p)
			if e != nil {
				return e
			}
			_, err = sq.Insert("programs").
				Columns("platform", "slug", "data").
				Values(name, p.Slug, string(raw)).
				RunWith(tx).
				ExecContext(ctx)
			if err != nil {
				return err
			}
		}
	}
	for name, h := range health {
		_, err = sq.Insert("health").
			Options("OR REPLACE").
			Columns("platform", "fails", "alerted", "last_error").
			Values(name, h.Fails, h.Alerted, h.LastError).
			RunWith(tx).
			ExecContext(ctx)
		if err != nil {
			return err
		}
	}
	now := float64(time.Now().UnixNano()) / 1e9
	for _, name := range options.Attempted {
		var success any
		if _, ok := snapshots[name]; ok && health[name].Fails == 0 {
			success = now
		}
		_, err = sq.Insert("platform_status").
			Columns("platform", "last_attempt", "last_success").
			Values(name, now, success).
			Suffix(`ON CONFLICT(platform) DO UPDATE SET
				last_attempt = excluded.last_attempt,
				last_success = COALESCE(excluded.last_success, platform_status.last_success)`).
			RunWith(tx).
			ExecContext(ctx)
		if err != nil {
			return err
		}
	}
	for name, programs := range snapshots {
		failed := map[string]bool{}
		for _, slug := range options.FailedPrograms[name] {
			failed[slug] = true
		}
		present := make([]string, 0, len(programs)+len(failed))
		for _, p := range programs {
			present = append(present, p.Slug)
			if failed[p.Slug] || p.MetaNumber("missing") > 0 {
				continue
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO program_health(platform,slug,fails,last_success) VALUES(?,?,0,?)
				ON CONFLICT(platform,slug) DO UPDATE SET fails=0,last_success=excluded.last_success`, name, p.Slug, now)
			if err != nil {
				return err
			}
		}
		for slug := range failed {
			present = append(present, slug)
			_, err = tx.ExecContext(ctx, `INSERT INTO program_health(platform,slug,fails) VALUES(?,?,1)
				ON CONFLICT(platform,slug) DO UPDATE SET fails=program_health.fails+1`, name, slug)
			if err != nil {
				return err
			}
		}
		_, err = sq.Delete("program_health").
			Where(sq.Eq{"platform": name}).
			Where(sq.NotEq{"slug": present}).
			RunWith(tx).
			ExecContext(ctx)
		if err != nil {
			return err
		}
	}
	if options.HistoryDays > 0 {
		cutoff := time.Now().Add(-time.Duration(options.HistoryDays) * 24 * time.Hour)
		_, err = sq.Delete("changes").
			Where(sq.Lt{"ts": float64(cutoff.UnixNano()) / 1e9}).
			RunWith(tx).
			ExecContext(ctx)
		if err != nil {
			return err
		}
	}
	for _, c := range changes {
		payload := struct {
			scope.Change
			Name string `json:"name"`
			URL  string `json:"url"`
		}{c, c.Program.Name, c.Program.URL}
		raw, e := json.Marshal(payload)
		if e != nil {
			return e
		}
		_, err = sq.Insert("changes").
			Columns("ts", "platform", "slug", "kind", "data").
			Values(now, c.Program.Platform, c.Program.Slug, c.Kind, string(raw)).
			RunWith(tx).
			ExecContext(ctx)
		if err != nil {
			return err
		}
	}
	for _, m := range messages {
		_, err = sq.Insert("outbox").
			Columns("ts", "html").
			Values(now, m).
			RunWith(tx).
			ExecContext(ctx)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) History(ctx context.Context, limit int, slug string) ([]map[string]any, error) {
	if limit < 1 {
		return nil, fmt.Errorf("history limit must be positive")
	}
	query := sq.Select("ts", "platform", "slug", "kind", "data").
		From("changes").
		OrderBy("id DESC").
		Limit(uint64(limit))
	if slug != "" {
		query = query.Where(sq.Eq{"slug": slug})
	}
	rows, err := query.RunWith(s.db).QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ts float64
		var platform, sl, kind, raw string
		if err = rows.Scan(&ts, &platform, &sl, &kind, &raw); err != nil {
			return nil, err
		}
		var v map[string]any
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		v["ts"] = ts
		v["platform"] = platform
		v["slug"] = sl
		v["kind"] = kind
		out = append(out, v)
	}
	return out, rows.Err()
}
