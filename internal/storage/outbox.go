package storage

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
)

type Message struct {
	ID   int64
	HTML string
}

// OutboxBatch returns at most limit deliverable messages. Quarantined messages
// remain in the outbox for inspection and explicit retry.
func (s *Store) OutboxBatch(ctx context.Context, limit uint64) ([]Message, error) {
	if limit == 0 {
		return nil, fmt.Errorf("outbox batch limit must be positive")
	}
	rows, err := sq.Select("id", "html").
		From("outbox").
		Where("NOT EXISTS (SELECT 1 FROM delivery_failures f WHERE f.message_id=outbox.id AND f.quarantined=1)").
		OrderBy("id").
		Limit(limit).
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

type QueueStatus struct {
	Pending     int `json:"pending_notifications"`
	Quarantined int `json:"quarantined_notifications"`
}

func (s *Store) QueueStatus(ctx context.Context) (QueueStatus, error) {
	var out QueueStatus
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN f.quarantined=1 THEN 1 ELSE 0 END),0)
		FROM outbox o LEFT JOIN delivery_failures f ON f.message_id=o.id`).Scan(&out.Pending, &out.Quarantined)
	return out, err
}

func (s *Store) OutboxDone(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM delivery_failures WHERE message_id=?", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM outbox WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeliveryFailed(ctx context.Context, id int64, message string, quarantine bool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO delivery_failures(message_id,attempts,last_error,quarantined) VALUES(?,1,?,?)
		ON CONFLICT(message_id) DO UPDATE SET attempts=delivery_failures.attempts+1,last_error=excluded.last_error,quarantined=excluded.quarantined`, id, message, quarantine)
	return err
}

type DeliveryFailure struct {
	ID          int64  `json:"id"`
	Attempts    int    `json:"attempts"`
	LastError   string `json:"last_error"`
	Quarantined bool   `json:"quarantined"`
	HTML        string `json:"html"`
}

func (s *Store) DeliveryFailures(ctx context.Context, limit uint64) ([]DeliveryFailure, error) {
	if limit == 0 {
		return nil, fmt.Errorf("failure list limit must be positive")
	}
	rows, err := sq.Select("o.id", "f.attempts", "f.last_error", "f.quarantined", "o.html").
		From("outbox o").
		Join("delivery_failures f ON f.message_id=o.id").
		OrderBy("o.id").
		Limit(limit).
		RunWith(s.db).
		QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryFailure{}
	for rows.Next() {
		var f DeliveryFailure
		if err := rows.Scan(&f.ID, &f.Attempts, &f.LastError, &f.Quarantined, &f.HTML); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) RetryNotification(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "UPDATE delivery_failures SET quarantined=0 WHERE message_id=? AND quarantined=1", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("quarantined notification %d not found", id)
	}
	return nil
}
