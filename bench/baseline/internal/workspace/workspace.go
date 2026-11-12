// Package workspace indexes the markdown notes under a root directory and
// resolves links between them.
package workspace

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"brain/internal/md"
)

type Document struct {
	Path string // absolute, cleaned
	Name string // path relative to the root, slash separated, without extension
	Doc  *md.Doc
	Open bool // owned by the editor; disk changes are ignored until closed
}

type Workspace struct {
	Root string
	docs map[string]*Document
}

// Ref is a link inside Doc.
type Ref struct {
	Doc  *Document
	Link md.Link
}

func New(root string) *Workspace {
	if root != "" {
		root = filepath.Clean(root)
	}
	return &Workspace{Root: root, docs: map[string]*Document{}}
}

func IsMarkdown(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

func stripExt(p string) string {
	if IsMarkdown(p) {
		return p[:len(p)-len(filepath.Ext(p))]
	}
	return p
}

// Scan loads every markdown file under Root, skipping hidden directories and node_modules.
func (w *Workspace) Scan() error {
	if w.Root == "" {
		return nil
	}
	return filepath.WalkDir(w.Root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if p != w.Root && (strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if IsMarkdown(p) {
			w.Load(p)
		}
		return nil
	})
}

func (w *Workspace) name(p string) string {
	if w.Root != "" {
		if rel, err := filepath.Rel(w.Root, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(stripExt(rel))
		}
	}
	return stripExt(filepath.Base(p))
}

// Set stores the text of a document. open marks it as owned by the editor.
func (w *Workspace) Set(p, text string, open bool) *Document {
	p = filepath.Clean(p)
	d := &Document{Path: p, Name: w.name(p), Doc: md.Parse(text), Open: open}
	w.docs[p] = d
	return d
}

// Load (re)reads a document from disk unless the editor owns it.
func (w *Workspace) Load(p string) *Document {
	p = filepath.Clean(p)
	if d := w.docs[p]; d != nil && d.Open {
		return d
	}
	b, err := os.ReadFile(p)
	if err != nil {
		delete(w.docs, p)
		return nil
	}
	return w.Set(p, string(b), false)
}

func (w *Workspace) Remove(p string) { delete(w.docs, filepath.Clean(p)) }

func (w *Workspace) Get(p string) *Document { return w.docs[filepath.Clean(p)] }

// Docs returns all documents sorted by name.
func (w *Workspace) Docs() []*Document {
	out := make([]*Document, 0, len(w.docs))
	for _, d := range w.docs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ShortName is the shortest wiki target that unambiguously refers to d:
// the base name when unique, otherwise the full name.
func (w *Workspace) ShortName(d *Document) string {
	base := path.Base(d.Name)
	for _, o := range w.docs {
		if o != d && strings.EqualFold(path.Base(o.Name), base) {
			return d.Name
		}
	}
	return base
}

// Resolve finds the document and heading (index or -1) a link points to.
func (w *Workspace) Resolve(from *Document, l md.Link) (*Document, int) {
	var target *Document
	switch {
	case l.Target == "":
		target = from
	case l.Kind == md.WikiLink:
		target = w.resolveWiki(from, l.Target)
	default:
		target = w.resolvePath(from, l.Target)
	}
	if target == nil {
		return nil, -1
	}
	return target, target.Doc.FindHeading(l.Anchor)
}

// ResolveName resolves a wiki style target name.
func (w *Workspace) ResolveName(from *Document, name string) *Document {
	if strings.TrimSpace(name) == "" {
		return from
	}
	return w.resolveWiki(from, name)
}

func (w *Workspace) resolveWiki(from *Document, target string) *Document {
	key := strings.ToLower(strings.Trim(stripExt(filepath.ToSlash(strings.TrimSpace(target))), "/"))
	key = strings.TrimPrefix(key, "./")
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
		n := strings.ToLower(d.Name)
		if strings.HasSuffix(n, "/"+key) && (best == nil || len(d.Name) < len(best.Name)) {
			best = d
		}
	}
	return best
}

func (w *Workspace) resolvePath(from *Document, target string) *Document {
	p := filepath.FromSlash(target)
	switch {
	case strings.HasPrefix(target, "/") && w.Root != "":
		p = filepath.Join(w.Root, p)
	case !filepath.IsAbs(p) && from != nil:
		p = filepath.Join(filepath.Dir(from.Path), p)
	}
	for _, c := range []string{p, p + ".md"} {
		if d := w.Get(c); d != nil {
			return d
		}
		if IsMarkdown(c) {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return w.Load(c)
			}
		}
	}
	return nil
}

// Backlinks lists links pointing at target. With heading >= 0 only links to
// that heading are returned.
func (w *Workspace) Backlinks(target *Document, heading int) []Ref {
	var refs []Ref
	for _, d := range w.Docs() {
		for _, l := range d.Doc.Links {
			t, h := w.Resolve(d, l)
			if t == target && (heading < 0 || h == heading) {
				refs = append(refs, Ref{d, l})
			}
		}
	}
	return refs
}
