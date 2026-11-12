package bases

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"brain/internal/vault"
)

// Value kinds beyond the JSON-ish nil/bool/float64/string/[]any/map[string]any.
type (
	Date struct {
		T       time.Time
		HasTime bool
	}
	// Duration is a calendar-aware span, e.g. "1 month 2 days".
	Duration struct {
		Years, Months, Days int
		Ms                  float64
	}
	Link struct {
		Target  string // as written
		Display string
		File    *vault.File // resolved, may be nil
	}
	FileVal struct{ F *vault.File }
	// RowVal is a row context: `this`, or the implicit current row.
	RowVal struct{ r *row }
	Image  struct{ Src string }
	Icon   struct{ Name string }
	HTML   struct{ S string }
	Regex  struct {
		Re     *regexp.Regexp
		Global bool // the g flag: replace() replaces every match
	}
	formNS  struct{ r *row }
	noteMap struct{ r *row }
)

var (
	dateRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}([T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?)?$`)
	wikiOnlyRe = regexp.MustCompile(`^!?\[\[([^\[\]|#]*)(#[^\[\]|]*)?(\|([^\[\]]*))?\]\]$`)
)

func parseDate(s string) (Date, bool) {
	s = strings.TrimSpace(s)
	if !matchDate(s) {
		return Date{}, false
	}
	if len(s) == 10 {
		t, err := time.ParseInLocation("2006-01-02", s, time.Local)
		return Date{T: t}, err == nil
	}
	s2 := strings.Replace(s, " ", "T", 1)
	// Without a zone only the zone-less layouts can match: skip the others
	// (whose failures allocate errors).
	layouts := []string{time.RFC3339Nano, "2006-01-02T15:04:05Z0700", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02T15:04:05.999999999"}
	if !hasZone(s2) {
		layouts = layouts[2:]
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s2, time.Local); err == nil {
			return Date{T: t, HasTime: true}, true
		}
	}
	return Date{}, false
}

// hasZone reports whether a string matched by matchDate ends with a zone.
func hasZone(s string) bool {
	return len(s) > 16 && (s[len(s)-1] == 'Z' || strings.ContainsAny(s[16:], "+-"))
}

// matchDate is dateRe.MatchString without the regexp engine:
// ^\d{4}-\d{2}-\d{2}([T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?)?$
func matchDate(s string) bool {
	digits := func(i, n int) bool {
		if i+n > len(s) {
			return false
		}
		for k := i; k < i+n; k++ {
			if !isDigit(s[k]) {
				return false
			}
		}
		return true
	}
	if len(s) < 10 || !digits(0, 4) || s[4] != '-' || !digits(5, 2) || s[7] != '-' || !digits(8, 2) {
		return false
	}
	i := 10
	if i == len(s) {
		return true
	}
	if s[i] != 'T' && s[i] != ' ' || !digits(i+1, 2) || i+3 >= len(s) || s[i+3] != ':' || !digits(i+4, 2) {
		return false
	}
	i += 6
	if i < len(s) && s[i] == ':' {
		if !digits(i+1, 2) {
			return false
		}
		i += 3
		if i < len(s) && s[i] == '.' {
			j := i + 1
			for j < len(s) && isDigit(s[j]) {
				j++
			}
			if j == i+1 {
				return false
			}
			i = j
		}
	}
	if i == len(s) {
		return true
	}
	switch s[i] {
	case 'Z':
		return i+1 == len(s)
	case '+', '-':
		if !digits(i+1, 2) {
			return false
		}
		i += 3
		if i < len(s) && s[i] == ':' {
			i++
		}
		return digits(i, 2) && i+2 == len(s)
	}
	return false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// wikiOnly is wikiOnlyRe.FindStringSubmatch(s) without the regexp engine:
// ^!?\[\[([^\[\]|#]*)(#[^\[\]|]*)?(\|([^\[\]]*))?\]\]$
// The character classes leave no choices, so a single scan decides.
func wikiOnly(s string) []string {
	if len(s) < 4 || !(s[0] == '[' || s[0] == '!') || s[len(s)-1] != ']' {
		return nil
	}
	i := 0
	if s[0] == '!' {
		i = 1
	}
	if !strings.HasPrefix(s[i:], "[[") {
		return nil
	}
	i += 2
	scan := func(stop string) int {
		j := i
		for j < len(s) && strings.IndexByte(stop, s[j]) < 0 {
			j++
		}
		return j
	}
	j := scan("[]|#")
	m := []string{"", s[i:j], "", "", ""}
	i = j
	if i < len(s) && s[i] == '#' {
		i++
		j = scan("[]|")
		m[2] = s[i-1 : j]
		i = j
	}
	if i < len(s) && s[i] == '|' {
		i++
		j = scan("[]")
		m[3], m[4] = s[i-1:j], s[i:j]
		i = j
	}
	if s[i:] != "]]" {
		return nil
	}
	m[0] = s
	return m
}

// propValue converts a raw front matter value into a typed value.
func (e *evaluator) propValue(v any, from *vault.File) any {
	switch x := v.(type) {
	case string:
		if d, ok := parseDate(x); ok {
			return d
		}
		if m := wikiOnly(strings.TrimSpace(x)); m != nil {
			return e.link(m[1], m[4], from)
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = e.propValue(y, from)
		}
		return out
	}
	return v
}

func (e *evaluator) link(target, display string, from *vault.File) Link {
	l := Link{Target: target, Display: display}
	rel := ""
	if from != nil {
		rel = from.Rel
	}
	l.File = e.v.Resolve(target, rel)
	return l
}

var unitRe = regexp.MustCompile(`(?i)^\s*([+-]?\d+(?:\.\d+)?)\s*([a-zA-Z]+)`)

// parseDuration parses "1d", "2 weeks", "1 year 3 months", "-1h".
func parseDuration(s string) (Duration, bool) {
	var d Duration
	rest := strings.TrimSpace(s)
	if rest == "" {
		return d, false
	}
	for rest != "" {
		m := unitRe.FindStringSubmatch(rest)
		if m == nil {
			return d, false
		}
		n, _ := strconv.ParseFloat(m[1], 64)
		u := m[2]
		// addDays keeps fractional days ("1.5d") as milliseconds.
		addDays := func(f float64) {
			whole := math.Trunc(f)
			d.Days += int(whole)
			d.Ms += (f - whole) * 86400e3
		}
		switch {
		case u == "y" || strings.HasPrefix(strings.ToLower(u), "year"):
			d.Years += int(n)
		case u == "M" || strings.HasPrefix(strings.ToLower(u), "month"):
			d.Months += int(n)
		case u == "w" || strings.HasPrefix(strings.ToLower(u), "week"):
			addDays(n * 7)
		case u == "d" || strings.HasPrefix(strings.ToLower(u), "day"):
			addDays(n)
		case u == "h" || strings.HasPrefix(strings.ToLower(u), "hour"):
			d.Ms += n * 3600e3
		case u == "m" || strings.HasPrefix(strings.ToLower(u), "min"):
			d.Ms += n * 60e3
		case u == "s" || strings.HasPrefix(strings.ToLower(u), "sec"):
			d.Ms += n * 1e3
		case u == "ms" || strings.HasPrefix(strings.ToLower(u), "milli"):
			d.Ms += n
		default:
			return d, false
		}
		rest = strings.TrimLeft(rest[len(m[0]):], " ,")
	}
	return d, true
}

func (d Duration) neg() Duration { return Duration{-d.Years, -d.Months, -d.Days, -d.Ms} }
func (d Duration) totalMs() float64 {
	return d.Ms + float64(d.Days)*86400e3 + float64(d.Months)*30*86400e3 + float64(d.Years)*365*86400e3
}

// scale multiplies a duration by k (duration('5h') * 2).
func (d Duration) scale(k float64) Duration {
	if k == math.Trunc(k) {
		ki := int(k)
		return Duration{d.Years * ki, d.Months * ki, d.Days * ki, d.Ms * k}
	}
	return Duration{int(math.Round(float64(d.Years) * k)), int(math.Round(float64(d.Months) * k)), 0, (d.Ms + float64(d.Days)*86400e3) * k}
}

func (d Date) add(x Duration) Date {
	t := d.T
	if x.Years != 0 || x.Months != 0 {
		// Like moment.js, month arithmetic clamps to the last day of the
		// target month: Jan 31 + 1 month is Feb 29 (not Mar 2).
		y, m, day := t.Date()
		first := time.Date(y+x.Years, m+time.Month(x.Months), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
		last := first.AddDate(0, 1, -1).Day()
		t = first.AddDate(0, 0, min(day, last)-1)
	}
	t = t.AddDate(0, 0, x.Days).Add(time.Duration(x.Ms * float64(time.Millisecond)))
	return Date{T: t, HasTime: d.HasTime || x.Ms != 0}
}

// truthy follows JavaScript, except that empty lists and objects are false.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	case Link:
		return x.Target != ""
	case FileVal:
		return x.F != nil
	}
	return true
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	case float64:
		return math.IsNaN(x)
	}
	return false
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "list"
	case map[string]any:
		return "object"
	case Date:
		return "date"
	case Duration:
		return "duration"
	case Link:
		return "link"
	case FileVal:
		return "file"
	case Regex:
		return "regexp"
	}
	return "object"
}

func toNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		t := strings.TrimSpace(x)
		// Like JavaScript's Number(): "inf" and "nan" are not numbers,
		// only "Infinity" is.
		if strings.ContainsAny(t, "nN") && strings.TrimLeft(t, "+-") != "Infinity" {
			return 0, false
		}
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	case Date:
		return float64(x.T.UnixMilli()), true
	case Duration:
		return x.totalMs(), true
	case nil:
		return 0, false
	}
	return 0, false
}

