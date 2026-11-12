package daemon

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"brain/internal/tex"
)

// A handler panic (here: a nil vault behind "status") becomes an error reply,
// and the same connection keeps serving afterwards.
func TestPanicInHandlerIsAnErrorReply(t *testing.T) {
	s := &Server{root: "/r", cache: t.TempDir()} // s.v == nil → status panics
	ready := make(chan struct{})
	close(ready)
	a, b := net.Pipe()
	defer a.Close()
	go s.serve(b, ready)

	r := bufio.NewReader(a)
	send := func(line string) map[string]any {
		t.Helper()
		a.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := a.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
		out, err := r.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := send(`{"id":1,"method":"status","params":{}}`)
	if m["id"] != float64(1) || !strings.Contains(m["error"].(string), "internal error in status") {
		t.Fatalf("want internal error reply, got %v", m)
	}
	m = send(`{"id":2,"method":"ping","params":{}}`)
	if m["id"] != float64(2) || m["error"] != nil {
		t.Fatalf("connection must survive the panic, got %v", m)
	}
}

// Many concurrent renders of one formula must all succeed and leave no temp files.
func TestRenderTeXConcurrentSameFormula(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make([]error, 16)
	pngs := make([]string, 16)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := RenderTeX(dir, `\frac{a}{b}`, tex.Options{Display: true})
			errs[i] = err
			if err == nil {
				pngs[i], _ = res["png"].(string)
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			if strings.Contains(err.Error(), "need rsvg-convert") {
				t.Skip("no rasterizer")
			}
			t.Fatalf("render %d: %v", i, err)
		}
		if pngs[i] != pngs[0] {
			t.Fatalf("render %d: %s != %s", i, pngs[i], pngs[0])
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "tex"))
	if len(entries) != 2 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("want exactly one png + one svg, got %v", names)
	}
}

func TestRenderTeXRejectsOversizedFormula(t *testing.T) {
	_, err := RenderTeX(t.TempDir(), strings.Repeat("x", 9000), tex.Options{Display: true})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want too large error, got %v", err)
	}
}
