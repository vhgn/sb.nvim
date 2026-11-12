// Package workspace indexes the markdown notes under a root directory and
// resolves links between them.
package workspace

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"brain/internal/md"
)

type Document struct {
	Path string // absolute, cleaned
	Name string // path relative to the root, slash separated, without extension
	Doc  *md.Doc
	Open bool // owned by the editor; disk changes are ignored until closed

	lowerName string
	lowerBase string
	linkKeys  []string // distinct link index keys of Doc.Links

	previewOnce sync.Once
	preview     string
}

// Preview returns f(d) computed once per Document. A Document is replaced,
// never mutated, when its text changes, so the cached value cannot go stale.
func (d *Document) Preview(f func(*Document) string) string {
	d.previewOnce.Do(func() { d.preview = f(d) })
	return d.preview
}

// Workspace holds every known document plus indexes that keep lookups,
// resolution and backlink queries independent of the workspace size.
// It is not safe for concurrent use.
type Workspace struct {
	Root string
	docs map[string]*Document // by path

	sorted []*Document                    // docs ordered by Name; nil when it must be rebuilt
	byName map[string][]*Document         // lower(Name) → docs
	byBase map[string][]*Document         // lower(base(Name)) → docs
	links  map[string]map[string]struct{} // link key → paths of docs with such a link
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
	return &Workspace{
		Root:   root,
		docs:   map[string]*Document{},
		byName: map[string][]*Document{},
		byBase: map[string][]*Document{},
		links:  map[string]map[string]struct{}{},
	}
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

// Scan loads every markdown file under Root, skipping hidden directories and
// node_modules. Files are read and parsed in parallel.
func (w *Workspace) Scan() error {
	if w.Root == "" {
		return nil
	}
	var paths []string
	err := filepath.WalkDir(w.Root, func(p string, e fs.DirEntry, err error) error {
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
			paths = append(paths, filepath.Clean(p))
		}
		return nil
	})

	parsed := make([]*md.Doc, len(paths))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), max(len(paths), 1)) {
		wg.Go(func() {
			for i := range jobs {
				if b, err := os.ReadFile(paths[i]); err == nil {
					parsed[i] = md.Parse(string(b))
				}
			}
		})
	}
	for i, p := range paths {
		if d := w.docs[p]; d != nil && d.Open {
			continue
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	for i, p := range paths {
		if d := w.docs[p]; d != nil && d.Open {
			continue
		}
		if parsed[i] == nil {
			w.Remove(p)
		} else {
			w.put(p, parsed[i], false)
		}
	}
	return err
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
	return w.put(filepath.Clean(p), md.Parse(text), open)
}

// put stores a parsed document at the cleaned path p and updates the indexes.
func (w *Workspace) put(p string, doc *md.Doc, open bool) *Document {
	name := w.name(p)
	d := &Document{Path: p, Name: name, Doc: doc, Open: open, lowerName: strings.ToLower(name)}
	d.lowerBase = path.Base(d.lowerName)
	d.linkKeys = linkKeys(doc.Links)

	old := w.docs[p]
	w.docs[p] = d
	if old != nil {
		w.unindexLinks(old)
		replace(w.byName, old.lowerName, old, d)
		replace(w.byBase, old.lowerBase, old, d)
		w.replaceSorted(old, d)
	} else {
		w.byName[d.lowerName] = append(w.byName[d.lowerName], d)
		w.byBase[d.lowerBase] = append(w.byBase[d.lowerBase], d)
		w.sorted = nil
	}
	w.indexLinks(d)
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
		w.Remove(p)
		return nil
	}
	return w.Set(p, string(b), false)
}

func (w *Workspace) Remove(p string) {
	p = filepath.Clean(p)
	d := w.docs[p]
	if d == nil {
		return
	}
	delete(w.docs, p)
	w.unindexLinks(d)
	remove(w.byName, d.lowerName, d)
	remove(w.byBase, d.lowerBase, d)
	w.sorted = nil
}

func (w *Workspace) Get(p string) *Document { return w.docs[filepath.Clean(p)] }

// Docs returns all documents sorted by name. The slice is shared and must not
// be modified; it stays valid until the next Set, Load or Remove.
func (w *Workspace) Docs() []*Document {
	if w.sorted == nil {
		w.sorted = make([]*Document, 0, len(w.docs))
		for _, d := range w.docs {
			w.sorted = append(w.sorted, d)
		}
		sort.Slice(w.sorted, func(i, j int) bool { return less(w.sorted[i], w.sorted[j]) })
	}
	return w.sorted
}

func less(a, b *Document) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Path < b.Path
}

// replaceSorted swaps old for d in the cached order. Both share a path and
// hence a name, so the position is unchanged.
func (w *Workspace) replaceSorted(old, d *Document) {
	if w.sorted == nil {
		return
	}
	i := sort.Search(len(w.sorted), func(i int) bool { return !less(w.sorted[i], old) })
	if i < len(w.sorted) && w.sorted[i] == old {
		w.sorted[i] = d
	} else {
		w.sorted = nil
	}
}

