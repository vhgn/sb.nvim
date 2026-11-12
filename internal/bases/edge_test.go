package bases

import (
	"testing"
	"time"
)

func TestEdgeExpressions(t *testing.T) {
	v := writeVault(t, map[string]string{"a.md": "---\nx: 12\n---\n"})
	cases := map[string]string{
		`"ab".repeat(number("x"))`:                       "",
		`"a:b:c".replace(":", "-")`:                      "a-b-c",
		`"a:b:c".replace(/:/, "-")`:                      "a-b:c",
		`"a:b:c".replace(/:/g, "-")`:                     "a-b-c",
		`"John Smith".replace(/(\w+) (\w+)/, "$2_$1")`:   "Smith_John",
		`"ab".replace(/b/, "[$&]$$")`:                    "a[b]$",
		`[1,[2,[3,4]]].flat().length`:                    "3",
		`1/0`:                                            "Infinity",
		`number("inf")`:                                  "NaN",
		`date("2024-01-31") + "1M"`:                      "2024-02-29",
		`date("2024-03-31") - "1 month"`:                 "2024-02-29",
		`(date("2024-01-05") - date("2024-01-01")).days`: "4",
		`date("2024-01-05") - date("2024-01-01")`:        "4 days",
		`duration("1.5d").hours`:                         "36",
		`(duration("5h") * 2).hours`:                     "10",
		`"abc"[1]`:                                       "b",
	}
	for src, want := range cases {
		b := &Base{Formulas: map[string]string{"f": src}, Views: []View{{Type: "table", Name: "t", Order: []string{"formula.f"}}}}
		r := b.Run(v, Options{Now: time.Now()})
		if got := r.Groups[0].Rows[0].Cells[0].Text; got != want {
			t.Errorf("%s = %q, want %q (errors %v)", src, got, want, r.Errors)
		}
	}
}

func TestBrokenFilterMatchesNothing(t *testing.T) {
	v := writeVault(t, map[string]string{"a.md": "x"})
	b, _ := ParseBase("filters: 'x >'\n")
	r := b.Run(v, Options{})
	if r.Total != 0 || len(r.Errors) == 0 {
		t.Fatalf("total %d errors %v", r.Total, r.Errors)
	}
}
