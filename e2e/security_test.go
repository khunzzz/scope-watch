//go:build unix

package e2e

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func assertNoToken(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{
		testToken,
		url.QueryEscape(testToken),
		strings.ReplaceAll(url.QueryEscape(testToken), "%3A", "%3a"),
		url.QueryEscape(url.QueryEscape(testToken)),
	} {
		if strings.Contains(text, secret) {
			t.Fatal("bot token exposed in process output or persisted data")
		}
	}
}

func (h *harness) setenv(key, value string) {
	for i, pair := range h.env {
		if strings.HasPrefix(pair, key+"=") {
			h.env[i] = key + "=" + value
			return
		}
	}
	h.env = append(h.env, key+"="+value)
}

func TestTelegramRedirectE2E(t *testing.T) {
	h := newHarness(t)
	h.fixture.mu.Lock()
	h.fixture.telegramRedirect = "https://" + bizoneHost + "/capture"
	h.fixture.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := h.command(ctx, "test-notify").CombinedOutput()
	h.fixture.mu.Lock()
	captured, leaked := h.fixture.captured, h.fixture.leakedReferer
	h.fixture.mu.Unlock()
	if captured {
		t.Fatalf("Telegram redirect reached another host; token in Referer: %t", leaked)
	}
	if err == nil {
		t.Fatal("redirect accepted as successful delivery")
	}
	assertNoToken(t, string(output))
}

func TestPlatformRedirectE2E(t *testing.T) {
	h := newHarness(t)
	h.fixture.mu.Lock()
	h.fixture.listRedirect = "https://" + bizoneHost + "/capture"
	h.fixture.mu.Unlock()
	h.run(1, "run", "--only", "standoff")
	h.fixture.mu.Lock()
	captured := h.fixture.captured
	h.fixture.mu.Unlock()
	if captured {
		t.Fatal("platform redirect reached another origin")
	}
}

func TestTokenRedactionE2E(t *testing.T) {
	h := newHarness(t)
	for _, description := range []string{
		testToken,
		url.QueryEscape(testToken),
		strings.ReplaceAll(url.QueryEscape(testToken), "%3A", "%3a"),
		url.QueryEscape(url.QueryEscape(testToken)),
	} {
		h.fixture.mu.Lock()
		h.fixture.telegramError = "rejected credential: " + description
		h.fixture.mu.Unlock()
		_, stderr := h.runCaptured(1, "test-notify", "--verbose")
		if !strings.Contains(stderr, "***") {
			t.Fatal("expected redacted API error")
		}
	}
	h.run(1, "run", "--verbose")
	if h.status().Pending != 1 {
		t.Fatal("delivery failure must keep the notification queued")
	}
	h.fixture.mu.Lock()
	h.fixture.telegramError = ""
	h.fixture.telegramDrop = true
	h.fixture.mu.Unlock()
	h.run(1, "test-notify")
}

func TestDotEnvErrorRedactionE2E(t *testing.T) {
	h := newHarness(t)
	malformed := "INVALID!=" + url.QueryEscape(testToken) + "\n"
	if err := os.WriteFile(filepath.Join(h.dir, ".env"), []byte(malformed), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr := h.runCaptured(2, "status")
	if !strings.Contains(stderr, "load .env") {
		t.Fatal("expected a configuration parsing error")
	}
}

func TestSecretFileAndStoredDataE2E(t *testing.T) {
	h := newHarness(t)
	secret := filepath.Join(h.dir, "bot-secret")
	if err := os.WriteFile(secret, []byte(testToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h.setenv("TG_BOT_TOKEN", "ignored-environment-value")
	h.setenv("TG_BOT_TOKEN_FILE", secret)
	h.run(0, "run", "--verbose")
	h.run(0, "status", "--json")
	h.run(0, "export")
	err := filepath.WalkDir(filepath.Join(h.dir, "data"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		assertNoToken(t, string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
