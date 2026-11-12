// Package md extracts the structure the language server cares about from a
// markdown document: front matter title, ATX headings and links.
package md

import (
	"net/url"
	"regexp"
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

var (
	headingRe = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	wikiRe    = regexp.MustCompile(`\[\[([^\[\]|#]*)(?:#([^\[\]|]*))?(?:\|([^\[\]]*))?\]\]`)
	mdLinkRe  = regexp.MustCompile(`(!?)\[([^\[\]]*)\]\(([^()\s]*)(?:\s+"[^"]*")?\)`)
	titleRe   = regexp.MustCompile(`^title:\s*(.+)$`)
	codeRe    = regexp.MustCompile("`[^`]*`")
)

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
					if m := titleRe.FindStringSubmatch(lines[k]); m != nil {
						d.Title = strings.Trim(strings.TrimSpace(m[1]), `"'`)
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
		if m := headingRe.FindStringSubmatchIndex(line); m != nil && m[4] >= 0 && m[5] > m[4] {
			text := line[m[4]:m[5]]
			d.Headings = append(d.Headings, Heading{
				Level: m[3] - m[2],
				Text:  text,
				Slug:  Slug(text),
				Span:  Span{Line: i, Start: m[4], End: m[5]},
			})
			if d.Title == "" && m[3]-m[2] == 1 {
				d.Title = text
			}
		}
		d.Links = append(d.Links, parseLinks(line, i)...)
	}
	return d
}

func parseLinks(line string, n int) []Link {
	if !strings.Contains(line, "[") {
		return nil
	}
	// Mask inline code so links inside `code` are ignored; byte offsets stay valid.
	masked := codeRe.ReplaceAllStringFunc(line, func(s string) string { return strings.Repeat(" ", len(s)) })
	var links []Link
	for _, m := range wikiRe.FindAllStringSubmatchIndex(masked, -1) {
		l := Link{Kind: WikiLink, Target: strings.TrimSpace(line[m[2]:m[3]]), Span: Span{n, m[0], m[1]}}
		if m[4] >= 0 {
			l.Anchor = strings.TrimSpace(line[m[4]:m[5]])
		}
		if m[6] >= 0 {
			l.Alias = strings.TrimSpace(line[m[6]:m[7]])
		}
		links = append(links, l)
	}
	for _, m := range mdLinkRe.FindAllStringSubmatchIndex(masked, -1) {
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
	sort.Slice(links, func(i, j int) bool { return links[i].Span.Start < links[j].Span.Start })
	return links
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
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
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
