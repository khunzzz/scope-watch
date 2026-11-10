package telegram

import (
	"fmt"
	"html"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/khunzzz/scope-watch/internal/scope"
)

const MessageMax = 4000

func Units(s string) int { return len(utf16.Encode([]rune(s))) }

func Clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func escaped(s string, budget int) string {
	var b strings.Builder
	used := 0
	for _, r := range s {
		part := html.EscapeString(string(r))
		if used+Units(part) > budget-1 {
			b.WriteString("…")
			break
		}
		b.WriteString(part)
		used += Units(part)
	}
	return b.String()
}

func Money(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + " " + s[i:]
	}
	return s
}

func Range(r []int) string {
	if len(r) != 2 {
		return "—"
	}
	if r[0] == 0 || r[0] == r[1] {
		return Money(r[1])
	}
	return Money(r[0]) + "–" + Money(r[1])
}

var severity = map[string]string{"low": "low", "medium": "medium", "high": "high", "critical": "crit"}

type Renderer struct{ titles map[string]string }

func NewRenderer(titles map[string]string) *Renderer {
	cp := map[string]string{}
	for k, v := range titles {
		cp[k] = v
	}
	return &Renderer{titles: cp}
}

func (r *Renderer) Title(name string) string {
	if t := r.titles[name]; t != "" {
		return t
	}
	return name
}

func (r *Renderer) link(p scope.Program, text string) string {
	if text == "" {
		text = r.Title(p.Platform)
	}
	return `<a href="` + html.EscapeString(p.URL) + `">` + escaped(text, 200) + `</a>`
}

