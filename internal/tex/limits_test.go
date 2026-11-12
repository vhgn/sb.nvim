package tex

import (
	"strings"
	"testing"
	"time"
)

func TestPathologicalInputIsBounded(t *testing.T) {
	cases := map[string]string{
		"deep frac":    strings.Repeat(`\frac{`, 2000) + "x" + strings.Repeat("}{y}", 2000),
		"deep sqrt":    strings.Repeat(`\sqrt{`, 3000) + "x" + strings.Repeat("}", 3000),
		"deep scripts": strings.Repeat("x^{", 3000) + "y" + strings.Repeat("}", 3000),
		"deep left":    strings.Repeat(`\left(`, 2000) + "x" + strings.Repeat(`\right)`, 2000),
		"deep optarg":  strings.Repeat(`\sqrt[`, 300) + "x" + strings.Repeat("]{y}", 300),
		"open braces":  strings.Repeat("{", 100000),
		"too long":     strings.Repeat("x", MaxSource+1),
		"too wide":     strings.Repeat("x", 9000),
	}
	for name, src := range cases {
		start := time.Now()
		_, err := SVG(src, Options{Display: true})
		if err == nil {
			t.Errorf("%s: want an error", name)
		}
		Unicode(src)
		if d := time.Since(start); d > 500*time.Millisecond {
			t.Errorf("%s: took %v", name, d)
		}
	}
}

func TestErrorsAndLimits(t *testing.T) {
	for src, want := range map[string]string{
		`\frac`:     "missing argument",
		`\frac{a}`:  "missing argument",
		`{`:         "missing }",
		`\left( x`:  `missing \right`,
		`x^`:        "missing argument",
		`x_{a}_{b}`: "",
	} {
		err := Check(src)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("%q: got %q want %q", src, got, want)
		}
	}
	// a nesting depth just under the limit still renders
	ok := strings.Repeat("{", MaxDepth-2) + "x" + strings.Repeat("}", MaxDepth-2)
	if _, err := SVG(ok, Options{}); err != nil {
		t.Errorf("depth %d: %v", MaxDepth-2, err)
	}
	// font size is clamped instead of producing a gigantic image
	svg, err := SVG("x", Options{Size: 1e9})
	if err != nil || !strings.Contains(svg, `font-size="256.00"`) {
		t.Errorf("size clamp: %v", err)
	}
}
