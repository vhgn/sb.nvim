package bases

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"brain/internal/vault"
)

func writeVault(t *testing.T, files map[string]string) *vault.Vault {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v := vault.New(dir, "")
	v.Scan()
	return v
}

func TestRunTable(t *testing.T) {
	v := writeVault(t, map[string]string{
		"books/a.md": "---\ntags: [book]\nprice: 10\nage: 2\nstatus: done\ndue: 2024-01-05\n---\nSee [[b]]",
		"books/b.md": "---\ntags: [book]\nprice: 30\nage: 3\nstatus: todo\n---\n#extra",
		"c.md":       "---\nprice: 5\n---\n",
		"img.png":    "x",
	})
	b, err := ParseBase(`
filters:
  and:
    - file.hasTag("book")
formulas:
  ppu: "(price / age).toFixed(1)"
  label: 'file.basename + ": " + status'
properties:
  formula.ppu:
    displayName: PPU
views:
  - type: table
    name: T
    order: [file.name, formula.ppu, formula.label, due]
    sort:
      - property: price
        direction: DESC
    summaries:
      formula.ppu: Max
`)
	if err != nil {
		t.Fatal(err)
	}
	r := b.Run(v, Options{Now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local)})
	if len(r.Errors) > 0 {
		t.Fatal(r.Errors)
	}
	if r.Columns[1].Name != "PPU" || len(r.Groups) != 1 || len(r.Groups[0].Rows) != 2 {
		t.Fatalf("%+v", r)
	}
	got := []string{}
	for _, row := range r.Groups[0].Rows {
		var cells []string
		for _, c := range row.Cells {
			cells = append(cells, c.Text)
		}
		got = append(got, strings.Join(cells, "|"))
	}
	want := []string{"b.md|10.0|b: todo|", "a.md|5.0|a: done|2024-01-05"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
	if r.Summaries[1].Text != "10" {
		t.Fatalf("summary %+v", r.Summaries)
	}
}

func TestExpressions(t *testing.T) {
	v := writeVault(t, map[string]string{
		"n.md": "---\ndate: 2024-03-10\nlist: [1, 2, 3]\nname: Hello World\nlink: \"[[o]]\"\n---\n[[o]] #tag/sub",
		"o.md": "",
	})
	e := &evaluator{v: v, now: time.Date(2024, 3, 12, 0, 0, 0, 0, time.Local), formulas: map[string]Node{}, rows: map[*vault.File]*row{}}
	r := e.rowOf(v.Get("n.md"))
	cases := map[string]string{
		`date + "1 day"`:                           "2024-03-11",
		`(now() - date) / 86400000`:                "2",
		`date.format("ddd, MMM Do YYYY")`:          "Sun, Mar 10th 2024",
		`list.map(value * 2).join("-")`:            "2-4-6",
		`list.filter(value > 1).length`:            "2",
		`list.reduce(acc + value, 0)`:              "6",
		`name.lower().split(" ")[1]`:               "world",
		`file.hasTag("tag")`:                       "true",
		`file.hasLink("o")`:                        "true",
		`link.asFile().name`:                       "o.md",
		`file.backlinks.length`:                    "0",
		`if(name.contains("Hello"), "y", "n")`:     "y",
		`/hel+o/i.matches(name)`:                   "true",
		`[1, 2] + [3]`:                             "1, 2, 3",
		`max(3, 9, 2) + min(list)`:                 "10",
		`1 + 2 * 3 - 4 / 2`:                        "5",
		`date > "2024-01-01" && !(date < today())`: "false",
	}
	for src, want := range cases {
		n, err := Parse(src)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		got, err := e.Eval(n, scope{r: r})
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if toString(got) != want {
			t.Errorf("%s = %q, want %q", src, toString(got), want)
		}
	}
}
