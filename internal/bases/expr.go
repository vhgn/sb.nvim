// Package bases evaluates Obsidian Bases (.base files): filters, formulas,
// sorting, grouping and summaries over the files of a vault.
package bases

import (
	"fmt"
	"strconv"
	"strings"
)

// ---- lexer ----

type tokKind int

const (
	tEOF tokKind = iota
	tNum
	tStr
	tRegex
	tIdent
	tOp
)

type token struct {
	kind tokKind
	s    string
	n    float64
	pos  int
}

func lex(src string) ([]token, error) {
	var out []token
	valueBefore := func() bool {
		if len(out) == 0 {
			return false
		}
		t := out[len(out)-1]
		return t.kind != tOp || t.s == ")" || t.s == "]" || t.s == "}"
	}
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9' && !valueBefore():
			j := i
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.' || src[j] == '_') {
				j++
			}
			if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
				k := j + 1
				if k < len(src) && (src[k] == '+' || src[k] == '-') {
					k++
				}
				if k < len(src) && src[k] >= '0' && src[k] <= '9' {
					j = k
					for j < len(src) && src[j] >= '0' && src[j] <= '9' {
						j++
					}
				}
			}
			n, err := strconv.ParseFloat(strings.ReplaceAll(src[i:j], "_", ""), 64)
			if err != nil {
				return nil, fmt.Errorf("bad number %q", src[i:j])
			}
			out = append(out, token{kind: tNum, n: n, s: src[i:j], pos: i})
			i = j
		case c == '"' || c == '\'' || c == '`':
			var b strings.Builder
			j := i + 1
			for ; j < len(src) && src[j] != c; j++ {
				if src[j] == '\\' && j+1 < len(src) {
					j++
					switch src[j] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(src[j])
					}
					continue
				}
				b.WriteByte(src[j])
			}
			if j >= len(src) {
				return nil, fmt.Errorf("unterminated string at %d", i)
			}
			out = append(out, token{kind: tStr, s: b.String(), pos: i})
			i = j + 1
		case c == '/' && !valueBefore():
			j := i + 1
			inClass := false
			for ; j < len(src); j++ {
				if src[j] == '\\' {
					j++
					continue
				}
				if src[j] == '[' {
					inClass = true
				} else if src[j] == ']' {
					inClass = false
				} else if src[j] == '/' && !inClass {
					break
				}
			}
			if j >= len(src) {
				return nil, fmt.Errorf("unterminated regex at %d", i)
			}
			k := j + 1
			for k < len(src) && (src[k] >= 'a' && src[k] <= 'z') {
				k++
			}
			out = append(out, token{kind: tRegex, s: src[i+1:j] + "/" + src[j+1:k], pos: i})
			i = k
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			out = append(out, token{kind: tIdent, s: src[i:j], pos: i})
			i = j
		default:
			ops := []string{"===", "!==", "==", "!=", "<=", ">=", "&&", "||", "??", "(", ")", "[", "]", "{", "}", ",", ".", "!", "+", "-", "*", "/", "%", "<", ">", "?", ":"}
			matched := false
			for _, op := range ops {
				if strings.HasPrefix(src[i:], op) {
					s := op
					if s == "===" {
						s = "=="
					} else if s == "!==" {
						s = "!="
					}
					out = append(out, token{kind: tOp, s: s, pos: i})
					i += len(op)
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("unexpected %q at %d", c, i)
			}
		}
	}
	return append(out, token{kind: tEOF, pos: len(src)}), nil
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '$' || c >= 0x80
}
func isIdentPart(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

// ---- AST ----

type Node interface{}

type (
	litNode    struct{ v any }
	identNode  struct{ name string }
	memberNode struct {
		obj  Node
		name string
	}
	indexNode struct{ obj, key Node }
	callNode  struct {
		fn   Node // identNode (global) or memberNode (method)
		args []Node
	}
	unaryNode struct {
		op string
		x  Node
	}
	binaryNode struct {
		op   string
		l, r Node
	}
	condNode struct{ c, a, b Node }
	listNode struct{ items []Node }
	objNode  struct {
		keys []string
		vals []Node
	}
	regexNode struct{ pattern, flags string }
)

// ---- parser ----

type parser struct {
	toks []token
	i    int
	src  string
}

// Parse compiles an expression.
func Parse(src string) (Node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, src: src}
	n, err := p.expr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, fmt.Errorf("unexpected %q at %d", p.peek().s, p.peek().pos)
	}
	return n, nil
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }
func (p *parser) isOp(s ...string) bool {
	t := p.peek()
	if t.kind != tOp {
		return false
	}
	for _, x := range s {
		if t.s == x {
			return true
		}
	}
	return false
}
func (p *parser) expect(s string) error {
	if !p.isOp(s) {
		t := p.peek()
		if t.kind == tEOF {
			return fmt.Errorf("expected %q at end of expression", s)
		}
		return fmt.Errorf("expected %q at %d, got %q", s, t.pos, t.s)
	}
	p.i++
	return nil
}

