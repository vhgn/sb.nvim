// Package md extracts the structure the language server cares about from a
// markdown document: front matter title, ATX headings and links.
package md

import (
	"net/url"
	"sort"
	"strings"
	"unicode"
)

type LinkKind int

const (
	WikiLink     LinkKind = iota // [[target#anchor|alias]]
	MarkdownLink                 // [text](path.md#anchor)
)

// Span is a byte range on a single line. End is exclusive.
type Span struct {
	Line, Start, End int
}

func (s Span) Contains(line, col int) bool {
	return s.Line == line && col >= s.Start && col < s.End
}

type Heading struct {
	Level int
	Text  string
	Slug  string
	Span  Span // the heading text
}

type Link struct {
	Kind   LinkKind
	Target string // path or note name, without anchor; empty means "this document"
	Anchor string
	Alias  string
	Span   Span // the whole link, brackets included
}

type Doc struct {
	Lines     []string
	Title     string // front matter title, else first H1, else ""
	BodyStart int    // first line after front matter
	Headings  []Heading
	Links     []Link
}

func Parse(text string) *Doc {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	d := &Doc{Lines: lines}

	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for j := 1; j < len(lines); j++ {
			if t := strings.TrimSpace(lines[j]); t == "---" || t == "..." {
				for k := 1; k < j; k++ {
					// equivalent to ^title:\s*(.+)$ followed by TrimSpace
					if v, ok := strings.CutPrefix(lines[k], "title:"); ok && v != "" {
						d.Title = strings.Trim(strings.TrimSpace(v), `"'`)
					}
				}
				i = j + 1
				break
			}
		}
	}
	d.BodyStart = i

	fence := ""
	for ; i < len(lines); i++ {
		line := lines[i]
		if fence != "" {
			if f := fenceOf(line); f != "" && f[0] == fence[0] && len(f) >= len(fence) &&
				strings.TrimSpace(strings.TrimLeft(line, " ")[len(f):]) == "" {
				fence = ""
			}
			continue
		}
		if f := fenceOf(line); f != "" {
			fence = f
			continue
		}
		if maybeHeading(line) {
			d.parseHeading(line, i)
		}
		d.Links = append(d.Links, parseLinks(line, i)...)
	}
	return d
}

// parseHeading recognises an ATX heading. It is a hand-written equivalent of
//
//	^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$
//
// keeping only matches with non-empty text: after the opening hashes and the
// blanks following them, the text is the shortest prefix whose remainder is
// an optional closing "[ \t]+#+" plus trailing blanks.
func (d *Doc) parseHeading(line string, n int) {
	i := 0
	for i < 3 && i < len(line) && line[i] == ' ' {
		i++
	}
	h := i
	for h < len(line) && line[h] == '#' {
		h++
	}
	level := h - i
	if level < 1 || level > 6 || h == len(line) || !isBlank(line[h]) {
		return
	}
	start := h
	for start < len(line) && isBlank(line[start]) {
		start++
	}
	end := start
	for ; end < len(line); end++ {
		if isBlank(line[end]) && closesHeading(line[end:]) {
			break
		}
	}
	if end == start {
		return
	}
	text := line[start:end]
	d.Headings = append(d.Headings, Heading{
		Level: level,
		Text:  text,
		Slug:  Slug(text),
		Span:  Span{Line: n, Start: start, End: end},
	})
	if d.Title == "" && level == 1 {
		d.Title = text
	}
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

// closesHeading reports whether s matches (?:[ \t]+#+)?[ \t]*$ ; s starts with a blank.
func closesHeading(s string) bool {
	t := strings.TrimRight(s, " \t")
	if t == "" {
		return true
	}
	t = strings.TrimLeft(t, " \t")
	return t != "" && strings.Trim(t, "#") == ""
}

func parseLinks(line string, n int) []Link {
	hasWiki, hasMd := strings.Contains(line, "[["), strings.Contains(line, "](")
	if !hasWiki && !hasMd {
		return nil
	}
	// Mask inline code so links inside `code` are ignored; byte offsets stay valid.
	masked := line
	if strings.IndexByte(line, '`') >= 0 {
		masked = maskCode(line)
	}
	var links []Link
	if hasWiki {
		links = wikiLinks(line, masked, n, links)
	}
	if hasMd {
		links = mdLinks(line, masked, n, links)
	}
	if hasWiki && hasMd {
		sort.SliceStable(links, func(i, j int) bool { return links[i].Span.Start < links[j].Span.Start })
	}
	return links
}

// maybeHeading reports whether line starts with up to three spaces and a '#'.
func maybeHeading(line string) bool {
	for i := 0; i < len(line) && i < 4; i++ {
		switch line[i] {
		case '#':
			return true
		case ' ':
		default:
			return false
		}
	}
	return false
}

// maskCode blanks inline code spans (`...`) with spaces, preserving offsets.
func maskCode(line string) string {
	b := []byte(line)
	for i := 0; i < len(b); i++ {
		if b[i] != '`' {
			continue
		}
		j := strings.IndexByte(line[i+1:], '`')
		if j < 0 {
			break
		}
		end := i + 1 + j
		for k := i; k <= end; k++ {
			b[k] = ' '
		}
		i = end
	}
	return string(b)
}

func wikiLinks(line, masked string, n int, links []Link) []Link {
	for _, m := range findAll(masked, "[[", matchWiki) {
		l := Link{Kind: WikiLink, Target: strings.TrimSpace(line[m[2]:m[3]]), Span: Span{n, m[0], m[1]}}
		if m[4] >= 0 {
			l.Anchor = strings.TrimSpace(line[m[4]:m[5]])
		}
		if m[6] >= 0 {
			l.Alias = strings.TrimSpace(line[m[6]:m[7]])
		}
		links = append(links, l)
	}
	return links
}

func mdLinks(line, masked string, n int, links []Link) []Link {
	for _, m := range findAll(masked, "[", matchMdLink) {
		if m[3] > m[2] { // image
			continue
		}
		raw := line[m[6]:m[7]]
		if raw == "" || strings.Contains(raw, "://") || strings.HasPrefix(raw, "mailto:") {
			continue
		}
		target, anchor, _ := strings.Cut(raw, "#")
		if u, err := url.PathUnescape(target); err == nil {
			target = u
		}
		links = append(links, Link{
			Kind: MarkdownLink, Target: target, Anchor: anchor,
			Alias: line[m[4]:m[5]], Span: Span{n, m[0], m[1]},
		})
	}
	return links
}

// The link matchers below are hand-written equivalents of these patterns,
// returning the same submatch index layout as regexp's FindStringSubmatchIndex:
//
//	wiki: \[\[([^\[\]|#]*)(?:#([^\[\]|]*))?(?:\|([^\[\]]*))?\]\]
//	md:   (!?)\[([^\[\]]*)\]\(([^()\s]*)(?:\s+"[^"]*")?\)
//
// Every quantifier in them is greedy over a class that excludes whatever must
// follow, so the first greedy attempt at a position is the only possible
// match there and no backtracking is needed.

type match [8]int

// findAll returns leftmost, non-overlapping matches like regexp's FindAll.
// Matches can only start at an occurrence of lead (or one byte before it).
func findAll(s, lead string, at func(string, int) (match, bool)) []match {
	var out []match
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], lead)
		if j < 0 {
			break
		}
		start := i + j
		m, ok := match{}, false
		if lead == "[" && start > i && s[start-1] == '!' {
			m, ok = at(s, start-1) // "![" starts further left, so the regexp tries it first
		}
		if !ok {
			m, ok = at(s, start)
		}
		if ok {
			out = append(out, m)
			i = m[1]
		} else {
			i = start + 1
		}
	}
	return out
}

