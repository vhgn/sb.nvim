package bases

import (
	"fmt"
	"math"
	"math/rand/v2"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"brain/internal/vault"
)

// row is the evaluation context of one file.
type row struct {
	f         *vault.File
	formulas  []propMemo // computed formula values
	computing []string   // formulas being computed (cycle detection)
	errs      map[string]error
	props     []propMemo // converted front matter values already looked up
}

type propMemo struct {
	name string
	v    any
}

type evaluator struct {
	v        *vault.Vault
	formulas map[string]Node
	now      time.Time
	this     *row
	rows     map[*vault.File]*row
}

func (e *evaluator) rowOf(f *vault.File) *row {
	if r := e.rows[f]; r != nil {
		return r
	}
	r := &row{f: f}
	e.rows[f] = r
	return r
}

type scope struct {
	r    *row
	vars map[string]any
}

func (s scope) with(k string, v any) scope {
	vars := make(map[string]any, len(s.vars)+1)
	for a, b := range s.vars {
		vars[a] = b
	}
	vars[k] = v
	return scope{s.r, vars}
}

type evalError struct{ msg string }

func (e evalError) Error() string { return e.msg }

func fail(format string, a ...any) { panic(evalError{fmt.Sprintf(format, a...)}) }

// Eval evaluates n for row r, converting evaluation panics into errors.
func (e *evaluator) Eval(n Node, s scope) (v any, err error) {
	defer func() {
		if x := recover(); x != nil {
			if ee, ok := x.(evalError); ok {
				err = ee
				return
			}
			panic(x)
		}
	}()
	return e.eval(n, s), nil
}

func (e *evaluator) formula(r *row, name string) any {
	for _, m := range r.formulas {
		if m.name == name {
			return m.v
		}
	}
	if err := r.errs[name]; err != nil {
		fail("%v", err)
	}
	n, ok := e.formulas[name]
	if !ok {
		return nil
	}
	if slices.Contains(r.computing, name) {
		fail("formula %q refers to itself", name)
	}
	r.computing = append(r.computing, name)
	v, err := e.Eval(n, scope{r: r})
	r.computing = r.computing[:len(r.computing)-1]
	if err != nil {
		if r.errs == nil {
			r.errs = map[string]error{}
		}
		r.errs[name] = err
		fail("%v", err)
	}
	r.formulas = append(r.formulas, propMemo{name, v})
	return v
}

func (e *evaluator) prop(r *row, name string) any {
	if r == nil || r.f == nil || r.f.Props == nil {
		return nil
	}
	// Converting a value parses dates and resolves links, so remember it.
	// Values are never modified in place, so sharing them is safe.
	for _, m := range r.props {
		if m.name == name {
			return m.v
		}
	}
	v := e.convertProp(r, name)
	if r.props == nil {
		r.props = make([]propMemo, 0, 8)
	}
	r.props = append(r.props, propMemo{name, v})
	return v
}

func (e *evaluator) convertProp(r *row, name string) any {
	v, ok := r.f.Props[name]
	if !ok {
		// Obsidian property names are case-insensitive.
		for k, x := range r.f.Props {
			if strings.EqualFold(k, name) {
				v = x
				break
			}
		}
	}
	return e.propValue(v, r.f)
}