func Rewards(p scope.Program) string {
	parts := []string{}
	for _, s := range scope.Severities {
		if v := p.Rewards[s]; len(v) == 2 {
			parts = append(parts, severity[s]+" "+Range(v))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "💰 " + strings.Join(parts, " · ") + " ₽"
}

func AssetBlock(items []string, budget int) string {
	out := []string{}
	used := 0
	for i, a := range items {
		a = html.EscapeString(a)
		if used+Units(a)+1 > budget {
			out = append(out, fmt.Sprintf("…ещё %d", len(items)-i))
			break
		}
		out = append(out, a)
		used += Units(a) + 1
	}
	return "<pre>" + strings.Join(out, "\n") + "</pre>"
}

func splitKnown(items []string, peers []scope.Program) ([]string, []string) {
	pool := map[string]bool{}
	for _, p := range peers {
		for _, a := range p.Assets {
			pool[a] = true
		}
	}
	fresh, known := []string{}, []string{}
	for _, a := range items {
		if scope.Covered(a, pool) {
			known = append(known, a)
		} else {
			fresh = append(fresh, a)
		}
	}
	return fresh, known
}

func (r *Renderer) peerNames(peers []scope.Program) string {
	names := []string{}
	for _, p := range peers {
		names = append(names, r.Title(p.Platform))
	}
	return escaped(strings.Join(names, ", "), 300)
}

func (r *Renderer) summary(p scope.Program, peers []scope.Program) []string {
	lines := []string{}
	if len(p.Rewards) > 0 {
		lines = append(lines, Rewards(p))
	}
	if len(p.Assets) > 0 {
		fresh, known := splitKnown(p.Assets, peers)
		src := ""
		if p.AssetsSource != "structured" {
			src = ", из текста"
		}
		lines = append(lines, fmt.Sprintf("Ассеты (%d%s):", len(p.Assets), src))
		shown := fresh
		if len(shown) == 0 {
			shown = p.Assets
		}
		lines = append(lines, AssetBlock(shown, 1200))
		if len(peers) > 0 && len(known) > 0 && len(fresh) > 0 {
			lines = append(lines, fmt.Sprintf("ещё %d уже были в скоупе на %s", len(known), r.peerNames(peers)))
		}
	} else {
		lines = append(lines, "Ассеты из текста не извлеклись, смотри описание.")
	}
	if len(p.OutOfScope) > 0 {
		lines = append(lines, "Out of scope: "+escaped(strings.Join(p.OutOfScope, ", "), 400))
	}
	return lines
}

func (r *Renderer) section(c scope.Change, peers []scope.Program) []string {
	p := c.Program
	lines := []string{"<b>" + escaped(r.Title(p.Platform), 100) + "</b> · " + r.link(p, p.Name)}
	if c.Kind == "new" {
		return append(append(lines, "🆕 <b>Новая программа</b>"), r.summary(p, peers)...)
	}
	if c.Kind == "removed" {
		return append(lines, "🗑 Пропала из публичного списка (стала приватной или удалена).")
	}
	reopened := c.Activated != nil && *c.Activated
	if reopened {
		lines = append(lines, "♻️ <b>Снова активна</b>")
		lines = append(lines, r.summary(p, peers)...)
	} else if c.Activated != nil {
		lines = append(lines, "🔒 <b>Закрыта</b> ("+escaped(p.Status, 100)+")")
	}
	if c.RenamedFrom != "" {
		lines = append(lines, "✏️ Было название: "+escaped(c.RenamedFrom, 200))
	}
	if len(c.AssetsAdded) > 0 && !reopened {
		fresh, known := splitKnown(c.AssetsAdded, peers)
		lines = append(lines, fmt.Sprintf("➕ <b>Новые ассеты (%d)</b>", len(c.AssetsAdded)))
		if len(fresh) > 0 {
			lines = append(lines, AssetBlock(fresh, 1200))
		}
		if len(known) > 0 {
			lines = append(lines, "уже были в скоупе на "+r.peerNames(peers)+":", AssetBlock(known, 400))
		}
	}
	for _, item := range []struct {
		title  string
		items  []string
		budget int
	}{
		{fmt.Sprintf("➖ Убраны ассеты (%d):", len(c.AssetsRemoved)), c.AssetsRemoved, 500},
		{fmt.Sprintf("💸 Больше не платят за (%d):", len(c.BountyLost)), c.BountyLost, 400},
		{fmt.Sprintf("💰 Теперь платят за (%d):", len(c.BountyGained)), c.BountyGained, 400},
		{"🚫 Добавлено в out of scope:", c.OOSAdded, 400},
		{"✅ Убрано из out of scope:", c.OOSRemoved, 400},
	} {
		if len(item.items) > 0 {
			lines = append(lines, item.title, AssetBlock(item.items, item.budget))
		}
	}
	if len(c.Rewards) > 0 {
		lines = append(lines, "💰 <b>Выплаты</b>")
		for _, d := range c.Rewards {
			a, b := 0, 0
			if len(d.Before) == 2 {
				a = d.Before[1]
			}
			if len(d.After) == 2 {
				b = d.After[1]
			}
			arrow := "📉"
			if b > a {
				arrow = "📈"
			}
			lines = append(lines, arrow+" "+severity[d.Severity]+": "+Range(d.Before)+" → "+Range(d.After))
		}
	}
	if len(c.NewVersions) > 0 {
		versions := []string{}
		for _, v := range c.NewVersions {
			k, n, _ := strings.Cut(v, ":")
			if k == "rule" {
				k = "правила"
			}
			if k == "price" {
				k = "выплаты"
			}
			versions = append(versions, k+" v"+n)
		}
		lines = append(lines, "🗂 Новая версия: "+escaped(strings.Join(versions, ", "), 300))
	}
	if c.TextDiff != "" {
		text := c.TextDiff
		if c.Old != nil {
			// Persisted diffs may have been clipped before HTML normalization.
			// Only complete snapshots can reconstruct the actual change.
			text = scope.TextDiff(c.Old.ScopeMD, p.ScopeMD, 1500)
		} else if legacyHTMLDiff(text) {
			lines = append(lines, "📝 Текст изменения недоступен в читаемом виде: сохранён старый сокращённый HTML-diff; полные правила по ссылке: "+r.link(p, "правила"))
			return lines
		}
		budget := 1500
		if len(c.AssetsAdded)+len(c.AssetsRemoved) > 0 {
			budget = 700
		}
		if text != "" {
			lines = append(lines, "📝 Текст скоупа/правил (− удалено, + добавлено):", "<pre>"+escapedDiff(text, budget)+"</pre>")
		}
	}
	return lines
}

// legacyHTMLDiff recognizes presentation markup, including unfinished tags
// clipped by older versions. It never attempts to reconstruct missing text.
func legacyHTMLDiff(text string) bool {
	lower := strings.ToLower(text)
	for _, tag := range []string{"table", "thead", "tbody", "tfoot", "tr", "td", "th", "div", "p", "span", "style", "script", "head", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "pre", "code", "a", "b", "strong", "i", "em", "u", "s", "del", "font", "html", "body", "section", "article", "blockquote", "dl", "dt", "dd", "br", "hr"} {
		for _, prefix := range []string{"<" + tag, "</" + tag} {
			rest := lower
			for {
				i := strings.Index(rest, prefix)
				if i < 0 {
					break
				}
				rest = rest[i+len(prefix):]
				if rest == "" || strings.ContainsAny(rest[:1], " >\t\r\n/") {
					return true
				}
			}
		}
	}
	return false
}

// Give each sign its own encoded budget as escaping can expand text fivefold.
func escapedDiff(text string, budget int) string {
	var sides [2][]string
	side := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "+") {
			side = 1
		} else if strings.HasPrefix(line, "-") {
			side = 0
		}
		sides[side] = append(sides[side], line)
	}
	if len(sides[0]) == 0 || len(sides[1]) == 0 {
		return escaped(text, budget)
	}
	return escaped(strings.Join(sides[0], "\n"), (budget-1)/2) + "\n" + escaped(strings.Join(sides[1], "\n"), (budget-1)/2)
}

