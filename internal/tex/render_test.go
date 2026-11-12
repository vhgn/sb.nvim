package tex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderSamples(t *testing.T) {
	samples := map[string]string{
		"quadratic": `x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`,
		"sum":       `\sum_{i=1}^{n} i^2 = \frac{n(n+1)(2n+1)}{6}`,
		"integral":  `\int_0^\infty e^{-x^2}\,dx = \frac{\sqrt{\pi}}{2}`,
		"matrix":    `A = \begin{pmatrix} a & b \\ c & d \end{pmatrix}, \quad \det A = ad - bc`,
		"cases":     `f(x) = \begin{cases} x^2 & x \geq 0 \\ -x & \text{otherwise} \end{cases}`,
		"left":      `\left( \frac{a}{b} \right)^2 + \left[ \sum_k x_k \right] \leq \lim_{n \to \infty} \hat{x}_n \vec{v}`,
		"aligned":   `\begin{aligned} a &= b + c \\ &= \mathbb{R}^n \cap \mathcal{L} \end{aligned}`,
	}
	dir := os.Getenv("TEX_OUT")
	for name, src := range samples {
		svg, err := SVG(src, Options{Display: true, FG: "#c0caf5", Size: 40})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if dir != "" {
			os.WriteFile(filepath.Join(dir, name+".svg"), []byte(svg), 0o644)
		}
	}
}

func TestUnicode(t *testing.T) {
	cases := map[string]string{
		`\alpha^2 + \beta_i`: "α² + βᵢ",
		`\frac{1}{2}`:        "½",
		`x \leq \sqrt{y}`:    "x ≤ √y",
		`\mathbb{R}^n`:       "ℝⁿ",
		`e^{i\pi} = -1`:      "e^(iπ) = −1",
	}
	for src, want := range cases {
		got, _ := Unicode(src)
		if got != want {
			t.Errorf("%s: got %q want %q", src, got, want)
		}
	}
}
