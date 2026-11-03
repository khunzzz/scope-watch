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

func (s *Store) CommitRun(
	ctx context.Context,
	snapshots map[string][]scope.Program,
	health map[string]scope.Health,
	changes []scope.Change,
	messages []string,
	attempted ...string,
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
	for _, name := range attempted {
		var success any
		if _, ok := snapshots[name]; ok {
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

type Message struct {
	ID   int64
	HTML string
}

func (s *Store) Outbox(ctx context.Context) ([]Message, error) {
	rows, err := sq.Select("id", "html").
		From("outbox").
		OrderBy("id").
		RunWith(s.db).
		QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		if err = rows.Scan(&m.ID, &m.HTML); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) OutboxDone(ctx context.Context, id int64) error {
	_, err := sq.Delete("outbox").
		Where(sq.Eq{"id": id}).
		RunWith(s.db).
		ExecContext(ctx)
	return err
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

// PlatformStatus reports data age separately from the current failure counter.
type PlatformStatus struct {
	Platform    string     `json:"platform"`
	Programs    int        `json:"programs"`
	Active      int        `json:"active"`
	Failures    int        `json:"failures"`
	LastError   string     `json:"last_error,omitempty"`
	LastAttempt *time.Time `json:"last_attempt"`
	LastSuccess *time.Time `json:"last_success"`
}

func (s *Store) Status(ctx context.Context, names []string) ([]PlatformStatus, error) {
	health, err := s.Health(ctx)
	if err != nil {
		return nil, err
	}
	out := []PlatformStatus{}
	for _, name := range names {
		programs, err := s.Load(ctx, name)
		if err != nil {
			return nil, err
		}
		h := health[name]
		status := PlatformStatus{Platform: name, Programs: len(programs), Failures: h.Fails, LastError: h.LastError}
		for _, p := range programs {
			if p.Active {
				status.Active++
			}
		}
		var attempt, success sql.NullFloat64
		err = sq.Select("last_attempt", "last_success").
			From("platform_status").
			Where(sq.Eq{"platform": name}).
			RunWith(s.db).
			QueryRowContext(ctx).
			Scan(&attempt, &success)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if attempt.Valid {
			t := time.Unix(0, int64(attempt.Float64*1e9)).UTC()
			status.LastAttempt = &t
		}
		if success.Valid {
			t := time.Unix(0, int64(success.Float64*1e9)).UTC()
			status.LastSuccess = &t
		}
		out = append(out, status)
	}
	return out, nil
}