func (e *evaluator) eval(n Node, s scope) any {
	switch n := n.(type) {
	case litNode:
		return n.v
	case regexNode:
		return compileRegex(n.pattern, n.flags)
	case identNode:
		if v, ok := s.vars[n.name]; ok {
			return v
		}
		switch n.name {
		case "file":
			return FileVal{s.r.f}
		case "note":
			return noteMap{s.r}
		case "formula":
			return formNS{s.r}
		case "this":
			if e.this == nil {
				return nil
			}
			return RowVal{e.this}
		}
		return e.prop(s.r, n.name)
	case listNode:
		out := make([]any, len(n.items))
		for i, x := range n.items {
			out[i] = e.eval(x, s)
		}
		return out
	case objNode:
		out := map[string]any{}
		for i, k := range n.keys {
			out[k] = e.eval(n.vals[i], s)
		}
		return out
	case memberNode:
		return e.member(e.eval(n.obj, s), n.name)
	case indexNode:
		obj := e.eval(n.obj, s)
		k := e.eval(n.key, s)
		if l, ok := obj.([]any); ok {
			if i, ok := toNumber(k); ok {
				idx := int(i)
				if idx < 0 {
					idx += len(l)
				}
				if idx >= 0 && idx < len(l) {
					return l[idx]
				}
				return nil
			}
		}
		if str, ok := obj.(string); ok {
			if i, ok := k.(float64); ok {
				r := []rune(str)
				if i >= 0 && int(i) < len(r) && i == math.Trunc(i) {
					return string(r[int(i)])
				}
				return nil
			}
		}
		return e.member(obj, toString(k))
	case condNode:
		if truthy(e.eval(n.c, s)) {
			return e.eval(n.a, s)
		}
		return e.eval(n.b, s)
	case unaryNode:
		x := e.eval(n.x, s)
		switch n.op {
		case "!":
			return !truthy(x)
		case "-":
			switch y := x.(type) {
			case Duration:
				return y.neg()
			case string:
				if d, ok := parseDuration(y); ok {
					return d.neg()
				}
			}
			f, ok := toNumber(x)
			if !ok {
				fail("cannot negate %s", typeName(x))
			}
			return -f
		case "+":
			f, _ := toNumber(x)
			return f
		}
	case binaryNode:
		switch n.op {
		case "&&":
			l := e.eval(n.l, s)
			if !truthy(l) {
				return l
			}
			return e.eval(n.r, s)
		case "||":
			l := e.eval(n.l, s)
			if truthy(l) {
				return l
			}
			return e.eval(n.r, s)
		case "??":
			l := e.eval(n.l, s)
			if l != nil {
				return l
			}
			return e.eval(n.r, s)
		}
		return binary(n.op, e.eval(n.l, s), e.eval(n.r, s))
	case callNode:
		switch fn := n.fn.(type) {
		case identNode:
			if _, ok := s.vars[fn.name]; !ok {
				return e.global(fn.name, n.args, s)
			}
		case memberNode:
			recv := e.eval(fn.obj, s)
			return e.method(recv, fn.name, n.args, s)
		}
		fail("not callable")
	}
	fail("cannot evaluate %T", n)
	return nil
}

func binary(op string, l, r any) any {
	switch op {
	case "==":
		return equal(l, r)
	case "!=":
		return !equal(l, r)
	case "<", ">", "<=", ">=":
		c, ok := compare(l, r)
		if !ok {
			return false
		}
		switch op {
		case "<":
			return c < 0
		case ">":
			return c > 0
		case "<=":
			return c <= 0
		default:
			return c >= 0
		}
	case "+":
		if d, ok := l.(Date); ok {
			if dur, ok := asDuration(r); ok {
				return d.add(dur)
			}
		}
		if d, ok := r.(Date); ok {
			if dur, ok := asDuration(l); ok {
				return d.add(dur)
			}
		}
		if a, ok := l.(Duration); ok {
			if b, ok := asDuration(r); ok {
				return Duration{a.Years + b.Years, a.Months + b.Months, a.Days + b.Days, a.Ms + b.Ms}
			}
		}
		if a, ok := l.([]any); ok {
			if b, ok := r.([]any); ok {
				return append(append([]any{}, a...), b...)
			}
			return append(append([]any{}, a...), r)
		}
		_, ls := l.(string)
		_, rs := r.(string)
		if ls || rs || isLinkish(l) || isLinkish(r) {
			return toString(l) + toString(r)
		}
		return arith(op, l, r)
	case "-":
		if d, ok := l.(Date); ok {
			if dur, ok := asDuration(r); ok {
				return d.add(dur.neg())
			}
			if s, ok := r.(string); ok {
				if d2, ok := parseDate(s); ok {
					r = d2
				}
			}
			if d2, ok := r.(Date); ok {
				// date - date is a duration
				return Duration{Ms: float64(d.T.Sub(d2.T).Milliseconds())}
			}
		}
		return arith(op, l, r)
	case "*", "/", "%":
		// duration * n scales the duration (the documented form); division
		// keeps working on milliseconds so (now() - date) / 86400000 is days.
		if d, ok := l.(Duration); ok && op == "*" {
			if k, ok := r.(float64); ok {
				return d.scale(k)
			}
		}
		return arith(op, l, r)
	}
	fail("unknown operator %s", op)
	return nil
}

func isLinkish(v any) bool {
	switch v.(type) {
	case Link, FileVal, Date:
		return true
	}
	return false
}

