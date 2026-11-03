package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/khunzzz/scope-watch/internal/model"
	"github.com/khunzzz/scope-watch/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsWithoutNetwork(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(t.TempDir())
	t.Setenv("TG_BOT_TOKEN_FILE", "")
	t.Setenv("TG_BOT_TOKEN", "")
	t.Setenv("TG_CHAT_ID", "")
	t.Setenv("SCOPEWATCH_DATA", dir)
	ctx := context.Background()
	s, e := storage.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	p := model.Program{Platform: "standoff", Slug: "x", Name: "Company", Active: true, Assets: []string{"a.corp.ru"}}
	e = s.CommitRun(ctx, map[string][]model.Program{"standoff": {p}}, nil, nil, []string{"queued"}, "standoff")
	s.Close()
	if e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		args []string
		code int
	}{{[]string{"--help"}, 0}, {[]string{"status", "--json"}, 0}, {[]string{"history"}, 0}, {[]string{"links"}, 0}, {[]string{"export"}, 0}, {[]string{"run"}, 2}, {[]string{"run", "--dry-run", "--only", "unknown"}, 2}, {[]string{"history", "-n", "-1"}, 2}, {[]string{"wat"}, 2}} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var out, err bytes.Buffer
			if code := Execute(ctx, tt.args, &out, &err); code != tt.code {
				t.Fatalf("code=%d out=%s err=%s", code, out.String(), err.String())
			}
			if tt.args[0] == "status" {
				var result struct {
					Platforms []storage.PlatformStatus
					Pending   int `json:"pending_notifications"`
				}
				if e := json.Unmarshal(out.Bytes(), &result); e != nil || result.Pending != 1 || result.Platforms[0].LastSuccess == nil {
					t.Fatal(result, e)
				}
			}
		})
	}
	var out, err bytes.Buffer
	path := filepath.Join(t.TempDir(), "companies.json")
	if code := Execute(ctx, []string{"export", "-o", path}, &out, &err); code != 0 {
		t.Fatal(err.String())
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal(e)
	}
}
func TestHealthcheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat")
	t.Setenv("SCOPEWATCH_HEARTBEAT", path)
	t.Setenv("SCOPEWATCH_INTERVAL", "900")
	if e := healthcheck(); e == nil {
		t.Fatal("missing heartbeat accepted")
	}
	os.WriteFile(path, nil, 0600)
	if e := healthcheck(); e != nil {
		t.Fatal(e)
	}
}
