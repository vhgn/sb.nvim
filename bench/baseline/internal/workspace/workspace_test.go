package workspace

import (
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	w := New("/notes")
	idx := w.Set("/notes/index.md", "[[alpha]] [[projects/alpha#Tasks]] [b](sub/beta.md) [[beta]] [[missing]] [[#Top]]\n# Top", false)
	alpha := w.Set("/notes/projects/alpha.md", "# Alpha\n## Tasks\n", false)
	beta := w.Set("/notes/sub/beta.md", "# Beta\n", false)
	w.Set("/notes/other/beta.md", "# Other beta\n", false)

	cases := []struct {
		doc     *Document
		heading int
	}{{alpha, -1}, {alpha, 1}, {beta, -1}, {nil, -1}, {nil, -1}, {idx, 0}}
	for i, c := range cases {
		d, h := w.Resolve(idx, idx.Doc.Links[i])
		if i == 3 { // ambiguous [[beta]] resolves to one of the two
			if d == nil || filepath.Base(d.Path) != "beta.md" {
				t.Errorf("link %d: %v", i, d)
			}
			continue
		}
		if d != c.doc || h != c.heading {
			t.Errorf("link %d (%+v): got %v,%d", i, idx.Doc.Links[i], d, h)
		}
	}
	if w.ShortName(alpha) != "alpha" || w.ShortName(beta) != "sub/beta" {
		t.Errorf("short names: %s %s", w.ShortName(alpha), w.ShortName(beta))
	}
	if refs := w.Backlinks(alpha, -1); len(refs) != 2 {
		t.Errorf("backlinks: %+v", refs)
	}
	if refs := w.Backlinks(alpha, 1); len(refs) != 1 {
		t.Errorf("heading backlinks: %+v", refs)
	}
}
