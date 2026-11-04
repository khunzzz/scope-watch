package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/khunzzz/scope-watch/internal/notify/telegram"
	"github.com/khunzzz/scope-watch/internal/storage"
)

type recordingNotifier struct {
	sent []string
	err  error
}

func (n *recordingNotifier) Send(_ context.Context, text string) error {
	if n.err != nil {
		return n.err
	}
	n.sent = append(n.sent, text)
	return nil
}

func TestFlushBoundsBacklogAndKeepsOrder(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	messages := make([]string, 205)
	for i := range messages {
		messages[i] = fmt.Sprint(i)
	}
	if err = s.CommitRun(ctx, nil, nil, nil, messages, storage.CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	n := &recordingNotifier{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, want := range []int{105, 5, 0} {
		remaining, err := Flush(ctx, s, n, log)
		if err != nil || remaining != want {
			t.Fatalf("remaining=%d want=%d err=%v", remaining, want, err)
		}
	}
	if len(n.sent) != len(messages) {
		t.Fatalf("delivered %d of %d", len(n.sent), len(messages))
	}
	for i := range messages {
		if n.sent[i] != messages[i] {
			t.Fatalf("message %d delivered out of order", i)
		}
	}
}

func TestChatWideFailureKeepsQueueAndCountsAttempts(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CommitRun(ctx, nil, nil, nil, []string{"first", "second"}, storage.CommitOptions{}); err != nil {
		t.Fatal(err)
	}
	n := &recordingNotifier{err: &telegram.DeliveryError{Status: 400, Description: "Bad Request: chat not found"}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for attempt := 1; attempt <= 2; attempt++ {
		remaining, err := Flush(ctx, s, n, log)
		if err != nil || remaining != 2 {
			t.Fatalf("remaining=%d err=%v", remaining, err)
		}
		failures, err := s.DeliveryFailures(ctx, 10)
		if err != nil || len(failures) != 1 || failures[0].Quarantined || failures[0].Attempts != attempt {
			t.Fatalf("failures=%+v err=%v", failures, err)
		}
	}
	n.err = nil
	if remaining, err := Flush(ctx, s, n, log); err != nil || remaining != 0 {
		t.Fatalf("recovery remaining=%d err=%v", remaining, err)
	}
	if len(n.sent) != 2 || n.sent[0] != "first" || n.sent[1] != "second" {
		t.Fatal(n.sent)
	}
}