func asDuration(v any) (Duration, bool) {
	switch x := v.(type) {
	case Duration:
		return x, true
	case string:
		return parseDuration(x)
	case float64:
		return Duration{Ms: x}, true
	}
	return Duration{}, false
}

func arith(op string, l, r any) any {
	if l == nil || r == nil {
		return nil
	}
	a, ok1 := toNumber(l)
	b, ok2 := toNumber(r)
	if !ok1 || !ok2 {
		fail("cannot apply %s to %s and %s", op, typeName(l), typeName(r))
	}
	switch op {
	case "+":
		return a + b
	case "-":
		return a - b
	case "*":
		return a * b
	case "/":
		return a / b
	default:
		return math.Mod(a, b)
	}
}

func compileRegex(pattern, flags string) Regex {
	prefix := ""
	global := false
	for _, f := range flags {
		switch f {
		case 'i', 'm', 's':
			prefix += string(f)
		case 'g':
			global = true
		}
	}
	if prefix != "" {
		pattern = "(?" + prefix + ")" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		fail("bad regex: %v", err)
	}
	return Regex{Re: re, Global: global}
}

// ---- members ----

func (e *evaluator) member(obj any, name string) any {
	switch x := obj.(type) {
	case nil:
		return nil
	case FileVal:
		return e.fileField(x.F, name)
	case RowVal:
		switch name {
		case "file":
			return FileVal{x.r.f}
		case "note":
			return noteMap{x.r}
		case "formula":
			return formNS{x.r}
		}
		return e.prop(x.r, name)
	case noteMap:
		return e.prop(x.r, name)
	case formNS:
		return e.formula(x.r, name)
	case map[string]any:
		return x[name]
	case string:
		if name == "length" {
			return float64(len([]rune(x)))
		}
	case []any:
		if name == "length" {
			return float64(len(x))
		}
	case Date:
		switch name {
		case "year":
			return float64(x.T.Year())
		case "month":
			return float64(x.T.Month())
		case "day":
			return float64(x.T.Day())
		case "hour":
			return float64(x.T.Hour())
		case "minute":
			return float64(x.T.Minute())
		case "second":
			return float64(x.T.Second())
		case "millisecond":
			return float64(x.T.Nanosecond() / 1e6)
		}
	case Duration:
		switch name {
		case "years":
			return float64(x.Years)
		case "months":
			return float64(x.Months)
		case "days":
			return float64(x.Days) + x.Ms/86400e3
		case "hours":
			return x.totalMs() / 3600e3
		case "minutes":
			return x.totalMs() / 60e3
		case "seconds":
			return x.totalMs() / 1e3
		case "milliseconds":
			return x.totalMs()
		}
	case Link:
		if x.File != nil {
			return e.fileField(x.File, name)
		}
	}
	return nil
}

func (e *evaluator) fileField(f *vault.File, name string) any {
	if f == nil {
		return nil
	}
	switch name {
	case "name":
		return f.Name()
	case "basename":
		return f.Basename()
	case "path":
		return f.Rel
	case "folder":
		return f.Folder()
	case "ext":
		return f.Ext()
	case "size":
		return float64(f.Size)
	case "ctime":
		return Date{T: time.UnixMilli(f.Ctime), HasTime: true}
	case "mtime":
		return Date{T: time.UnixMilli(f.Mtime), HasTime: true}
	case "tags":
		out := make([]any, len(f.Tags))
		for i, t := range f.Tags {
			out[i] = "#" + t
		}
		return out
	case "links":
		if targets, ok := e.v.LinkTargets(f); ok {
			return linkList(f.Links, targets)
		}
		return e.links(f.Links, f)
	case "embeds":
		return e.links(f.Embeds, f)
	case "backlinks":
		var out []any
		for _, rel := range e.v.Backlinks(f.Rel) {
			if b := e.v.Get(rel); b != nil {
				out = append(out, Link{Target: b.Rel, File: b})
			}
		}
		if out == nil {
			out = []any{}
		}
		return out
	case "properties":
		out := map[string]any{}
		for k, v := range f.Props {
			out[k] = e.propValue(v, f)
		}
		return out
	case "file":
		return FileVal{f}
	}
	return nil
}

