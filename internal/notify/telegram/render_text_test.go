package telegram

import (
	"strings"
	"testing"

	"github.com/khunzzz/scope-watch/internal/scope"
)

func TestRenderLegacyHTMLDiff(t *testing.T) {
	p := scope.Program{Name: "Bank", Platform: "bizone", URL: "https://example.org/rules", ScopeMD: "<p>Current complete rules</p>"}
	c := scope.Change{Kind: "updated", Program: p, TextDiff: `-<table style="padding: 100px;…`}
	r := NewRenderer(nil)
	got := strings.Join(r.section(c, nil), "\n")
	if strings.Contains(got, "&lt;table") || !strings.Contains(got, "полные правила по ссылке") || !strings.Contains(got, p.URL) {
		t.Fatal(got)
	}
	old := p
	old.ScopeMD = "<p>Previous rules</p>"
	c.Old = &old
	got = strings.Join(r.section(c, nil), "\n")
	if !strings.Contains(got, "-Previous rules") || !strings.Contains(got, "+Current complete rules") || strings.Contains(got, "&lt;table") {
		t.Fatal(got)
	}
}

func TestRenderLegacyFormattingFallback(t *testing.T) {
	for _, tag := range []string{"b", "strong", "pre", "code", "h4", "h5", "h6", "a", "blockquote", "section", "article", "font"} {
		c := scope.Change{Kind: "updated", Program: scope.Program{Name: "Bank", URL: "https://example.org/rules"}, TextDiff: "-<" + tag + " style=\"padding: 100px;…"}
		got := strings.Join(NewRenderer(nil).section(c, nil), "\n")
		if strings.Contains(got, "&lt;"+tag) || !strings.Contains(got, "полные правила по ссылке") {
			t.Errorf("clipped %s markup not handled: %s", tag, got)
		}
	}
	for _, text := range []string{"-x < 10\n+x < 20", "+<https://example.org/rules>", "+<placeholder>"} {
		if legacyHTMLDiff(text) {
			t.Errorf("plain text misclassified as legacy markup: %q", text)
		}
	}
}

func TestRenderDiffKeepsBothEncodedSides(t *testing.T) {
	p := scope.Program{Name: "Bank", Platform: "bizone", URL: "https://example.org/rules"}
	old := p
	old.ScopeMD = strings.Repeat("<p>"+strings.Repeat("&amp;&lt;&gt;", 10)+" old policy</p>", 80)
	p.ScopeMD = "<p>Only assets in Tasks are eligible.</p>"
	c := scope.ComparePrograms(old, p)
	// The encoded removal side must not consume the added sentence's budget.
	got := strings.Join(NewRenderer(nil).section(c, nil), "\n")
	if !strings.Contains(got, "+Only assets in Tasks are eligible.") || !strings.Contains(got, "-&amp;&lt;&gt;") {
		t.Fatal(got)
	}
}
