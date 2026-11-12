package yaml

import (
	"reflect"
	"testing"
)

func TestEdgeCases(t *testing.T) {
	cases := []struct {
		src  string
		want any
	}{
		{"f: a &&\n  b\nn: 1", map[string]any{"f": "a && b", "n": 1.0}},
		{"f: \"a &&\n  b\"\nn: 1", map[string]any{"f": "a && b", "n": 1.0}},
		{"- a ||\n  b\n- c", []any{"a || b", "c"}},
		{"o: [a,\n  b]\nn: 1", map[string]any{"o": []any{"a", "b"}, "n": 1.0}},
		{"z: 012\nh: 0x1F\ni: +inf\nj: -Infinity\nk: nan", map[string]any{"z": 12.0, "h": 31.0, "i": "+inf", "j": "-Infinity", "k": "nan"}},
		{"a: x # c\n# full\nb: |\n  keep # this\n", map[string]any{"a": "x", "b": "keep # this\n"}},
	}
	for _, c := range cases {
		got, err := Parse(c.src)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) = %#v, %v; want %#v", c.src, got, err, c.want)
		}
	}
}