func (e *evaluator) links(targets []string, from *vault.File) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, t := range targets {
		l := e.link(t, "", from)
		// [[b]], [[sub/b]] and [x](sub/b.md) are one link when they resolve
		// to the same file.
		key := "?" + strings.ToLower(t)
		if l.File != nil {
			key = l.File.Rel
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	return out
}

// linkList is links() with the targets already resolved.
func linkList(targets []string, files []*vault.File) []any {
	out := []any{}
	seen := map[string]bool{}
	for i, t := range targets {
		l := Link{Target: t, File: files[i]}
		key := "?" + strings.ToLower(t)
		if l.File != nil {
			key = l.File.Rel
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	return out
}

// ---- global functions ----

func (e *evaluator) args(args []Node, s scope) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = e.eval(a, s)
	}
	return out
}

func arg(a []any, i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}

func (e *evaluator) global(name string, argNodes []Node, s scope) any {
	if name == "if" {
		if len(argNodes) < 2 {
			fail("if() needs a condition and a value")
		}
		if truthy(e.eval(argNodes[0], s)) {
			return e.eval(argNodes[1], s)
		}
		if len(argNodes) > 2 {
			return e.eval(argNodes[2], s)
		}
		return nil
	}
	a := e.args(argNodes, s)
	switch name {
	case "now":
		return Date{T: e.now, HasTime: true}
	case "today":
		y, m, d := e.now.Date()
		return Date{T: time.Date(y, m, d, 0, 0, 0, 0, e.now.Location())}
	case "date":
		switch x := arg(a, 0).(type) {
		case Date:
			return x
		case string:
			if d, ok := parseDate(x); ok {
				return d
			}
			fail("invalid date %q", x)
		case float64:
			return Date{T: time.UnixMilli(int64(x)), HasTime: true}
		}
		return nil
	case "duration":
		if d, ok := asDuration(arg(a, 0)); ok {
			return d
		}
		fail("invalid duration %q", toString(arg(a, 0)))
	case "number":
		if f, ok := toNumber(arg(a, 0)); ok {
			return f
		}
		return math.NaN()
	case "string":
		return toString(arg(a, 0))
	case "list":
		if l, ok := arg(a, 0).([]any); ok {
			return l
		}
		if arg(a, 0) == nil {
			return []any{}
		}
		return []any{arg(a, 0)}
	case "max", "min":
		var best any
		for _, x := range flatten(a) {
			if x == nil {
				continue
			}
			if best == nil {
				best = x
				continue
			}
			if c, ok := compare(x, best); ok && (name == "max" && c > 0 || name == "min" && c < 0) {
				best = x
			}
		}
		return best
	case "link":
		target := toString(arg(a, 0))
		if f := fileOf(arg(a, 0)); f != nil {
			target = f.Rel
		}
		return e.link(target, toString(arg(a, 1)), s.r.f)
	case "file":
		if f := fileOf(arg(a, 0)); f != nil {
			return FileVal{f}
		}
		if f := e.v.Resolve(toString(arg(a, 0)), s.r.f.Rel); f != nil {
			return FileVal{f}
		}
		return nil
	case "image":
		if f := fileOf(arg(a, 0)); f != nil {
			return Image{f.Rel}
		}
		return Image{toString(arg(a, 0))}
	case "icon":
		return Icon{toString(arg(a, 0))}
	case "html":
		return HTML{toString(arg(a, 0))}
	case "random":
		return rand.Float64()
	case "escapeHTML":
		r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
		return r.Replace(toString(arg(a, 0)))
	}
	fail("unknown function %s()", name)
	return nil
}

func flatten(a []any) []any {
	var out []any
	for _, x := range a {
		if l, ok := x.([]any); ok {
			out = append(out, flatten(l)...)
		} else {
			out = append(out, x)
		}
	}
	return out
}

// ---- methods ----

func (e *evaluator) method(recv any, name string, argNodes []Node, s scope) any {
	// Lambda-taking list methods evaluate their arguments per element.
	if l, ok := recv.([]any); ok {
		switch name {
		case "filter", "map", "reduce", "sort", "find", "some", "every":
			return e.listLambda(l, name, argNodes, s)
		}
	}
	a := e.args(argNodes, s)

	// Methods available on every type.
	switch name {
	case "isTruthy":
		return truthy(recv)
	case "isType":
		return typeName(recv) == strings.ToLower(toString(arg(a, 0)))
	case "toString":
		return toString(recv)
	case "isEmpty":
		return isEmpty(recv)
	}

	switch x := recv.(type) {
	case nil:
		return nil
	case string:
		return e.stringMethod(x, name, a)
	case float64:
		return numberMethod(x, name, a)
	case []any:
		return e.listMethod(x, name, a)
	case Date:
		return dateMethod(x, name, a, e.now)
	case map[string]any:
		switch name {
		case "keys":
			ks := make([]string, 0, len(x))
			for k := range x {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			out := make([]any, len(ks))
			for i, k := range ks {
				out[i] = k
			}
			return out
		case "values":
			ks := make([]string, 0, len(x))
			for k := range x {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			out := make([]any, len(ks))
			for i, k := range ks {
				out[i] = x[k]
			}
			return out
		}
	case Regex:
		if name == "matches" {
			return x.Re.MatchString(toString(arg(a, 0)))
		}
	case FileVal:
		return e.fileMethod(x.F, name, a)
	case RowVal:
		return e.fileMethod(x.r.f, name, a)
	case Link:
		switch name {
		case "asFile":
			if x.File == nil {
				return nil
			}
			return FileVal{x.File}
		case "linksTo":
			t := fileOf(arg(a, 0))
			if x.File == nil || t == nil {
				return false
			}
			return e.fileMethod(x.File, "hasLink", []any{FileVal{t}})
		}
		if x.File != nil {
			return e.fileMethod(x.File, name, a)
		}
	case noteMap:
		return e.method(e.fileField(x.r.f, "properties"), name, argNodes, s)
	}
	fail("unknown method %s() on %s", name, typeName(recv))
	return nil
}

func (e *evaluator) fileMethod(f *vault.File, name string, a []any) any {
	if f == nil {
		return nil
	}
	switch name {
	case "hasTag":
		for _, want := range flatten(a) {
			w := strings.ToLower(strings.TrimPrefix(toString(want), "#"))
			for _, t := range f.Tags {
				lt := strings.ToLower(t)
				if lt == w || strings.HasPrefix(lt, w+"/") {
					return true
				}
			}
		}
		return false
	case "hasLink":
		target := arg(a, 0)
		tf := fileOf(target)
		if tf == nil {
			if s, ok := target.(string); ok {
				tf = e.v.Resolve(linkTarget(s), f.Rel)
			}
		}
		if tf != nil {
			// The vault knows which files link where.
			if linked, ok := e.v.LinksTo(f, tf); ok {
				return linked
			}
			for _, l := range f.Links {
				if e.v.Resolve(l, f.Rel) == tf {
					return true
				}
			}
			return false
		}
		for _, l := range f.Links {
			if strings.EqualFold(linkName(l), linkName(target)) {
				return true
			}
		}
		return false
	case "inFolder":
		dir := strings.Trim(toString(arg(a, 0)), "/")
		if dir == "" {
			return true
		}
		folder := f.Folder()
		return folder == dir || strings.HasPrefix(folder, dir+"/")
	case "hasProperty":
		want := toString(arg(a, 0))
		if _, ok := f.Props[want]; ok {
			return true
		}
		for k := range f.Props { // property names are case-insensitive
			if strings.EqualFold(k, want) {
				return true
			}
		}
		return false
	case "asLink":
		return Link{Target: f.Rel, Display: toString(arg(a, 0)), File: f}
	}
	fail("unknown file method %s()", name)
	return nil
}

// jsReplacement converts a JavaScript replacement string ($1, $&, $$, $<name>)
// into a Go regexp template.
func jsReplacement(s string, groups int) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '$' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			b.WriteString("$$")
			continue
		}
		n := s[i+1]
		switch {
		case n == '$':
			b.WriteString("$$")
			i++
		case n == '&':
			b.WriteString("${0}")
			i++
		case n >= '0' && n <= '9':
			num := int(n - '0')
			used := 1
			if i+2 < len(s) && s[i+2] >= '0' && s[i+2] <= '9' && num*10+int(s[i+2]-'0') <= groups {
				num = num*10 + int(s[i+2]-'0')
				used = 2
			}
			if num == 0 || num > groups {
				b.WriteString("$$")
				continue
			}
			fmt.Fprintf(&b, "${%d}", num)
			i += used
		case n == '<':
			if j := strings.IndexByte(s[i:], '>'); j > 0 {
				b.WriteString("${" + s[i+2:i+j] + "}")
				i += j
				continue
			}
			b.WriteString("$$")
		default:
			b.WriteString("$$")
		}
	}
	return b.String()
}

