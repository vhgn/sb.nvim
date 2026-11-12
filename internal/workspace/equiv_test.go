package workspace

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"brain/internal/benchdata"
	"brain/internal/md"
)

// The naive* functions are the original, index-free algorithms. The indexed
// implementation must agree with them exactly.

func naiveResolve(w *Workspace, from *Document, l md.Link) (*Document, int) {
	var t *Document
	switch {
	case l.Target == "":
		t = from
	case l.Kind == md.WikiLink:
		t = naiveWiki(w, from, l.Target)
	default:
		t = w.resolvePath(from, l.Target)
	}
	if t == nil {
		return nil, -1
	}
	return t, t.Doc.FindHeading(l.Anchor)
}

func naiveWiki(w *Workspace, from *Document, target string) *Document {
	key := wikiKey(target)
	docs := w.Docs()
	for _, d := range docs {
		if strings.ToLower(d.Name) == key {
			return d
		}
	}
	if from != nil {
		if d := w.resolvePath(from, target); d != nil {
			return d
		}
	}
	var best *Document
	for _, d := range docs {
		if strings.HasSuffix(strings.ToLower(d.Name), "/"+key) && (best == nil || len(d.Name) < len(best.Name)) {
			best = d
		}
	}
	return best
}

func naiveBacklinks(w *Workspace, target *Document, heading int) []Ref {
	var refs []Ref
	for _, d := range w.Docs() {
		for _, l := range d.Doc.Links {
			if t, h := naiveResolve(w, d, l); t == target && (heading < 0 || h == heading) {
				refs = append(refs, Ref{d, l})
			}
		}
	}
	return refs
}

func naiveShortName(w *Workspace, d *Document) string {
	base := path.Base(d.Name)
	for _, o := range w.docs {
		if o != d && strings.EqualFold(path.Base(o.Name), base) {
			return d.Name
		}
	}
	return base
}

const edgeLinks = `[[]] [[#Section 1]] [[NOTE-3]] [[./note-4]] [[note-5.md]] [[/projects/note-6/]]
[a](note-7.md) [b](../note-8.md) [c](/areas/note-9) [d](Note-10.md) [e](note-11.markdown)
[[daily/note-12]] [[notes/daily/note-12#Details]] [[weird.md]] [f](weird.md.md) [[Weird.MD]]
[g](./sub/../note-13.md) [hid](/.hidden/note-1.md) [[missing]] [h](missing.md#x) [[note-2|alias]]`

func checkEquivalent(t *testing.T, w *Workspace) {
	t.Helper()
	docs := append([]*Document(nil), w.Docs()...)
	for _, d := range docs {
		if got, want := w.ShortName(d), naiveShortName(w, d); got != want {
			t.Errorf("ShortName(%s) = %q, want %q", d.Name, got, want)
		}
		for _, l := range d.Doc.Links {
			gt, gh := w.Resolve(d, l)
			nt, nh := naiveResolve(w, d, l)
			if gt != nt || gh != nh {
				t.Errorf("Resolve(%s, %+v) = %v,%d want %v,%d", d.Name, l, gt, gh, nt, nh)
			}
		}
	}
	for _, d := range docs {
		for h := -1; h < min(len(d.Doc.Headings), 3); h++ {
			got, want := w.Backlinks(d, h), naiveBacklinks(w, d, h)
			if len(got) != len(want) {
				t.Fatalf("Backlinks(%s,%d): %d refs, want %d", d.Name, h, len(got), len(want))
			}
			for i := range got {
				if got[i].Doc != want[i].Doc || got[i].Link != want[i].Link {
					t.Fatalf("Backlinks(%s,%d)[%d] = %+v, want %+v", d.Name, h, i, got[i], want[i])
				}
			}
		}
	}
}

func TestIndexEquivalence(t *testing.T) {
	root := t.TempDir()
	const n = 48
	if err := benchdata.Write(root, n); err != nil {
		t.Fatal(err)
	}
	extra := map[string]string{
		"weird.md.md":              "# Weird\n",
		"Weird.md":                 "# Weird upper\n",
		"note-11.markdown":         "# Eleven\n",
		"notes/edge.md":            edgeLinks,
		"projects/sub/note-13.md":  "# Thirteen\n" + edgeLinks,
		"notes/daily/Note-3.md":    "# Upper three\n[[note-3]]",
		".hidden/note-1.md":        "# hidden\n",
		"projects/note-6/index.md": "# dir note\n",
	}
	for p, s := range extra {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(s), 0o644)
	}
	w := New(root)
	if err := w.Scan(); err != nil {
		t.Fatal(err)
	}
	checkEquivalent(t, w)

	// Edits, opens and removals must keep the indexes consistent.
	edge := filepath.Join(root, "notes/edge.md")
	w.Set(edge, edgeLinks+"\n[[note-20]] [[note-21#Section 2]]", true)
	w.Remove(filepath.Join(root, benchdata.Name(5)+".md"))
	w.Set(filepath.Join(root, "new/note-21.md"), "# new\n## Section 2\n[[edge]]", false)
	w.Load(filepath.Join(root, benchdata.Name(7)+".md"))
	checkEquivalent(t, w)
}
