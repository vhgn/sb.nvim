package yaml

import (
	"strings"
	"testing"

	"brain/internal/benchdata"
)

func BenchmarkParseFrontMatter(b *testing.B) {
	note := benchdata.RichNote(3, 1000)
	fm := strings.SplitN(note, "---\n", 3)[1]
	b.SetBytes(int64(len(fm)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(fm); err != nil {
			b.Fatal(err)
		}
	}
}

const benchBase = `filters:
  and:
    - file.hasTag("bench")
    - 'status != "archived"'
formulas:
  score: 'priority * 2 + rating'
  overdue: 'if(due, due < now(), false)'
properties:
  formula.score:
    displayName: Score
views:
  - type: table
    name: All
    order: [file.name, status, priority, due, project, formula.score, formula.overdue]
    sort:
      - property: priority
        direction: DESC
      - property: file.name
    groupBy:
      property: status
    summaries:
      priority: Average
      formula.score: Max
`

func BenchmarkParseBaseYAML(b *testing.B) {
	b.SetBytes(int64(len(benchBase)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(benchBase); err != nil {
			b.Fatal(err)
		}
	}
}