func linkTarget(s string) string {
	if m := wikiOnly(strings.TrimSpace(s)); m != nil {
		return m[1]
	}
	return s
}

func (e *evaluator) stringMethod(x, name string, a []any) any {
	switch name {
	case "contains":
		return strings.Contains(x, toString(arg(a, 0)))
	case "containsAll":
		for _, y := range flatten(a) {
			if !strings.Contains(x, toString(y)) {
				return false
			}
		}
		return true
	case "containsAny":
		for _, y := range flatten(a) {
			if strings.Contains(x, toString(y)) {
				return true
			}
		}
		return false
	case "startsWith":
		return strings.HasPrefix(x, toString(arg(a, 0)))
	case "endsWith":
		return strings.HasSuffix(x, toString(arg(a, 0)))
	case "lower":
		return strings.ToLower(x)
	case "upper":
		return strings.ToUpper(x)
	case "title":
		words := strings.Fields(x)
		for i, w := range words {
			r := []rune(w)
			words[i] = strings.ToUpper(string(r[:1])) + strings.ToLower(string(r[1:]))
		}
		return strings.Join(words, " ")
	case "trim":
		return strings.TrimSpace(x)
	case "replace":
		repl := toString(arg(a, 1))
		if re, ok := arg(a, 0).(Regex); ok {
			tmpl := jsReplacement(repl, re.Re.NumSubexp())
			if re.Global {
				return re.Re.ReplaceAllString(x, tmpl)
			}
			loc := re.Re.FindStringSubmatchIndex(x)
			if loc == nil {
				return x
			}
			return x[:loc[0]] + string(re.Re.ExpandString(nil, tmpl, x, loc)) + x[loc[1]:]
		}
		// A string pattern replaces every occurrence.
		return strings.ReplaceAll(x, toString(arg(a, 0)), repl)
	case "replaceAll":
		if re, ok := arg(a, 0).(Regex); ok {
			return re.Re.ReplaceAllString(x, jsReplacement(toString(arg(a, 1)), re.Re.NumSubexp()))
		}
		return strings.ReplaceAll(x, toString(arg(a, 0)), toString(arg(a, 1)))
	case "repeat":
		n, _ := toNumber(arg(a, 0))
		if math.IsNaN(n) || n < 0 || n > 10000 {
			fail("invalid repeat count")
		}
		return strings.Repeat(x, int(n))
	case "reverse":
		r := []rune(x)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		return string(r)
	case "slice":
		r := []rune(x)
		s, en := sliceBounds(len(r), a)
		return string(r[s:en])
	case "split":
		var parts []string
		if re, ok := arg(a, 0).(Regex); ok {
			parts = re.Re.Split(x, -1)
		} else {
			parts = strings.Split(x, toString(arg(a, 0)))
		}
		if n, ok := toNumber(arg(a, 1)); ok && int(n) < len(parts) && n >= 0 {
			parts = parts[:int(n)]
		}
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = p
		}
		return out
	case "matches":
		if re, ok := arg(a, 0).(Regex); ok {
			return re.Re.MatchString(x)
		}
		return strings.Contains(x, toString(arg(a, 0)))
	case "length":
		return float64(len([]rune(x)))
	case "asLink":
		return e.link(x, toString(arg(a, 0)), nil)
	}
	// Strings that look like dates or links get those methods too.
	if d, ok := parseDate(x); ok {
		return dateMethod(d, name, a, e.now)
	}
	if f, ok := toNumber(x); ok {
		return numberMethod(f, name, a)
	}
	fail("unknown string method %s()", name)
	return nil
}

