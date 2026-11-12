// Package tex renders a practical subset of LaTeX math to SVG (for display
// math images) and to Unicode text (for inline math).
package tex

import (
	"fmt"
	"strings"
	"unicode"
)

type node interface{}

type (
	charNode struct {
		r    rune
		cls  int
		font string // "" (math: italic letters), rm, bf, it, bb, cal, frak, tt, sf, text
	}
	groupNode struct{ list []node }
	fracNode  struct {
		num, den    node
		bar         bool
		left, right string
		style       int // -1 = inherit
	}
	sqrtNode   struct{ body, index node }
	scriptNode struct{ base, sup, sub node }
	opNode     struct {
		s      string
		limits bool
		big    bool // a large symbol rather than a name
		forced int  // 0 default, 1 \limits, -1 \nolimits
	}
	accentNode struct {
		acc  string
		body node
	}
	lineNode struct {
		over bool
		body node
	}
	braceNode struct {
		over  bool
		body  node
		label node
	}
	leftRight struct {
		l, r string
		body []node
	}
	textNode  struct{ s, font string }
	spaceNode struct{ em float64 }
	envNode   struct {
		name string
		rows [][]node // each cell a groupNode
		cols string
	}
	colorNode struct {
		color string
		body  node
	}
	styleNode struct{ style int }
	stackNode struct {
		top  node
		base node
		over bool
	}
	phantomNode struct{ body node }
	boxedNode   struct{ body node }
	newlineNode struct{}
	alignNode   struct{}
)

type parser struct {
	src   []rune
	i     int
	depth int
}

// Limits that keep pathological input cheap: anything beyond them is an
// error rather than seconds of layout and rasterization.
const (
	MaxSource = 20000 // bytes of LaTeX
	MaxDepth  = 64    // nested groups/arguments
)

func parseMath(s string) (list []node, err error) { return parseMathDepth(s, 0) }

func parseMathDepth(s string, depth int) (list []node, err error) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(parseErr); ok {
				err = pe
				return
			}
			panic(r)
		}
	}()
	if len(s) > MaxSource {
		return nil, parseErr(fmt.Sprintf("formula too long (%d bytes, max %d)", len(s), MaxSource))
	}
	p := &parser{src: []rune(s), depth: depth}
	list = p.list("")
	if p.i < len(p.src) {
		p.fail("unexpected %q", string(p.src[p.i]))
	}
	return list, nil
}

type parseErr string

func (e parseErr) Error() string { return string(e) }

func (p *parser) fail(f string, a ...any) { panic(parseErr(fmt.Sprintf(f, a...))) }

func (p *parser) skipSpace() {
	for p.i < len(p.src) && unicode.IsSpace(p.src[p.i]) {
		p.i++
	}
}

func (p *parser) peekCmd() string {
	j := p.i
	if j >= len(p.src) || p.src[j] != '\\' {
		return ""
	}
	j++
	k := j
	for k < len(p.src) && unicode.IsLetter(p.src[k]) {
		k++
	}
	if k == j && k < len(p.src) {
		k++
	}
	return string(p.src[j:k])
}

func (p *parser) readCmd() string {
	p.i++ // backslash
	j := p.i
	for p.i < len(p.src) && unicode.IsLetter(p.src[p.i]) {
		p.i++
	}
	if p.i == j && p.i < len(p.src) {
		p.i++
	}
	return string(p.src[j:p.i])
}

