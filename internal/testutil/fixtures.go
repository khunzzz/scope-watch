// Package testutil loads repository fixtures for offline regression tests.
package testutil

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/khunzzz/scope-watch/internal/model"
)

func Load(t testing.TB, name string, out any) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	data, e := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "testdata", name))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, out); e != nil {
		t.Fatal(e)
	}
}
func Programs(t testing.TB) map[string]model.Program {
	t.Helper()
	var ps []model.Program
	Load(t, "python_programs.json", &ps)
	out := map[string]model.Program{}
	for _, p := range ps {
		out[p.Key()] = p
	}
	return out
}

// CheckProgram compares metadata and the full scope text against the Python oracle.
func CheckProgram(t testing.TB, got model.Program, extractorVersion int) {
	t.Helper()
	var fixtures []struct {
		model.Program
		ScopeHash string `json:"scope_md_sha256"`
	}
	Load(t, "python_programs.json", &fixtures)
	for _, fixture := range fixtures {
		if fixture.Key() != got.Key() {
			continue
		}
		if hash := fmt.Sprintf("%x", sha256.Sum256([]byte(got.ScopeMD))); hash != fixture.ScopeHash {
			t.Errorf("%s: scope text differs from Python", got.Key())
		}
		got.ScopeMD = ""
		fixture.Meta["extractor"] = extractorVersion
		if !reflect.DeepEqual(got, fixture.Program) {
			t.Errorf("%s metadata mismatch: got %+v; want %+v", got.Key(), got, fixture.Program)
		}
		return
	}
	t.Fatalf("missing Python oracle for %s", got.Key())
}