// ShortName is the shortest wiki target that unambiguously refers to d:
// the base name when unique, otherwise the full name.
func (w *Workspace) ShortName(d *Document) string {
	for _, o := range w.byBase[d.lowerBase] {
		if o != d {
			return d.Name
		}
	}
	return path.Base(d.Name)
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

// NewNotePath returns the path where the note a link points at would be
// created when it does not resolve: wiki links are placed under Root (or
// next to from without a root), markdown links resolve like paths. ".md" is
// appended when the target has no extension. Targets with a non markdown
// extension (images, PDFs, …) give "".
func (w *Workspace) NewNotePath(from *Document, l md.Link) string {
	target := strings.TrimSpace(l.Target)
	if target == "" {
		return ""
	}
	if ext := filepath.Ext(target); ext != "" && !IsMarkdown(target) {
		return ""
	}
	if !IsMarkdown(target) {
		target += ".md"
	}
	p := filepath.FromSlash(target)
	base := w.Root
	if from != nil && (base == "" || l.Kind == md.MarkdownLink) {
		base = filepath.Dir(from.Path)
	}
	switch {
	case strings.HasPrefix(target, "/") && w.Root != "":
		p = filepath.Join(w.Root, p)
	case !filepath.IsAbs(p) && base != "":
		p = filepath.Join(base, p)
	}
	if !filepath.IsAbs(p) {
		return ""
	}
	return filepath.Clean(p)
}

// ResolveName resolves a wiki style target name.
func (w *Workspace) ResolveName(from *Document, name string) *Document {
	if strings.TrimSpace(name) == "" {
		return from
	}
	return w.resolveWiki(from, name)
}

func wikiKey(target string) string {
	key := strings.ToLower(strings.Trim(stripExt(filepath.ToSlash(strings.TrimSpace(target))), "/"))
	return strings.TrimPrefix(key, "./")
}

func (w *Workspace) resolveWiki(from *Document, target string) *Document {
	key := wikiKey(target)
	if d := first(w.byName[key]); d != nil {
		return d
	}
	if from != nil {
		if d := w.resolvePath(from, target); d != nil {
			return d
		}
	}
	var best *Document
	for _, d := range w.byBase[path.Base(key)] {
		if strings.HasSuffix(d.lowerName, "/"+key) &&
			(best == nil || len(d.Name) < len(best.Name) || len(d.Name) == len(best.Name) && less(d, best)) {
			best = d
		}
	}
	return best
}

// first returns the document that comes first in Docs order.
func first(ds []*Document) *Document {
	var best *Document
	for _, d := range ds {
		if best == nil || less(d, best) {
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
//
// Every way a link can resolve (exact name, relative or rooted path, path plus
// ".md", name suffix) lands on a document whose lowercased base name equals
// one of the link's keys, so only links sharing a key with target are resolved.
func (w *Workspace) Backlinks(target *Document, heading int) []Ref {
	srcs := map[string]bool{target.Path: true} // links with an empty target point at their own doc
	for p := range w.links[target.lowerBase] {
		srcs[p] = true
	}
	docs := make([]*Document, 0, len(srcs))
	for p := range srcs {
		if d := w.docs[p]; d != nil {
			docs = append(docs, d)
		}
	}
	sort.Slice(docs, func(i, j int) bool { return less(docs[i], docs[j]) })

	var refs []Ref
	for _, d := range docs {
		for _, l := range d.Doc.Links {
			if l.Target == "" {
				if d != target {
					continue
				}
			} else if k1, k2 := keysOf(l.Target); k1 != target.lowerBase && k2 != target.lowerBase {
				continue
			}
			t, h := w.Resolve(d, l)
			if t == target && (heading < 0 || h == heading) {
				refs = append(refs, Ref{d, l})
			}
		}
	}
	return refs
}

// keysOf returns the backlink index keys of a link target: its lowercased base
// name with and without a markdown extension.
func keysOf(target string) (string, string) {
	t := strings.ToLower(strings.TrimSpace(filepath.ToSlash(target)))
	base := path.Base(path.Clean(t))
	return stripExt(base), base
}

func linkKeys(links []md.Link) []string {
	var keys []string
	add := func(k string) {
		for _, o := range keys {
			if o == k {
				return
			}
		}
		keys = append(keys, k)
	}
	for _, l := range links {
		if l.Target == "" {
			continue
		}
		k1, k2 := keysOf(l.Target)
		add(k1)
		add(k2)
	}
	return keys
}

func (w *Workspace) indexLinks(d *Document) {
	for _, k := range d.linkKeys {
		m := w.links[k]
		if m == nil {
			m = map[string]struct{}{}
			w.links[k] = m
		}
		m[d.Path] = struct{}{}
	}
}

func (w *Workspace) unindexLinks(d *Document) {
	for _, k := range d.linkKeys {
		if m := w.links[k]; m != nil {
			delete(m, d.Path)
			if len(m) == 0 {
				delete(w.links, k)
			}
		}
	}
}

func replace(idx map[string][]*Document, key string, old, d *Document) {
	for i, o := range idx[key] {
		if o == old {
			idx[key][i] = d
			return
		}
	}
	idx[key] = append(idx[key], d)
}

func remove(idx map[string][]*Document, key string, d *Document) {
	ds := idx[key]
	for i, o := range ds {
		if o == d {
			ds = append(ds[:i], ds[i+1:]...)
			break
		}
	}
	if len(ds) == 0 {
		delete(idx, key)
	} else {
		idx[key] = ds
	}
}
