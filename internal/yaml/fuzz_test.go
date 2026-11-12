package yaml

import "testing"

func FuzzParse(f *testing.F) {
	for _, s := range []string{"a: 1\nb: [1, {c: d}]\n", "- a\n- b: |\n    x\n  c: >-\n    y\n", "k: 'x''y' # c\n", "a:\n- 1\n- 2", "\"q\": {a: [1,2], b: \"x\"}", "a: |+\n\n", "- - - a"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { Parse(s) })
}
