// Package yaml is a small YAML subset parser, enough for note front matter
// and Obsidian .base files: block maps and sequences, flow collections,
// plain/quoted scalars, block scalars (| and >) and comments.
//
// Maps decode to map[string]any, sequences to []any, and scalars to nil,
// bool, float64 or string.
package yaml

import (
	"fmt"
	"strconv"
	"strings"
)

type line struct {
	indent int
	text   string // without indentation; trailing spaces trimmed
	num    int
}

type parser struct {
	raw   []string
	lines []line
	pos   int
}

// Parse decodes a YAML document. An empty document yields nil.
func Parse(src string) (v any, err error) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(parseError); ok {
				err = pe
				return
			}
			panic(r)
		}
	}()
	p := &parser{raw: strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")}
	p.lines = make([]line, 0, len(p.raw))
	for i, r := range p.raw {
		t := strings.TrimRight(r, " \t")
		trimmed := strings.TrimLeft(t, " ")
		if trimmed == "---" || trimmed == "..." {
			if len(p.lines) == 0 {
				continue
			}
			break
		}
		p.lines = append(p.lines, line{indent: len(t) - len(trimmed), text: trimmed, num: i})
	}
	p.skip()
	if p.pos >= len(p.lines) {
		return nil, nil
	}
	return p.node(p.lines[p.pos].indent), nil
}

type parseError struct {
	line int
	msg  string
}

func (e parseError) Error() string { return fmt.Sprintf("yaml: line %d: %s", e.line+1, e.msg) }

func (p *parser) fail(msg string) {
	n := 0
	if p.pos < len(p.lines) {
		n = p.lines[p.pos].num
	}
	panic(parseError{n, msg})
}

func isComment(t string) bool { return t == "" || t[0] == '#' }

// skip advances past blank and comment lines.
func (p *parser) skip() {
	for p.pos < len(p.lines) && isComment(p.lines[p.pos].text) {
		p.pos++
	}
}

func (p *parser) cur() *line {
	p.skip()
	if p.pos >= len(p.lines) {
		return nil
	}
	return &p.lines[p.pos]
}

func isSeqItem(t string) bool {
	return t == "-" || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "-\t")
}

// node parses the block node starting at the current line with the given indent.
func (p *parser) node(indent int) any {
	l := p.cur()
	if l == nil || l.indent < indent {
		return nil
	}
	if isSeqItem(l.text) {
		return p.seq(l.indent)
	}
	if _, _, ok := splitKey(l.text); ok {
		return p.mapping(l.indent)
	}
	p.pos++
	return p.inline(p.continuation(l.text, indent))
}

// continuation joins the lines of a multi-line plain, quoted or flow scalar:
// following non-blank, non-comment lines indented at least minIndent are
// folded into the value with single spaces, as YAML does.
func (p *parser) continuation(first string, minIndent int) string {
	for p.pos < len(p.lines) {
		l := p.lines[p.pos]
		if isComment(l.text) || l.indent < minIndent {
			break
		}
		first += " " + l.text
		p.pos++
	}
	return first
}

func (p *parser) seq(indent int) []any {
	out := []any{}
	for {
		l := p.cur()
		if l == nil || l.indent != indent || !isSeqItem(l.text) {
			return out
		}
		rest := strings.TrimLeft(l.text[1:], " \t")
		if isComment(rest) {
			p.pos++
			out = append(out, p.child(indent))
			continue
		}
		// Re-read the item content as if it were its own line, so maps and
		// nested sequences inside items work naturally.
		l.indent += len(l.text) - len(rest)
		l.text = rest
		out = append(out, p.node(l.indent))
	}
}

// child parses the value of a key or item whose content starts on the next line.
func (p *parser) child(indent int) any {
	n := p.cur()
	if n == nil {
		return nil
	}
	if n.indent > indent {
		return p.node(n.indent)
	}
	return nil
}

