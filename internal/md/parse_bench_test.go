package md

import (
	"testing"

	"brain/internal/benchdata"
)

func BenchmarkParse(b *testing.B) {
	text := benchdata.Note(42, 1000)
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	for b.Loop() {
		Parse(text)
	}
}
