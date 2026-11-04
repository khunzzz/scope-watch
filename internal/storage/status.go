package storage

import (
	"context"
	"database/sql"
	"time"

	sq "github.com/Masterminds/squirrel"
)

type ProgramFailure struct {
	Slug        string     `json:"slug"`
	Failures    int        `json:"failures"`
	LastSuccess *time.Time `json:"last_success"`
}

// PlatformStatus reports data age separately from the current failure counter.
type PlatformStatus struct {
	Platform       string           `json:"platform"`
	FailedPrograms []ProgramFailure `json:"failed_programs"`
	Programs       int              `json:"programs"`
	Active         int              `json:"active"`
	Failures       int              `json:"failures"`
	LastError      string           `json:"last_error,omitempty"`
	LastAttempt    *time.Time       `json:"last_attempt"`
	LastSuccess    *time.Time       `json:"last_success"`
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
		status.FailedPrograms, err = s.ProgramFailures(ctx, name)
		if err != nil {
			return nil, err
		}
		out = append(out, status)
	}
	return out, nil
}

func (s *Store) ProgramFailures(ctx context.Context, platform string) ([]ProgramFailure, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT slug,fails,last_success FROM program_health WHERE platform=? AND fails>0 ORDER BY slug", platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProgramFailure{}
	for rows.Next() {
		var f ProgramFailure
		var success sql.NullFloat64
		if err := rows.Scan(&f.Slug, &f.Failures, &success); err != nil {
			return nil, err
		}
		if success.Valid {
			v := time.Unix(0, int64(success.Float64*1e9)).UTC()
			f.LastSuccess = &v
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
