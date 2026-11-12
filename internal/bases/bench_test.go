package bases

import (
	"fmt"
	"os"
	"testing"
	"time"

	"brain/internal/benchdata"
	"brain/internal/vault"
)

var benchVaults = map[int]*vault.Vault{}
var benchDirs []string

func TestMain(m *testing.M) {
	code := m.Run()
	for _, d := range benchDirs {
		os.RemoveAll(d)
	}
	os.Exit(code)
}

func benchVault(b *testing.B, n int) *vault.Vault {
	b.Helper()
	if v, ok := benchVaults[n]; ok {
		return v
	}
	d, err := os.MkdirTemp("", "bases-bench-")
	if err != nil {
		b.Fatal(err)
	}
	benchDirs = append(benchDirs, d)
	if err := benchdata.WriteRich(d, n); err != nil {
		b.Fatal(err)
	}
	v := vault.New(d, "")
	v.Scan()
	benchVaults[n] = v
	return v
}

// Bases as they show up in real vaults. Each is parsed and run per
// iteration, as the daemon does per query.
var benchBases = map[string]string{
	// Tag filter, formulas, two-key sort, groupBy and summaries.
	"table": `filters:
  and:
    - file.hasTag("bench")
    - 'status != "archived"'
formulas:
  score: 'priority * 2 + rating'
  overdue: 'if(due, due < now(), false)'
  age: '(now() - created).days.round(0)'
properties:
  formula.score:
    displayName: Score
views:
  - type: table
    name: All
    order: [file.name, status, priority, due, project, formula.score, formula.overdue, formula.age]
    sort:
      - property: priority
        direction: DESC
      - property: file.name
    groupBy:
      property: status
    summaries:
      priority: Average
      formula.score: Max
      due: Earliest
`,
	// Notes linking to the embedding note, plus backlinks per row.
	"links": `filters:
  or:
    - file.hasLink(this.file)
    - project == this
views:
  - type: table
    name: Linked
    order: [file.name, file.backlinks, project, related]
    sort:
      - property: file.mtime
        direction: DESC
`,
	// Sort by a text property and a link property, no filter.
	"sorted": `views:
  - type: table
    name: Sorted
    order: [file.name, title, project, file.tags]
    sort:
      - property: title
      - property: project
    limit: 50
`,
}

func BenchmarkRun(b *testing.B) {
	now := time.Date(2024, 6, 1, 0, 0, 0, 0, time.Local)
	for _, n := range []int{1000, 10000} {
		v := benchVault(b, n)
		this := benchdata.Name(n/2) + ".md"
		for _, name := range []string{"table", "links", "sorted"} {
			src := benchBases[name]
			b.Run(fmt.Sprintf("%s/%dk", name, n/1000), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					base, err := ParseBase(src)
					if err != nil {
						b.Fatal(err)
					}
					r := base.Run(v, Options{This: this, Now: now})
					if len(r.Errors) > 0 {
						b.Fatal(r.Errors)
					}
				}
			})
		}
	}
}

func BenchmarkParseBase(b *testing.B) {
	src := benchBases["table"]
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseBase(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNaturalCompare(b *testing.B) {
	pairs := [][2]string{{"Note 12", "Note 102"}, {"alpha beta", "Alpha Beta"}, {"archive/2024/note-454", "archive/2024/note-45"}, {"x", "y"}}
	b.ReportAllocs()
	for b.Loop() {
		for _, p := range pairs {
			naturalCompare(p[0], p[1])
		}
	}
}
