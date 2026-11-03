//go:build unix

package e2e

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	standoffHost = "bugbounty.standoff365.com"
	bizoneHost   = "bugbounty.bi.zone"
	telegramHost = "api.telegram.org"
)

// All requests from the actual binary are confined to a loopback CONNECT proxy.
// The app keeps its normal endpoints, TLS checks and HTTP transport.
type platformFixture struct {
	mu                                                sync.Mutex
	listFailures                                      []int
	listRequests                                      int
	newProgram, scopeChanged, telegramDown, tasksDown bool
	messages                                          []string
	blockList, listStarted                            chan struct{}
	standoff                                          map[string]json.RawMessage
	bizone, tasks, rewards                            json.RawMessage
}

func readFixture(t *testing.T, root, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func newFixture(t *testing.T, root string) *platformFixture {
	t.Helper()
	f := &platformFixture{standoff: map[string]json.RawMessage{}, bizone: readFixture(t, root, "bizone_tbank.json"), tasks: readFixture(t, root, "bizone_tasks.json"), rewards: readFixture(t, root, "standoff_flowwow_page.json")}
	f.listFailures = []int{http.StatusTooManyRequests, http.StatusServiceUnavailable}
	var programs []json.RawMessage
	if err := json.Unmarshal(readFixture(t, root, "standoff_list.json"), &programs); err != nil {
		t.Fatal(err)
	}
	for _, raw := range programs {
		var p struct{ Slug string }
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		f.standoff[p.Slug] = raw
	}
	return f
}
func (f *platformFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	newProgram, scopeChanged, telegramDown, tasksDown := f.newProgram, f.scopeChanged, f.telegramDown, f.tasksDown
	block, started := f.blockList, f.listStarted
	f.mu.Unlock()
	write := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
	switch r.Host {
	case standoffHost:
		if r.URL.Path == "/programs" {
			f.mu.Lock()
			f.listRequests++
			status := http.StatusOK
			if len(f.listFailures) > 0 {
				status = f.listFailures[0]
				f.listFailures = f.listFailures[1:]
			}
			f.mu.Unlock()
			if status != http.StatusOK {
				w.Header().Set("Retry-After", "0")
				http.Error(w, "temporary platform failure", status)
				return
			}
			if block != nil {
				select {
				case started <- struct{}{}:
				default:
				}
				select {
				case <-block:
				case <-r.Context().Done():
					return
				}
			}
			var flowwow map[string]any
			if err := json.Unmarshal(f.standoff["flowwow"], &flowwow); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if scopeChanged {
				flowwow["description"] = fmt.Sprint(flowwow["description"]) + "\n\n# Scope\n*.fresh.flowwow.com"
			}
			programs := []any{flowwow}
			if newProgram {
				programs = append(programs, f.standoff["anyquery"])
			}
			pp := map[string]any{"programs": programs, "total": 1}
			raw, _ := json.Marshal(map[string]any{"props": map[string]any{"pageProps": pp}})
			fmt.Fprintf(w, `<script id="__NEXT_DATA__" type="application/json">%s</script>`, raw)
		} else {
			raw, _ := json.Marshal(map[string]any{"props": map[string]any{"pageProps": f.rewards}})
			fmt.Fprintf(w, `<script id="__NEXT_DATA__">%s</script>`, raw)
		}
	case bizoneHost:
		switch r.URL.Path {
		case "/api/bug-bounty/tasks/":
			if tasksDown {
				http.Error(w, "unavailable", http.StatusBadRequest)
				return
			}
			var groups map[string][]json.RawMessage
			if err := json.Unmarshal(f.tasks, &groups); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			tasks := []json.RawMessage{}
			for _, items := range groups {
				tasks = append(tasks, items...)
			}
			write(map[string]any{"count": len(tasks), "results": tasks, "next": nil})
		case "/api/bug-bounty/companies/":
			write(map[string]any{"count": 1, "results": []map[string]any{{"id": 1, "slug": "tbank", "public": true}}})
		case "/api/bug-bounty/companies/tbank/":
			write(f.bizone)
		case "/api/bug-bounty/companies/tbank/program-versions/":
			write([]map[string]any{{"type": "rule", "version": 1}})
		default:
			http.NotFound(w, r)
		}
	case telegramHost:
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			http.NotFound(w, r)
			return
		}
		if telegramDown {
			w.WriteHeader(http.StatusBadRequest)
			write(map[string]any{"ok": false, "description": "delivery unavailable"})
			return
		}
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f.mu.Lock()
		f.messages = append(f.messages, payload.Text)
		f.mu.Unlock()
		write(map[string]any{"ok": true})
	default:
		http.Error(w, "unexpected host", http.StatusForbidden)
	}
}
func localHTTPS(t *testing.T, f *platformFixture, dir string) (proxyURL, caFile string) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ScopeWatch local E2E"}, DNSNames: []string{standoffHost, bizoneHost, telegramHost}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	caFile = filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	backend.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	backend.StartTLS()
	t.Cleanup(backend.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := map[string]bool{standoffHost + ":443": true, bizoneHost + ":443": true, telegramHost + ":443": true}
		if r.Method != http.MethodConnect || !allowed[r.Host] {
			http.Error(w, "external network is forbidden in E2E", 403)
			return
		}
		upstream, err := net.DialTimeout("tcp", backend.Listener.Addr().String(), time.Second)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer upstream.Close()
		client, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		if _, err = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { _, _ = io.Copy(upstream, buffered); _ = upstream.Close(); close(done) }()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
		<-done
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL, caFile
}