func fmtNumber(f float64) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 0) {
		if f < 0 {
			return "-Infinity"
		}
		return "Infinity"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// toString renders a value for display and string concatenation.
func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return fmtNumber(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, y := range x {
			parts[i] = toString(y)
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ": " + toString(x[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case Date:
		if x.HasTime {
			return x.T.Format("2006-01-02 15:04")
		}
		return x.T.Format("2006-01-02")
	case Duration:
		return fmtDuration(x)
	case Link:
		if x.Display != "" {
			return x.Display
		}
		if x.File != nil {
			return x.File.Basename()
		}
		return x.Target
	case FileVal:
		if x.F == nil {
			return ""
		}
		return x.F.Basename()
	case Image:
		return "🖼 " + x.Src
	case Icon:
		return x.Name
	case HTML:
		return x.S
	case Regex:
		return "/" + x.Re.String() + "/"
	case RowVal:
		return x.r.f.Basename()
	}
	return fmt.Sprint(v)
}

func fmtDuration(d Duration) string {
	var parts []string
	add := func(n int, u string) {
		if n != 0 {
			s := u
			if n != 1 && n != -1 {
				s += "s"
			}
			parts = append(parts, fmt.Sprintf("%d %s", n, s))
		}
	}
	add(d.Years, "year")
	add(d.Months, "month")
	ms := d.Ms
	days := d.Days + int(ms/86400e3)
	ms = math.Mod(ms, 86400e3)
	add(days, "day")
	add(int(ms/3600e3), "hour")
	ms = math.Mod(ms, 3600e3)
	add(int(ms/60e3), "minute")
	ms = math.Mod(ms, 60e3)
	add(int(ms/1e3), "second")
	if len(parts) == 0 {
		return "0 seconds"
	}
	return strings.Join(parts, " ")
}

// sameFile compares link-ish values by the file they point to.
func fileOf(v any) *vault.File {
	switch x := v.(type) {
	case Link:
		return x.File
	case FileVal:
		return x.F
	case RowVal:
		return x.r.f
	}
	return nil
}

func equal(a, b any) bool {
	if fa, fb := fileOf(a), fileOf(b); fa != nil || fb != nil {
		if fa != nil && fb != nil {
			return fa == fb
		}
		// link vs string: compare by name
		return strings.EqualFold(linkName(a), linkName(b))
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case Date:
		switch y := b.(type) {
		case Date:
			return x.T.Equal(y.T)
		case string:
			if d, ok := parseDate(y); ok {
				return x.T.Equal(d.T)
			}
		}
		return false
	case string:
		switch y := b.(type) {
		case string:
			return x == y
		case Date:
			return equal(b, a)
		case float64:
			n, ok := toNumber(x)
			return ok && n == y
		case bool:
			return x == strconv.FormatBool(y)
		}
		return false
	case float64:
		switch y := b.(type) {
		case float64:
			return x == y
		case string:
			return equal(b, a)
		}
		return false
	case bool:
		switch y := b.(type) {
		case bool:
			return x == y
		case string:
			return equal(b, a)
		}
		return false
	}
	if b == nil {
		return false
	}
	return toString(a) == toString(b) && typeName(a) == typeName(b)
}

func linkName(v any) string {
	switch x := v.(type) {
	case Link:
		if x.File != nil {
			return x.File.Basename()
		}
		t := x.Target
		if i := strings.LastIndexByte(t, '/'); i >= 0 {
			t = t[i+1:]
		}
		return strings.TrimSuffix(t, ".md")
	case FileVal:
		if x.F != nil {
			return x.F.Basename()
		}
	case RowVal:
		return x.r.f.Basename()
	case string:
		if m := wikiOnly(x); m != nil {
			x = m[1]
		}
		if i := strings.LastIndexByte(x, '/'); i >= 0 {
			x = x[i+1:]
		}
		return strings.TrimSuffix(x, ".md")
	}
	return toString(v)
}

// compare orders two values; ok is false when they are not comparable.
// nil sorts after everything.
func compare(a, b any) (int, bool) {
	if a == nil || b == nil {
		switch {
		case a == nil && b == nil:
			return 0, true
		case a == nil:
			return 1, false
		default:
			return -1, false
		}
	}
	if da, ok := a.(Date); ok {
		if s, ok := b.(string); ok {
			if d, ok := parseDate(s); ok {
				b = d
			}
		}
		if db, ok := b.(Date); ok {
			return da.T.Compare(db.T), true
		}
	}
	if _, ok := b.(Date); ok {
		if s, ok := a.(string); ok {
			if d, ok := parseDate(s); ok {
				return compare(d, b)
			}
		}
	}
	_, an := a.(float64)
	_, bn := b.(float64)
	_, as := a.(string)
	_, bs := b.(string)
	if an || bn {
		x, ok1 := toNumber(a)
		y, ok2 := toNumber(b)
		if ok1 && ok2 {
			return cmpFloat(x, y), true
		}
	}
	if (as || bs) && !(an || bn) || (!an && !bn) {
		if da, ok := a.(Duration); ok {
			if db, ok := b.(Duration); ok {
				return cmpFloat(da.totalMs(), db.totalMs()), true
			}
		}
		if ba, ok := a.(bool); ok {
			if bb, ok := b.(bool); ok {
				x, y := 0, 0
				if ba {
					x = 1
				}
				if bb {
					y = 1
				}
				return x - y, true
			}
		}
		if la, ok := a.([]any); ok {
			if lb, ok := b.([]any); ok {
				for i := 0; i < len(la) && i < len(lb); i++ {
					if c, _ := compare(la[i], lb[i]); c != 0 {
						return c, true
					}
				}
				return len(la) - len(lb), true
			}
		}
		return naturalCompare(toString(a), toString(b)), true
	}
	return 0, false
}

func cmpFloat(x, y float64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// naturalCompare compares case-insensitively with digit runs compared numerically.
// It walks the strings rune by rune like comparing their []rune forms.
func naturalCompare(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, wa := decodeRune(a, i)
		cb, wb := decodeRune(b, j)
		if unicode.IsDigit(ca) && unicode.IsDigit(cb) {
			si := i
			for i < len(a) {
				c, w := decodeRune(a, i)
				if !unicode.IsDigit(c) {
					break
				}
				i += w
			}
			sj := j
			for j < len(b) {
				c, w := decodeRune(b, j)
				if !unicode.IsDigit(c) {
					break
				}
				j += w
			}
			// Digit runs are valid UTF-8, so these equal string([]rune(...)).
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) - len(nb)
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		la, lb := ca, cb
		if la >= utf8.RuneSelf || lb >= utf8.RuneSelf || la >= 'A' && la <= 'Z' || lb >= 'A' && lb <= 'Z' {
			la, lb = unicode.ToLower(ca), unicode.ToLower(cb)
		}
		if la != lb {
			if la < lb {
				return -1
			}
			return 1
		}
		i += wa
		j += wb
	}
	return utf8.RuneCountInString(a[i:]) - utf8.RuneCountInString(b[j:])
}

func decodeRune(s string, i int) (rune, int) {
	if c := s[i]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeRuneInString(s[i:])
}