func (p *parser) expr() (Node, error) {
	c, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if p.isOp("?") {
		p.i++
		a, err := p.expr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		b, err := p.expr()
		if err != nil {
			return nil, err
		}
		return condNode{c, a, b}, nil
	}
	return c, nil
}

var precedence = map[string]int{
	"??": 1, "||": 2, "&&": 3,
	"==": 4, "!=": 4,
	"<": 5, ">": 5, "<=": 5, ">=": 5,
	"+": 6, "-": 6,
	"*": 7, "/": 7, "%": 7,
}

func (p *parser) binary(minPrec int) (Node, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		var op string
		switch {
		case t.kind == tOp:
			op = t.s
		case t.kind == tIdent && (t.s == "and" || t.s == "or"):
			op = map[string]string{"and": "&&", "or": "||"}[t.s]
		}
		prec, ok := precedence[op]
		if !ok || prec <= minPrec {
			return l, nil
		}
		p.i++
		r, err := p.binary(prec)
		if err != nil {
			return nil, err
		}
		l = binaryNode{op, l, r}
	}
}

func (p *parser) unary() (Node, error) {
	if p.isOp("!", "-", "+") || p.peek().kind == tIdent && p.peek().s == "not" && p.toks[p.i+1].kind != tEOF && !(p.toks[p.i+1].kind == tOp && p.toks[p.i+1].s != "(" && p.toks[p.i+1].s != "!") {
		op := p.next().s
		if op == "not" {
			op = "!"
		}
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return unaryNode{op, x}, nil
	}
	return p.postfix()
}

func (p *parser) postfix() (Node, error) {
	n, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.isOp("."):
			p.i++
			t := p.next()
			if t.kind != tIdent {
				return nil, fmt.Errorf("expected a name after '.' at %d", t.pos)
			}
			n = memberNode{n, t.s}
		case p.isOp("["):
			p.i++
			k, err := p.expr()
			if err != nil {
				return nil, err
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			n = indexNode{n, k}
		case p.isOp("("):
			p.i++
			var args []Node
			for !p.isOp(")") {
				a, err := p.expr()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				if !p.isOp(",") {
					break
				}
				p.i++
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			n = callNode{n, args}
		default:
			return n, nil
		}
	}
}

func (p *parser) primary() (Node, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		return litNode{t.n}, nil
	case tStr:
		return litNode{t.s}, nil
	case tRegex:
		i := strings.LastIndexByte(t.s, '/')
		return regexNode{t.s[:i], t.s[i+1:]}, nil
	case tIdent:
		switch t.s {
		case "true":
			return litNode{true}, nil
		case "false":
			return litNode{false}, nil
		case "null", "undefined":
			return litNode{nil}, nil
		}
		return identNode{t.s}, nil
	case tOp:
		switch t.s {
		case "(":
			n, err := p.expr()
			if err != nil {
				return nil, err
			}
			return n, p.expect(")")
		case "[":
			var items []Node
			for !p.isOp("]") {
				n, err := p.expr()
				if err != nil {
					return nil, err
				}
				items = append(items, n)
				if !p.isOp(",") {
					break
				}
				p.i++
			}
			return listNode{items}, p.expect("]")
		case "{":
			o := objNode{}
			for !p.isOp("}") {
				k := p.next()
				if k.kind != tIdent && k.kind != tStr {
					return nil, fmt.Errorf("expected a key at %d", k.pos)
				}
				if err := p.expect(":"); err != nil {
					return nil, err
				}
				v, err := p.expr()
				if err != nil {
					return nil, err
				}
				o.keys = append(o.keys, k.s)
				o.vals = append(o.vals, v)
				if !p.isOp(",") {
					break
				}
				p.i++
			}
			return o, p.expect("}")
		}
	case tEOF:
		return nil, fmt.Errorf("unexpected end of expression")
	}
	return nil, fmt.Errorf("unexpected %q at %d", t.s, t.pos)
}
