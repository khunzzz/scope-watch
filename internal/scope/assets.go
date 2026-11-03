package scope

import (
	_ "embed"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ExtractorVersion changes when extraction semantics change, preventing false asset alerts.
const ExtractorVersion = 1

//go:embed tlds.txt
var tldText string
var tlds = set(strings.Fields(tldText))
var extensions = set(strings.Fields("pdf png jpg jpeg gif svg webp txt html htm php asp aspx jsp js json xml yml yaml md zip rar gz tar apk ipa exe doc docx xls xlsx csv log py sh env bak sql conf ini cfg css map ts go rb java class jar war so dll git lock mp4"))
var references = strings.Fields("owasp.org mitre.org first.org nist.gov cve.org portswigger.net hackerone.com bugcrowd.com wikipedia.org w3.org cvss.js example.com example.ru example.org test.com t.me telegram.me youtube.com youtu.be habr.com consultant.ru garant.ru")
var secondLevel = set(strings.Fields("com.ru net.ru org.ru pp.ru msk.ru spb.ru msk.su co.uk org.uk com.ua com.kz org.kz com.by co.il com.tr com.cn com.au co.jp"))
var pkgTails = set(strings.Fields("android app mobile ios client"))
var domainRE = regexp.MustCompile(`(?i)(?:\*\.)?(?:[a-z0-9а-яё*](?:[a-z0-9а-яё*-]{0,61}[a-z0-9а-яё*])?\.)+(?:[a-zа-яё]{2,24}|xn--[a-z0-9-]+)`)
var urlRE = regexp.MustCompile("(?i)https?://[^\\s<>()\\[\\]\\\"'`]+")
var storeRE = regexp.MustCompile("(?i)(?:www\\.)?(?:rustore\\.ru/catalog/app/|play\\.google\\.com/store/apps/details\\?id=|apps\\.apple\\.com/[^\\s]*?/id[0-9]|appgallery\\.huawei\\.com/app/)[^\\s<>()\\[\\]\\\"'`]*")
var mdLinkRE = regexp.MustCompile(`\[([^\]\n]*)\]\(([^)\s]*)\)`)
var altRE = regexp.MustCompile(`(?i)([\pL\pN_*.-]+)\.\(([a-z0-9|-]+)\)`)
var pkgRE = regexp.MustCompile(`(?i)(?:com|ru|org|net|io|app|me|tech|kz|by|uz)(?:\.[a-z][a-z0-9_]*){1,6}`)
var emailRE = regexp.MustCompile(`[\pL\pN_.+-]+@[\pL\pN_-]+(?:\.[\pL\pN_-]+)+`)
var ipRE = regexp.MustCompile(`(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])(?:\.(?:25[0-5]|2[0-4][0-9]|1?[0-9]?[0-9])){3}(?:/[0-9]{1,2})?`)
var iosRE = regexp.MustCompile(`/id([0-9]+)`)
var rustoreRE = regexp.MustCompile(`/app/([\pL\pN_.]+)`)
var huaweiRE = regexp.MustCompile(`/app/(C?[0-9]+)`)
var oosRE = regexp.MustCompile(`(?i)не\s*(?:нужно\s*|надо\s*|следует\s*)?(?:иска|тестир|атак|проверя|исследова)|вне\s+(?:скоуп|област|рам|программ)|out[\s-]*of[\s-]*scope|not\s+in\s+scope|не\s+вход[яи]т|не\s+относ[яи]тся|исключени|исключа[ею]тся|не\s+рассматрива|не\s+принима|не\s+являются\s+цел|запрещ|excluded|exclusion|non[\s-]*qualifying|ineligible`)
var headingRE = regexp.MustCompile(`^\s{0,3}(?:(#{1,6})\s*(.+?)\s*#*\s*$|\*\*(.+?)\*\*\s*:?\s*$|__(.+?)__\s*:?\s*$)`)
var labelRE = regexp.MustCompile(`^\s*[-*]?\s*([^:]{3,80}):\s*(.*)$`)

func set(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

func sorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

func word(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func before(s string, i int) rune {
	if i == 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

func after(s string, i int) rune {
	if i == len(s) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}

func replaceBounded(s string, re *regexp.Regexp, left, right func(rune) bool, f func(string) string) string {
	var b strings.Builder
	pos := 0
	for _, loc := range re.FindAllStringIndex(s, -1) {
		if left(before(s, loc[0])) || right(after(s, loc[1])) {
			continue
		}
		b.WriteString(s[pos:loc[0]])
		b.WriteString(f(s[loc[0]:loc[1]]))
		pos = loc[1]
	}
	b.WriteString(s[pos:])
	return b.String()
}

func normDomain(d string, exclude []string) string {
	d = strings.Trim(strings.ToLower(d), ".")
	if strings.HasPrefix(d, "*") {
		tail := strings.TrimLeft(d, "*")
		if strings.HasPrefix(tail, ".") {
			d = "*" + tail
		} else if !strings.HasPrefix(tail, "*") {
			d = tail
		}
	}
	parts := strings.Split(d, ".")
	tld := parts[len(parts)-1]
	if !tlds[tld] || extensions[tld] || len([]rune(d)) < 4 {
		return ""
	}
	bare := strings.TrimLeft(d, "*.")
	for _, r := range append(append([]string{}, references...), exclude...) {
		if bare == r || strings.HasSuffix(bare, "."+r) {
			return ""
		}
	}
	return d
}

func appURL(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return ""
	}
	var re *regexp.Regexp
	prefix := ""
	switch strings.ToLower(u.Hostname()) {
	case "play.google.com":
		if id := u.Query().Get("id"); id != "" {
			return "android:" + id
		}
	case "apps.apple.com", "itunes.apple.com":
		re = iosRE
		prefix = "ios:id"
	case "rustore.ru", "www.rustore.ru":
		re = rustoreRE
		prefix = "rustore:"
	case "appgallery.huawei.com":
		re = huaweiRE
		prefix = "appgallery:"
	}
	if re != nil {
		if m := re.FindStringSubmatch(u.Path); m != nil {
			return prefix + m[1]
		}
	}
	return ""
}

var reserved = func() []netip.Prefix {
	var p []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
		"192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
		"224.0.0.0/4", "240.0.0.0/4",
	} {
		p = append(p, netip.MustParsePrefix(s))
	}
	return p
}()

func globalIP(raw string) bool {
	p, e := netip.ParsePrefix(raw)
	if e != nil {
		a, e := netip.ParseAddr(raw)
		if e != nil {
			return false
		}
		p = netip.PrefixFrom(a, 32)
	}
	if !p.Addr().Is4() {
		return false
	}
	for _, r := range reserved {
		if r.Overlaps(p) {
			return false
		}
	}
	return true
}

func ExtractAssets(md string, exclude []string) []string {
	found := map[string]bool{}
	text := mdLinkRE.ReplaceAllString(md, "${1} ${2}")
	text = replaceBounded(text, storeRE,
		func(r rune) bool { return word(r) || r == '/' || r == '.' },
		func(r rune) bool { return false },
		func(s string) string { return "https://" + s },
	)
	var b strings.Builder
	for i := 0; i < len(text); {
		if strings.HasPrefix(text[i:], "**") && (i == 0 || text[i-1] != '\\') {
			b.WriteByte(' ')
			i += 2
		} else {
			b.WriteByte(text[i])
			i++
		}
	}
	text = b.String()
	for _, raw := range urlRE.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ".,;:!?*_")
		if app := appURL(raw); app != "" {
			found[app] = true
			continue
		}
		u, e := url.Parse(raw)
		if e == nil {
			if globalIP(u.Hostname()) {
				found[u.Hostname()] = true
			} else if d := normDomain(u.Hostname(), exclude); d != "" {
				found[d] = true
			}
		}
	}
	text = urlRE.ReplaceAllString(text, " ")
	text = emailRE.ReplaceAllString(text, " ")
	text = strings.NewReplacer(`\*`, "*", `\.`, ".").Replace(text)
	text = altRE.ReplaceAllStringFunc(text, func(s string) string {
		m := altRE.FindStringSubmatch(s)
		var parts []string
		for _, a := range strings.Split(m[2], "|") {
			parts = append(parts, m[1]+"."+a)
		}
		return strings.Join(parts, " ")
	})
	text = replaceBounded(text, pkgRE,
		func(r rune) bool { return word(r) || strings.ContainsRune(".@/-", r) && r != 0 },
		func(r rune) bool { return word(r) || r == '-' },
		func(s string) string {
			tok := strings.ToLower(s)
			parts := strings.Split(tok, ".")
			last := parts[len(parts)-1]
			if !tlds[last] || pkgTails[last] || strings.Contains(tok, "_") {
				found["android:"+tok] = true
				return " "
			}
			return s
		},
	)
	replaceBounded(text, ipRE,
		func(r rune) bool { return unicode.IsDigit(r) || r == '.' },
		func(r rune) bool { return unicode.IsDigit(r) || r == '.' },
		func(s string) string {
			if globalIP(s) {
				found[s] = true
			}
			return s
		},
	)
	replaceBounded(text, domainRE,
		func(r rune) bool { return word(r) || r != 0 && strings.ContainsRune("@.-", r) },
		func(r rune) bool { return word(r) || r == '-' },
		func(s string) string {
			if d := normDomain(s, exclude); d != "" {
				found[d] = true
			}
			return s
		},
	)
	return sorted(found)
}

