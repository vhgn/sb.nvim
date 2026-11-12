package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"brain/internal/benchdata"
	"brain/internal/tex"
	"brain/internal/vault"
)

// A base as embedded in notes: tag filter, formula, sort, limit.
const benchBase = `filters:
  and:
    - file.hasTag("bench")
    - 'status != "archived"'
formulas:
  score: 'priority * 2 + rating'
views:
  - type: table
    name: Top
    order: [file.name, status, priority, due, formula.score]
    sort:
      - property: priority
        direction: DESC
      - property: file.name
    limit: 50
`

// TestWriteBenchVault writes a vault for the Lua benchmarks
// (nvim/tests/bench): BRAIN_BENCH_VAULT=dir BRAIN_BENCH_N=1000.
func TestWriteBenchVault(t *testing.T) {
	dir := os.Getenv("BRAIN_BENCH_VAULT")
	if dir == "" {
		t.Skip("BRAIN_BENCH_VAULT not set")
	}
	n, _ := strconv.Atoi(os.Getenv("BRAIN_BENCH_N"))
	if n <= 0 {
		n = 1000
	}
	if err := benchdata.WriteRich(dir, n); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bench.base"), []byte(benchBase), 0o644); err != nil {
		t.Fatal(err)
	}
}

type benchEnv struct {
	s    *Server
	dir  string
	conn net.Conn
	r    *bufio.Reader
}

var (
	benchEnvs = map[int]*benchEnv{}
	benchDirs []string
)

func TestMain(m *testing.M) {
	code := m.Run()
	for _, e := range benchEnvs {
		e.conn.Close()
	}
	for _, d := range benchDirs {
		os.RemoveAll(d)
	}
	os.Exit(code)
}

// env serves an n-note vault over a real unix socket (like the editor talks to it).
func env(b *testing.B, n int) *benchEnv {
	b.Helper()
	if e, ok := benchEnvs[n]; ok {
		return e
	}
	dir, err := os.MkdirTemp("", "daemon-bench-")
	if err != nil {
		b.Fatal(err)
	}
	benchDirs = append(benchDirs, dir)
	root := filepath.Join(dir, "vault")
	if err := benchdata.WriteRich(root, n); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bench.base"), []byte(benchBase), 0o644); err != nil {
		b.Fatal(err)
	}
	s := &Server{root: root, cache: filepath.Join(dir, "cache")}
	s.v = vault.New(root, "")
	s.v.Scan()
	ready := make(chan struct{})
	close(ready)
	ln, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		b.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c, ready)
		}
	}()
	c, err := net.Dial("unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		b.Fatal(err)
	}
	e := &benchEnv{s: s, dir: root, conn: c, r: bufio.NewReaderSize(c, 1<<16)}
	benchEnvs[n] = e
	return e
}

var benchID atomic.Int64

// call sends one request and waits for its reply line.
func (e *benchEnv) call(b *testing.B, method string, params any) []byte {
	id := benchID.Add(1)
	p, _ := json.Marshal(params)
	line := fmt.Sprintf(`{"id":%d,"method":%q,"params":%s}`+"\n", id, method, p)
	if _, err := e.conn.Write([]byte(line)); err != nil {
		b.Fatal(err)
	}
	out, err := e.r.ReadBytes('\n')
	if err != nil {
		b.Fatal(err)
	}
	if len(out) > 9 && string(out[:9]) == `{"error":` {
		b.Fatalf("%s: %s", method, out)
	}
	return out
}

var benchInline = []string{
	`\alpha^2 + \beta_i`, `x \leq \sqrt{y}`, `\mathbb{R}^n`, `e^{i\pi} = -1`, `\frac{1}{2}`,
	`O(n \log n)`, `a_{ij}`, `\sum_k x_k`, `f'(x)`, `\vec{v} \cdot \vec{w}`,
}

// Round trips through the socket, including JSON on both ends.
func BenchmarkRoundTrip(b *testing.B) {
	e := env(b, 1000)
	b.Run("ping", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			e.call(b, "ping", map[string]any{})
		}
	})
	items := make([]string, 0, 50)
	for len(items) < 50 {
		items = append(items, benchInline...)
	}
	items = items[:50]
	b.Run("texUnicode50", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			e.call(b, "texUnicode", map[string]any{"items": items})
		}
	})
	from := filepath.Join(e.dir, benchdata.Name(10)+".md")
	b.Run("resolve", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			e.call(b, "resolve", map[string]any{"targets": []string{"note-3", "missing.png"}, "from": from})
		}
	})
	b.Run("tex/cached", func(b *testing.B) {
		p := map[string]any{"src": `\frac{a}{b} + \sqrt{x}`, "display": true, "fg": "#c0caf5", "size": 21,
			"palette": map[string]string{"red": "#ff0000", "green": "#00ff00", "blue": "#0000ff"}}
		e.call(b, "tex", p) // warm the png cache
		b.ReportAllocs()
		for b.Loop() {
			e.call(b, "tex", p)
		}
	})
}

func BenchmarkQuery(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		e := env(b, n)
		params := map[string]any{"path": filepath.Join(e.dir, "bench.base"), "this": filepath.Join(e.dir, benchdata.Name(n/2)+".md")}
		b.Run(fmt.Sprintf("%dk", n/1000), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				e.call(b, "query", params)
			}
		})
		// Eight bases embedded in one note: the editor asks for all of them
		// at once, after the vault went stale (>2s since the last scan).
		b.Run(fmt.Sprintf("%dk/8-concurrent-stale", n/1000), func(b *testing.B) {
			conns := make([]*benchEnv, 8)
			for i := range conns {
				c, err := net.Dial("unix", filepath.Join(filepath.Dir(e.dir), "s.sock"))
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				conns[i] = &benchEnv{conn: c, r: bufio.NewReaderSize(c, 1<<16)}
			}
			old := freshFor
			freshFor = 0 // every request finds the index stale
			defer func() { freshFor = old }()
			b.ReportAllocs()
			for b.Loop() {
				var wg sync.WaitGroup
				for _, c := range conns {
					wg.Add(1)
					go func() {
						defer wg.Done()
						c.call(b, "query", params)
					}()
				}
				wg.Wait()
			}
		})
	}
}

func BenchmarkRenderTeX(b *testing.B) {
	opt := tex.Options{Display: true, FG: "#c0caf5", Size: 21, Palette: map[string]string{"red": "#ff0000", "blue": "#0000ff"}}
	src := `x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`
	b.Run("cached", func(b *testing.B) {
		dir := b.TempDir()
		if _, err := RenderTeX(dir, src, opt); err != nil {
			b.Skip(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := RenderTeX(dir, src, opt); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("cold", func(b *testing.B) {
		dir := b.TempDir()
		i := 0
		b.ReportAllocs()
		for b.Loop() {
			i++
			if _, err := RenderTeX(dir, fmt.Sprintf(`%s + %d`, src, i), opt); err != nil {
				b.Skip(err)
			}
		}
	})
}