// list parses atoms until a closing brace, \right, \end or end of input.
func (p *parser) list(until string) []node {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > MaxDepth {
		p.fail("formula nested too deeply (max %d levels)", MaxDepth)
	}
	var out []node
	for {
		p.skipSpace()
		if p.i >= len(p.src) {
			if until == "}" {
				p.fail("missing }")
			}
			return out
		}
		c := p.src[p.i]
		if c == '}' {
			if until != "}" {
				p.fail("unexpected }")
			}
			return out
		}
		if c == '\\' {
			switch cmd := p.peekCmd(); cmd {
			case "right", "end":
				return out
			case "\\", "cr", "newline":
				p.readCmd()
				p.optArg() // [2pt]
				out = append(out, newlineNode{})
				continue
			case "over", "atop", "choose":
				p.readCmd()
				rest := p.list(until)
				f := fracNode{num: groupNode{out}, den: groupNode{rest}, bar: cmd == "over", style: -1}
				if cmd == "choose" {
					f.left, f.right = "(", ")"
				}
				return []node{f}
			}
		}
		if c == '&' {
			p.i++
			out = append(out, alignNode{})
			continue
		}
		if c == '^' || c == '_' {
			var base node = groupNode{}
			if len(out) > 0 {
				if _, ok := out[len(out)-1].(newlineNode); !ok {
					base = out[len(out)-1]
					out = out[:len(out)-1]
				}
			}
			out = append(out, p.scripts(base))
			continue
		}
		a := p.atom()
		if a == nil {
			continue
		}
		if op, ok := a.(opNode); ok {
			p.skipSpace()
			switch p.peekCmd() {
			case "limits":
				p.readCmd()
				op.forced = 1
			case "nolimits":
				p.readCmd()
				op.forced = -1
			}
			a = op
		}
		out = append(out, a)
	}
}

func (p *parser) scripts(base node) node {
	s := scriptNode{base: base}
	if prev, ok := base.(scriptNode); ok {
		s = prev
	}
	for p.i < len(p.src) {
		p.skipSpace()
		if p.i >= len(p.src) {
			break
		}
		c := p.src[p.i]
		if c != '^' && c != '_' && c != '\'' {
			break
		}
		p.i++
		if c == '\'' {
			s.sup = appendNode(s.sup, charNode{r: '′'})
			continue
		}
		arg := p.arg()
		if c == '^' {
			if s.sup != nil {
				s.sup = appendNode(s.sup, arg)
			} else {
				s.sup = arg
			}
		} else {
			s.sub = arg
		}
	}
	return s
}

func appendNode(a node, b node) node {
	if a == nil {
		return groupNode{[]node{b}}
	}
	if g, ok := a.(groupNode); ok {
		return groupNode{append(append([]node{}, g.list...), b)}
	}
	return groupNode{[]node{a, b}}
}

// arg parses a macro argument: a {group} or a single token.
func (p *parser) arg() node {
	p.skipSpace()
	if p.i >= len(p.src) {
		p.fail("missing argument")
	}
	if p.src[p.i] == '{' {
		p.i++
		l := p.list("}")
		p.i++
		return groupNode{l}
	}
	a := p.atom()
	if a == nil {
		return groupNode{}
	}
	return a
}

// rawArg returns the text of a {group} argument without parsing it.
func (p *parser) rawArg() string {
	p.skipSpace()
	if p.i >= len(p.src) {
		return ""
	}
	if p.src[p.i] != '{' {
		c := p.src[p.i]
		p.i++
		return string(c)
	}
	depth := 0
	start := p.i + 1
	for ; p.i < len(p.src); p.i++ {
		switch p.src[p.i] {
		case '\\':
			p.i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				s := string(p.src[start:p.i])
				p.i++
				return s
			}
		}
	}
	p.fail("missing }")
	return ""
}

func (p *parser) optArg() node {
	p.skipSpace()
	if p.i < len(p.src) && p.src[p.i] == '[' {
		p.i++
		start := p.i
		depth := 0
		for ; p.i < len(p.src); p.i++ {
			if p.src[p.i] == '{' {
				depth++
			} else if p.src[p.i] == '}' {
				depth--
			} else if p.src[p.i] == ']' && depth == 0 {
				break
			}
		}
		inner := string(p.src[start:p.i])
		p.i++
		l, err := parseMathDepth(inner, p.depth)
		if err != nil {
			p.fail("%v", err)
		}
		return groupNode{l}
	}
	return nil
}

func (p *parser) delim() string {
	p.skipSpace()
	if p.i >= len(p.src) {
		p.fail("missing delimiter")
	}
	if p.src[p.i] == '\\' {
		cmd := p.readCmd()
		if s, ok := symbols[cmd]; ok {
			return s.s
		}
		return map[string]string{"lvert": "|", "rvert": "|", "lVert": "‖", "rVert": "‖"}[cmd]
	}
	c := p.src[p.i]
	p.i++
	if c == '.' {
		return ""
	}
	return string(c)
}

