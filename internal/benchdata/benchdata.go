// Package benchdata generates deterministic synthetic note workspaces for
// benchmarks. It is not used by the server itself.
package benchdata

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

// Folders notes are spread over. Some base names repeat across folders so
// that short-name disambiguation and suffix resolution are exercised.
var folders = []string{"", "projects", "areas", "notes/daily", "notes/meetings", "archive/2024", "archive/2025", "people"}

// Name returns the workspace-relative name (no extension) of note i.
func Name(i int) string {
	dir := folders[i%len(folders)]
	base := fmt.Sprintf("note-%d", i/2) // every base name is shared by two notes
	if dir == "" {
		return base
	}
	return dir + "/" + base
}

// Note returns the markdown text of note i in a workspace of n notes.
func Note(i, n int) string {
	r := rand.New(rand.NewSource(int64(i)))
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: \"Note %d\"\ntags: [bench, n%d]\n---\n", i, i%7)
	fmt.Fprintf(&b, "# Note %d\n\nIntro paragraph for note %d with some prose that does not link anywhere.\n\n", i, i)
	for s := 0; s < 6; s++ {
		fmt.Fprintf(&b, "## Section %d\n\n", s)
		for p := 0; p < 4; p++ {
			t := r.Intn(n)
			switch r.Intn(6) {
			case 0:
				fmt.Fprintf(&b, "See [[%s#Section %d]] for details, also `inline [[code]]` here.\n", Name(t), r.Intn(6))
			case 1:
				fmt.Fprintf(&b, "Related: [[%s|alias %d]] and [[missing-%d]].\n", baseOf(Name(t)), t, t)
			case 2:
				fmt.Fprintf(&b, "A [markdown link](/%s.md#section-%d) and [web](https://example.com/%d).\n", Name(t), r.Intn(6), t)
			case 3:
				fmt.Fprintf(&b, "- item about [[%s]] with @mention text and email a@b.c\n", Name(t))
			default:
				b.WriteString("Plain prose line without links, long enough to look like a real sentence in a note.\n")
			}
		}
		if s == 3 {
			b.WriteString("\n```go\n# not a heading [[nope]]\nfunc main() {}\n```\n")
		}
		b.WriteString("\n### Details\n\nMore text under a third level heading.\n\n")
	}
	return b.String()
}

func baseOf(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// Write creates a workspace of n notes under dir.
func Write(dir string, n int) error {
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, filepath.FromSlash(Name(i))+".md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(Note(i, n)), 0o644); err != nil {
			return err
		}
	}
	return nil
}
