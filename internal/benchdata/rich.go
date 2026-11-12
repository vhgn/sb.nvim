package benchdata

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

var statuses = []string{"todo", "doing", "done", "archived"}

// RichNote returns note i of an n-note workspace with Obsidian-style front
// matter (status, numbers, dates, link properties, lists) on top of the body
// of Note(i, n). It is meant for Bases and front matter benchmarks.
func RichNote(i, n int) string {
	r := rand.New(rand.NewSource(int64(i) + 7919))
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: \"Note %d\"\n", i)
	fmt.Fprintf(&b, "aliases: [Note %d, N%d]\n", i, i)
	fmt.Fprintf(&b, "tags:\n  - bench\n  - n%d\n", i%7)
	if i%3 == 0 {
		b.WriteString("  - project/active\n")
	}
	fmt.Fprintf(&b, "status: %s\n", statuses[i%len(statuses)])
	fmt.Fprintf(&b, "priority: %d\n", r.Intn(5)+1)
	fmt.Fprintf(&b, "rating: %.1f\n", float64(r.Intn(50))/10)
	if i%4 != 0 {
		fmt.Fprintf(&b, "due: 2024-%02d-%02d\n", r.Intn(12)+1, r.Intn(28)+1)
	}
	fmt.Fprintf(&b, "created: 2023-%02d-%02dT%02d:%02d\n", r.Intn(12)+1, r.Intn(28)+1, r.Intn(24), r.Intn(60))
	fmt.Fprintf(&b, "project: \"[[%s]]\"\n", baseOf(Name(r.Intn(n))))
	fmt.Fprintf(&b, "related:\n  - \"[[%s]]\"\n  - \"[[%s]]\"\n", Name(r.Intn(n)), baseOf(Name(r.Intn(n))))
	fmt.Fprintf(&b, "done: %t\n", i%2 == 0)
	b.WriteString("summary: >\n  A folded summary line that\n  spans two lines.\n")
	b.WriteString("---\n")
	body := Note(i, n)
	if _, rest, ok := strings.Cut(body, "\n---\n"); ok {
		body = rest
	}
	b.WriteString(body)
	return b.String()
}

// WriteRich creates a workspace of n RichNote notes under dir.
func WriteRich(dir string, n int) error {
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, filepath.FromSlash(Name(i))+".md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(RichNote(i, n)), 0o644); err != nil {
			return err
		}
	}
	return nil
}
