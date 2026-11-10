package scope

import (
	"fmt"
	stdhtml "html"
	"strings"

	"golang.org/x/net/html"
)

// readableText normalizes presentation before diffing, never stored snapshots.
// Tokenization (rather than tag-shaped regular expressions) keeps comparisons
// and unknown angle-bracket notation intact.
func readableText(s string) string {
	// Shield Markdown code spans/fences from the HTML tokenizer and whitespace
	// folding. Placeholders are chosen outside the source's vocabulary.
	marker := "\x00scope-code"
	for strings.Contains(s, marker) {
		marker += "x"
	}
	var literals []string
	stash := func(text string) string {
		literals = append(literals, text)
		return fmt.Sprintf("%s%d\x00", marker, len(literals)-1)
	}
	var protected strings.Builder
	for i := 0; i < len(s); {
		if (i == 0 || (i >= 2 && s[i-2:i] == "\n\n")) && (strings.HasPrefix(s[i:], "    ") || strings.HasPrefix(s[i:], "\t")) {
			end := i
			for end < len(s) && (strings.HasPrefix(s[end:], "    ") || strings.HasPrefix(s[end:], "\t")) {
				n := strings.IndexByte(s[end:], '\n')
				if n < 0 {
					end = len(s)
					break
				}
				end += n + 1
			}
			protected.WriteString(stash(s[i:end]))
			i = end
			continue
		}
		if s[i] == '`' || (s[i] == '~' && (i == 0 || s[i-1] == '\n')) {
			j := i + 1
			for j < len(s) && s[j] == s[i] {
				j++
			}
			delimiter := s[i:j]
			if s[i] == '`' || len(delimiter) >= 3 {
				end := -1
				lineStart := strings.LastIndexByte(s[:i], '\n') + 1
				if len(delimiter) >= 3 && i-lineStart <= 3 && strings.Trim(s[lineStart:i], " ") == "" {
					// Fences close only on their own line, not on a delimiter
					// embedded in literal code or followed by non-whitespace.
					for line := j; line < len(s); {
						n := strings.IndexByte(s[line:], '\n')
						if n < 0 {
							break
						}
						line += n + 1
						start := line
						for start < len(s) && start-line < 3 && s[start] == ' ' {
							start++
						}
						close := start
						for close < len(s) && s[close] == s[i] {
							close++
						}
						suffix := close
						for suffix < len(s) && strings.ContainsAny(s[suffix:suffix+1], " 	\r") {
							suffix++
						}
						if close-start >= len(delimiter) && (suffix == len(s) || s[suffix] == '\n') {
							end = close
							break
						}
					}
				} else if k := strings.Index(s[j:], delimiter); k >= 0 {
					end = j + k + len(delimiter)
				}
				if end >= 0 {
					protected.WriteString(stash(s[i:end]))
					i = end
					continue
				}
			}
		}
		protected.WriteByte(s[i])
		i++
	}
	z := html.NewTokenizer(strings.NewReader(protected.String()))
	var out strings.Builder
	ignored, literal := "", ""
	var code strings.Builder
	cell := false
	flow, tables := 0, 0
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		raw := string(z.Raw())
		token := z.Token()
		end := kind == html.EndTagToken
		if ignored != "" {
			if end && token.Data == ignored {
				ignored = ""
			}
			continue
		}
		if literal != "" {
			if literal == "pre" && token.Data == "code" && (kind == html.StartTagToken || end) {
				continue
			}
			if end && token.Data == literal {
				out.WriteString(stash(code.String()))
				if literal == "pre" {
					out.WriteByte('\n')
				}
				code.Reset()
				literal = ""
			} else {
				code.WriteString(stdhtml.UnescapeString(raw))
			}
			continue
		}
		if kind == html.StartTagToken {
			switch token.Data {
			case "head", "script", "style":
				ignored = token.Data
				continue
			case "pre", "code":
				if token.Data == "pre" {
					out.WriteByte('\n')
				}
				literal = token.Data
				continue
			}
		}
		if kind == html.TextToken {
			text := token.Data
			if tables > 0 && flow == 0 && strings.TrimSpace(text) == "" {
				continue
			}
			if flow > 0 {
				folded := strings.Join(strings.Fields(text), " ")
				if len(text) > 0 && strings.ContainsAny(text[:1], " \t\r\n") {
					out.WriteByte(' ')
				}
				out.WriteString(folded)
				if len(text) > 0 && strings.ContainsAny(text[len(text)-1:], " \t\r\n") {
					out.WriteByte(' ')
				}
			} else {
				out.WriteString(text)
			}
			continue
		}
		if kind == html.CommentToken || kind == html.DoctypeToken {
			continue
		}
		switch token.Data {
		case "p", "li", "td", "th", "h1", "h2", "h3", "h4", "h5", "h6":
			if end {
				if flow > 0 {
					flow--
				}
			} else {
				flow++
			}
		case "table":
			if end {
				if tables > 0 {
					tables--
				}
			} else {
				tables++
			}
		}
		switch token.Data {
		case "table", "thead", "tbody", "tfoot", "div", "p", "section", "article", "blockquote", "ul", "ol", "dl", "dt", "dd", "br", "hr":
			out.WriteByte('\n')
		case "tr":
			out.WriteByte('\n')
			cell = false
		case "td", "th":
			if !end {
				if cell {
					out.WriteString(" | ")
				}
				cell = true
			}
		case "h1", "h2", "h3", "h4", "h5", "h6":
			out.WriteByte('\n')
			if !end {
				out.WriteString(strings.Repeat("#", int(token.Data[1]-'0')) + " ")
			}
		case "li":
			out.WriteByte('\n')
			if !end {
				out.WriteString("- ")
			}
		case "a":
			if !end {
				for _, a := range token.Attr {
					if a.Key == "href" {
						out.WriteString("[" + a.Val + "] ")
					}
				}
			}
		case "b", "strong", "i", "em", "u", "s", "del", "span", "font", "html", "body":
		default:
			out.WriteString(raw)
		}
	}
	if literal != "" {
		out.WriteString(stash(code.String()))
	}
	var lines []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			lines = append(lines, line)
		}
	}
	result := strings.Join(lines, "\n")
	// Restore in reverse order because HTML code may enclose Markdown code.
	for i := len(literals) - 1; i >= 0; i-- {
		result = strings.ReplaceAll(result, fmt.Sprintf("%s%d\x00", marker, i), literals[i])
	}
	return result
}