func sliceBounds(n int, a []any) (int, int) {
	s, en := 0, n
	if f, ok := toNumber(arg(a, 0)); ok {
		s = int(f)
	}
	if f, ok := toNumber(arg(a, 1)); ok {
		en = int(f)
	}
	if s < 0 {
		s += n
	}
	if en < 0 {
		en += n
	}
	s = max(0, min(s, n))
	en = max(s, min(en, n))
	return s, en
}

func numberMethod(x float64, name string, a []any) any {
	switch name {
	case "abs":
		return math.Abs(x)
	case "ceil":
		return math.Ceil(x)
	case "floor":
		return math.Floor(x)
	case "round":
		d, _ := toNumber(arg(a, 0))
		p := math.Pow(10, d)
		return math.Round(x*p) / p
	case "toFixed":
		d, _ := toNumber(arg(a, 0))
		return strconv.FormatFloat(x, 'f', max(0, min(int(d), 20)), 64)
	}
	fail("unknown number method %s()", name)
	return nil
}

func (e *evaluator) listMethod(x []any, name string, a []any) any {
	switch name {
	case "contains":
		for _, y := range x {
			if equal(y, arg(a, 0)) {
				return true
			}
		}
		return false
	case "containsAll", "containsAny":
		want := flatten(a)
		hits := 0
		for _, w := range want {
			for _, y := range x {
				if equal(y, w) {
					hits++
					break
				}
			}
		}
		if name == "containsAll" {
			return hits == len(want)
		}
		return hits > 0
	case "flat": // one level, like Array.prototype.flat()
		out := []any{}
		for _, y := range x {
			if l, ok := y.([]any); ok {
				out = append(out, l...)
			} else {
				out = append(out, y)
			}
		}
		return out
	case "join":
		sep := ", "
		if len(a) > 0 {
			sep = toString(a[0])
		}
		parts := make([]string, len(x))
		for i, y := range x {
			parts[i] = toString(y)
		}
		return strings.Join(parts, sep)
	case "reverse":
		out := make([]any, len(x))
		for i, y := range x {
			out[len(x)-1-i] = y
		}
		return out
	case "slice":
		s, en := sliceBounds(len(x), a)
		return append([]any{}, x[s:en]...)
	case "unique":
		var out []any
		for _, y := range x {
			dup := false
			for _, z := range out {
				if equal(y, z) {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, y)
			}
		}
		if out == nil {
			out = []any{}
		}
		return out
	case "first":
		return arg(x, 0)
	case "last":
		if len(x) == 0 {
			return nil
		}
		return x[len(x)-1]
	case "length":
		return float64(len(x))
	}
	fail("unknown list method %s()", name)
	return nil
}