func (p *parser) mapping(indent int) map[string]any {
	out := map[string]any{}
	for {
		l := p.cur()
		if l == nil || l.indent != indent {
			return out
		}
		key, rest, ok := splitKey(l.text)
		if !ok {
			if isSeqItem(l.text) {
				return out
			}
			p.fail("expected a key")
		}
		p.pos++
		if rest != "" && rest[0] != '|' && rest[0] != '>' && rest[0] != '&' && rest[0] != '!' {
			rest = p.continuation(rest, indent+1)
		}
		rest = stripComment(rest)
		switch {
		case rest == "":
			n := p.cur()
			if n != nil && n.indent == indent && isSeqItem(n.text) {
				out[key] = p.seq(indent) // "key:\n- a" at the same indent
			} else {
				out[key] = p.child(indent)
			}
		case rest[0] == '|' || rest[0] == '>':
			out[key] = p.blockScalar(rest, indent)
		case rest[0] == '&' || rest[0] == '!': // anchors/tags: ignore the marker
			if i := strings.IndexAny(rest, " \t"); i >= 0 {
				out[key] = p.inline(strings.TrimSpace(rest[i:]))
			} else {
				out[key] = p.child(indent)
			}
		default:
			out[key] = p.inline(rest)
		}
	}
}

// blockScalar reads a | or > scalar from the raw lines following the key.
func (p *parser) blockScalar(header string, indent int) string {
	folded := header[0] == '>'
	chomp := byte(0)
	if strings.ContainsRune(header, '-') {
		chomp = '-'
	} else if strings.ContainsRune(header, '+') {
		chomp = '+'
	}
	start := p.lines[p.pos-1].num + 1
	var body []string
	blockIndent := -1
	end := start
	for i := start; i < len(p.raw); i++ {
		r := strings.TrimRight(p.raw[i], " \t\r")
		if r == "" {
			body = append(body, "")
			end = i + 1
			continue
		}
		ind := len(r) - len(strings.TrimLeft(r, " "))
		if blockIndent < 0 {
			if ind <= indent {
				break
			}
			blockIndent = ind
		}
		if ind < blockIndent {
			break
		}
		body = append(body, r[blockIndent:])
		end = i + 1
	}
	for p.pos < len(p.lines) && p.lines[p.pos].num < end {
		p.pos++
	}
	trail := 0
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
		trail++
	}
	var s string
	if folded {
		var b strings.Builder
		for i, l := range body {
			if i > 0 {
				if l == "" || body[i-1] == "" {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
			}
			b.WriteString(l)
		}
		s = b.String()
	} else {
		s = strings.Join(body, "\n")
	}
	switch chomp {
	case '-':
	case '+':
		s += strings.Repeat("\n", trail+1)
	default:
		if len(body) > 0 {
			s += "\n"
		}
	}
	return s
}

// splitKey splits "key: value". The key may be quoted.
func splitKey(t string) (key, rest string, ok bool) {
	if t == "" || t[0] == '[' || t[0] == '{' || isSeqItem(t) {
		return "", "", false
	}
	if t[0] == '"' || t[0] == '\'' {
		s, n, err := quoted(t)
		if err != nil {
			return "", "", false
		}
		r := strings.TrimLeft(t[n:], " \t")
		if strings.HasPrefix(r, ":") && (len(r) == 1 || r[1] == ' ' || r[1] == '\t') {
			return s, strings.TrimSpace(r[1:]), true
		}
		return "", "", false
	}
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case ':':
			if i+1 == len(t) || t[i+1] == ' ' || t[i+1] == '\t' {
				return strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+1:]), true
			}
		case '#':
			if i > 0 && (t[i-1] == ' ' || t[i-1] == '\t') {
				return "", "", false
			}
		}
	}
	return "", "", false
}

