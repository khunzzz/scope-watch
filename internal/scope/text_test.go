package scope

import (
	"strings"
	"testing"
)

func TestReadableTextSemantics(t *testing.T) {
	cases := []struct{ name, old, new string }{
		{"formatting", `<h2 class="x">Rules</h2><p>Only <strong>safe</strong> tests.</p>`, `<h2>Rules</h2><p style="color:red">Only safe tests.</p>`},
		{"ignored", `<head><title>OLD</title></head><style>OLD</style><script>OLD</script><!--OLD--><p>Rules</p>`, `<head><title>NEW</title></head><style>NEW</style><script>NEW</script><p>Rules</p>`},
		{"wrapped table", `<table><tr><td>Only safe tests</td><td>10 requests</td></tr></table>`, "<table>\n <tr>\n <td>Only\n safe tests</td>\n <td>10 requests</td>\n </tr>\n</table>"},
		{"entities", `<p>A &amp; B &lt; 10&nbsp;requests &#x3e; 0</p>`, "A & B < 10 requests > 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TextDiff(tc.old, tc.new, 1500); got != "" {
				t.Fatalf("presentation-only diff: %q", got)
			}
		})
	}
	for _, s := range []string{"# Rules\n- x < 10 && y > 2\n<https://example.org/scope>", "    <table>payload</table>", "~~~html\n<table>payload</table>\n~~~", "<pre><code>  literal &lt;div&gt;\n    next</code></pre>", "`<img src=x onerror=alert(1)>`", "```html\n<table><tr><td>payload</td></tr></table>\n```", "<pre>  a &lt; b\n    &lt;img src=x&gt;</pre>", "<code>&lt;script&gt;alert(1)&lt;/script&gt;</code>"} {
		got := TextDiff("", s, 1500)
		want := s
		if strings.HasPrefix(s, "<pre>") {
			want = "  a < b\n    <img src=x>"
		}
		if strings.HasPrefix(s, "<pre><code>") {
			want = "  literal <div>\n    next"
		}
		if strings.HasPrefix(s, "<code>") {
			want = "<script>alert(1)</script>"
		}
		for _, line := range strings.Split(want, "\n") {
			if !strings.Contains(got, "+"+line) {
				t.Errorf("lost literal semantics: input=%q got=%q missing=%q", s, got, line)
			}
		}
	}
}

func TestTextDiffFencedCodeClosingLines(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"embedded backticks", "```html\nconst marker = \"```\";\n<style>old policy</style>\n```"},
		{"closing suffix", "```html\n``` not a closing fence\n<style>old policy</style>\n```"},
		{"short closing run", "````html\n```\n<style>old policy</style>\n````"},
		{"long closing run", "```html\nconst marker = \"```\";\n<style>old policy</style>\n```` \t"},
		{"indented closing fence", "```html\nconst marker = \"```\";\n<style>old policy</style>\n   ```\t"},
		{"tilde fence", "~~~html\nconst marker = \"~~~\";\n<style>old policy</style>\n~~~"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newText := strings.Replace(tc.text, "old policy", "new policy", 1)
			got := TextDiff(tc.text, newText, 1500)
			if !strings.Contains(got, "-<style>old policy</style>") || !strings.Contains(got, "+<style>new policy</style>") {
				t.Fatalf("literal fenced code edit lost: %q", got)
			}
		})
	}
	if got := readableText("before ```<style>literal</style>``` after"); got != "before ```<style>literal</style>``` after" {
		t.Fatalf("inline code changed: %q", got)
	}
}

func TestTextDiffPreBlockBoundaries(t *testing.T) {
	old := "<pre>allow</pre><pre>deny</pre>"
	new := "<pre>allowdeny</pre>"
	if got := TextDiff(old, new, 1500); got == "" {
		t.Fatal("separate preformatted blocks merged")
	}
	if got := readableText("before<pre>  a\n  b</pre>after"); got != "before\n  a\n  b\nafter" {
		t.Fatalf("preformatted boundaries or whitespace lost: %q", got)
	}
	if got := readableText("a<code>b</code>c"); got != "abc" {
		t.Fatalf("inline code became block-level: %q", got)
	}
}

func TestTextDiffKeepsBothSides(t *testing.T) {
	old := strings.Repeat("<tr><td>Old asset</td><td>excluded</td></tr>", 80)
	got := TextDiff("<table>"+old+"</table>", "<p>Only assets in Tasks are eligible.</p>", 500)
	if !strings.Contains(got, "+Only assets in Tasks are eligible.") || !strings.Contains(got, "-Old asset | excluded") || !strings.Contains(got, "…") {
		t.Fatalf("lost side of large replacement: %q", got)
	}
	if len([]rune(got)) > 500 {
		t.Fatalf("budget exceeded: %d", len([]rune(got)))
	}
}

func TestTextDiffLinkDestination(t *testing.T) {
	got := TextDiff(`<p>Test <a href="https://old.example/scope?a=1&amp;b=2">these assets</a></p>`, `<p>Test <a href="https://new.example/scope?a=1&amp;b=2">these assets</a></p>`, 1500)
	for _, value := range []string{"https://old.example/scope?a=1&b=2", "https://new.example/scope?a=1&b=2"} {
		if !strings.Contains(got, value) {
			t.Fatal(got)
		}
	}
}

func TestTextDiffHTMLBeforeClipping(t *testing.T) {
	wrap := func(value string) string {
		return "# Rules\n<table style=\"" + strings.Repeat("padding: 1px;", 80) + "\"><tr><th>Limit</th><td>" + value + "</td></tr></table>"
	}
	got := TextDiff(wrap("5 requests"), wrap("10 requests"), 1500)
	if !strings.Contains(got, "-Limit | 5 requests") || !strings.Contains(got, "+Limit | 10 requests") || strings.Contains(got, "style") || strings.Contains(got, "<table") {
		t.Fatalf("unreadable or lost substantive diff: %q", got)
	}
}