func (p *parser) atom() node {
	c := p.src[p.i]
	switch {
	case c == '{':
		p.i++
		l := p.list("}")
		p.i++
		return groupNode{l}
	case c == '\\':
		return p.command(p.readCmd())
	case c == '~':
		p.i++
		return spaceNode{.33}
	}
	p.i++
	cls := clsOrd
	switch c {
	case '+', '*':
		cls = clsBin
	case '-':
		c, cls = '−', clsBin
	case '=', '<', '>', ':':
		cls = clsRel
	case ',', ';':
		cls = clsPunct
	case '(', '[':
		cls = clsOpen
	case ')', ']', '!', '?':
		cls = clsClose
	case '|', '/', '.':
		cls = clsOrd
	case '\'':
		return charNode{r: '′'}
	}
	return charNode{r: c, cls: cls}
}

func (p *parser) command(cmd string) node {
	if s, ok := symbols[cmd]; ok {
		return charNode{r: []rune(s.s)[0], cls: s.cls, font: map[bool]string{true: "rm"}[isUpright(cmd)]}
	}
	if op, ok := bigOps[cmd]; ok {
		return opNode{s: op.s, limits: op.limits, big: true}
	}
	if lim, ok := namedOps[cmd]; ok {
		name := cmd
		switch cmd {
		case "argmax":
			name = "arg max"
		case "argmin":
			name = "arg min"
		case "limsup":
			name = "lim sup"
		case "liminf":
			name = "lim inf"
		case "bmod":
			name = "mod"
		}
		return opNode{s: name, limits: lim}
	}
	if acc, ok := accents[cmd]; ok {
		return accentNode{acc: acc, body: p.arg()}
	}
	switch cmd {
	case "frac", "dfrac", "tfrac", "cfrac":
		st := -1
		if cmd == "dfrac" || cmd == "cfrac" {
			st = 0
		} else if cmd == "tfrac" {
			st = 1
		}
		return fracNode{num: p.arg(), den: p.arg(), bar: true, style: st}
	case "binom", "dbinom", "tbinom":
		return fracNode{num: p.arg(), den: p.arg(), left: "(", right: ")", style: -1}
	case "sqrt":
		idx := p.optArg()
		return sqrtNode{body: p.arg(), index: idx}
	case "left":
		l := p.delim()
		body := p.list("")
		if p.peekCmd() != "right" {
			p.fail(`missing \right`)
		}
		p.readCmd()
		r := p.delim()
		return leftRight{l: l, r: r, body: body}
	case "big", "Big", "bigg", "Bigg", "bigl", "bigr", "Bigl", "Bigr", "biggl", "biggr", "Biggl", "Biggr":
		d := p.delim()
		return charNode{r: firstRune(d), cls: clsOrd, font: "rm"}
	case "overline", "underline":
		return lineNode{over: cmd == "overline", body: p.arg()}
	case "overbrace", "underbrace":
		b := braceNode{over: cmd == "overbrace", body: p.arg()}
		return b
	case "overset", "underset", "stackrel":
		top := p.arg()
		return stackNode{top: top, base: p.arg(), over: cmd != "underset"}
	case "text", "textrm", "mbox", "textnormal", "hbox":
		return textNode{s: p.rawArg(), font: "rm"}
	case "textbf":
		return textNode{s: p.rawArg(), font: "bf"}
	case "textit", "emph":
		return textNode{s: p.rawArg(), font: "it"}
	case "texttt":
		return textNode{s: p.rawArg(), font: "tt"}
	case "operatorname":
		return opNode{s: p.rawArg()}
	case "mathrm", "mathbf", "mathit", "mathbb", "mathcal", "mathfrak", "mathscr", "mathsf", "mathtt", "boldsymbol", "bm", "mathnormal":
		font := map[string]string{"mathrm": "rm", "mathbf": "bf", "mathit": "it", "mathbb": "bb", "mathcal": "cal",
			"mathfrak": "frak", "mathscr": "scr", "mathsf": "sf", "mathtt": "tt", "boldsymbol": "bfit", "bm": "bfit", "mathnormal": ""}[cmd]
		return withFont(p.arg(), font)
	case "color", "textcolor":
		c := p.rawArg()
		return colorNode{color: c, body: p.arg()}
	case "displaystyle":
		return styleNode{0}
	case "textstyle":
		return styleNode{1}
	case "scriptstyle":
		return styleNode{2}
	case "scriptscriptstyle":
		return styleNode{3}
	case ",", "thinspace":
		return spaceNode{3.0 / 18}
	case ":", ">", "medspace":
		return spaceNode{4.0 / 18}
	case ";", "thickspace":
		return spaceNode{5.0 / 18}
	case "!", "negthinspace":
		return spaceNode{-3.0 / 18}
	case " ":
		return spaceNode{.33}
	case "quad":
		return spaceNode{1}
	case "qquad":
		return spaceNode{2}
	case "hspace", "kern", "mkern", "hskip":
		raw := p.rawArg()
		var f float64
		var unit string
		fmt.Sscanf(raw, "%f%s", &f, &unit)
		if unit == "mu" {
			f /= 18
		} else if unit == "pt" {
			f /= 10
		}
		return spaceNode{f}
	case "phantom":
		return phantomNode{p.arg()}
	case "boxed", "fbox":
		return boxedNode{p.arg()}
	case "pmod":
		arg := p.arg()
		return groupNode{[]node{spaceNode{1}, charNode{r: '(', cls: clsOpen}, textNode{s: "mod ", font: "rm"}, arg, charNode{r: ')', cls: clsClose}}}
	case "not":
		next := p.arg()
		if ch, ok := next.(charNode); ok {
			switch ch.r {
			case '=':
				return charNode{r: '≠', cls: clsRel}
			case '∈':
				return charNode{r: '∉', cls: clsRel}
			case '<':
				return charNode{r: '≮', cls: clsRel}
			case '>':
				return charNode{r: '≯', cls: clsRel}
			}
			return charNode{r: ch.r, cls: ch.cls, font: "not"}
		}
		return next
	case "begin":
		return p.env(p.rawArg())
	case "limits", "nolimits", "nonumber", "notag", "label", "tag", "left.", "middle":
		if cmd == "label" || cmd == "tag" {
			p.rawArg()
		}
		return nil
	case "dots", "ldots":
		return charNode{r: '…', cls: clsInner}
	}
	// Unknown command: show its name upright so the user sees what failed.
	return textNode{s: "\\" + cmd, font: "unknown"}
}