// stripComment removes a trailing " # comment" outside of quotes.
func stripComment(s string) string {
	inS, inD := false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case c == '\\' && inD:
			i++
		case c == '#' && !inS && !inD && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

// inline parses a single-line value: a quoted or plain scalar, or a flow collection.
func (p *parser) inline(t string) any {
	t = strings.TrimSpace(t)
	if t == "" {
		return nil
	}
	switch t[0] {
	case '"', '\'':
		s, n, err := quoted(t)
		if err != nil {
			p.fail(err.Error())
		}
		if r := stripComment(t[n:]); strings.TrimSpace(r) != "" {
			// Not a lone quoted scalar, e.g. "a" + "b": keep the text as is.
			return stripComment(t)
		}
		return s
	case '[', '{':
		v, n, err := flow(t, 0)
		if err != nil {
			p.fail(err.Error())
		}
		_ = n
		return v
	}
	return Scalar(stripComment(t))
}

// Scalar converts a plain scalar to its typed value.
func Scalar(s string) any {
	switch s {
	case "", "~", "null", "Null", "NULL":
		return nil
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	}
	c := s[0]
	// Only digit-bearing scalars are numbers: "+inf", "nan" and "Infinity"
	// stay strings (Go's parsers would otherwise accept them).
	if (c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.') && numberChars(s) && strings.ContainsAny(s, "0123456789") {
		// YAML 1.2: a leading zero does not make an octal number ("012" is 12).
		if d := strings.TrimLeft(s, "+-"); len(d) > 1 && d[0] == '0' && strings.Trim(d, "0123456789") == "" {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return float64(n)
			}
		}
		if n, err := strconv.ParseInt(s, 0, 64); err == nil {
			return float64(n)
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && !strings.ContainsAny(s, "xXpP_") {
			return f
		}
	}
	return s
}

// numberChars reports whether s only has characters strconv's integer
// (base 0) and float parsers can accept, with signs only at the start or
// after an exponent marker. It is a cheap filter that keeps dates, times
// and similar scalars away from the parsers.
func numberChars(s string) bool {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '+' || c == '-':
			if i > 0 {
				switch s[i-1] {
				case 'e', 'E', 'p', 'P':
				default:
					return false
				}
			}
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F',
			c == 'x', c == 'X', c == 'o', c == 'O', c == 'p', c == 'P', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// quoted parses a quoted string at the start of t, returning the value and bytes consumed.
func quoted(t string) (string, int, error) {
	q := t[0]
	// Fast path: no escapes before the closing quote, so the value is a
	// substring of t.
	for i := 1; i < len(t); i++ {
		c := t[i]
		if q == '"' && c == '\\' {
			break
		}
		if c == q {
			if q == '\'' && i+1 < len(t) && t[i+1] == '\'' {
				break // '' escape
			}
			return t[1:i], i + 1, nil
		}
	}
	var b strings.Builder
	for i := 1; i < len(t); i++ {
		c := t[i]
		if q == '\'' {
			if c == '\'' {
				if i+1 < len(t) && t[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				return b.String(), i + 1, nil
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '"':
			return b.String(), i + 1, nil
		case '\\':
			i++
			if i >= len(t) {
				return "", 0, fmt.Errorf("unterminated escape")
			}
			switch t[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case 'u':
				if i+4 < len(t) {
					if r, err := strconv.ParseUint(t[i+1:i+5], 16, 32); err == nil {
						b.WriteRune(rune(r))
						i += 4
						continue
					}
				}
				b.WriteString(`\u`)
			default:
				b.WriteByte(t[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", 0, fmt.Errorf("unterminated string")
}

// flow parses a flow sequence or map starting at t[i].
func flow(t string, i int) (any, int, error) {
	skipWS := func() {
		for i < len(t) && (t[i] == ' ' || t[i] == '\t') {
			i++
		}
	}
	value := func(stop string) (any, error) {
		skipWS()
		if i >= len(t) {
			return nil, fmt.Errorf("unterminated flow collection")
		}
		switch t[i] {
		case '[', '{':
			v, n, err := flow(t, i)
			i = n
			return v, err
		case '"', '\'':
			s, n, err := quoted(t[i:])
			i += n
			return s, err
		}
		start := i
		depth := 0
		for i < len(t) {
			c := t[i]
			if c == '(' {
				depth++
			} else if c == ')' {
				depth--
			} else if depth == 0 && strings.IndexByte(stop, c) >= 0 {
				if c != ':' || i+1 == len(t) || t[i+1] == ' ' {
					break
				}
			}
			i++
		}
		return Scalar(strings.TrimSpace(t[start:i])), nil
	}
	open := t[i]
	i++
	if open == '[' {
		out := []any{}
		for {
			skipWS()
			if i < len(t) && t[i] == ']' {
				return out, i + 1, nil
			}
			v, err := value(",]")
			if err != nil {
				return nil, i, err
			}
			out = append(out, v)
			skipWS()
			if i < len(t) && t[i] == ',' {
				i++
			}
			if i >= len(t) {
				return nil, i, fmt.Errorf("unterminated flow sequence")
			}
		}
	}
	out := map[string]any{}
	for {
		skipWS()
		if i < len(t) && t[i] == '}' {
			return out, i + 1, nil
		}
		k, err := value(":,}")
		if err != nil {
			return nil, i, err
		}
		skipWS()
		var v any
		if i < len(t) && t[i] == ':' {
			i++
			if v, err = value(",}"); err != nil {
				return nil, i, err
			}
		}
		out[fmt.Sprint(k)] = v
		skipWS()
		if i < len(t) && t[i] == ',' {
			i++
		}
		if i >= len(t) {
			return nil, i, fmt.Errorf("unterminated flow map")
		}
	}
}