func SplitScope(md string, exclude []string) ([]string, []string) {
	type section struct {
		level   int
		outside bool
	}
	var stack []section
	var in, out []string
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		if h := headingRE.FindStringSubmatch(line); h != nil {
			level := len(h[1])
			if level == 0 {
				level = 7
			}
			title := ""
			for _, s := range h[2:] {
				if s != "" {
					title = s
					break
				}
			}
			for len(stack) > 0 && stack[len(stack)-1].level >= level {
				stack = stack[:len(stack)-1]
			}
			outside := oosRE.MatchString(title) || len(stack) > 0 && stack[len(stack)-1].outside
			stack = append(stack, section{level, outside})
			continue
		}
		if m := labelRE.FindStringSubmatch(line); m != nil && oosRE.MatchString(m[1]) {
			out = append(out, m[2])
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1].outside {
			out = append(out, line)
		} else {
			in = append(in, line)
		}
	}
	ins := ExtractAssets(strings.Join(in, "\n"), exclude)
	outs := ExtractAssets(strings.Join(out, "\n"), exclude)
	inside := set(ins)
	filtered := []string{}
	for _, s := range outs {
		if !inside[s] {
			filtered = append(filtered, s)
		}
	}
	return ins, filtered
}

func Registrable(asset string) string {
	if strings.ContainsAny(asset, ": ") {
		return ""
	}
	if _, e := netip.ParseAddr(asset); e == nil {
		return ""
	}
	if _, e := netip.ParsePrefix(asset); e == nil {
		return ""
	}
	labels := strings.Split(strings.TrimLeft(asset, "*."), ".")
	if len(labels) < 2 {
		return ""
	}
	n := 2
	if secondLevel[strings.Join(labels[len(labels)-2:], ".")] && len(labels) >= 3 {
		n = 3
	}
	return strings.Join(labels[len(labels)-n:], ".")
}

func LinkTokens(items []string) map[string]bool {
	out := map[string]bool{}
	for _, a := range items {
		p, _, _ := strings.Cut(a, ":")
		if slices.Contains([]string{"android", "ios", "rustore", "appgallery"}, p) {
			out[a] = true
		} else if r := Registrable(a); r != "" {
			out[r] = true
		}
	}
	return out
}

func Covered(asset string, pool map[string]bool) bool {
	if pool[asset] {
		return true
	}
	bare := strings.TrimLeft(asset, "*.")
	for p := range pool {
		if strings.HasPrefix(p, "*.") {
			base := p[2:]
			if bare == base || strings.HasSuffix(bare, "."+base) {
				return true
			}
		}
	}
	return false
}