func (e *evaluator) listLambda(x []any, name string, args []Node, s scope) any {
	call := func(i int, v any, extra ...any) any {
		sc := s.with("value", v).with("index", float64(i))
		if len(extra) > 0 {
			sc = sc.with("acc", extra[0])
		}
		if len(args) == 0 {
			return v
		}
		return e.eval(args[0], sc)
	}
	switch name {
	case "filter":
		out := []any{}
		for i, v := range x {
			if truthy(call(i, v)) {
				out = append(out, v)
			}
		}
		return out
	case "map":
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = call(i, v)
		}
		return out
	case "reduce":
		var acc any
		if len(args) > 1 {
			acc = e.eval(args[1], s)
		}
		for i, v := range x {
			acc = call(i, v, acc)
		}
		return acc
	case "find":
		for i, v := range x {
			if truthy(call(i, v)) {
				return v
			}
		}
		return nil
	case "some":
		for i, v := range x {
			if truthy(call(i, v)) {
				return true
			}
		}
		return false
	case "every":
		for i, v := range x {
			if !truthy(call(i, v)) {
				return false
			}
		}
		return true
	case "sort":
		out := append([]any{}, x...)
		if len(args) == 0 {
			sort.SliceStable(out, func(i, j int) bool { c, _ := compare(out[i], out[j]); return c < 0 })
			return out
		}
		keys := make([]any, len(out))
		for i, v := range out {
			keys[i] = call(i, v)
		}
		idx := make([]int, len(out))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool { c, _ := compare(keys[idx[i]], keys[idx[j]]); return c < 0 })
		res := make([]any, len(out))
		for i, k := range idx {
			res[i] = out[k]
		}
		return res
	}
	return nil
}

func dateMethod(d Date, name string, a []any, now time.Time) any {
	switch name {
	case "format":
		f := toString(arg(a, 0))
		if f == "" {
			f = "YYYY-MM-DD"
		}
		return formatMoment(d.T, f)
	case "date":
		y, m, dd := d.T.Date()
		return Date{T: time.Date(y, m, dd, 0, 0, 0, 0, d.T.Location())}
	case "time":
		return d.T.Format("15:04:05")
	case "relative":
		return relative(d.T, now)
	case "year", "month", "day", "hour", "minute", "second":
		return (&evaluator{}).member(d, name)
	}
	fail("unknown date method %s()", name)
	return nil
}

