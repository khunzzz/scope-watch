package storage

import (
	"context"
	"database/sql"
	"github.com/khunzzz/scope-watch/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestPythonDatabaseCompatibility(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(dir, "scope-watch.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../testdata/legacy.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(raw)); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ps, e := s.Load(ctx, "standoff")
	if e != nil || ps["legacy"].Rewards["critical"][1] != 2 || ps["legacy"].MetaNumber("extractor") != 2 {
		t.Fatal(ps, e)
	}
	h, e := s.Health(ctx)
	if e != nil || !h["standoff"].Alerted || h["standoff"].Fails != 2 {
		t.Fatal(h, e)
	}
	queue, e := s.Outbox(ctx)
	if e != nil || len(queue) != 1 || queue[0].HTML != "pending from Python" {
		t.Fatal(queue, e)
	}
	rows, e := s.History(ctx, 20, "legacy")
	if e != nil || len(rows) != 1 || rows[0]["name"] != "Legacy" {
		t.Fatal(rows, e)
	}
	if e = s.OutboxDone(ctx, queue[0].ID); e != nil {
		t.Fatal(e)
	}
	queue, e = s.Outbox(ctx)
	if e != nil || len(queue) != 0 {
		t.Fatal(queue, e)
	}
}
func TestCommitIsAtomic(t *testing.T) {
	ctx := context.Background()
	s, e := Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p := model.Program{Platform: "standoff", Slug: "x", Name: "old", Active: true}
	if e = s.CommitRun(ctx, map[string][]model.Program{"standoff": {p}}, nil, nil, nil); e != nil {
		t.Fatal(e)
	}
	p.Name = "changed"
	e = s.CommitRun(ctx, map[string][]model.Program{"standoff": {p, p}}, map[string]model.Health{"standoff": {Fails: 9}}, nil, []string{"must roll back"})
	if e == nil {
		t.Fatal("duplicate insert accepted")
	}
	ps, e := s.Load(ctx, "standoff")
	if e != nil || ps["x"].Name != "old" {
		t.Fatal(ps, e)
	}
	h, _ := s.Health(ctx)
	queue, _ := s.Outbox(ctx)
	if len(h) != 0 || len(queue) != 0 {
		t.Fatal("partial transaction")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if e = s.CommitRun(ctx, nil, nil, nil, []string{"cancelled"}); e == nil {
		t.Fatal("cancel ignored")
	}
}
