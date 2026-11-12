package lsp

import (
	"reflect"
	"strings"
	"testing"
)

func format(text string, width int) string {
	lines := strings.Split(text, "\n")
	changed := formatLines(lines, width)
	var out []string
	for i, l := range lines {
		if c, ok := changed[i]; ok {
			out = append(out, c...)
		} else {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func TestFormatWrapsBetweenWords(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"paragraph", "aaa bbb ccc ddd eee", "aaa bbb\nccc ddd\neee"},
		{"short lines stay", "aaa\nbbb", "aaa\nbbb"},
		{"long word alone", "a verylongwordhere b", "a\nverylongwordhere\nb"},
		{"list continuation", "- aaa bbb ccc ddd", "- aaa bbb\n  ccc ddd"},
		{"ordered list", "10. aaa bbb ccc", "10. aaa\n    bbb\n    ccc"},
		{"task", "- [ ] aaa bbb ccc", "- [ ] aaa\n      bbb\n      ccc"},
		{"quote", "> aaa bbb ccc ddd", "> aaa bbb\n> ccc ddd"},
		{"quoted list", "> - aaa bbb ccc", "> - aaa\n>   bbb\n>   ccc"},
		{"wiki link kept whole", "see [[Some Long Note]] now", "see\n[[Some Long Note]]\nnow"},
		{"md link kept whole", "see [a b c](x.md) now ok", "see\n[a b c](x.md)\nnow ok"},
		{"code kept whole", "run `go test ./...` now", "run\n`go test ./...`\nnow"},
		{"no new list item", "aaa bbbbbb - ccc", "aaa bbbbbb -\nccc"},
		{"no new heading", "aaa bbbbbbb # ccc", "aaa\nbbbbbbb #\nccc"},
		{"hard break kept", "aaa bbb ccc ddd  ", "aaa bbb\nccc ddd  "},
		{"heading untouched", "# aaa bbb ccc ddd", "# aaa bbb ccc ddd"},
		{"table untouched", "| aaa | bbb | ccc |", "| aaa | bbb | ccc |"},
		{"fence untouched", "```\naaa bbb ccc ddd\n```", "```\naaa bbb ccc ddd\n```"},
		{"math untouched", "$$\naaa bbb ccc ddd\n$$", "$$\naaa bbb ccc ddd\n$$"},
		{"indented code untouched", "x\n\n    aaa bbb ccc ddd", "x\n\n    aaa bbb ccc ddd"},
		{"front matter untouched", "---\ntitle: aaa bbb ccc ddd\n---\nx", "---\ntitle: aaa bbb ccc ddd\n---\nx"},
	}
	for _, c := range cases {
		if got := format(c.in, 10); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestFormatIsIdempotentAndKeepsWords(t *testing.T) {
	in := "- **Hardware:** Arch laptop 32 GB (main lab), Mac mini 16 GB (always-on targets/monitoring), Windows 8 GB (avoid). See [[Lab Overview]] and [the plan](plan.md#phase-0)."
	once := format(in, 80)
	for _, l := range strings.Split(once, "\n") {
		if cells(l) > 80 {
			t.Errorf("line over 80: %q", l)
		}
	}
	if twice := format(once, 80); twice != once {
		t.Errorf("not idempotent:\n%s\n---\n%s", once, twice)
	}
	if !reflect.DeepEqual(strings.Fields(in), strings.Fields(once)) {
		t.Errorf("words changed:\n%s", once)
	}
}

func TestFormatWidthZeroIsOff(t *testing.T) {
	if got := formatLines([]string{strings.Repeat("a ", 100)}, 0); len(got) != 0 {
		t.Errorf("width 0 changed lines: %v", got)
	}
}