var lowerGreek = map[string]bool{}

func init() {
	for _, g := range strings.Fields("alpha beta gamma delta epsilon varepsilon zeta eta theta vartheta iota kappa lambda mu nu xi omicron pi varpi rho varrho sigma varsigma tau upsilon phi varphi chi psi omega") {
		lowerGreek[g] = true
	}
}

// isUpright reports whether a symbol command renders upright (everything but lower-case greek).
func isUpright(cmd string) bool { return !lowerGreek[cmd] }

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return ' '
}

func withFont(n node, font string) node {
	switch x := n.(type) {
	case charNode:
		x.font = font
		return x
	case groupNode:
		out := make([]node, len(x.list))
		for i, c := range x.list {
			out[i] = withFont(c, font)
		}
		return groupNode{out}
	case scriptNode:
		x.base = withFont(x.base, font)
		return x
	}
	return n
}

func (p *parser) env(name string) node {
	e := envNode{name: name}
	if name == "array" || name == "alignat" || name == "alignat*" {
		e.cols = p.rawArg()
	}
	var row []node
	var cell []node
	flushCell := func() {
		row = append(row, groupNode{cell})
		cell = nil
	}
	flushRow := func() {
		flushCell()
		e.rows = append(e.rows, row)
		row = nil
	}
	for {
		l := p.list("")
		for _, n := range l {
			switch n.(type) {
			case alignNode:
				flushCell()
			case newlineNode:
				flushRow()
			default:
				cell = append(cell, n)
			}
		}
		if p.i >= len(p.src) {
			p.fail(`missing \end{%s}`, name)
		}
		if p.peekCmd() == "end" {
			p.readCmd()
			end := p.rawArg()
			if end != name {
				p.fail(`\begin{%s} ended by \end{%s}`, name, end)
			}
			break
		}
		if p.peekCmd() == "right" {
			p.fail(`unexpected \right inside %s`, name)
		}
	}
	if len(cell) > 0 || len(row) > 0 {
		flushRow()
	}
	return e
}