func relative(t, now time.Time) string {
	diff := now.Sub(t)
	future := diff < 0
	if future {
		diff = -diff
	}
	var s string
	switch {
	case diff < 45*time.Second:
		s = "a few seconds"
	case diff < 90*time.Second:
		s = "a minute"
	case diff < 45*time.Minute:
		s = fmt.Sprintf("%d minutes", int(math.Round(diff.Minutes())))
	case diff < 90*time.Minute:
		s = "an hour"
	case diff < 22*time.Hour:
		s = fmt.Sprintf("%d hours", int(math.Round(diff.Hours())))
	case diff < 36*time.Hour:
		s = "a day"
	case diff < 26*24*time.Hour:
		s = fmt.Sprintf("%d days", int(math.Round(diff.Hours()/24)))
	case diff < 46*24*time.Hour:
		s = "a month"
	case diff < 320*24*time.Hour:
		s = fmt.Sprintf("%d months", int(math.Round(diff.Hours()/24/30.4)))
	case diff < 548*24*time.Hour:
		s = "a year"
	default:
		s = fmt.Sprintf("%d years", int(math.Round(diff.Hours()/24/365)))
	}
	if future {
		return "in " + s
	}
	return s + " ago"
}

// formatMoment formats t with moment.js tokens.
func formatMoment(t time.Time, f string) string {
	tokens := []struct {
		tok string
		fn  func() string
	}{
		{"YYYY", func() string { return fmt.Sprintf("%04d", t.Year()) }},
		{"YY", func() string { return fmt.Sprintf("%02d", t.Year()%100) }},
		{"MMMM", func() string { return t.Month().String() }},
		{"MMM", func() string { return t.Month().String()[:3] }},
		{"MM", func() string { return fmt.Sprintf("%02d", int(t.Month())) }},
		{"M", func() string { return strconv.Itoa(int(t.Month())) }},
		{"dddd", func() string { return t.Weekday().String() }},
		{"ddd", func() string { return t.Weekday().String()[:3] }},
		{"DDDD", func() string { return fmt.Sprintf("%03d", t.YearDay()) }},
		{"DD", func() string { return fmt.Sprintf("%02d", t.Day()) }},
		{"Do", func() string { return ordinal(t.Day()) }},
		{"D", func() string { return strconv.Itoa(t.Day()) }},
		{"HH", func() string { return fmt.Sprintf("%02d", t.Hour()) }},
		{"H", func() string { return strconv.Itoa(t.Hour()) }},
		{"hh", func() string { return fmt.Sprintf("%02d", (t.Hour()+11)%12+1) }},
		{"h", func() string { return strconv.Itoa((t.Hour()+11)%12 + 1) }},
		{"mm", func() string { return fmt.Sprintf("%02d", t.Minute()) }},
		{"m", func() string { return strconv.Itoa(t.Minute()) }},
		{"ss", func() string { return fmt.Sprintf("%02d", t.Second()) }},
		{"s", func() string { return strconv.Itoa(t.Second()) }},
		{"SSS", func() string { return fmt.Sprintf("%03d", t.Nanosecond()/1e6) }},
		{"A", func() string { return map[bool]string{true: "PM", false: "AM"}[t.Hour() >= 12] }},
		{"a", func() string { return map[bool]string{true: "pm", false: "am"}[t.Hour() >= 12] }},
		{"ZZ", func() string { return t.Format("-0700") }},
		{"Z", func() string { return t.Format("-07:00") }},
		{"X", func() string { return strconv.FormatInt(t.Unix(), 10) }},
		{"x", func() string { return strconv.FormatInt(t.UnixMilli(), 10) }},
		{"W", func() string { _, w := t.ISOWeek(); return strconv.Itoa(w) }},
	}
	var b strings.Builder
	for i := 0; i < len(f); {
		if f[i] == '[' {
			if j := strings.IndexByte(f[i:], ']'); j > 0 {
				b.WriteString(f[i+1 : i+j])
				i += j + 1
				continue
			}
		}
		matched := false
		for _, tk := range tokens {
			if strings.HasPrefix(f[i:], tk.tok) {
				b.WriteString(tk.fn())
				i += len(tk.tok)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(f[i])
			i++
		}
	}
	return b.String()
}

func ordinal(n int) string {
	suf := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suf = "st"
		case 2:
			suf = "nd"
		case 3:
			suf = "rd"
		}
	}
	return strconv.Itoa(n) + suf
}

var _ = path.Base
