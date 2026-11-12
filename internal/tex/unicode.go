package tex

import (
	"strings"
	"unicode/utf8"
)

var supMap = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '−': '⁻', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾', 'n': 'ⁿ', 'i': 'ⁱ', 'a': 'ᵃ', 'b': 'ᵇ',
	'c': 'ᶜ', 'd': 'ᵈ', 'e': 'ᵉ', 'f': 'ᶠ', 'g': 'ᵍ', 'h': 'ʰ', 'j': 'ʲ', 'k': 'ᵏ', 'l': 'ˡ', 'm': 'ᵐ',
	'o': 'ᵒ', 'p': 'ᵖ', 'r': 'ʳ', 's': 'ˢ', 't': 'ᵗ', 'u': 'ᵘ', 'v': 'ᵛ', 'w': 'ʷ', 'x': 'ˣ', 'y': 'ʸ',
	'z': 'ᶻ', 'T': 'ᵀ', 'A': 'ᴬ', 'B': 'ᴮ', 'D': 'ᴰ', 'E': 'ᴱ', 'G': 'ᴳ', 'H': 'ᴴ', 'I': 'ᴵ', 'J': 'ᴶ',
	'K': 'ᴷ', 'L': 'ᴸ', 'M': 'ᴹ', 'N': 'ᴺ', 'O': 'ᴼ', 'P': 'ᴾ', 'R': 'ᴿ', 'U': 'ᵁ', 'V': 'ⱽ', 'W': 'ᵂ',
	'′': '′', '∗': '*', '*': '*', 'α': 'ᵅ', 'β': 'ᵝ', 'γ': 'ᵞ', 'δ': 'ᵟ', 'θ': 'ᶿ', 'ϕ': 'ᵠ', 'χ': 'ᵡ', ' ': ' ',
}

var subMap = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '−': '₋', '-': '₋', '=': '₌', '(': '₍', ')': '₎', 'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ',
	'j': 'ⱼ', 'k': 'ₖ', 'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ', 'p': 'ₚ', 'r': 'ᵣ', 's': 'ₛ', 't': 'ₜ',
	'u': 'ᵤ', 'v': 'ᵥ', 'x': 'ₓ', 'β': 'ᵦ', 'γ': 'ᵧ', 'ρ': 'ᵨ', 'ϕ': 'ᵩ', 'χ': 'ᵪ', ' ': ' ',
}

// Check reports whether src parses as math.
func Check(src string) error {
	_, err := parseMath(src)
	return err
}

// UnicodeCheck is Unicode and Check with a single parse: err is what Check
// would report.
func UnicodeCheck(src string) (string, bool, error) {
	nodes, err := parseMath(src)
	if err != nil {
		return src, false, err
	}
	u := &uconv{exact: true}
	return strings.TrimSpace(u.list(nodes)), u.exact, nil
}

// Unicode converts LaTeX math to plain Unicode text, e.g. "\alpha^2" → "α²".
// ok is false when the result is only an approximation (e.g. a fraction or
// script that has no Unicode form).
func Unicode(src string) (string, bool) {
	nodes, err := parseMath(src)
	if err != nil {
		return src, false
	}
	u := &uconv{exact: true}
	return strings.TrimSpace(u.list(nodes)), u.exact
}

type uconv struct{ exact bool }

func (u *uconv) list(nodes []node) string {
	var b strings.Builder
	prevOp := true // start of list: a leading binary operator is unary
	for _, n := range nodes {
		s := u.node(n)
		cls, isOp := relOrBin(n)
		if isOp && (cls == clsRel || !prevOp) && b.Len() > 0 {
			s = " " + s + " "
		}
		b.WriteString(s)
		prevOp = isOp
		if c, ok := n.(charNode); ok && (c.cls == clsOpen || c.cls == clsPunct) {
			prevOp = true
		}
	}
	return strings.ReplaceAll(b.String(), "  ", " ")
}

func relOrBin(n node) (int, bool) {
	if c, ok := n.(charNode); ok && (c.cls == clsRel || c.cls == clsBin) {
		return c.cls, true
	}
	return 0, false
}

