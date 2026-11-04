package scope

import (
	"reflect"
	"testing"
)

func TestExtractAssetsBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       []string
	}{
		{"wildcards and links", `[API](https://api.acme.ru/path) *.acme.ru`, []string{"*.acme.ru", "api.acme.ru"}},
		{"references and email", `security@acme.ru https://owasp.org https://docs.acme.ru`, []string{"docs.acme.ru"}},
		{"public IP only", `8.8.8.8 10.1.2.3 192.168.1.1 127.0.0.1 203.0.113.1`, []string{"8.8.8.8"}},
		{"mobile application", `https://play.google.com/store/apps/details?id=com.acme.mobile`, []string{"android:com.acme.mobile"}},
		{"alternative suffixes", `api.acme.(ru|com)`, []string{"api.acme.com", "api.acme.ru"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractAssets(tc.text, nil); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestSplitScopeRespectsNestedHeadings(t *testing.T) {
	in, out := SplitScope("# Scope\n*.acme.ru\n## Out of scope\nold.acme.ru\n### Details\nlegacy.acme.ru\n## Targets\napi.acme.ru", nil)
	if !reflect.DeepEqual(in, []string{"*.acme.ru", "api.acme.ru"}) || !reflect.DeepEqual(out, []string{"legacy.acme.ru", "old.acme.ru"}) {
		t.Fatalf("in=%v out=%v", in, out)
	}
}

func TestCoveredRequiresDomainBoundary(t *testing.T) {
	pool := map[string]bool{"*.acme.ru": true}
	for _, tc := range []struct {
		asset string
		want  bool
	}{{"api.acme.ru", true}, {"acme.ru", true}, {"evilacme.ru", false}, {"acme.ru.attacker.ru", false}} {
		if got := Covered(tc.asset, pool); got != tc.want {
			t.Fatalf("Covered(%q)=%t want %t", tc.asset, got, tc.want)
		}
	}
}

func TestLinkerDoesNotMergeProgramsOnOnePlatform(t *testing.T) {
	programs := []Program{
		{Platform: "a", Slug: "one", Assets: []string{"*.acme.ru", "api.acme.ru"}},
		{Platform: "a", Slug: "two", Assets: []string{"*.acme.ru"}},
		{Platform: "b", Slug: "three", Assets: []string{"*.acme.ru", "api.acme.ru"}},
	}
	l := LinkPrograms(programs)
	if l.GroupKey(programs[0]) == l.GroupKey(programs[1]) {
		t.Fatal("same-platform programs merged")
	}
	// A shared domain alone has insufficient weight here; no arbitrary tie wins.
	if len(l.Links) != 0 {
		t.Fatalf("ambiguous shared domain caused a link: %+v", l.Links)
	}
}