func icon(changes []scope.Change) string {
	for _, c := range changes {
		if c.Kind == "new" {
			return "🆕"
		}
	}
	for _, c := range changes {
		if len(c.AssetsAdded) > 0 {
			return "🎯"
		}
	}
	for _, c := range changes {
		if c.Kind == "removed" || c.Activated != nil && !*c.Activated {
			return "🔒"
		}
	}
	return "🔔"
}

func (r *Renderer) Render(changes []scope.Change, l *scope.Linker) []string {
	groups := map[string][]scope.Change{}
	keys := []string{}
	before := map[string]*scope.Program{}
	for _, c := range changes {
		k := l.GroupKey(c.Program)
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], c)
		before[c.Program.Key()] = c.Old
	}
	messages := []string{}
	for _, key := range keys {
		chs := groups[key]
		head := icon(chs) + " <b>" + escaped(chs[0].Program.Name, 200) + "</b>\n\n"
		parts := []string{}
		changed := map[string]bool{}
		for _, c := range chs {
			changed[c.Program.Key()] = true
		}
		others := map[string]scope.Program{}
		for _, c := range chs {
			peers := []scope.Program{}
			for _, p := range l.Peers(c.Program) {
				if !changed[p.Key()] {
					others[p.Key()] = p
				}
				if old, ok := before[p.Key()]; ok {
					if old != nil {
						peers = append(peers, *old)
					}
				} else {
					peers = append(peers, p)
				}
			}
			if len(parts) > 0 {
				parts = append(parts, "")
			}
			parts = append(parts, r.section(c, peers)...)
		}
		if len(others) > 0 {
			names := []string{}
			otherKeys := []string{}
			for k := range others {
				otherKeys = append(otherKeys, k)
			}
			slices.Sort(otherKeys)
			for _, k := range otherKeys {
				p := others[k]
				names = append(names, escaped(p.Name, 100)+" ("+r.link(p, "")+")")
			}
			parts = append(parts, "", "🔗 Также на: "+strings.Join(names, ", "))
		}
		msg := head
		for _, part := range parts {
			if Units(msg)+Units(part)+1 > MessageMax && msg != head {
				messages = append(messages, strings.TrimSpace(msg))
				msg = head
			}
			if Units(part)+Units(head) > MessageMax {
				part = escaped(part, MessageMax-Units(head)-10)
			}
			msg += part + "\n"
		}
		messages = append(messages, strings.TrimSpace(msg))
	}
	return messages
}

func (r *Renderer) MassChange(platform string, changes []scope.Change) string {
	lines := []string{fmt.Sprintf(
		"⚠️ <b>%s</b>: за один прогон изменилось сразу %d программ. "+
			"Похоже, площадка поменяла формат. Поштучно не рассылаю, "+
			"детали: <code>scope-watch history</code>.",
		escaped(r.Title(platform), 100), len(changes),
	)}
	for _, kind := range []string{"new", "removed", "updated"} {
		names := []string{}
		for _, c := range changes {
			if c.Kind == kind {
				names = append(names, c.Program.Name)
			}
		}
		if len(names) > 0 {
			label := map[string]string{
				"new":     "🆕 новые",
				"removed": "🗑 пропали",
				"updated": "🔔 изменились",
			}[kind]
			lines = append(lines, fmt.Sprintf("%s (%d): %s",
				label, len(names), escaped(strings.Join(names[:min(25, len(names))], ", "), 800),
			))
		}
	}
	return strings.Join(lines, "\n")
}

func Seeded(lines []string) string {
	return "🌱 <b>scope-watch</b>: база сохранена, по ней уведомлений не будет.\n" + html.EscapeString(strings.Join(lines, "\n"))
}

func (r *Renderer) HealthAlert(platform string, fails int, err string) string {
	return fmt.Sprintf(
		"⚠️ <b>%s</b>: сбор неполон %d проверок подряд.\n<code>%s</code>\nВозможно, поменяли API или вёрстку.",
		escaped(r.Title(platform), 100), fails, escaped(err, 300),
	)
}

func (r *Renderer) HealthOK(platform string) string {
	return "✅ <b>" + escaped(r.Title(platform), 100) + "</b> снова доступен."
}
