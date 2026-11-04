package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/khunzzz/scope-watch/internal/scope"
)

func TestHistoryRetentionIsOptInAndTransactional(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := float64(time.Now().Add(-200 * 24 * time.Hour).Unix())
	if _, err = s.db.ExecContext(ctx, `INSERT INTO changes(ts,platform,slug,kind,data) VALUES(?,'test','old','new','{}')`, old); err != nil {
		t.Fatal(err)
	}
	if err = s.CommitRun(ctx, nil, nil, nil, []string{"durable"}, CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	history, err := s.History(ctx, 10, "")
	if err != nil || len(history) != 1 {
		t.Fatal("retention must default to disabled", history, err)
	}
	// Reject the change after retention has run inside the transaction.
	if _, err = s.db.ExecContext(ctx, `CREATE TRIGGER reject_bad_change BEFORE INSERT ON changes WHEN NEW.slug='bad' BEGIN SELECT RAISE(ABORT,'rejected change'); END`); err != nil {
		t.Fatal(err)
	}
	bad := scope.Change{Kind: "new", Program: scope.Program{Platform: "test", Slug: "bad"}}
	if err = s.CommitRun(ctx, nil, nil, []scope.Change{bad}, nil, CommitOptions{HistoryDays: 90}); err == nil {
		t.Fatal("trigger did not reject run")
	}
	history, err = s.History(ctx, 10, "")
	if err != nil || len(history) != 1 {
		t.Fatal("failed run deleted history", history, err)
	}
	change := scope.Change{Kind: "new", Program: scope.Program{Platform: "test", Slug: "fresh", Name: "Fresh"}}
	if err = s.CommitRun(ctx, nil, nil, []scope.Change{change}, nil, CommitOptions{HistoryDays: 90}); err != nil {
		t.Fatal(err)
	}
	history, err = s.History(ctx, 10, "")
	if err != nil || len(history) != 1 || history[0]["slug"] != "fresh" {
		t.Fatal("retention removed recent history or kept old rows", history, err)
	}
	q, err := s.QueueStatus(ctx)
	if err != nil || q.Pending != 1 {
		t.Fatal("retention affected notification queue", q, err)
	}
}

func TestOpenUpgradesLegacyDatabaseWithoutLosingQueue(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "scope-watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE outbox(id INTEGER PRIMARY KEY,ts REAL NOT NULL,html TEXT NOT NULL);
        INSERT INTO outbox(id,ts,html) VALUES(42,1,'existing message');
        CREATE TABLE programs(platform TEXT NOT NULL,slug TEXT NOT NULL,data TEXT NOT NULL,PRIMARY KEY(platform,slug));
        INSERT INTO programs(platform,slug,data) VALUES('test','old','{"platform":"test","slug":"old","name":"Existing","active":true}');`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	programs, err := s.Load(ctx, "test")
	if err != nil || len(programs) != 1 || programs["old"].Name != "Existing" {
		t.Fatal(programs, err)
	}
	messages, err := s.OutboxBatch(ctx, 10)
	if err != nil || len(messages) != 1 || messages[0].ID != 42 || messages[0].HTML != "existing message" {
		t.Fatal(messages, err)
	}
	if err = s.DeliveryFailed(ctx, 42, "bad message", true); err != nil {
		t.Fatal(err)
	}
	if queue, err := s.QueueStatus(ctx); err != nil || queue.Pending != 1 || queue.Quarantined != 1 {
		t.Fatal(queue, err)
	}
	if err = s.RetryNotification(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if err = s.OutboxDone(ctx, 42); err != nil {
		t.Fatal(err)
	}
}
