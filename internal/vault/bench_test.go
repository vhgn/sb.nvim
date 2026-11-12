package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"brain/internal/benchdata"
)

var benchDirs = map[int]string{}

func TestMain(m *testing.M) {
	code := m.Run()
	for _, d := range benchDirs {
		os.RemoveAll(d)
	}
	os.Exit(code)
}

// benchVault returns a directory holding a synthetic n-note vault, shared by
// all benchmarks of this process.
func benchVault(b *testing.B, n int) string {
	b.Helper()
	if d, ok := benchDirs[n]; ok {
		return d
	}
	d, err := os.MkdirTemp("", "vault-bench-")
	if err != nil {
		b.Fatal(err)
	}
	if err := benchdata.WriteRich(d, n); err != nil {
		b.Fatal(err)
	}
	benchDirs[n] = d
	return d
}

var benchSizes = []int{1000, 10000}

func sizeName(n int) string { return fmt.Sprintf("%dk", n/1000) }

// Cold start as the daemon does it: New, LoadCache, Scan.
func BenchmarkColdStart(b *testing.B) {
	for _, n := range benchSizes {
		dir := benchVault(b, n)
		b.Run(sizeName(n)+"/nocache", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v := New(dir, "")
				v.LoadCache()
				v.Scan()
			}
		})
		cache := b.TempDir()
		v := New(dir, cache)
		v.Scan()
		if err := v.SaveCache(); err != nil {
			b.Fatal(err)
		}
		b.Run(sizeName(n)+"/warmcache", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v := New(dir, cache)
				v.LoadCache()
				v.Scan()
			}
		})
	}
}

func BenchmarkSaveCache(b *testing.B) {
	for _, n := range benchSizes {
		dir := benchVault(b, n)
		cache := b.TempDir()
		v := New(dir, cache)
		v.Scan()
		b.Run(sizeName(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v.mu.Lock()
				v.dirty = true
				v.mu.Unlock()
				if err := v.SaveCache(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Steady state: the stat-only walk ScanIfOlder does when nothing changed.
func BenchmarkScanUnchanged(b *testing.B) {
	for _, n := range benchSizes {
		dir := benchVault(b, n)
		v := New(dir, "")
		v.Scan()
		b.Run(sizeName(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if v.Scan() {
					b.Fatal("unexpected change")
				}
			}
		})
	}
}

// Touch after saving one note.
func BenchmarkTouch(b *testing.B) {
	for _, n := range benchSizes {
		dir := benchVault(b, n)
		v := New(dir, "")
		v.Scan()
		p := filepath.Join(dir, filepath.FromSlash(benchdata.Name(n/2))+".md")
		b.Run(sizeName(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v.Touch(p)
			}
		})
	}
}

func BenchmarkParseNote(b *testing.B) {
	text := benchdata.RichNote(3, 1000)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	for b.Loop() {
		f := &File{Rel: "a.md"}
		ParseNote(f, text)
	}
}

func BenchmarkResolve(b *testing.B) {
	dir := benchVault(b, 1000)
	v := New(dir, "")
	v.Scan()
	targets := []string{"note-10", "projects/note-200", "areas/note-33#Section 1", "missing-thing", "note-499.md"}
	b.ReportAllocs()
	for b.Loop() {
		for _, t := range targets {
			v.Resolve(t, "notes/daily/note-3.md")
		}
	}
}