func (u *uconv) node(n node) string {
	switch x := n.(type) {
	case charNode:
		r := x.r
		switch x.font {
		case "bb", "cal", "frak", "scr":
			r = mapAlphabet(x.font, r)
		case "not":
			return string(r) + "̸"
		}
		return string(r)
	case groupNode:
		return u.list(x.list)
	case textNode:
		return x.s
	case spaceNode:
		if x.em >= .9 {
			return "  "
		}
		if x.em > .1 {
			return " "
		}
		return ""
	case opNode:
		s := x.s
		return s + " "
	case scriptNode:
		base := u.node(x.base)
		if op, ok := x.base.(opNode); ok {
			base = op.s
		}
		out := base
		if x.sub != nil {
			out += u.script(u.node(x.sub), subMap, "_")
		}
		if x.sup != nil {
			out += u.script(u.node(x.sup), supMap, "^")
		}
		if _, ok := x.base.(opNode); ok {
			out += " "
		}
		return out
	case fracNode:
		num, den := strings.TrimSpace(u.node(x.num)), strings.TrimSpace(u.node(x.den))
		if !x.bar {
			u.exact = false
			return "(" + num + " choose " + den + ")"
		}
		if v, ok := vulgar[num+"/"+den]; ok {
			return v
		}
		if allIn(num, supMap) && allIn(den, subMap) && len(num)+len(den) <= 8 {
			return mapAll(num, supMap) + "⁄" + mapAll(den, subMap)
		}
		u.exact = false
		return wrap(num) + "/" + wrap(den)
	case sqrtNode:
		body := strings.TrimSpace(u.node(x.body))
		prefix := "√"
		if x.index != nil {
			switch strings.TrimSpace(u.node(x.index)) {
			case "3":
				prefix = "∛"
			case "4":
				prefix = "∜"
			default:
				u.exact = false
				prefix = mapAll(u.node(x.index), supMap) + "√"
			}
		}
		if utf8.RuneCountInString(body) == 1 {
			return prefix + body
		}
		return prefix + "(" + body + ")"
	case accentNode:
		body := u.node(x.body)
		comb := map[string]string{"ˆ": "̂", "ˇ": "̌", "˜": "̃", "´": "́", "`": "̀",
			"˙": "̇", "¨": "̈", "˘": "̆", "¯": "̄", "→": "⃗", "˚": "̊"}[x.acc]
		if utf8.RuneCountInString(body) == 1 {
			return body + comb
		}
		var b strings.Builder
		for _, r := range body {
			b.WriteRune(r)
			b.WriteString(comb)
		}
		return b.String()
	case lineNode:
		mark := "̅"
		if !x.over {
			mark = "̲"
		}
		var b strings.Builder
		for _, r := range u.node(x.body) {
			b.WriteRune(r)
			b.WriteString(mark)
		}
		return b.String()
	case leftRight:
		return x.l + u.list(x.body) + x.r
	case colorNode:
		return u.node(x.body)
	case boxedNode:
		return "[" + u.node(x.body) + "]"
	case braceNode:
		return u.node(x.body)
	case stackNode:
		u.exact = false
		return u.node(x.base)
	case phantomNode:
		return " "
	case envNode:
		u.exact = false
		var rows []string
		for _, row := range x.rows {
			var cells []string
			for _, c := range row {
				cells = append(cells, strings.TrimSpace(u.node(c)))
			}
			rows = append(rows, strings.Join(cells, " "))
		}
		return "[" + strings.Join(rows, "; ") + "]"
	case newlineNode:
		return "; "
	}
	return ""
}

var vulgar = map[string]string{
	"1/2": "½", "1/3": "⅓", "2/3": "⅔", "1/4": "¼", "3/4": "¾", "1/5": "⅕", "1/6": "⅙", "1/8": "⅛", "3/8": "⅜", "5/8": "⅝", "7/8": "⅞",
}

func wrap(s string) string {
	if utf8.RuneCountInString(s) <= 1 || !strings.ContainsAny(s, " +−-=/") {
		return s
	}
	return "(" + s + ")"
}

func allIn(s string, m map[rune]rune) bool {
	for _, r := range s {
		if _, ok := m[r]; !ok {
			return false
		}
	}
	return s != ""
}

func mapAll(s string, m map[rune]rune) string {
	var b strings.Builder
	for _, r := range s {
		if x, ok := m[r]; ok {
			b.WriteRune(x)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (u *uconv) script(s string, m map[rune]rune, mark string) string {
	s = strings.TrimSpace(s)
	if allIn(s, m) {
		return mapAll(s, m)
	}
	u.exact = false
	if utf8.RuneCountInString(s) == 1 {
		return mark + s
	}
	return mark + "(" + s + ")"
}
