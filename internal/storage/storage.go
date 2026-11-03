// Package storage persists snapshots, history and the transactional notification outbox.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/khunzzz/scope-watch/internal/model"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS programs (platform TEXT NOT NULL,slug TEXT NOT NULL,data TEXT NOT NULL,PRIMARY KEY(platform,slug));
CREATE TABLE IF NOT EXISTS changes (id INTEGER PRIMARY KEY,ts REAL NOT NULL,platform TEXT NOT NULL,slug TEXT NOT NULL,kind TEXT NOT NULL,data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS changes_program ON changes(platform,slug,ts);
CREATE TABLE IF NOT EXISTS health (platform TEXT PRIMARY KEY,fails INTEGER NOT NULL DEFAULT 0,alerted INTEGER NOT NULL DEFAULT 0,last_error TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS outbox (id INTEGER PRIMARY KEY,ts REAL NOT NULL,html TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS platform_status(platform TEXT PRIMARY KEY,last_attempt REAL NOT NULL,last_success REAL);`

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
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	if _, err = db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return fail(err)
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		return fail(fmt.Errorf("initialize SQLite: %w", err))
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Load(ctx context.Context, platform string) (map[string]model.Program, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM programs WHERE platform = ? ORDER BY slug", platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]model.Program{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p model.Program
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
func (s *Store) Health(ctx context.Context) (map[string]model.Health, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT platform,fails,alerted,last_error FROM health")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]model.Health{}
	for rows.Next() {
		var p string
		var h model.Health
		if err = rows.Scan(&p, &h.Fails, &h.Alerted, &h.LastError); err != nil {
			return nil, err
		}
		out[p] = h
	}
	return out, rows.Err()
}
func (s *Store) CommitRun(ctx context.Context, snapshots map[string][]model.Program, health map[string]model.Health, changes []model.Change, messages []string, attempted ...string) error {
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
		if _, err = tx.ExecContext(ctx, "DELETE FROM programs WHERE platform = ?", name); err != nil {
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
			if _, err = tx.ExecContext(ctx, "INSERT INTO programs(platform,slug,data) VALUES(?,?,?)", name, p.Slug, string(raw)); err != nil {
				return err
			}
		}
	}
	for name, h := range health {
		if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO health(platform,fails,alerted,last_error) VALUES(?,?,?,?)", name, h.Fails, h.Alerted, h.LastError); err != nil {
			return err
		}
	}
	now := float64(time.Now().UnixNano()) / 1e9
	for _, name := range attempted {
		var success any
		if _, ok := snapshots[name]; ok {
			success = now
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO platform_status(platform,last_attempt,last_success) VALUES(?,?,?) ON CONFLICT(platform) DO UPDATE SET last_attempt=excluded.last_attempt,last_success=COALESCE(excluded.last_success,platform_status.last_success)", name, now, success); err != nil {
			return err
		}
	}
	for _, c := range changes {
		payload := struct {
			model.Change
			Name string `json:"name"`
			URL  string `json:"url"`
		}{c, c.Program.Name, c.Program.URL}
		raw, e := json.Marshal(payload)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO changes(ts,platform,slug,kind,data) VALUES(?,?,?,?,?)", now, c.Program.Platform, c.Program.Slug, c.Kind, string(raw)); err != nil {
			return err
		}
	}
	for _, m := range messages {
		if _, err = tx.ExecContext(ctx, "INSERT INTO outbox(ts,html) VALUES(?,?)", now, m); err != nil {
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
	rows, err := s.db.QueryContext(ctx, "SELECT id,html FROM outbox ORDER BY id")
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
	_, err := s.db.ExecContext(ctx, "DELETE FROM outbox WHERE id = ?", id)
	return err
}
func (s *Store) History(ctx context.Context, limit int, slug string) ([]map[string]any, error) {
	q := "SELECT ts,platform,slug,kind,data FROM changes"
	args := []any{}
	if slug != "" {
		q += " WHERE slug = ?"
		args = append(args, slug)
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
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
		err = s.db.QueryRowContext(ctx, "SELECT last_attempt,last_success FROM platform_status WHERE platform=?", name).Scan(&attempt, &success)
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
