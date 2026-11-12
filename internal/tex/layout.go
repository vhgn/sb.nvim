package tex

import (
	"errors"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Options control rendering.
type Options struct {
	FG      string  // text color, e.g. "#c0caf5"
	Error   string  // color for unknown commands
	Size    float64 // font size in px (1em)
	Display bool    // display style (block) or text style (inline)
	Font    string  // CSS font-family list
	Scale   float64 // output zoom used by the PNG converter; informational
	// Palette maps color names (red, blue, …) to hex, e.g. the terminal's ANSI colors.
	Palette map[string]string
}

// Rendering limits: font size in px, and the largest image produced.
const (
	MaxSize   = 256
	MaxPixels = 8192
	MaxArea   = 16 << 20
)

func (o *Options) defaults() {
	if o.FG == "" {
		o.FG = "#d0d0d0"
	}
	if o.Error == "" {
		o.Error = "#e06c75"
	}
	if o.Size <= 0 || o.Size != o.Size {
		o.Size = 20
	}
	if o.Size > MaxSize {
		o.Size = MaxSize
	}
	if o.Font == "" {
		o.Font = "'Latin Modern Math','STIX Two Math','Noto Serif','DejaVu Serif','Noto Sans Math',serif"
	}
}

const (
	stD = iota
	stT
	stS
	stSS
)

const (
	axis = 0.25
	rule = 0.05
)

func scaleOf(st int) float64 { return []float64{1, 1, .72, .55}[st] }
func supStyle(st int) int {
	if st < stS {
		return stS
	}
	return stSS
}
func fracStyle(st int) int { return []int{stT, stS, stSS, stSS}[st] }

type item struct {
	kind   byte // 't' text, 'r' rect, 'p' path (stroked), 'b' rect outline
	x, y   float64
	s      string
	size   float64
	font   string // it, rm, bf, bfit, tt, sf
	color  string
	w, h   float64
	width  float64 // textLength for runs
	stroke float64
}

type box struct {
	w, h, d float64
	items   []item
}

func (b *box) add(o box, dx, dy float64) {
	for _, it := range o.items {
		it.x += dx
		it.y += dy
		b.items = append(b.items, it)
	}
}

type ctx struct {
	st    int
	color string
	opt   *Options
}

func (c ctx) with(st int) ctx { c.st = st; return c }
func (c ctx) sc() float64     { return scaleOf(c.st) }

// ---- glyphs ----

func (c ctx) glyph(r rune, font string) box {
	s := c.sc()
	italic := false
	switch font {
	case "":
		italic = r < 0x80 && unicode.IsLetter(r) || r >= 0x3B1 && r <= 0x3D6
		font = "rm"
		if italic {
			font = "it"
		}
	case "bb", "cal", "frak", "scr":
		r = mapAlphabet(font, r)
		font = "rm"
	case "it":
		italic = true
	case "bfit":
		italic = true
	}
	w := advance(r, italic) * s
	h, d := extents(r)
	if r >= 0x1D400 {
		h, d = .7, .05
	}
	b := box{w: w, h: h * s, d: d * s}
	b.items = append(b.items, item{kind: 't', s: string(r), size: s, font: font, color: c.color})
	return b
}

func (c ctx) run(text, font string) box {
	s := c.sc()
	var w, h, d float64
	for _, r := range text {
		w += advance(r, font == "it") * s
		rh, rd := extents(r)
		h, d = math.Max(h, rh*s), math.Max(d, rd*s)
	}
	if text == "" {
		return box{}
	}
	color := c.color
	if font == "unknown" {
		color, font = c.opt.Error, "rm"
	}
	return box{w: w, h: h, d: d, items: []item{{kind: 't', s: text, size: s, font: font, color: color, width: w}}}
}

// ---- lists ----

var spacing = [8][8]int{
	//        ord op bin rel open close punct inner
	/*ord*/ {0, 1, 2, 3, 0, 0, 0, -1},
	/*op*/ {1, 1, 0, 3, 0, 0, 0, -1},
	/*bin*/ {2, 2, 0, 0, 2, 0, 0, 2},
	/*rel*/ {3, 3, 0, 0, 3, 0, 0, 3},
	/*open*/ {0, 0, 0, 0, 0, 0, 0, 0},
	/*close*/ {0, 1, 2, 3, 0, 0, 0, -1},
	/*punct*/ {-1, -1, 0, -1, -1, -1, -1, -1},
	/*inner*/ {-1, 1, 2, 3, -1, 0, -1, -1},
}

func spaceEm(code, st int) float64 {
	switch code {
	case 1:
		return 3.0 / 18
	case -1:
		if st <= stT {
			return 3.0 / 18
		}
	case 2:
		if st <= stT {
			return 4.0 / 18
		}
	case 3:
		if st <= stT {
			return 5.0 / 18
		}
	}
	return 0
}

func (c ctx) list(nodes []node) box {
	// Split on top-level line breaks / alignment into an aligned environment.
	for _, n := range nodes {
		switch n.(type) {
		case newlineNode, alignNode:
			return c.topLevelRows(nodes)
		}
	}
	var out box
	prev := -1
	for i, n := range nodes {
		if s, ok := n.(styleNode); ok {
			c.st = s.style
			continue
		}
		b, cls := c.atom(n)
		if cls == clsBin && (prev == -1 || prev == clsBin || prev == clsOp || prev == clsRel || prev == clsOpen || prev == clsPunct) {
			cls = clsOrd
		}
		if cls == clsBin && i+1 < len(nodes) {
			if _, ncls := classOf(nodes[i+1]); ncls == clsRel || ncls == clsClose || ncls == clsPunct {
				cls = clsOrd
			}
		} else if cls == clsBin && i+1 == len(nodes) {
			cls = clsOrd
		}
		if prev >= 0 && cls >= 0 {
			out.w += spaceEm(spacing[prev][cls], c.st) * c.sc()
		}
		out.add(b, out.w, 0)
		out.w += b.w
		out.h = math.Max(out.h, b.h)
		out.d = math.Max(out.d, b.d)
		if cls >= 0 {
			prev = cls
		}
	}
	return out
}

func classOf(n node) (node, int) {
	switch x := n.(type) {
	case charNode:
		return n, x.cls
	case opNode:
		return n, clsOp
	case scriptNode:
		return classOf(x.base)
	case leftRight:
		return n, clsInner
	case fracNode:
		return n, clsInner
	}
	return n, clsOrd
}

func (c ctx) atom(n node) (box, int) {
	switch x := n.(type) {
	case charNode:
		if x.font == "not" {
			b := c.glyph(x.r, "rm")
			b.items = append(b.items, item{kind: 'p', s: fmt.Sprintf("M %g %g L %g %g", b.w*.75, -b.h-.05, b.w*.25, b.d+.05), stroke: rule, color: c.color})
			return b, x.cls
		}
		return c.glyph(x.r, x.font), x.cls
	case groupNode:
		return c.list(x.list), clsOrd
	case textNode:
		return c.run(x.s, x.font), clsOrd
	case spaceNode:
		return box{w: x.em * c.sc()}, -1
	case opNode:
		return c.op(x, nil, nil), clsOp
	case scriptNode:
		_, cls := classOf(x.base)
		if op, ok := x.base.(opNode); ok {
			return c.op(op, x.sup, x.sub), clsOp
		}
		return c.scripts(x), cls
	case fracNode:
		return c.frac(x), clsInner
	case sqrtNode:
		return c.sqrt(x), clsOrd
	case accentNode:
		return c.accent(x), clsOrd
	case lineNode:
		b := c.atomBox(x.body)
		t := rule * c.sc()
		if x.over {
			b.items = append(b.items, item{kind: 'r', x: 0, y: -b.h - .12*c.sc() - t, w: b.w, h: t, color: c.color})
			b.h += .12*c.sc() + t + .04
		} else {
			b.items = append(b.items, item{kind: 'r', x: 0, y: b.d + .1*c.sc(), w: b.w, h: t, color: c.color})
			b.d += .1*c.sc() + t + .04
		}
		return b, clsOrd
	case braceNode:
		return c.brace(x), clsOrd
	case stackNode:
		base := c.atomBox(x.base)
		top := c.with(supStyle(c.st)).atomBox(x.top)
		var out box
		w := math.Max(base.w, top.w)
		out.w = w
		out.add(base, (w-base.w)/2, 0)
		if x.over {
			shift := base.h + .1 + top.d
			out.add(top, (w-top.w)/2, -shift)
			out.h, out.d = shift+top.h, base.d
		} else {
			shift := base.d + .1 + top.h
			out.add(top, (w-top.w)/2, shift)
			out.h, out.d = base.h, shift+top.d
		}
		_, cls := classOf(x.base)
		return out, cls
	case leftRight:
		return c.leftRight(x), clsInner
	case envNode:
		return c.env(x), clsInner
	case colorNode:
		cc := c
		if v, ok := c.opt.Palette[strings.ToLower(x.color)]; ok {
			cc.color = v
		} else if v, ok := colors[strings.ToLower(x.color)]; ok {
			cc.color = v
		} else if strings.HasPrefix(x.color, "#") || isHex(x.color) {
			cc.color = "#" + strings.TrimPrefix(x.color, "#")
		}
		return cc.atomBox(x.body), clsOrd
	case phantomNode:
		b := c.atomBox(x.body)
		b.items = nil
		return b, clsOrd
	case boxedNode:
		b := c.atomBox(x.body)
		pad := .15
		var out box
		out.w, out.h, out.d = b.w+2*pad, b.h+pad, b.d+pad
		out.add(b, pad, 0)
		out.items = append(out.items, item{kind: 'b', x: 0, y: -out.h, w: out.w, h: out.h + out.d, stroke: rule, color: c.color})
		return out, clsOrd
	case styleNode, newlineNode, alignNode, nil:
		return box{}, -1
	}
	return box{}, clsOrd
}

func isHex(s string) bool {
	if len(s) != 6 && len(s) != 3 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func (c ctx) atomBox(n node) box { b, _ := c.atom(n); return b }

// ---- constructions ----

func (c ctx) scripts(x scriptNode) box {
	base := c.atomBox(x.base)
	ss := c.with(supStyle(c.st))
	out := box{w: base.w, h: base.h, d: base.d}
	out.add(base, 0, 0)
	s := c.sc()
	italicCorr := 0.0
	if ch, ok := x.base.(charNode); ok && ch.font == "" && unicode.IsLetter(ch.r) {
		italicCorr = .06 * s
	}
	var sup, sub box
	var u, v float64
	if x.sup != nil {
		sup = ss.atomBox(x.sup)
		u = math.Max(base.h-.25*s, .4*s)
		if c.st > stT {
			u = math.Max(base.h-.2*s, .3*s)
		}
		u = math.Max(u, sup.d+.12*s)
	}
	if x.sub != nil {
		sub = ss.atomBox(x.sub)
		v = math.Max(base.d+.08*s, .18*s)
		v = math.Max(v, sub.h-.4*s)
	}
	if x.sup != nil && x.sub != nil {
		if gap := (u - sup.d) - (sub.h - v); gap < .2*s {
			v += .2*s - gap
		}
	}
	w := 0.0
	if x.sup != nil {
		out.add(sup, base.w+italicCorr, -u)
		out.h = math.Max(out.h, u+sup.h)
		w = italicCorr + sup.w
	}
	if x.sub != nil {
		out.add(sub, base.w, v)
		out.d = math.Max(out.d, v+sub.d)
		w = math.Max(w, sub.w)
	}
	out.w += w + .04*s
	return out
}

func (c ctx) op(x opNode, sup, sub node) box {
	s := c.sc()
	var base box
	if x.big {
		size := 1.0
		if c.st == stD {
			size = 1.55
			if !x.limits {
				size = 1.9
			}
		} else if c.st == stT {
			size = 1.15
		}
		r := []rune(x.s)[0]
		w := advance(r, false) * size * s
		if !x.limits {
			w *= .75
		}
		// Centre the symbol on the math axis.
		half := .5 * size * s
		base = box{w: w, h: axis*s + half, d: half - axis*s}
		base.items = append(base.items, item{kind: 't', s: x.s, size: size * s, font: "rm", color: c.color, y: -axis*s + .3*size*s})
	} else {
		base = c.run(x.s, "rm")
	}
	limits := x.limits && c.st == stD
	if x.forced == 1 {
		limits = true
	} else if x.forced == -1 {
		limits = false
	}
	if sup == nil && sub == nil {
		return base
	}
	if !limits {
		corr := 0.0
		if x.big && !x.limits {
			corr = .22 * s
			if c.st == stD {
				corr = .35 * s
			}
		}
		return c.scriptsOnBox(base, sup, sub, corr)
	}
	ss := c.with(supStyle(c.st))
	var top, bot box
	w := base.w
	if sup != nil {
		top = ss.atomBox(sup)
		w = math.Max(w, top.w)
	}
	if sub != nil {
		bot = ss.atomBox(sub)
		w = math.Max(w, bot.w)
	}
	out := box{w: w, h: base.h, d: base.d}
	out.add(base, (w-base.w)/2, 0)
	if sup != nil {
		shift := base.h + .12*s + top.d
		out.add(top, (w-top.w)/2, -shift)
		out.h = shift + top.h
	}
	if sub != nil {
		shift := base.d + .2*s + bot.h
		out.add(bot, (w-bot.w)/2, shift)
		out.d = shift + bot.d
	}
	return out
}

func (c ctx) scriptsOnBox(base box, sup, sub node, corr float64) box {
	// Reuse scripts() by wrapping the box in a phantom-like node.
	ss := c.with(supStyle(c.st))
	s := c.sc()
	out := box{w: base.w, h: base.h, d: base.d}
	out.add(base, 0, 0)
	w := 0.0
	var u, v float64
	var supB, subB box
	if sup != nil {
		supB = ss.atomBox(sup)
		u = math.Max(base.h-.3*s, .4*s)
	}
	if sub != nil {
		subB = ss.atomBox(sub)
		v = math.Max(base.d-.1*s, .2*s)
	}
	if sup != nil {
		out.add(supB, base.w+corr, -u)
		out.h = math.Max(out.h, u+supB.h)
		w = supB.w + corr
	}
	if sub != nil {
		out.add(subB, base.w-.1*s-corr/3, v)
		out.d = math.Max(out.d, v+subB.d)
		w = math.Max(w, subB.w-.1*s)
	}
	out.w += w + .05*s
	return out
}

func (c ctx) frac(x fracNode) box {
	st := c.st
	if x.style >= 0 {
		st = x.style
	}
	cs := c.with(st)
	inner := cs.with(fracStyle(st))
	num := inner.atomBox(x.num)
	den := inner.atomBox(x.den)
	s := cs.sc()
	t := rule * s
	gap := .1 * s
	minUp, minDown := .4*s, .35*s
	if st == stD {
		gap, minUp, minDown = .18*s, .68*s, .68*s
	}
	if !x.bar {
		t = 0
		gap *= 1.6
	}
	up := math.Max(num.d+gap+axis*s+t/2, minUp)
	down := math.Max(den.h+gap-axis*s+t/2, minDown)
	pad := .12 * s
	w := math.Max(num.w, den.w) + 2*pad
	out := box{w: w, h: up + num.h, d: down + den.d}
	out.add(num, (w-num.w)/2, -up)
	out.add(den, (w-den.w)/2, down)
	if x.bar {
		out.items = append(out.items, item{kind: 'r', x: pad / 2, y: -axis*s - t/2, w: w - pad, h: t, color: c.color})
	}
	if x.left != "" || x.right != "" {
		return c.fence(x.left, x.right, out)
	}
	return out
}

func (c ctx) sqrt(x sqrtNode) box {
	body := c.atomBox(x.body)
	s := c.sc()
	t := rule * s
	clear := .12 * s
	if c.st == stD {
		clear = .18 * s
	}
	top := body.h + clear + t
	bottom := math.Max(body.d, .05*s) + .05*s
	lw := .62 * s
	if top+bottom > 1.6*s {
		lw = .75 * s
	}
	var idx box
	ix := 0.0
	if x.index != nil {
		idx = c.with(stSS).atomBox(x.index)
		ix = math.Max(0, idx.w-.35*s)
	}
	out := box{w: ix + lw + body.w + .1*s, h: top + .05*s, d: bottom}
	mid := (top-bottom)/2 - .05*s
	x0 := ix
	path := fmt.Sprintf("M %g %g L %g %g L %g %g L %g %g L %g %g",
		x0, -mid+.08*s,
		x0+.12*lw, -mid,
		x0+.45*lw, bottom-.02*s,
		x0+lw-.04*s, -top+t/2,
		x0+lw+body.w+.1*s, -top+t/2)
	out.items = append(out.items, item{kind: 'p', s: path, stroke: t, color: c.color})
	out.add(body, x0+lw, 0)
	if x.index != nil {
		out.add(idx, 0, -mid-.15*s-idx.d)
		out.h = math.Max(out.h, mid+.15*s+idx.d+idx.h)
	}
	return out
}

func (c ctx) accent(x accentNode) box {
	body := c.atomBox(x.body)
	s := c.sc()
	out := box{w: body.w, h: body.h, d: body.d}
	out.add(body, 0, 0)
	wide := body.w > .7*s
	switch x.acc {
	case "→":
		w := math.Max(body.w, .5*s)
		y := -body.h - .12*s
		out.items = append(out.items, item{kind: 'p', s: fmt.Sprintf("M %g %g L %g %g M %g %g L %g %g L %g %g",
			(body.w-w)/2+.03*s, y, (body.w+w)/2, y, (body.w+w)/2-.12*s, y-.08*s, (body.w+w)/2, y, (body.w+w)/2-.12*s, y+.08*s), stroke: rule * s, color: c.color})
		out.h = body.h + .25*s
		return out
	case "¯":
		if wide {
			out.items = append(out.items, item{kind: 'r', y: -body.h - .12*s, w: body.w, h: rule * s, color: c.color})
			out.h = body.h + .2*s
			return out
		}
	case "ˆ", "˜":
		if wide {
			y := -body.h - .08*s
			if x.acc == "ˆ" {
				out.items = append(out.items, item{kind: 'p', s: fmt.Sprintf("M %g %g L %g %g L %g %g", 0.0, y, body.w/2, y-.2*s, body.w, y), stroke: rule * s * .8, color: c.color})
			} else {
				out.items = append(out.items, item{kind: 'p', s: fmt.Sprintf("M %g %g C %g %g %g %g %g %g", 0.0, y, body.w*.3, y-.25*s, body.w*.7, y+.1*s, body.w, y-.15*s), stroke: rule * s * .8, color: c.color})
			}
			out.h = body.h + .28*s
			return out
		}
	}
	acc := c.glyph([]rune(x.acc)[0], "rm")
	italicShift := 0.0
	if ch, ok := x.body.(charNode); ok && ch.font == "" {
		italicShift = .06 * s
	}
	out.add(acc, (body.w-acc.w)/2+italicShift, -(body.h - .45*s))
	out.h = body.h + .25*s
	return out
}

func (c ctx) brace(x braceNode) box {
	body := c.atomBox(x.body)
	s := c.sc()
	out := box{w: body.w, h: body.h, d: body.d}
	out.add(body, 0, 0)
	w := body.w
	q := .12 * s
	var y0, tip float64
	if x.over {
		y0 = -body.h - .1*s
		tip = y0 - 2*q
		out.h = body.h + .1*s + 2*q
	} else {
		y0 = body.d + .1*s
		tip = y0 + 2*q
		out.d = body.d + .1*s + 2*q
	}
	ym := (y0 + tip) / 2
	path := fmt.Sprintf("M %g %g Q %g %g %g %g L %g %g Q %g %g %g %g Q %g %g %g %g L %g %g Q %g %g %g %g",
		0.0, y0, 0.0, ym, q, ym,
		w/2-q, ym, w/2, ym, w/2, tip, w/2, ym, w/2+q, ym,
		w-q, ym, w, ym, w, y0)
	out.items = append(out.items, item{kind: 'p', s: path, stroke: rule * s, color: c.color})
	return out
}

func (c ctx) leftRight(x leftRight) box {
	body := c.list(x.body)
	return c.fence(x.l, x.r, body)
}

// fence wraps a box in delimiters stretched to its height, centred on the axis.
func (c ctx) fence(l, r string, body box) box {
	s := c.sc()
	ext := math.Max(body.h-axis*s, body.d+axis*s) + .08*s
	ext = math.Max(ext, .5*s)
	var out box
	if l != "" {
		d := c.delimiter(l, ext)
		out.add(d, 0, 0)
		out.w += d.w
		out.h, out.d = math.Max(out.h, d.h), math.Max(out.d, d.d)
	}
	out.add(body, out.w, 0)
	out.w += body.w
	out.h, out.d = math.Max(out.h, body.h), math.Max(out.d, body.d)
	if r != "" {
		d := c.delimiter(r, ext)
		out.add(d, out.w, 0)
		out.w += d.w
		out.h, out.d = math.Max(out.h, d.h), math.Max(out.d, d.d)
	}
	return out
}

// delimiter draws a delimiter extending ext above and below the axis.
func (c ctx) delimiter(d string, ext float64) box {
	s := c.sc()
	if ext <= .62*s {
		b := c.glyph(firstRune(d), "rm")
		return b
	}
	top, bot := -axis*s-ext, -axis*s+ext
	mid := -axis * s
	h := bot - top
	w := math.Min(.35*s+h*.06, .7*s)
	t := rule * s * 1.1
	var p string
	m := .12 * s // side margin
	switch d {
	case "(":
		p = fmt.Sprintf("M %g %g Q %g %g %g %g", w-m, top, -w*.15+m, mid, w-m, bot)
	case ")":
		p = fmt.Sprintf("M %g %g Q %g %g %g %g", m, top, w*1.15-m, mid, m, bot)
	case "[":
		p = fmt.Sprintf("M %g %g L %g %g L %g %g L %g %g", w-m, top, m+.05*s, top, m+.05*s, bot, w-m, bot)
	case "]":
		p = fmt.Sprintf("M %g %g L %g %g L %g %g L %g %g", m, top, w-m-.05*s, top, w-m-.05*s, bot, m, bot)
	case "{", "}":
		q := math.Min(.2*s, h/6)
		xa, xc := w-m, w/2
		xb := m
		if d == "}" {
			xa, xb = m, w-m
		}
		p = fmt.Sprintf("M %g %g Q %g %g %g %g L %g %g Q %g %g %g %g Q %g %g %g %g L %g %g Q %g %g %g %g",
			xa, top, xc, top, xc, top+q,
			xc, mid-q, xc, mid, xb, mid, xc, mid, xc, mid+q,
			xc, bot-q, xc, bot, xa, bot)
	case "|", "∣":
		w = .3 * s
		p = fmt.Sprintf("M %g %g L %g %g", w/2, top, w/2, bot)
	case "‖":
		w = .45 * s
		p = fmt.Sprintf("M %g %g L %g %g M %g %g L %g %g", w/3, top, w/3, bot, 2*w/3, top, 2*w/3, bot)
	case "⟨":
		p = fmt.Sprintf("M %g %g L %g %g L %g %g", w-m, top, m, mid, w-m, bot)
	case "⟩":
		p = fmt.Sprintf("M %g %g L %g %g L %g %g", m, top, w-m, mid, m, bot)
	case "⌊":
		p = fmt.Sprintf("M %g %g L %g %g L %g %g", m+.05*s, top, m+.05*s, bot, w-m, bot)
	case "⌋":
		p = fmt.Sprintf("M %g %g L %g %g L %g %g", w-m-.05*s, top, w-m-.05*s, bot, m, bot)
	case "⌈":
		p = fmt.Sprintf("M %g %g L %g %g L %g %g", w-m, top, m+.05*s, top, m+.05*s, bot)
	case "⌉":
		p = fmt.Sprintf("M %g %g L %g %g L %g %g", m, top, w-m-.05*s, top, w-m-.05*s, bot)
	case "/":
		p = fmt.Sprintf("M %g %g L %g %g", w-m, top, m, bot)
	default:
		b := c.glyph(firstRune(d), "rm")
		return b
	}
	return box{w: w, h: -top, d: bot, items: []item{{kind: 'p', s: p, stroke: t, color: c.color}}}
}

// ---- environments ----

func (c ctx) topLevelRows(nodes []node) box {
	e := envNode{name: "gathered"}
	var row, cell []node
	hasAlign := false
	for _, n := range nodes {
		switch n.(type) {
		case alignNode:
			hasAlign = true
			row = append(row, groupNode{cell})
			cell = nil
		case newlineNode:
			row = append(row, groupNode{cell})
			e.rows = append(e.rows, row)
			row, cell = nil, nil
		default:
			cell = append(cell, n)
		}
	}
	if len(cell) > 0 || len(row) > 0 {
		e.rows = append(e.rows, append(row, groupNode{cell}))
	}
	if hasAlign {
		e.name = "aligned"
	}
	return c.env(e)
}

func (c ctx) env(e envNode) box {
	name := strings.TrimSuffix(e.name, "*")
	cellStyle := stT
	if c.st > stT {
		cellStyle = c.st
	}
	var left, right string
	var align func(col int) byte
	colSep := 1.0
	switch name {
	case "matrix", "smallmatrix":
		align = func(int) byte { return 'c' }
	case "pmatrix":
		left, right, align = "(", ")", func(int) byte { return 'c' }
	case "bmatrix":
		left, right, align = "[", "]", func(int) byte { return 'c' }
	case "Bmatrix":
		left, right, align = "{", "}", func(int) byte { return 'c' }
	case "vmatrix":
		left, right, align = "|", "|", func(int) byte { return 'c' }
	case "Vmatrix":
		left, right, align = "‖", "‖", func(int) byte { return 'c' }
	case "cases", "dcases":
		left, align = "{", func(int) byte { return 'l' }
		if name == "dcases" {
			cellStyle = stD
		}
	case "rcases":
		right, align = "}", func(int) byte { return 'l' }
	case "aligned", "align", "split", "alignat", "eqnarray", "flalign":
		cellStyle = stD
		colSep = 0
		align = func(col int) byte {
			if col%2 == 0 {
				return 'r'
			}
			return 'l'
		}
	case "gathered", "gather", "equation", "multline":
		cellStyle = stD
		align = func(int) byte { return 'c' }
	case "array":
		var spec []byte
		for _, r := range e.cols {
			if r == 'l' || r == 'c' || r == 'r' {
				spec = append(spec, byte(r))
			}
		}
		align = func(col int) byte {
			if col < len(spec) {
				return spec[col]
			}
			return 'c'
		}
		colSep = .5
	default:
		align = func(int) byte { return 'c' }
	}
	if name == "smallmatrix" {
		cellStyle = stS
	}
	cc := c.with(cellStyle)
	s := c.sc()

	ncols := 0
	cells := make([][]box, len(e.rows))
	isAlign := name == "aligned" || name == "align" || name == "split" || name == "alignat" || name == "eqnarray" || name == "flalign"
	for i, row := range e.rows {
		for j, cell := range row {
			if g, ok := cell.(groupNode); ok && isAlign && j%2 == 1 {
				// TeX inserts {} so a leading relation keeps its spacing.
				cell = groupNode{append([]node{groupNode{}}, g.list...)}
			}
			cells[i] = append(cells[i], cc.atomBox(cell))
		}
		ncols = max(ncols, len(row))
	}
	colW := make([]float64, ncols)
	for _, row := range cells {
		for j, b := range row {
			colW[j] = math.Max(colW[j], b.w)
		}
	}
	// In aligned envs, a column pair is separated by 2em; the relation side gets a thick space.
	var out box
	y := 0.0
	rowH := 1.2 * s
	if cellStyle == stD {
		rowH = 1.45 * s
	}
	var rowYs []float64
	for i, row := range cells {
		h, d := .7*s, .25*s
		for _, b := range row {
			h, d = math.Max(h, b.h), math.Max(d, b.d)
		}
		if i == 0 {
			y = h
		} else {
			prevD := .25 * s
			for _, b := range cells[i-1] {
				prevD = math.Max(prevD, b.d)
			}
			y += math.Max(rowH, prevD+h+.25*s)
		}
		rowYs = append(rowYs, y)
		x := 0.0
		for j := 0; j < ncols; j++ {
			if j > 0 {
				if name == "aligned" || name == "align" || name == "split" || name == "alignat" || name == "eqnarray" || name == "flalign" {
					if j%2 == 0 {
						x += 2 * s
					}
				} else {
					x += colSep * s
				}
			}
			if j < len(row) {
				b := row[j]
				dx := 0.0
				switch align(j) {
				case 'c':
					dx = (colW[j] - b.w) / 2
				case 'r':
					dx = colW[j] - b.w
				}
				out.add(b, x+dx, y)
			}
			x += colW[j]
		}
		out.w = math.Max(out.w, x)
		_ = d
	}
	if len(rowYs) == 0 {
		return box{}
	}
	lastD := .25 * s
	for _, b := range cells[len(cells)-1] {
		lastD = math.Max(lastD, b.d)
	}
	total := rowYs[len(rowYs)-1] + lastD
	// Centre vertically on the axis: shift so the middle of [0,total] sits at -axis.
	shift := -total/2 - axis*s
	var res box
	res.add(out, 0, shift)
	res.w = out.w
	res.h = -shift
	res.d = total + shift
	if left != "" || right != "" {
		pad := .1 * s
		padded := box{w: res.w + 2*pad, h: res.h, d: res.d}
		padded.add(res, pad, 0)
		return c.fence(left, right, padded)
	}
	return res
}

// ---- output ----

// SVG renders LaTeX math to an SVG document.
func SVG(src string, opt Options) (string, error) {
	opt.defaults()
	nodes, err := parseMath(src)
	if err != nil {
		return "", err
	}
	st := stT
	if opt.Display {
		st = stD
	}
	c := ctx{st: st, color: opt.FG, opt: &opt}
	b := c.list(nodes)
	pad := .15
	S := opt.Size
	W := (b.w + 2*pad) * S
	H := (b.h + b.d + 2*pad) * S
	ox, oy := pad, pad+b.h
	if W > MaxPixels || H > MaxPixels || W*H > MaxArea || W != W || H != H {
		return "", fmt.Errorf("formula too large to render (%.0fx%.0f px)", W, H)
	}
	sb := make([]byte, 0, 256+160*len(b.items))
	sb = append(sb, `<svg xmlns="http://www.w3.org/2000/svg" width="`...)
	sb = appendF(sb, W, 1)
	sb = append(sb, `" height="`...)
	sb = appendF(sb, H, 1)
	sb = append(sb, `" viewBox="0 0 `...)
	sb = appendF(sb, W, 1)
	sb = append(sb, ' ')
	sb = appendF(sb, H, 1)
	sb = append(sb, `"><g font-family="`...)
	sb = append(sb, html.EscapeString(opt.Font)...)
	sb = append(sb, `">`...)
	px := func(v, o float64) float64 { return (v + o) * S }
	for _, it := range b.items {
		switch it.kind {
		case 't':
			style := ""
			switch it.font {
			case "it":
				style = ` font-style="italic"`
			case "bf":
				style = ` font-weight="bold"`
			case "bfit":
				style = ` font-weight="bold" font-style="italic"`
			case "tt":
				style = ` font-family="monospace"`
			case "sf":
				style = ` font-family="sans-serif"`
			}
			sb = append(sb, `<text x="`...)
			sb = appendF(sb, px(it.x, ox), 2)
			sb = append(sb, `" y="`...)
			sb = appendF(sb, px(it.y, oy), 2)
			sb = append(sb, `" font-size="`...)
			sb = appendF(sb, it.size*S, 2)
			sb = append(sb, `" fill="`...)
			sb = append(sb, it.color...)
			sb = append(sb, '"')
			sb = append(sb, style...)
			if it.width > 0 && utf8.RuneCountInString(it.s) > 1 {
				sb = append(sb, ` textLength="`...)
				sb = appendF(sb, it.width*S, 2)
				sb = append(sb, `" lengthAdjust="spacing"`...)
			}
			sb = append(sb, ` xml:space="preserve">`...)
			sb = append(sb, html.EscapeString(it.s)...)
			sb = append(sb, `</text>`...)
		case 'r':
			sb = append(sb, `<rect x="`...)
			sb = appendF(sb, px(it.x, ox), 2)
			sb = append(sb, `" y="`...)
			sb = appendF(sb, px(it.y, oy), 2)
			sb = append(sb, `" width="`...)
			sb = appendF(sb, it.w*S, 2)
			sb = append(sb, `" height="`...)
			sb = appendF(sb, math.Max(it.h*S, 1), 2)
			sb = append(sb, `" fill="`...)
			sb = append(sb, it.color...)
			sb = append(sb, `"/>`...)
		case 'b':
			sb = append(sb, `<rect x="`...)
			sb = appendF(sb, px(it.x, ox), 2)
			sb = append(sb, `" y="`...)
			sb = appendF(sb, px(it.y, oy), 2)
			sb = append(sb, `" width="`...)
			sb = appendF(sb, it.w*S, 2)
			sb = append(sb, `" height="`...)
			sb = appendF(sb, it.h*S, 2)
			sb = append(sb, `" fill="none" stroke="`...)
			sb = append(sb, it.color...)
			sb = append(sb, `" stroke-width="`...)
			sb = appendF(sb, it.stroke*S, 2)
			sb = append(sb, `"/>`...)
		case 'p':
			sb = append(sb, `<path d="`...)
			sb = appendPath(sb, it.s, it.x+ox, it.y+oy, S)
			sb = append(sb, `" fill="none" stroke="`...)
			sb = append(sb, it.color...)
			sb = append(sb, `" stroke-width="`...)
			sb = appendF(sb, math.Max(it.stroke*S, 1), 2)
			sb = append(sb, `" stroke-linecap="round" stroke-linejoin="round"/>`...)
		}
	}
	sb = append(sb, `</g></svg>`...)
	return string(sb), nil
}

// appendF appends v like fmt's %.<prec>f.
func appendF(b []byte, v float64, prec int) []byte {
	if math.IsInf(v, 0) || v != v {
		return fmt.Appendf(b, "%.*f", prec, v)
	}
	return strconv.AppendFloat(b, v, 'f', prec, 64)
}

// appendPath converts a path in em (relative to the item origin) to pixels.
func appendPath(b []byte, p string, ox, oy, S float64) []byte {
	isX := true
	for i, tok := range strings.Fields(p) {
		if i > 0 {
			b = append(b, ' ')
		}
		v, err := strconv.ParseFloat(tok, 64)
		if err != nil {
			// commands (M, L, Q, C) and anything else: what %g scanning says
			if c := tok[0]; c >= 'A' && c <= 'Z' && c != 'I' && c != 'N' && c != 'E' && c != 'P' {
				err = errNotNumber
			} else {
				_, err = fmt.Sscanf(tok, "%g", &v)
			}
			if err != nil {
				b = append(b, tok...)
				isX = true
				continue
			}
		}
		if isX {
			b = appendF(b, (v+ox)*S, 2)
		} else {
			b = appendF(b, (v+oy)*S, 2)
		}
		isX = !isX
	}
	return b
}

var errNotNumber = errors.New("not a number")
