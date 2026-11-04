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

type routedRecorder struct {
	recipients []string
	sent       map[string][]string
	failed     map[string]bool
}

func (n *routedRecorder) Recipients() []string { return n.recipients }
func (n *routedRecorder) Send(context.Context, string) error {
	return fmt.Errorf("unexpected broadcast")
}
func (n *routedRecorder) SendTo(_ context.Context, chatID, text string) error {
	if n.failed[chatID] {
		return &telegram.DeliveryError{Status: 400, Description: "chat not found"}
	}
	n.sent[chatID] = append(n.sent[chatID], text)
	return nil
}

func TestRecipientFailuresDoNotBlockOrRepeatOtherChats(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n := &routedRecorder{recipients: []string{"123", "-100456"}, sent: map[string][]string{}, failed: map[string]bool{"123": true}}
	// A blocked recipient's backlog exceeds one batch. Other recipients must
	// still receive their messages even when the first batch is all blocked.
	first := make([]string, 150)
	for i := range first {
		first[i] = fmt.Sprint(i)
	}
	if err = s.CommitRun(ctx, nil, nil, nil, first, storage.CommitOptions{Recipients: []string{"123"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.CommitRun(ctx, nil, nil, nil, []string{"other chat"}, storage.CommitOptions{Recipients: []string{"-100456"}}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for range 2 {
		remaining, err := Flush(ctx, s, n, log)
		if err != nil || remaining != 150 {
			t.Fatalf("remaining=%d err=%v", remaining, err)
		}
	}
	if len(n.sent["-100456"]) != 1 {
		t.Fatal("other chat blocked or duplicated", n.sent)
	}
	n.recipients = []string{"-100456", "789"}
	n.failed["123"] = false
	for _, want := range []int{50, 0} {
		if remaining, err := Flush(ctx, s, n, log); err != nil || remaining != want {
			t.Fatalf("remaining=%d want=%d err=%v", remaining, want, err)
		}
	}
	if len(n.sent["123"]) != 150 || len(n.sent["-100456"]) != 1 || len(n.sent["789"]) != 0 {
		t.Fatal("recovery lost or duplicated deliveries")
	}
	for i, text := range n.sent["123"] {
		if text != first[i] {
			t.Fatal("recipient order changed")
		}
	}
}
