package lsp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"brain/internal/benchdata"
)

var sizes = []int{100, 1000, 5000}

type benchEnv struct {
	s    *Server
	root string
	uri  string // the "current" document
	text string
}

func newBenchEnv(b *testing.B, n int) *benchEnv {
	b.Helper()
	root := b.TempDir()
	if err := benchdata.Write(root, n); err != nil {
		b.Fatal(err)
	}
	s := NewServer(strings.NewReader(""), nil)
	call(b, s, "initialize", map[string]any{"rootUri": pathToURI(root)})
	call(b, s, "initialized", map[string]any{})
	cur := n / 2
	p := filepath.Join(root, filepath.FromSlash(benchdata.Name(cur))+".md")
	e := &benchEnv{s: s, root: root, uri: pathToURI(p), text: benchdata.Note(cur, n)}
	call(b, s, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": e.uri, "text": e.text, "languageId": "markdown", "version": 1}})
	return e
}

// call dispatches a request and JSON-encodes the result, like the real reply path.
func call(b *testing.B, s *Server, method string, params any) int {
	raw, err := json.Marshal(params)
	if err != nil {
		b.Fatal(err)
	}
	res, rerr := s.dispatch(method, raw)
	if rerr != nil {
		b.Fatalf("%s: %s", method, rerr.Message)
	}
	out, err := json.Marshal(res)
	if err != nil {
		b.Fatal(err)
	}
	return len(out)
}

func posParams(uri string, line, char int) map[string]any {
	return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": line, "character": char}}
}

// findLink returns the position just inside the first occurrence of substr on a line.
func (e *benchEnv) find(b *testing.B, substr string) (int, int) {
	for i, l := range strings.Split(e.text, "\n") {
		if j := strings.Index(l, substr); j >= 0 {
			return i, j
		}
	}
	b.Fatalf("%q not found", substr)
	return 0, 0
}

func each(b *testing.B, f func(b *testing.B, e *benchEnv)) {
	for _, n := range sizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			e := newBenchEnv(b, n)
			b.ReportAllocs()
			f(b, e)
		})
	}
}

func BenchmarkInitialize(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			root := b.TempDir()
			if err := benchdata.Write(root, n); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				s := NewServer(strings.NewReader(""), nil)
				call(b, s, "initialize", map[string]any{"rootUri": pathToURI(root)})
			}
		})
	}
}

// BenchmarkCompletionWiki: cursor right after "[[" on a fresh line.
func BenchmarkCompletionWiki(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		text := e.text + "\n[[\n"
		line := strings.Count(text, "\n") - 1
		call(b, e.s, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": e.uri, "version": 2}, "contentChanges": []any{map[string]any{"text": text}}})
		p := posParams(e.uri, line, 2)
		var size int
		for b.Loop() {
			size = call(b, e.s, "textDocument/completion", p)
		}
		b.ReportMetric(float64(size), "resp-bytes")
	})
}

// BenchmarkCompletionAt: cursor after "@no".
func BenchmarkCompletionAt(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		text := e.text + "\n@no\n"
		line := strings.Count(text, "\n") - 1
		call(b, e.s, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": e.uri, "version": 2}, "contentChanges": []any{map[string]any{"text": text}}})
		p := posParams(e.uri, line, 3)
		for b.Loop() {
			call(b, e.s, "textDocument/completion", p)
		}
	})
}

// BenchmarkTypingCompletion simulates a keystroke: full-text didChange followed by a [[ completion.
func BenchmarkTypingCompletion(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		text := e.text + "\n[[\n"
		line := strings.Count(text, "\n") - 1
		change := map[string]any{"textDocument": map[string]any{"uri": e.uri, "version": 2}, "contentChanges": []any{map[string]any{"text": text}}}
		p := posParams(e.uri, line, 2)
		for b.Loop() {
			call(b, e.s, "textDocument/didChange", change)
			call(b, e.s, "textDocument/completion", p)
		}
	})
}

func BenchmarkCompletionHeading(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		name := benchdata.Name(3)
		text := e.text + "\n[[" + name + "#\n"
		line := strings.Count(text, "\n") - 1
		call(b, e.s, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": e.uri, "version": 2}, "contentChanges": []any{map[string]any{"text": text}}})
		p := posParams(e.uri, line, len(name)+3)
		for b.Loop() {
			call(b, e.s, "textDocument/completion", p)
		}
	})
}

func BenchmarkDefinition(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		l, c := e.find(b, "[[")
		p := posParams(e.uri, l, c+3)
		for b.Loop() {
			call(b, e.s, "textDocument/definition", p)
		}
	})
}

func BenchmarkHover(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		l, c := e.find(b, "[[")
		p := posParams(e.uri, l, c+3)
		for b.Loop() {
			call(b, e.s, "textDocument/hover", p)
		}
	})
}

// BenchmarkReferences: backlinks to the current note (cursor on plain text).
func BenchmarkReferences(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		l, _ := e.find(b, "Intro paragraph")
		p := posParams(e.uri, l, 0)
		p["context"] = map[string]any{"includeDeclaration": false}
		for b.Loop() {
			call(b, e.s, "textDocument/references", p)
		}
	})
}

// BenchmarkReferencesHeading: links to a heading of the current note.
func BenchmarkReferencesHeading(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		l, _ := e.find(b, "## Section 2")
		p := posParams(e.uri, l, 4)
		p["context"] = map[string]any{"includeDeclaration": true}
		for b.Loop() {
			call(b, e.s, "textDocument/references", p)
		}
	})
}

func BenchmarkDocumentSymbol(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		p := map[string]any{"textDocument": map[string]any{"uri": e.uri}}
		for b.Loop() {
			call(b, e.s, "textDocument/documentSymbol", p)
		}
	})
}

func BenchmarkWorkspaceSymbol(b *testing.B) {
	each(b, func(b *testing.B, e *benchEnv) {
		p := map[string]any{"query": "nt42sec"}
		for b.Loop() {
			call(b, e.s, "workspace/symbol", p)
		}
	})
}
