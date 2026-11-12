package md

// The reference parser below is the original regexp-based implementation.
// The hand-written scanners in parse.go must agree with it exactly.

import (
	"math/rand"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"

	"brain/internal/benchdata"
)

var (
	refHeadingRe = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	refWikiRe    = regexp.MustCompile(`\[\[([^\[\]|#]*)(?:#([^\[\]|]*))?(?:\|([^\[\]]*))?\]\]`)
	refMdLinkRe  = regexp.MustCompile(`(!?)\[([^\[\]]*)\]\(([^()\s]*)(?:\s+"[^"]*")?\)`)
	refTitleRe   = regexp.MustCompile(`^title:\s*(.+)$`)
	refCodeRe    = regexp.MustCompile("`[^`]*`")
)

func refParse(text string) *Doc {
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
					if m := refTitleRe.FindStringSubmatch(lines[k]); m != nil {
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
		if m := refHeadingRe.FindStringSubmatchIndex(line); m != nil && m[4] >= 0 && m[5] > m[4] {
			text := line[m[4]:m[5]]
			d.Headings = append(d.Headings, Heading{
				Level: m[3] - m[2],
				Text:  text,
				Slug:  refSlug(text),
				Span:  Span{Line: i, Start: m[4], End: m[5]},
			})
			if d.Title == "" && m[3]-m[2] == 1 {
				d.Title = text
			}
		}
		d.Links = append(d.Links, refParseLinks(line, i)...)
	}
	return d
}
func refParseLinks(line string, n int) []Link {
	if !strings.Contains(line, "[") {
		return nil
	}
	// Mask inline code so links inside `code` are ignored; byte offsets stay valid.
	masked := refCodeRe.ReplaceAllStringFunc(line, func(s string) string { return strings.Repeat(" ", len(s)) })
	var links []Link
	for _, m := range refWikiRe.FindAllStringSubmatchIndex(masked, -1) {
		l := Link{Kind: WikiLink, Target: strings.TrimSpace(line[m[2]:m[3]]), Span: Span{n, m[0], m[1]}}
		if m[4] >= 0 {
			l.Anchor = strings.TrimSpace(line[m[4]:m[5]])
		}
		if m[6] >= 0 {
			l.Alias = strings.TrimSpace(line[m[6]:m[7]])
		}
		links = append(links, l)
	}
	for _, m := range refMdLinkRe.FindAllStringSubmatchIndex(masked, -1) {
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
func refSlug(s string) string {
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

func checkSame(t *testing.T, s string) {
	t.Helper()
	got, want := Parse(s), refParse(s)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse(%q)\n got %+v\nwant %+v", s, got, want)
	}
}

func TestParseMatchesReference(t *testing.T) {
	for i := 0; i < 500; i++ {
		checkSame(t, benchdata.Note(i, 500))
	}
	frags := []string{"[[", "]]", "[", "]", "(", ")", "](", "![", "`", "#", "# ", "## ", "   #", "    #", "#######",
		"a", "Ä", "é", " ", "\t", "\f", "|", "#x", "x.md", "a%20b", "http://", "mailto:", "!", "\"", " \"t\")",
		"---\n", "```\n", "~~~\n", "\n", "\r\n", "title: x\n", "title:", "title:  \n", "'q'", "\xff"}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 200000; i++ {
		var b strings.Builder
		for j := r.Intn(30); j > 0; j-- {
			b.WriteString(frags[r.Intn(len(frags))])
		}
		checkSame(t, b.String())
	}
}

func TestSlugMatchesReference(t *testing.T) {
	for _, s := range []string{"", "  ", "!!!", "Hello, World! 2", "  a_b-c  ", "Ünïcödé Title", "MiXeD 123", "tab\there", "\xff x"} {
		if got, want := Slug(s), refSlug(s); got != want {
			t.Errorf("Slug(%q) = %q, want %q", s, got, want)
		}
	}
}
