package bases

import (
	"fmt"
	"math"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"brain/internal/vault"
	"brain/internal/yaml"
)

// Base is a parsed .base file.
type Base struct {
	Filters    any
	Formulas   map[string]string
	Properties map[string]map[string]any
	Summaries  map[string]string
	Views      []View
}

type SortSpec struct {
	Property  string
	Direction string // ASC or DESC
}

type View struct {
	Type      string
	Name      string
	Limit     int
	Filters   any
	Order     []string
	Sort      []SortSpec
	GroupBy   *SortSpec
	Summaries map[string]string
	Raw       map[string]any
}

// ParseBase decodes a .base document.
func ParseBase(src string) (*Base, error) {
	doc, err := yaml.Parse(src)
	if err != nil {
		return nil, err
	}
	m, _ := doc.(map[string]any)
	if doc != nil && m == nil {
		return nil, fmt.Errorf("a base must be a YAML mapping")
	}
	b := &Base{Formulas: map[string]string{}, Properties: map[string]map[string]any{}, Summaries: map[string]string{}}
	b.Filters = m["filters"]
	for k, v := range asMap(m["formulas"]) {
		b.Formulas[k] = toString(v)
	}
	for k, v := range asMap(m["properties"]) {
		b.Properties[k] = asMap(v)
	}
	for k, v := range asMap(m["summaries"]) {
		b.Summaries[k] = toString(v)
	}
	for _, rv := range asList(m["views"]) {
		vm := asMap(rv)
		if vm == nil {
			continue
		}
		v := View{Type: toString(vm["type"]), Name: toString(vm["name"]), Filters: vm["filters"], Raw: vm, Summaries: map[string]string{}}
		if v.Type == "" {
			v.Type = "table"
		}
		if n, ok := toNumber(vm["limit"]); ok && n > 0 {
			v.Limit = int(n)
		}
		for _, o := range asList(vm["order"]) {
			v.Order = append(v.Order, toString(o))
		}
		for _, s := range asList(vm["sort"]) {
			if sp := sortSpec(s); sp != nil {
				v.Sort = append(v.Sort, *sp)
			}
		}
		v.GroupBy = sortSpec(vm["groupBy"])
		for k, s := range asMap(vm["summaries"]) {
			v.Summaries[k] = toString(s)
		}
		b.Views = append(b.Views, v)
	}
	if len(b.Views) == 0 {
		b.Views = []View{{Type: "table", Name: "Table", Raw: map[string]any{}, Summaries: map[string]string{}}}
	}
	for i := range b.Views {
		if b.Views[i].Name == "" {
			b.Views[i].Name = strings.ToUpper(b.Views[i].Type[:1]) + b.Views[i].Type[1:]
		}
	}
	return b, nil
}

func sortSpec(v any) *SortSpec {
	switch x := v.(type) {
	case string:
		return &SortSpec{x, "ASC"}
	case map[string]any:
		p := toString(x["property"])
		if p == "" {
			return nil
		}
		d := strings.ToUpper(toString(x["direction"]))
		if d != "DESC" {
			d = "ASC"
		}
		return &SortSpec{p, d}
	}
	return nil
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case nil:
		return nil
	}
	return []any{v}
}

// ---- results ----

type Cell struct {
	Text string `json:"t"`
	Kind string `json:"k,omitempty"` // link, date, number, bool, tag, list, empty, image, error
	// Link is the absolute path a link cell points to.
	Link string `json:"l,omitempty"`
}

type Row struct {
	Path   string   `json:"path"`
	Rel    string   `json:"rel"`
	Cells  []Cell   `json:"cells"`
	Image  string   `json:"image,omitempty"`
	Lat    *float64 `json:"lat,omitempty"`
	Lng    *float64 `json:"lng,omitempty"`
	Marker string   `json:"marker,omitempty"`
}

type Group struct {
	Key       string `json:"key"`
	HasKey    bool   `json:"hasKey"`
	Rows      []Row  `json:"rows"`
	Summaries []Cell `json:"summaries,omitempty"`
}

type Column struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
}

type ViewInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Result struct {
	Views     []ViewInfo     `json:"views"`
	View      int            `json:"view"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Columns   []Column       `json:"columns"`
	Groups    []Group        `json:"groups"`
	Summaries []Cell         `json:"summaries,omitempty"`
	Total     int            `json:"total"`
	Shown     int            `json:"shown"`
	Options   map[string]any `json:"options,omitempty"`
	Errors    []string       `json:"errors,omitempty"`
}

// Options control a query.
type Options struct {
	View string    // view name or index ("" = first)
	This string    // vault-relative path of the file `this` refers to
	Now  time.Time // zero = time.Now()
}

type compiled struct {
	node Node
	all  []compiled
	op   string // "", and, or, not
}

func compileFilter(v any) (*compiled, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		n, err := Parse(x)
		if err != nil {
			return nil, fmt.Errorf("filter %q: %w", x, err)
		}
		return &compiled{node: n}, nil
	case bool:
		return &compiled{node: litNode{x}}, nil
	case map[string]any:
		for _, op := range []string{"and", "or", "not"} {
			if sub, ok := x[op]; ok {
				c := &compiled{op: op}
				for _, s := range asList(sub) {
					cs, err := compileFilter(s)
					if err != nil {
						return nil, err
					}
					if cs != nil {
						c.all = append(c.all, *cs)
					}
				}
				return c, nil
			}
		}
		return nil, fmt.Errorf("filter must use and, or or not")
	case []any:
		return compileFilter(map[string]any{"and": x})
	}
	return nil, fmt.Errorf("invalid filter %v", v)
}

func (e *evaluator) test(c *compiled, r *row) (bool, error) {
	if c == nil {
		return true, nil
	}
	switch c.op {
	case "":
		v, err := e.Eval(c.node, scope{r: r})
		return truthy(v), err
	case "and":
		for i := range c.all {
			ok, err := e.test(&c.all[i], r)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case "or":
		var firstErr error
		for i := range c.all {
			ok, err := e.test(&c.all[i], r)
			if ok {
				return true, nil
			}
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return false, firstErr
	case "not":
		for i := range c.all {
			ok, err := e.test(&c.all[i], r)
			if err != nil {
				return false, err
			}
			if ok {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

// propNode compiles a property id such as file.name, note.status, formula.x or status.
func propNode(id string) Node {
	ns, rest, ok := strings.Cut(id, ".")
	switch {
	case ok && ns == "file":
		return memberNode{identNode{"file"}, rest}
	case ok && ns == "note":
		return memberNode{identNode{"note"}, rest}
	case ok && ns == "formula":
		return memberNode{identNode{"formula"}, rest}
	}
	return memberNode{identNode{"note"}, id}
}

// DisplayName returns the column header for a property id.
func (b *Base) DisplayName(id string) string {
	for _, k := range []string{id, strings.TrimPrefix(id, "note.")} {
		if p := b.Properties[k]; p != nil {
			if n := toString(p["displayName"]); n != "" {
				return n
			}
		}
	}
	if !strings.Contains(id, ".") {
		if p := b.Properties["note."+id]; p != nil {
			if n := toString(p["displayName"]); n != "" {
				return n
			}
		}
	}
	switch id {
	case "file.name":
		return "file name"
	case "file.basename":
		return "file base name"
	}
	_, rest, ok := strings.Cut(id, ".")
	if ok && (strings.HasPrefix(id, "note.") || strings.HasPrefix(id, "formula.") || strings.HasPrefix(id, "file.")) {
		if strings.HasPrefix(id, "file.") {
			return "file " + rest
		}
		return rest
	}
	return id
}

// Run evaluates one view of the base against the vault.
func (b *Base) Run(v *vault.Vault, opt Options) *Result {
	res := &Result{Groups: []Group{}, Columns: []Column{}}
	for _, vw := range b.Views {
		res.Views = append(res.Views, ViewInfo{vw.Name, vw.Type})
	}
	vi := 0
	for i, vw := range b.Views {
		if vw.Name == opt.View || fmt.Sprint(i) == opt.View {
			vi = i
			break
		}
	}
	if opt.View != "" && vi == 0 && b.Views[0].Name != opt.View && opt.View != "0" {
		res.Errors = append(res.Errors, fmt.Sprintf("no view named %q", opt.View))
	}
	view := b.Views[vi]
	res.View, res.Type, res.Name, res.Options = vi, view.Type, view.Name, view.Raw

	now := opt.Now
	if now.IsZero() {
		now = time.Now()
	}
	e := &evaluator{v: v, now: now, formulas: map[string]Node{}, rows: map[*vault.File]*row{}}
	addErr := func(err error) {
		if err == nil {
			return
		}
		msg := err.Error()
		for _, x := range res.Errors {
			if x == msg {
				return
			}
		}
		if len(res.Errors) < 20 {
			res.Errors = append(res.Errors, msg)
		}
	}
	for name, src := range b.Formulas {
		n, err := Parse(src)
		if err != nil {
			addErr(fmt.Errorf("formula %s: %w", name, err))
			n = litNode{nil}
		}
		e.formulas[name] = n
	}
	var thisFile *vault.File
	if opt.This != "" {
		if thisFile = v.Get(opt.This); thisFile == nil {
			thisFile = &vault.File{Rel: opt.This}
		}
	}

	// A filter that does not compile matches nothing rather than everything.
	never := &compiled{node: litNode{false}}
	global, err := compileFilter(b.Filters)
	if err != nil {
		addErr(err)
		global = never
	}
	local, err := compileFilter(view.Filters)
	if err != nil {
		addErr(err)
		local = never
	}

	order := view.Order
	if len(order) == 0 {
		order = []string{"file.name"}
	}
	imageProp := toString(view.Raw["image"])
	coordProp := toString(view.Raw["coordinates"])
	latProp, lngProp := toString(view.Raw["lat"]), toString(view.Raw["long"])
	if lngProp == "" {
		lngProp = toString(view.Raw["lng"])
	}
	markerProp := toString(view.Raw["markerIcon"])
	if view.Type == "map" && coordProp == "" && latProp == "" {
		coordProp = "note.coordinates"
	}

	// Rows are evaluated in parallel. Each worker has its own evaluator
	// (and its own `this` row); a row is only touched by one goroutine at a
	// time. Errors are collected per row and replayed in row order, so the
	// result is the same as a sequential evaluation.
	files := v.Files()
	pool := newPool(len(files), func() *evaluator {
		w := &evaluator{v: v, now: now, formulas: e.formulas, rows: map[*vault.File]*row{}}
		if thisFile != nil {
			w.this = &row{f: thisFile}
		}
		return w
	})
	e.this = pool.evs[0].this

	nodes := map[string]Node{}
	for _, id := range append(append([]string{}, order...), imageProp, coordProp, latProp, lngProp, markerProp) {
		nodes[id] = propNode(id)
	}
	for _, s := range view.Sort {
		nodes[s.Property] = propNode(s.Property)
	}
	if view.GroupBy != nil {
		nodes[view.GroupBy.Property] = propNode(view.GroupBy.Property)
	}
	value := func(e *evaluator, r *row, id string, errs *[]error) any {
		n, ok := nodes[id]
		if !ok {
			n = propNode(id)
		}
		val, err := e.Eval(n, scope{r: r})
		if err != nil {
			*errs = append(*errs, err)
			return nil
		}
		return val
	}

	keep := make([]bool, len(files))
	all0 := make([]*row, len(files))
	errs := make([][]error, len(files))
	pool.run(len(files), func(e *evaluator, i int) {
		r := &row{f: files[i]}
		all0[i] = r
		ok, err := e.test(global, r)
		if err != nil {
			errs[i] = append(errs[i], err)
		}
		if !ok {
			return
		}
		ok, err = e.test(local, r)
		if err != nil {
			errs[i] = append(errs[i], err)
		}
		keep[i] = ok
	})
	var rows []*row
	for i, r := range all0 {
		for _, err := range errs[i] {
			addErr(err)
		}
		if keep[i] {
			rows = append(rows, r)
		}
	}

	if len(view.Sort) > 0 {
		keys := make([][]any, len(rows))
		errs := make([][]error, len(rows))
		pool.run(len(rows), func(e *evaluator, i int) {
			for _, s := range view.Sort {
				keys[i] = append(keys[i], value(e, rows[i], s.Property, &errs[i]))
			}
		})
		for _, es := range errs {
			for _, err := range es {
				addErr(err)
			}
		}
		idx := make([]int, len(rows))
		for i := range idx {
			idx[i] = i
		}
		stableSort(idx, func(a, c int) bool {
			for k, s := range view.Sort {
				x, y := keys[a][k], keys[c][k]
				if x == nil || y == nil { // empty values last regardless of direction
					if x == nil && y == nil {
						continue
					}
					return y == nil
				}
				cmp, _ := compare(x, y)
				if cmp == 0 {
					continue
				}
				if s.Direction == "DESC" {
					return cmp > 0
				}
				return cmp < 0
			}
			return false
		})
		sorted := make([]*row, len(rows))
		for i, k := range idx {
			sorted[i] = rows[k]
		}
		rows = sorted
	}

	res.Total = len(rows)
	if view.Limit > 0 && len(rows) > view.Limit {
		rows = rows[:view.Limit]
	}
	res.Shown = len(rows)

	for _, id := range order {
		c := Column{ID: id, Name: b.DisplayName(id), Summary: view.Summaries[id]}
		if c.Summary == "" && !strings.Contains(id, ".") {
			c.Summary = view.Summaries["note."+id]
		}
		if c.Summary == "" && strings.HasPrefix(id, "note.") {
			c.Summary = view.Summaries[strings.TrimPrefix(id, "note.")]
		}
		res.Columns = append(res.Columns, c)
	}

	makeRow := func(e *evaluator, r *row, errs *[]error) (Row, []any) {
		out := Row{Path: v.Abs(r.f.Rel), Rel: r.f.Rel}
		vals := make([]any, len(order))
		out.Cells = make([]Cell, 0, len(order))
		for i, id := range order {
			vals[i] = value(e, r, id, errs)
			out.Cells = append(out.Cells, e.cell(vals[i]))
		}
		if imageProp != "" {
			out.Image = e.imagePath(value(e, r, imageProp, errs), r.f)
		}
		if coordProp != "" || latProp != "" {
			var lat, lng float64
			var ok1, ok2 bool
			if coordProp != "" {
				lat, lng, ok1 = coords(value(e, r, coordProp, errs))
				ok2 = ok1
			} else {
				lat, ok1 = toNumber(value(e, r, latProp, errs))
				lng, ok2 = toNumber(value(e, r, lngProp, errs))
			}
			if ok1 && ok2 && !math.IsNaN(lat) && !math.IsNaN(lng) {
				out.Lat, out.Lng = &lat, &lng
			}
		}
		if markerProp != "" {
			out.Marker = toString(value(e, r, markerProp, errs))
		}
		return out, vals
	}

	// Evaluate the group key (if any) and the cells of every shown row.
	type built struct {
		key  any
		vals []any
		errs []error
	}
	out := make([]built, len(rows))
	rowsOut := make([]Row, len(rows))
	pool.run(len(rows), func(e *evaluator, i int) {
		o := &out[i]
		if view.GroupBy != nil {
			o.key = value(e, rows[i], view.GroupBy.Property, &o.errs)
		}
		rowsOut[i], o.vals = makeRow(e, rows[i], &o.errs)
	})
	e = pool.evs[0]

	type groupAcc struct {
		key    any
		rows   []Row
		values [][]any
	}
	var groups []*groupAcc
	all := make([][]any, len(out))
	for i := range out {
		all[i] = out[i].vals
	}
	if view.GroupBy != nil {
		byKey := map[string]*groupAcc{}
		member := make([]*groupAcc, len(out))
		count := map[*groupAcc]int{}
		for i := range out {
			o := &out[i]
			for _, err := range o.errs {
				addErr(err)
			}
			k := o.key
			ks := typeName(k) + "\x00" + toString(k)
			if l, ok := k.([]any); ok && len(l) == 0 {
				k, ks = nil, "null\x00"
			}
			g := byKey[ks]
			if g == nil {
				g = &groupAcc{key: k}
				byKey[ks] = g
				groups = append(groups, g)
			}
			member[i] = g
			count[g]++
		}
		for _, g := range groups {
			g.rows = make([]Row, 0, count[g])
			g.values = make([][]any, 0, count[g])
		}
		for i, g := range member {
			g.rows = append(g.rows, rowsOut[i])
			g.values = append(g.values, out[i].vals)
		}
		sort.SliceStable(groups, func(i, j int) bool {
			x, y := groups[i].key, groups[j].key
			if x == nil || y == nil {
				return y == nil && x != nil
			}
			c, _ := compare(x, y)
			if view.GroupBy.Direction == "DESC" {
				return c > 0
			}
			return c < 0
		})
	} else {
		g := &groupAcc{}
		for i := range out {
			for _, err := range out[i].errs {
				addErr(err)
			}
		}
		if len(rowsOut) > 0 {
			g.rows, g.values = rowsOut, all
		}
		groups = []*groupAcc{g}
	}

	hasSummaries := false
	for _, c := range res.Columns {
		if c.Summary != "" {
			hasSummaries = true
		}
	}
	summarize := func(values [][]any) []Cell {
		if !hasSummaries {
			return nil
		}
		out := make([]Cell, len(order))
		for i, c := range res.Columns {
			if c.Summary == "" {
				continue
			}
			col := make([]any, len(values))
			for j, vals := range values {
				col[j] = vals[i]
			}
			val, err := e.summary(c.Summary, col, b.Summaries)
			if err != nil {
				out[i] = Cell{Text: "error", Kind: "error"}
				addErr(fmt.Errorf("summary %s: %w", c.Summary, err))
				continue
			}
			out[i] = e.cell(val)
		}
		return out
	}

	for _, g := range groups {
		grp := Group{Key: toString(g.key), HasKey: view.GroupBy != nil, Rows: g.rows}
		if grp.Rows == nil {
			grp.Rows = []Row{}
		}
		if view.GroupBy != nil {
			grp.Summaries = summarize(g.values)
			if g.key == nil {
				grp.Key = "(empty)"
			}
		}
		res.Groups = append(res.Groups, grp)
	}
	res.Summaries = summarize(all)
	return res
}

// pool runs per-row work on several goroutines, each with its own
// evaluator. Small inputs run on the calling goroutine.
type pool struct {
	evs []*evaluator
}

func newPool(n int, mk func() *evaluator) *pool {
	workers := max(1, min(runtime.GOMAXPROCS(0), n/128))
	p := &pool{evs: make([]*evaluator, workers)}
	for i := range p.evs {
		p.evs[i] = mk()
	}
	return p
}

// run calls fn(e, i) for every i in [0, n). A panic in fn is re-raised on
// the calling goroutine.
func (p *pool) run(n int, fn func(e *evaluator, i int)) {
	workers := min(len(p.evs), max(1, n/64))
	if workers <= 1 {
		for i := range n {
			fn(p.evs[0], i)
		}
		return
	}
	const chunk = 64
	var next atomic.Int64
	var wg sync.WaitGroup
	var panicked atomic.Value
	for w := range workers {
		wg.Go(func() {
			defer func() {
				if x := recover(); x != nil {
					panicked.CompareAndSwap(nil, &x)
				}
			}()
			for {
				lo := int(next.Add(chunk)) - chunk
				if lo >= n {
					return
				}
				for i := lo; i < min(n, lo+chunk); i++ {
					fn(p.evs[w], i)
				}
			}
		})
	}
	wg.Wait()
	if x := panicked.Load(); x != nil {
		panic(*x.(*any))
	}
}

func coords(v any) (float64, float64, bool) {
	switch x := v.(type) {
	case []any:
		if len(x) >= 2 {
			a, ok1 := toNumber(x[0])
			b, ok2 := toNumber(x[1])
			return a, b, ok1 && ok2
		}
	case string:
		parts := strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
		if len(parts) >= 2 {
			a, ok1 := toNumber(parts[0])
			b, ok2 := toNumber(parts[1])
			return a, b, ok1 && ok2
		}
	}
	return 0, 0, false
}

func (e *evaluator) imagePath(v any, from *vault.File) string {
	var target string
	switch x := v.(type) {
	case Image:
		target = x.Src
	case Link:
		if x.File != nil {
			return e.v.Abs(x.File.Rel)
		}
		target = x.Target
	case FileVal:
		if x.F != nil {
			return e.v.Abs(x.F.Rel)
		}
	case string:
		target = linkTarget(x)
	case []any:
		if len(x) > 0 {
			return e.imagePath(x[0], from)
		}
	}
	if target == "" {
		return ""
	}
	if strings.Contains(target, "://") {
		return target
	}
	if f := e.v.Resolve(target, from.Rel); f != nil {
		return e.v.Abs(f.Rel)
	}
	return ""
}

func (e *evaluator) cell(v any) Cell {
	switch x := v.(type) {
	case nil:
		return Cell{Kind: "empty"}
	case bool:
		if x {
			return Cell{Text: "☑", Kind: "bool"}
		}
		return Cell{Text: "☐", Kind: "bool"}
	case float64:
		return Cell{Text: fmtNumber(x), Kind: "number"}
	case Date:
		return Cell{Text: toString(x), Kind: "date"}
	case Link:
		c := Cell{Text: toString(x), Kind: "link"}
		if x.File != nil {
			c.Link = e.v.Abs(x.File.Rel)
		}
		return c
	case FileVal:
		c := Cell{Text: toString(x), Kind: "link"}
		if x.F != nil {
			c.Link = e.v.Abs(x.F.Rel)
		}
		return c
	case Image:
		return Cell{Text: path.Base(x.Src), Kind: "image"}
	case []any:
		c := Cell{Text: toString(x), Kind: "list"}
		allTags := len(x) > 0
		for _, y := range x {
			if s, ok := y.(string); !ok || !strings.HasPrefix(s, "#") {
				allTags = false
			}
			if c.Link == "" {
				if f := fileOf(y); f != nil {
					c.Link = e.v.Abs(f.Rel)
				}
			}
		}
		if allTags {
			c.Kind = "tag"
		}
		return c
	case string:
		if strings.HasPrefix(x, "#") && !strings.Contains(x, " ") && len(x) > 1 {
			return Cell{Text: x, Kind: "tag"}
		}
	}
	return Cell{Text: toString(v)}
}

// summary computes a built-in or custom summary over column values.
func (e *evaluator) summary(name string, col []any, custom map[string]string) (any, error) {
	nonEmpty := make([]any, 0, len(col))
	for _, v := range col {
		if !isEmpty(v) {
			nonEmpty = append(nonEmpty, v)
		}
	}
	nums := func() []float64 {
		var out []float64
		for _, v := range nonEmpty {
			if _, isDate := v.(Date); isDate {
				continue
			}
			if f, ok := toNumber(v); ok && !math.IsNaN(f) {
				out = append(out, f)
			}
		}
		return out
	}
	dates := func() []Date {
		var out []Date
		for _, v := range nonEmpty {
			switch d := v.(type) {
			case Date:
				out = append(out, d)
			case string:
				if pd, ok := parseDate(d); ok {
					out = append(out, pd)
				}
			}
		}
		return out
	}
	switch name {
	case "Average", "Sum", "Min", "Max", "Range", "Median", "Stddev":
		ns := nums()
		if len(ns) == 0 {
			if name == "Min" || name == "Max" || name == "Range" {
				if ds := dates(); len(ds) > 0 {
					return e.dateSummary(name, ds), nil
				}
			}
			return nil, nil
		}
		sort.Float64s(ns)
		sum := 0.0
		for _, f := range ns {
			sum += f
		}
		switch name {
		case "Sum":
			return round(sum), nil
		case "Average":
			return round(sum / float64(len(ns))), nil
		case "Min":
			return ns[0], nil
		case "Max":
			return ns[len(ns)-1], nil
		case "Range":
			return round(ns[len(ns)-1] - ns[0]), nil
		case "Median":
			m := len(ns) / 2
			if len(ns)%2 == 1 {
				return ns[m], nil
			}
			return round((ns[m-1] + ns[m]) / 2), nil
		case "Stddev":
			mean := sum / float64(len(ns))
			ss := 0.0
			for _, f := range ns {
				ss += (f - mean) * (f - mean)
			}
			return round(math.Sqrt(ss / float64(len(ns)))), nil
		}
	case "Earliest", "Latest":
		ds := dates()
		if len(ds) == 0 {
			return nil, nil
		}
		if name == "Earliest" {
			return e.dateSummary("Min", ds), nil
		}
		return e.dateSummary("Max", ds), nil
	case "Checked", "Unchecked":
		n := 0
		for _, v := range col {
			if b, ok := v.(bool); ok && b == (name == "Checked") {
				n++
			}
		}
		return float64(n), nil
	case "Empty":
		return float64(len(col) - len(nonEmpty)), nil
	case "Filled":
		return float64(len(nonEmpty)), nil
	case "Unique":
		var seen []any
		for _, v := range nonEmpty {
			dup := false
			for _, s := range seen {
				if equal(s, v) {
					dup = true
					break
				}
			}
			if !dup {
				seen = append(seen, v)
			}
		}
		return float64(len(seen)), nil
	}
	src, ok := custom[name]
	if !ok {
		return nil, fmt.Errorf("unknown summary %q", name)
	}
	n, err := Parse(src)
	if err != nil {
		return nil, err
	}
	vals := append([]any{}, col...)
	dummy := e.rowOf(&vault.File{})
	return e.Eval(n, scope{r: dummy, vars: map[string]any{"values": vals}})
}

func (e *evaluator) dateSummary(name string, ds []Date) any {
	sort.Slice(ds, func(i, j int) bool { return ds[i].T.Before(ds[j].T) })
	switch name {
	case "Min":
		return ds[0]
	case "Max":
		return ds[len(ds)-1]
	}
	return Duration{Ms: float64(ds[len(ds)-1].T.Sub(ds[0].T).Milliseconds())}
}

func round(f float64) float64 { return math.Round(f*1e6) / 1e6 }