type harness struct {
	t           *testing.T
	binary, dir string
	env         []string
	fixture     *platformFixture
}

func (h *harness) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, h.binary, args...)
	cmd.Dir = h.dir
	cmd.Env = h.env
	return cmd
}
func (h *harness) run(code int, args ...string) string {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := h.command(ctx, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	actual := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			h.t.Fatal(err)
		}
		actual = exit.ExitCode()
	}
	if actual != code {
		h.t.Fatalf("%v: exit=%d want=%d\nstdout=%s\nstderr=%s", args, actual, code, out.String(), stderr.String())
	}
	return out.String()
}

type statusReport struct {
	Pending   int `json:"pending_notifications"`
	Platforms []struct {
		Platform           string
		Programs, Failures int
		LastSuccess        *time.Time `json:"last_success"`
	}
}

func (h *harness) status() statusReport {
	h.t.Helper()
	var s statusReport
	if err := json.Unmarshal([]byte(h.run(0, "status", "--json")), &s); err != nil {
		h.t.Fatal(err)
	}
	return s
}
func (f *platformFixture) messageCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}
func (f *platformFixture) lastMessage() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		return ""
	}
	return f.messages[len(f.messages)-1]
}

func TestServiceE2E(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "scope-watch")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-race", "-o", binary, "./cmd/scope-watch")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	fixture := newFixture(t, root)
	proxy, ca := localHTTPS(t, fixture, dir)
	overrides := map[string]string{"SCOPEWATCH_DATA": filepath.Join(dir, "data"), "SCOPEWATCH_DELAY": "0", "SCOPEWATCH_FAIL_ALERT": "1", "SCOPEWATCH_HEARTBEAT": filepath.Join(dir, "heartbeat"), "SCOPEWATCH_INTERVAL": "900", "TG_BOT_TOKEN_FILE": "", "TG_BOT_TOKEN": "1234567890:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "TG_CHAT_ID": "1", "HTTPS_PROXY": proxy, "HTTP_PROXY": proxy, "https_proxy": proxy, "http_proxy": proxy, "ALL_PROXY": "", "all_proxy": "", "NO_PROXY": "", "no_proxy": "", "SSL_CERT_FILE": ca, "SSL_CERT_DIR": dir, "GORACE": "atexit_sleep_ms=0"}
	env := []string{}
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if _, ok := overrides[k]; !ok {
			env = append(env, v)
		}
	}
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	h := &harness{t: t, binary: binary, dir: dir, env: env, fixture: fixture}
	if text := h.run(0, "run", "--help"); !strings.Contains(text, "--dry-run") {
		t.Fatal(text)
	}
	if text := h.run(0, "completion", "bash"); !strings.Contains(text, "scope-watch") {
		t.Fatal("missing completion")
	}
	h.run(2, "history", "-n", "0")
	h.run(2, "run", "--only", "unknown")
	h.run(0, "run")
	fixture.mu.Lock()
	listRequests := fixture.listRequests
	fixture.mu.Unlock()
	if listRequests != 3 {
		t.Fatalf("expected recovery after HTTP 429 and 503; list requests: %d", listRequests)
	}
	if fixture.messageCount() != 1 {
		t.Fatal("initial snapshot should produce one summary")
	}
	initial := h.status()
	if initial.Pending != 0 || initial.Platforms[0].Programs != 1 || initial.Platforms[1].Programs != 1 || initial.Platforms[0].LastSuccess == nil {
		t.Fatalf("status %+v", initial)
	}
	h.run(0, "run")
	if fixture.messageCount() != 1 {
		t.Fatal("unchanged state notified")
	}
	fixture.mu.Lock()
	fixture.newProgram = true
	fixture.mu.Unlock()
	if text := h.run(0, "run", "--dry-run"); !strings.Contains(text, "AnyQuery") {
		t.Fatal(text)
	}
	if s := h.status(); s.Platforms[0].Programs != 1 || s.Pending != 0 {
		t.Fatal("dry-run persisted changes", s)
	}
	fixture.mu.Lock()
	fixture.telegramDown = true
	fixture.mu.Unlock()
	h.run(1, "run")
	if s := h.status(); s.Pending != 1 || s.Platforms[0].Programs != 2 {
		t.Fatal("queue/snapshot not committed", s)
	}
	h.run(1, "run")
	if s := h.status(); s.Pending != 1 {
		t.Fatal("queued event duplicated", s)
	}
	fixture.mu.Lock()
	fixture.telegramDown = false
	fixture.mu.Unlock()
	h.run(0, "run")
	if fixture.messageCount() != 2 || h.status().Pending != 0 {
		t.Fatal("queue not recovered")
	}
	h.run(0, "run")
	if fixture.messageCount() != 2 {
		t.Fatal("acknowledged message repeated")
	}
	if text := h.run(0, "history", "--program", "anyquery"); !strings.Contains(text, "AnyQuery") {
		t.Fatal(text)
	}
	fixture.mu.Lock()
	fixture.tasksDown = true
	fixture.mu.Unlock()
	h.run(1, "run")
	failed := h.status()
	if failed.Platforms[1].Failures != 1 || failed.Platforms[1].Programs != 1 || !strings.Contains(fixture.lastMessage(), "⚠️") {
		t.Fatal("platform failure lost snapshot or alert", failed)
	}
	fixture.mu.Lock()
	fixture.tasksDown = false
	fixture.scopeChanged = true
	fixture.mu.Unlock()
	h.run(0, "run")
	if !strings.Contains(fixture.lastMessage(), "*.fresh.flowwow.com") {
		t.Fatal("scope change missing", fixture.lastMessage())
	}
	fixture.mu.Lock()
	fixture.newProgram = false
	fixture.mu.Unlock()
	before := fixture.messageCount()
	h.run(0, "run")
	if fixture.messageCount() != before {
		t.Fatal("program removed before confirmation")
	}
	h.run(0, "run")
	if !strings.Contains(fixture.lastMessage(), "🗑") {
		t.Fatal("confirmed removal missing")
	}
	var exported []map[string]any
	if err = json.Unmarshal([]byte(h.run(0, "export")), &exported); err != nil || len(exported) != 2 {
		t.Fatal("export", exported, err)
	}
	h.run(0, "links")
	output := filepath.Join(dir, "export.json")
	h.run(0, "export", "-o", output)
	if _, err = os.Stat(output); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	fixture.blockList = make(chan struct{})
	fixture.listStarted = make(chan struct{}, 1)
	started := fixture.listStarted
	fixture.mu.Unlock()
	loopCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	cmd := h.command(loopCtx, "loop", "--only", "standoff")
	var log bytes.Buffer
	cmd.Stderr = &log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		stop()
		<-exited
		t.Fatal("loop did not start request", log.String())
	}
	h.run(1, "run", "--only", "standoff")
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-exited:
		if err != nil {
			t.Fatal("graceful shutdown", err, log.String())
		}
	case <-time.After(5 * time.Second):
		stop()
		<-exited
		t.Fatal("shutdown ignored context")
	}
	if s := h.status(); s.Pending != 0 || s.Platforms[0].Programs != 1 {
		t.Fatal("cancelled run changed snapshot", s)
	}
}
