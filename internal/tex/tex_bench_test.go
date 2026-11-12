package tex

import "testing"

// Display formulas as they appear in notes (rendered to SVG by the daemon).
var benchDisplay = []string{
	`x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`,
	`\sum_{i=1}^{n} i^2 = \frac{n(n+1)(2n+1)}{6}`,
	`\int_0^\infty e^{-x^2}\,dx = \frac{\sqrt{\pi}}{2}`,
	`A = \begin{pmatrix} a & b \\ c & d \end{pmatrix}, \quad \det A = ad - bc`,
	`f(x) = \begin{cases} x^2 & x \geq 0 \\ -x & \text{otherwise} \end{cases}`,
	`\left( \frac{a}{b} \right)^2 + \left[ \sum_k x_k \right] \leq \lim_{n \to \infty} \hat{x}_n \vec{v}`,
	`\begin{aligned} a &= b + c \\ &= \mathbb{R}^n \cap \mathcal{L} \end{aligned}`,
}

// Inline formulas (converted to Unicode for conceal).
var benchInline = []string{
	`\alpha^2 + \beta_i`, `x \leq \sqrt{y}`, `\mathbb{R}^n`, `e^{i\pi} = -1`, `\frac{1}{2}`,
	`O(n \log n)`, `a_{ij}`, `\sum_k x_k`, `f'(x)`, `\vec{v} \cdot \vec{w}`,
}

func BenchmarkSVG(b *testing.B) {
	opt := Options{Display: true, FG: "#c0caf5", Size: 21, Palette: map[string]string{"red": "#ff0000", "blue": "#0000ff"}}
	b.ReportAllocs()
	for b.Loop() {
		for _, src := range benchDisplay {
			if _, err := SVG(src, opt); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkUnicode(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		for _, src := range benchInline {
			Unicode(src)
		}
	}
}
