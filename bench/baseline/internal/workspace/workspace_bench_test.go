package workspace

import (
	"fmt"
	"path/filepath"
	"testing"

	"brain/internal/benchdata"
)

var sizes = []int{100, 1000, 5000}

func diskWorkspace(b *testing.B, n int) string {
	b.Helper()
	dir := b.TempDir()
	if err := benchdata.Write(dir, n); err != nil {
		b.Fatal(err)
	}
	return dir
}

func memWorkspace(n int) *Workspace {
	w := New("/bench")
	for i := 0; i < n; i++ {
		w.Set("/bench/"+benchdata.Name(i)+".md", benchdata.Note(i, n), false)
	}
	return w
}

func BenchmarkScan(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			dir := diskWorkspace(b, n)
			b.ReportAllocs()
			for b.Loop() {
				if err := New(dir).Scan(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkResolveDoc resolves every link of one note (what hover/definition do per link).
func BenchmarkResolveDoc(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			w := memWorkspace(n)
			d := w.Get(filepath.FromSlash("/bench/" + benchdata.Name(n/2) + ".md"))
			b.ReportAllocs()
			for b.Loop() {
				for _, l := range d.Doc.Links {
					w.Resolve(d, l)
				}
			}
		})
	}
}

func BenchmarkBacklinks(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			w := memWorkspace(n)
			d := w.Get("/bench/" + benchdata.Name(n/2) + ".md")
			b.ReportAllocs()
			for b.Loop() {
				w.Backlinks(d, -1)
			}
		})
	}
}

func BenchmarkShortNameAll(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			w := memWorkspace(n)
			b.ReportAllocs()
			for b.Loop() {
				for _, d := range w.Docs() {
					w.ShortName(d)
				}
			}
		})
	}
}
