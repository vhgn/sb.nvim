package md

import "testing"

const sample = "---\ntitle: \"My Note\"\n---\n# Top\n\nsee [[other#Sub Part|alias]] and [x](dir/a%20b.md#sec)\n" +
	"![img](pic.png) [web](https://x.y) `[[not a link]]`\n" +
	"```\n# not heading [[nope]]\n```\n## Sub ##\n### Deeper\n## Second\n"

func TestParse(t *testing.T) {
	d := Parse(sample)
	if d.Title != "My Note" || d.BodyStart != 3 {
		t.Fatalf("title=%q bodyStart=%d", d.Title, d.BodyStart)
	}
	want := []struct {
		level int
		text  string
		line  int
	}{{1, "Top", 3}, {2, "Sub", 10}, {3, "Deeper", 11}, {2, "Second", 12}}
	if len(d.Headings) != len(want) {
		t.Fatalf("headings: %+v", d.Headings)
	}
	for i, w := range want {
		h := d.Headings[i]
		if h.Level != w.level || h.Text != w.text || h.Span.Line != w.line {
			t.Errorf("heading %d = %+v, want %+v", i, h, w)
		}
	}
	if len(d.Links) != 2 {
		t.Fatalf("links: %+v", d.Links)
	}
	w := d.Links[0]
	if w.Kind != WikiLink || w.Target != "other" || w.Anchor != "Sub Part" || w.Alias != "alias" || w.Span != (Span{5, 4, 28}) {
		t.Errorf("wiki link = %+v", w)
	}
	m := d.Links[1]
	if m.Kind != MarkdownLink || m.Target != "dir/a b.md" || m.Anchor != "sec" {
		t.Errorf("md link = %+v", m)
	}
	if d.SectionEnd(1) != 11 || d.SectionEnd(3) != len(d.Lines)-1 {
		t.Errorf("section ends: %d %d", d.SectionEnd(1), d.SectionEnd(3))
	}
	if d.FindHeading("second") != 3 || d.FindHeading("Sub") != 1 {
		t.Error("FindHeading")
	}
}

func TestSlug(t *testing.T) {
	if got := Slug("Hello, World! 2"); got != "hello-world-2" {
		t.Fatal(got)
	}
}