// span advances from i over bytes not in stop.
func span(s string, i int, stop string) int {
	for i < len(s) && strings.IndexByte(stop, s[i]) < 0 {
		i++
	}
	return i
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }

func matchWiki(s string, i int) (match, bool) {
	m := match{i, 0, i + 2, 0, -1, -1, -1, -1}
	j := span(s, i+2, "[]|#")
	m[3] = j
	if j < len(s) && s[j] == '#' {
		m[4] = j + 1
		j = span(s, j+1, "[]|")
		m[5] = j
	}
	if j < len(s) && s[j] == '|' {
		m[6] = j + 1
		j = span(s, j+1, "[]")
		m[7] = j
	}
	if !strings.HasPrefix(s[j:], "]]") {
		return m, false
	}
	m[1] = j + 2
	return m, true
}

func matchMdLink(s string, i int) (match, bool) {
	m := match{i, 0, i, i, 0, 0, 0, 0}
	j := i
	if s[j] == '!' {
		j++
		m[3] = j
	}
	if j >= len(s) || s[j] != '[' {
		return m, false
	}
	m[4] = j + 1
	j = span(s, j+1, "[]")
	m[5] = j
	if !strings.HasPrefix(s[j:], "](") {
		return m, false
	}
	m[6] = j + 2
	j = m[6]
	for j < len(s) && s[j] != '(' && s[j] != ')' && !isSpace(s[j]) {
		j++
	}
	m[7] = j
	if j < len(s) && isSpace(s[j]) {
		k := j
		for k < len(s) && isSpace(s[k]) {
			k++
		}
		if k < len(s) && s[k] == '"' {
			if q := strings.IndexByte(s[k+1:], '"'); q >= 0 {
				j = k + 1 + q + 1
			}
		}
	}
	if j >= len(s) || s[j] != ')' {
		return m, false
	}
	m[1] = j + 1
	return m, true
}

func fenceOf(line string) string {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 {
		return ""
	}
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(t) && t[n] == c {
			n++
		}
		if n >= 3 {
			return t[:n]
		}
	}
	return ""
}

// Slug produces a GitHub style heading anchor.
func Slug(s string) string {
	s = strings.TrimSpace(s)
	if slug, ok := slugASCII(s); ok {
		return slug
	}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// slugASCII is Slug for pure ASCII input; ok is false for anything else.
func slugASCII(s string) (slug string, ok bool) {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 0x80:
			return "", false
		case c >= 'A' && c <= 'Z':
			b = append(b, c+'a'-'A')
		case c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-':
			b = append(b, c)
		case c == ' ':
			b = append(b, '-')
		}
	}
	return string(b), true
}

// LinkAt returns the link covering the byte position, if any.
func (d *Doc) LinkAt(line, col int) *Link {
	for i := range d.Links {
		if d.Links[i].Span.Contains(line, col) {
			return &d.Links[i]
		}
	}
	return nil
}

// HeadingAt returns the index of the heading on the line, or -1.
func (d *Doc) HeadingAt(line int) int {
	for i, h := range d.Headings {
		if h.Span.Line == line {
			return i
		}
	}
	return -1
}

// FindHeading matches an anchor against heading text (case-insensitive) or slug.
func (d *Doc) FindHeading(anchor string) int {
	if anchor == "" {
		return -1
	}
	slug := Slug(anchor)
	for i, h := range d.Headings {
		if strings.EqualFold(h.Text, anchor) || h.Slug == slug {
			return i
		}
	}
	return -1
}

// SectionEnd returns the last line belonging to heading i.
func (d *Doc) SectionEnd(i int) int {
	for _, h := range d.Headings[i+1:] {
		if h.Level <= d.Headings[i].Level {
			return h.Span.Line - 1
		}
	}
	return len(d.Lines) - 1
}
