package yaml

import (
	"reflect"
	"testing"
)

func TestParseBase(t *testing.T) {
	src := `filters:
  or:
    - file.hasTag("tag")
    - and:
        - file.hasTag("book")
        - 'file.hasLink("Textbook")'
formulas:
  formatted_price: 'if(price, price.toFixed(2) + " dollars")'
  ppu: "(price / age).toFixed(2)"
properties:
  status:
    displayName: Status # comment
views:
  - type: table
    name: "My table"
    limit: 10
    order:
      - file.name
      - formula.ppu
    sort:
    - property: file.name
      direction: DESC
  - type: cards
    name: Cards
    filters: "has_coords == true"
tags: [a, "b c", 3]
obj: {x: 1, y: two}
text: |
  line one
  line two
folded: >-
  a
  b
`
	v, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"filters": map[string]any{"or": []any{
			`file.hasTag("tag")`,
			map[string]any{"and": []any{`file.hasTag("book")`, `file.hasLink("Textbook")`}},
		}},
		"formulas": map[string]any{
			"formatted_price": `if(price, price.toFixed(2) + " dollars")`,
			"ppu":             "(price / age).toFixed(2)",
		},
		"properties": map[string]any{"status": map[string]any{"displayName": "Status"}},
		"views": []any{
			map[string]any{"type": "table", "name": "My table", "limit": 10.0,
				"order": []any{"file.name", "formula.ppu"},
				"sort":  []any{map[string]any{"property": "file.name", "direction": "DESC"}}},
			map[string]any{"type": "cards", "name": "Cards", "filters": "has_coords == true"},
		},
		"tags":   []any{"a", "b c", 3.0},
		"obj":    map[string]any{"x": 1.0, "y": "two"},
		"text":   "line one\nline two\n",
		"folded": "a b",
	}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("got  %#v\nwant %#v", v, want)
	}
}

func TestFrontMatterish(t *testing.T) {
	v, err := Parse("---\ntitle: Hello: world\ndate: 2024-01-02\ndone: true\nn: ~\nlink: \"[[Note]]\"\n---\nbody")
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["title"] != "Hello: world" || m["date"] != "2024-01-02" || m["done"] != true || m["n"] != nil || m["link"] != "[[Note]]" {
		t.Fatalf("%#v", m)
	}
}
