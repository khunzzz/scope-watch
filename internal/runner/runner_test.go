package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/khunzzz/scope-watch/internal/config"
	"github.com/khunzzz/scope-watch/internal/model"
	"github.com/khunzzz/scope-watch/internal/runlock"
	"github.com/khunzzz/scope-watch/internal/storage"
	"github.com/khunzzz/scope-watch/internal/testutil"
)

type fakeAdapter struct {
	name     string
	programs []model.Program
	err      error
	failed   []string
	before   func()
}

func (a *fakeAdapter) Name() string { return a.name }
func (a *fakeAdapter) Title() string {
	if a.name == "standoff" {
		return "Standoff 365"
	}
	return "BI.ZONE"
}
func (a *fakeAdapter) Fetch(ctx context.Context, prev map[string]model.Program) (model.Fetched, error) {
	if a.before != nil {
		a.before()
	}
	out := model.Fetched{Total: len(a.programs), Failed: a.failed}
	for _, p := range a.programs {
		out.Programs = append(out.Programs, p.Clone())
	}
	return out, a.err
}

type collector struct {
	sent      []string
	failAfter int
}

func (c *collector) Send(ctx context.Context, s string) error {
	if c.failAfter >= 0 && len(c.sent) >= c.failAfter {
		return fmt.Errorf("telegram down")
	}
	c.sent = append(c.sent, s)
	return nil
}
func setup(t *testing.T) (*Runner, *fakeAdapter, config.Settings) {
	t.Helper()
	s := config.Defaults()
	s.DataDir = t.TempDir()
	s.FailAlertAfter = 2
	a := &fakeAdapter{name: "standoff", programs: []model.Program{testutil.Programs(t)["standoff/flowwow"]}}
	r, e := New(s, []Adapter{a}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	return r, a, s
}
func run(t *testing.T, r *Runner, opt Options) (Result, *collector) {
	t.Helper()
	c := &collector{failAfter: -1}
	res, e := r.Run(context.Background(), c, opt)
	if e != nil {
		t.Fatal(e)
	}
	return res, c
}
func TestSeedChangeQuiet(t *testing.T) {
	r, a, _ := setup(t)
	res, c := run(t, r, Options{})
	if len(res.Seeded) != 1 || len(c.sent) != 1 {
		t.Fatal(res)
	}
	a.programs = append(a.programs, testutil.Programs(t)["standoff/anyquery"])
	res, c = run(t, r, Options{})
	if len(res.Changes) != 1 || len(c.sent) != 1 || !strings.Contains(c.sent[0], "🆕") {
		t.Fatal(res, c)
	}
	_, c = run(t, r, Options{})
	if len(c.sent) != 0 {
		t.Fatal(c.sent)
	}
}
func TestDryRunDoesNotSave(t *testing.T) {
	r, a, _ := setup(t)
	run(t, r, Options{})
	a.programs[0].Active = false
	res, _ := run(t, r, Options{DryRun: true})
	if len(res.Changes) != 1 {
		t.Fatal(res)
	}
	res, _ = run(t, r, Options{})
	if len(res.Changes) != 1 {
		t.Fatal("dry run saved change")
	}
}
func TestFailureAndRecovery(t *testing.T) {
	r, a, s := setup(t)
	run(t, r, Options{})
	a.err = fmt.Errorf("down")
	sent := []string{}
	for i := 0; i < 3; i++ {
		_, c := run(t, r, Options{})
		sent = append(sent, c.sent...)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "⚠️") {
		t.Fatal(sent)
	}
	db, e := storage.Open(context.Background(), s.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	status, e := db.Status(context.Background(), []string{"standoff"})
	db.Close()
	if e != nil || status[0].LastSuccess == nil || status[0].LastAttempt == nil || status[0].Failures != 3 {
		t.Fatal(status, e)
	}
	a.err = nil
	_, c := run(t, r, Options{})
	if len(c.sent) != 1 || !strings.Contains(c.sent[0], "✅") {
		t.Fatal(c.sent)
	}
}
func TestMissingAndReturn(t *testing.T) {
	r, a, _ := setup(t)
	p := testutil.Programs(t)
	full := []model.Program{p["standoff/flowwow"], p["standoff/anyquery"], p["standoff/tbank"]}
	a.programs = full
	run(t, r, Options{})
	a.programs = full[:2]
	_, c := run(t, r, Options{})
	if len(c.sent) != 0 {
		t.Fatal("removed too early")
	}
	a.programs = full
	_, c = run(t, r, Options{})
	if len(c.sent) != 0 {
		t.Fatal("return alerted")
	}
	a.programs = full[:2]
	run(t, r, Options{})
	_, c = run(t, r, Options{})
	if len(c.sent) != 1 || !strings.Contains(c.sent[0], "🗑") {
		t.Fatal(c.sent)
	}
}
func TestTruncationAndMassFailure(t *testing.T) {
	r, a, s := setup(t)
	base := a.programs[0]
	full := []model.Program{}
	for i := 0; i < 10; i++ {
		p := base.Clone()
		p.Slug = fmt.Sprintf("p%d", i)
		full = append(full, p)
	}
	a.programs = full
	run(t, r, Options{})
	a.programs = full[:3]
	res, _ := run(t, r, Options{})
	if len(res.FailedPlatforms) != 1 {
		t.Fatal("truncation accepted")
	}
	a.programs = full
	a.failed = []string{"p0", "p1"}
	a.programs[5].Active = false
	res, _ = run(t, r, Options{})
	if len(res.Changes) != 1 {
		t.Fatal(res)
	}
	a.failed = append(a.failed, "p2")
	a.programs[6].Active = false
	res, _ = run(t, r, Options{})
	if len(res.FailedPlatforms) != 1 {
		t.Fatal("mass failure accepted")
	}
	db, e := storage.Open(context.Background(), s.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ps, e := db.Load(context.Background(), "standoff")
	if e != nil || !ps["p6"].Active {
		t.Fatal(ps, e)
	}
}
func TestOutboxFailureAndRetry(t *testing.T) {
	r, a, _ := setup(t)
	run(t, r, Options{})
	p := testutil.Programs(t)
	a.programs[0].Active = false
	a.programs = append(a.programs, p["standoff/anyquery"], p["standoff/tbank"])
	c := &collector{failAfter: 1}
	res, e := r.Run(context.Background(), c, Options{})
	if e != nil || res.Unsent != 2 || len(c.sent) != 1 {
		t.Fatal(res, e)
	}
	first := c.sent[0]
	res, dry := run(t, r, Options{DryRun: true})
	if len(dry.sent) != 0 {
		t.Fatal("dry run consumed queue")
	}
	res, c = run(t, r, Options{})
	if res.Unsent != 0 || len(c.sent) != 2 {
		t.Fatal(res, c)
	}
	for _, m := range c.sent {
		if m == first {
			t.Fatal("acknowledged message repeated")
		}
	}
	_, c = run(t, r, Options{})
	if len(c.sent) != 0 {
		t.Fatal(c.sent)
	}
}
func TestCancellationDoesNotCommit(t *testing.T) {
	r, a, s := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	a.before = cancel
	_, e := r.Run(ctx, &collector{failAfter: -1}, Options{})
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	db, e := storage.Open(context.Background(), s.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ps, e := db.Load(context.Background(), "standoff")
	if e != nil || len(ps) != 0 {
		t.Fatal("cancelled snapshot saved")
	}
}
func TestCompetingRunRejected(t *testing.T) {
	r, _, s := setup(t)
	lock, e := runlock.Acquire(s.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	_, e = r.Run(context.Background(), &collector{failAfter: -1}, Options{})
	if !errors.Is(e, runlock.ErrBusy) {
		t.Fatal(e)
	}
	lock.Close()
	run(t, r, Options{})
}
func TestRegistryRejectsDuplicate(t *testing.T) {
	_, a, s := setup(t)
	if _, e := New(s, []Adapter{a, a}, nil); e == nil {
		t.Fatal("duplicate accepted")
	}
}
func TestMassChange(t *testing.T) {
	r, a, _ := setup(t)
	base := a.programs[0]
	a.programs = nil
	for i := 0; i < 12; i++ {
		p := base.Clone()
		p.Slug = fmt.Sprintf("p%d", i)
		a.programs = append(a.programs, p)
	}
	run(t, r, Options{})
	for i := range a.programs {
		a.programs[i].ScopeMD += "\nновое правило"
	}
	_, c := run(t, r, Options{})
	if len(c.sent) != 1 || !strings.Contains(c.sent[0], "сразу 12 программ") {
		t.Fatal(c.sent)
	}
}
