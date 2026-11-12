// Package vault keeps an index of every file under a root directory together
// with the front matter, tags and links of markdown notes. The index is
// persisted to a cache file so restarts only re-read files that changed.
package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"brain/internal/yaml"
)

const cacheVersion = 3

// File is one file in the vault.
type File struct {
	Rel    string         `json:"rel"` // slash separated, relative to the root
	Size   int64          `json:"size"`
	Mtime  int64          `json:"mtime"` // unix ms
	Ctime  int64          `json:"ctime"` // unix ms
	Props  map[string]any `json:"props,omitempty"`
	Tags   []string       `json:"tags,omitempty"` // without '#'
	Links  []string       `json:"links,omitempty"`
	Embeds []string       `json:"embeds,omitempty"`
	Error  string         `json:"error,omitempty"` // front matter parse error
}

func (f *File) Name() string { return path.Base(f.Rel) }
func (f *File) Ext() string  { return strings.TrimPrefix(path.Ext(f.Rel), ".") }
func (f *File) Basename() string {
	n := f.Name()
	return strings.TrimSuffix(n, path.Ext(n))
}
func (f *File) Folder() string {
	d := path.Dir(f.Rel)
	if d == "." {
		return ""
	}
	return d
}
func (f *File) IsMarkdown() bool { return isMarkdown(f.Rel) }

func isMarkdown(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// Vault is safe for concurrent use.
type Vault struct {
	Root      string
	CachePath string

	mu       sync.RWMutex
	files    map[string]*File // by Rel
	sorted   []*File
	byName   map[string][]*File // lower(basename) for notes, lower(name) for everything
	backs    map[string][]string // target rel → sorted source rels
	// outs holds, per file, the rel each of its Links resolves to ("" when
	// unresolved), aligned with File.Links. Resolution only depends on the
	// set of paths in the vault, so it stays valid until a file is added or
	// removed.
	outs     map[string][]string
	dirty    bool
	lastScan time.Time
}

func New(root, cacheDir string) *Vault {
	root = filepath.Clean(root)
	v := &Vault{Root: root, files: map[string]*File{}}
	if cacheDir != "" {
		sum := sha256.Sum256([]byte(root))
		v.CachePath = filepath.Join(cacheDir, "index-"+hex.EncodeToString(sum[:8])+".json")
	}
	return v
}

// The cache is JSON lines: a header object, then one File per line, so it
// can be encoded and decoded in parallel.
type cacheHeader struct {
	Version int    `json:"version"`
	Root    string `json:"root"`
}

// LoadCache reads the persisted index, if any. Call Scan afterwards.
func (v *Vault) LoadCache() {
	if v.CachePath == "" {
		return
	}
	b, err := os.ReadFile(v.CachePath)
	if err != nil {
		return
	}
	nl := bytes.IndexByte(b, '\n')
	if nl < 0 {
		return
	}
	var h cacheHeader
	if json.Unmarshal(b[:nl], &h) != nil || h.Version != cacheVersion || h.Root != v.Root {
		return
	}
	body := b[nl+1:]
	// Split the lines into roughly equal chunks decoded concurrently.
	n := min(runtime.GOMAXPROCS(0), max(1, len(body)/(256<<10)))
	var bounds []int
	bounds = append(bounds, 0)
	for k := 1; k < n; k++ {
		at := len(body) * k / n
		if at <= bounds[len(bounds)-1] {
			continue
		}
		i := bytes.IndexByte(body[at:], '\n')
		if i < 0 {
			break
		}
		bounds = append(bounds, at+i+1)
	}
	bounds = append(bounds, len(body))
	parts := make([][]*File, len(bounds)-1)
	failed := make([]bool, len(parts))
	var wg sync.WaitGroup
	for k := range parts {
		wg.Go(func() {
			chunk := body[bounds[k]:bounds[k+1]]
			var out []*File
			for len(chunk) > 0 {
				line := chunk
				if i := bytes.IndexByte(chunk, '\n'); i >= 0 {
					line, chunk = chunk[:i], chunk[i+1:]
				} else {
					chunk = nil
				}
				if len(line) == 0 {
					continue
				}
				f := &File{}
				if json.Unmarshal(line, f) != nil {
					failed[k] = true
					return
				}
				out = append(out, f)
			}
			parts[k] = out
		})
	}
	wg.Wait()
	total := 0
	for k, p := range parts {
		if failed[k] {
			return
		}
		total += len(p)
	}
	files := make(map[string]*File, total)
	for _, p := range parts {
		for _, f := range p {
			files[f.Rel] = f
		}
	}
	v.mu.Lock()
	v.files = files
	v.reindex()
	v.mu.Unlock()
}

// SaveCache writes the index if it changed since the last save.
func (v *Vault) SaveCache() error {
	if v.CachePath == "" {
		return nil
	}
	v.mu.Lock()
	if !v.dirty {
		v.mu.Unlock()
		return nil
	}
	files := v.sorted // never modified in place, see reindex and update
	v.dirty = false
	v.mu.Unlock()

	head, err := json.Marshal(cacheHeader{cacheVersion, v.Root})
	if err != nil {
		return err
	}
	const chunk = 512
	parts := make([][]byte, (len(files)+chunk-1)/chunk)
	errs := make([]error, len(parts))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for k := range parts {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf) // Encode appends the newline
			for _, f := range files[k*chunk : min(len(files), (k+1)*chunk)] {
				if err := enc.Encode(f); err != nil {
					errs[k] = err
					return
				}
			}
			parts[k] = buf.Bytes()
		})
	}
	wg.Wait()
	size := len(head) + 1
	for k, p := range parts {
		if errs[k] != nil {
			return errs[k]
		}
		size += len(p)
	}
	b := make([]byte, 0, size)
	b = append(append(b, head...), '\n')
	for _, p := range parts {
		b = append(b, p...)
	}
	if err := os.MkdirAll(filepath.Dir(v.CachePath), 0o755); err != nil {
		return err
	}
	tmp := v.CachePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, v.CachePath)
}

func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

type stat struct {
	rel          string
	size         int64
	mtime, ctime int64
}

// Scan walks the root, re-reading files whose size or mtime changed and
// dropping files that disappeared. It reports whether anything changed.
func (v *Vault) Scan() bool {
	found := v.walk()

	v.mu.RLock()
	var todo []stat
	known := 0
	for _, s := range found {
		f := v.files[s.rel]
		if f != nil {
			known++
		}
		if f == nil || f.Size != s.size || f.Mtime != s.mtime {
			todo = append(todo, s)
		}
	}
	var gone []string
	// found holds distinct paths, so when every indexed file was found
	// nothing disappeared.
	if known != len(v.files) {
		seen := make(map[string]bool, len(found))
		for _, s := range found {
			seen[s.rel] = true
		}
		for rel := range v.files {
			if !seen[rel] {
				gone = append(gone, rel)
			}
		}
	}
	v.mu.RUnlock()

	parsed := v.readAll(todo)

	v.mu.Lock()
	defer v.mu.Unlock()
	v.lastScan = time.Now()
	if len(todo) == 0 && len(gone) == 0 {
		return false
	}
	v.dirty = true
	v.apply(parsed, gone)
	return true
}

// walk lists every indexable file under the root with its stat data.
func (v *Vault) walk() []stat {
	if info, err := os.Lstat(v.Root); err != nil || !info.IsDir() {
		return v.walkSerial()
	}
	var (
		mu    sync.Mutex
		found []stat
		wg    sync.WaitGroup
		sem   = make(chan struct{}, runtime.GOMAXPROCS(0))
	)
	statAll := func(dir, rel string, ents []fs.DirEntry) {
		defer wg.Done()
		sem <- struct{}{}
		out := make([]stat, 0, len(ents))
		for _, e := range ents {
			var info fs.FileInfo
			var err error
			if e.Type().IsRegular() {
				info, err = e.Info()
			} else { // symlink: follow it
				info, err = os.Stat(filepath.Join(dir, e.Name()))
			}
			if err != nil || info.IsDir() {
				continue
			}
			r := e.Name()
			if rel != "" {
				r = rel + "/" + r
			}
			out = append(out, statOf(r, info))
		}
		<-sem
		mu.Lock()
		found = append(found, out...)
		mu.Unlock()
	}
	var visit func(dir, rel string)
	visit = func(dir, rel string) {
		defer wg.Done()
		sem <- struct{}{}
		var ents []fs.DirEntry
		if d, err := os.Open(dir); err == nil {
			ents, _ = d.ReadDir(-1) // keep what was read on error, as WalkDir does
			d.Close()
		}
		var files []fs.DirEntry
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() {
				if skipDir(name) {
					continue
				}
				sub := name
				if rel != "" {
					sub = rel + "/" + name
				}
				wg.Add(1)
				go visit(filepath.Join(dir, name), sub)
				continue
			}
			if strings.HasPrefix(name, ".") || !e.Type().IsRegular() && e.Type()&fs.ModeSymlink == 0 {
				continue
			}
			files = append(files, e)
		}
		<-sem
		const chunk = 128
		for len(files) > 0 {
			n := min(chunk, len(files))
			wg.Add(1)
			go statAll(dir, rel, files[:n])
			files = files[n:]
		}
	}
	wg.Add(1)
	visit(v.Root, "")
	wg.Wait()
	return found
}

// walkSerial is the plain WalkDir walk, used when the root is not a
// directory (e.g. a symlink), to keep WalkDir's exact semantics there.
func (v *Vault) walkSerial() []stat {
	var found []stat
	_ = filepath.WalkDir(v.Root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if p != v.Root && skipDir(e.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(e.Name(), ".") || !e.Type().IsRegular() && e.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(v.Root, p)
		if err != nil {
			return nil
		}
		found = append(found, statOf(filepath.ToSlash(rel), info))
		return nil
	})
	return found
}

// ScanIfOlder rescans when the last scan is older than d.
func (v *Vault) ScanIfOlder(d time.Duration) bool {
	v.mu.RLock()
	stale := time.Since(v.lastScan) > d
	v.mu.RUnlock()
	return stale && v.Scan()
}

// Touch re-reads the given absolute paths (e.g. after the editor saved them).
func (v *Vault) Touch(paths ...string) {
	var todo []stat
	var gone []string
	for _, p := range paths {
		rel, err := filepath.Rel(v.Root, filepath.Clean(p))
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		info, err := os.Stat(p)
		if err != nil {
			gone = append(gone, rel)
			continue
		}
		todo = append(todo, statOf(rel, info))
	}
	parsed := v.readAll(todo)
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(parsed)+len(gone) > 0 {
		v.dirty = true
		v.apply(parsed, gone)
	}
}

// apply stores re-read files and drops gone ones, then brings the lookup
// tables up to date; v.mu must be held for writing. When the set of paths
// is unchanged (files were only edited) link resolution is unaffected, so
// only the edited files' entries are updated instead of rebuilding.
func (v *Vault) apply(parsed []*File, gone []string) {
	reshaped := v.sorted == nil && len(v.files) > 0
	for _, rel := range gone {
		if _, ok := v.files[rel]; ok {
			delete(v.files, rel)
			reshaped = true
		}
	}
	var edited []*File
	for _, f := range parsed {
		if old := v.files[f.Rel]; old == nil {
			reshaped = true
		} else if !reshaped {
			edited = append(edited, f)
		}
		v.files[f.Rel] = f
	}
	if reshaped {
		v.reindex()
		return
	}
	v.update(edited)
}

func statOf(rel string, info os.FileInfo) stat {
	s := stat{rel: rel, size: info.Size(), mtime: info.ModTime().UnixMilli()}
	s.ctime = s.mtime
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if c := time.Unix(st.Ctim.Sec, st.Ctim.Nsec).UnixMilli(); c < s.ctime {
			s.ctime = c
		}
	}
	return s
}

func (v *Vault) readAll(todo []stat) []*File {
	out := make([]*File, len(todo))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), max(len(todo), 1)) {
		wg.Go(func() {
			for i := range jobs {
				s := todo[i]
				f := &File{Rel: s.rel, Size: s.size, Mtime: s.mtime, Ctime: s.ctime}
				if isMarkdown(s.rel) && s.size < 4<<20 {
					if b, err := os.ReadFile(filepath.Join(v.Root, filepath.FromSlash(s.rel))); err == nil {
						ParseNote(f, string(b))
					}
				}
				out[i] = f
			}
		})
	}
	for i := range todo {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}

// reindex rebuilds lookup tables; v.mu must be held for writing. The
// slices it publishes (sorted, backs values) are never modified in place
// afterwards, so callers of Files and Backlinks may keep them.
func (v *Vault) reindex() {
	sorted := make([]*File, 0, len(v.files))
	for _, f := range v.files {
		sorted = append(sorted, f)
	}
	slices.SortFunc(sorted, func(a, b *File) int { return strings.Compare(a.Rel, b.Rel) })
	v.sorted = sorted
	v.byName = make(map[string][]*File, len(sorted)*2)
	for _, f := range sorted {
		for _, k := range nameKeys(f) {
			v.byName[k] = append(v.byName[k], f)
		}
	}
	for _, fs := range v.byName {
		if len(fs) > 1 {
			slices.SortFunc(fs, byLenRel)
		}
	}

	// Resolve every link; resolution only reads files and byName.
	outs := make([][]string, len(sorted))
	parallel(len(sorted), 256, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			outs[i] = v.resolveLinks(sorted[i])
		}
	})
	v.outs = make(map[string][]string, len(sorted))
	v.backs = map[string][]string{}
	for i, f := range sorted {
		if outs[i] == nil {
			continue
		}
		v.outs[f.Rel] = outs[i]
		for _, t := range distinct(outs[i]) {
			v.backs[t] = append(v.backs[t], f.Rel)
		}
	}
}

// update refreshes the lookup tables for files whose content changed but
// whose paths were already indexed; v.files already holds the new values.
func (v *Vault) update(edited []*File) {
	if len(edited) == 0 {
		return
	}
	sorted := slices.Clone(v.sorted)
	for _, f := range edited {
		if i, ok := slices.BinarySearchFunc(sorted, f.Rel, func(x *File, rel string) int { return strings.Compare(x.Rel, rel) }); ok {
			sorted[i] = f
		}
		for _, k := range nameKeys(f) {
			for i, x := range v.byName[k] {
				if x.Rel == f.Rel {
					v.byName[k][i] = f
				}
			}
		}
	}
	v.sorted = sorted
	for _, f := range edited {
		old := distinct(v.outs[f.Rel])
		out := v.resolveLinks(f)
		if out == nil {
			delete(v.outs, f.Rel)
		} else {
			v.outs[f.Rel] = out
		}
		now := distinct(out)
		for _, t := range old {
			if !slices.Contains(now, t) {
				v.backs[t] = removeSorted(v.backs[t], f.Rel)
				if len(v.backs[t]) == 0 {
					delete(v.backs, t)
				}
			}
		}
		for _, t := range now {
			if !slices.Contains(old, t) {
				v.backs[t] = insertSorted(v.backs[t], f.Rel)
			}
		}
	}
}

// removeSorted and insertSorted return new slices (copy on write).
func removeSorted(s []string, x string) []string {
	i, ok := slices.BinarySearch(s, x)
	if !ok {
		return s
	}
	out := make([]string, 0, len(s)-1)
	return append(append(out, s[:i]...), s[i+1:]...)
}

func insertSorted(s []string, x string) []string {
	i, ok := slices.BinarySearch(s, x)
	if ok {
		return s
	}
	out := make([]string, 0, len(s)+1)
	out = append(append(out, s[:i]...), x)
	return append(out, s[i:]...)
}

func byLenRel(a, b *File) int {
	if len(a.Rel) != len(b.Rel) {
		return len(a.Rel) - len(b.Rel)
	}
	return strings.Compare(a.Rel, b.Rel)
}

// nameKeys returns the byName keys of f: lower(name), plus lower(basename)
// for notes (possibly the same key twice, as before).
func nameKeys(f *File) []string {
	lname := strings.ToLower(f.Name())
	if f.IsMarkdown() {
		return []string{lname, strings.ToLower(f.Basename())}
	}
	return []string{lname}
}

// resolveLinks resolves f.Links; nil when f has no links.
func (v *Vault) resolveLinks(f *File) []string {
	if len(f.Links) == 0 {
		return nil
	}
	out := make([]string, len(f.Links))
	for i, l := range f.Links {
		if t := v.resolve(l, f.Rel); t != nil {
			out[i] = t.Rel
		}
	}
	return out
}

// distinct returns the non-empty values of s without duplicates, in order.
func distinct(s []string) []string {
	var out []string
	for _, x := range s {
		if x != "" && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// parallel runs fn over [0,n) in chunks of at least min items.
func parallel(n, minChunk int, fn func(lo, hi int)) {
	workers := min(runtime.GOMAXPROCS(0), (n+minChunk-1)/minChunk)
	if workers <= 1 {
		fn(0, n)
		return
	}
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() { fn(n*w/workers, n*(w+1)/workers) })
	}
	wg.Wait()
}

// Files returns every file ordered by path. The slice must not be modified.
func (v *Vault) Files() []*File {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.sorted
}

func (v *Vault) Get(rel string) *File {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.files[rel]
}

// RelOf converts an absolute path to a vault-relative one.
func (v *Vault) RelOf(abs string) (string, bool) {
	rel, err := filepath.Rel(v.Root, filepath.Clean(abs))
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (v *Vault) Abs(rel string) string { return filepath.Join(v.Root, filepath.FromSlash(rel)) }

// Backlinks returns the paths of files linking to rel.
func (v *Vault) Backlinks(rel string) []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.backs[rel]
}

// Resolve finds the file a link target points to, Obsidian style: by path
// from the root, relative to the source, then by (base) name.
func (v *Vault) Resolve(target, from string) *File {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.resolve(target, from)
}

func (v *Vault) resolve(target, from string) *File {
	target = strings.TrimSpace(target)
	if i := strings.IndexAny(target, "#|"); i >= 0 {
		target = target[:i]
	}
	target = strings.TrimPrefix(target, "/")
	if target == "" {
		return nil
	}
	var buf [256]byte
	clean := path.Clean(target) // no allocation when already clean
	if f := v.try(append(buf[:0], clean...)); f != nil {
		return f
	}
	if from != "" {
		// path.Join(dir, target), without allocating in the common case.
		if dir := path.Dir(from); dir != "." {
			b := append(append(append(buf[:0], dir...), '/'), target...)
			if !isCleanPath(b) {
				b = append(buf[:0], path.Join(dir, target)...)
			}
			if f := v.try(b); f != nil {
				return f
			}
		} // else Join(".", target) == Clean(target): already tried
	}
	key := strings.ToLower(path.Base(target))
	cands := v.byName[key]
	if len(cands) == 0 {
		return nil
	}
	// Prefer a candidate whose path ends with the target's path.
	lt := strings.ToLower(target)
	for _, f := range cands {
		lr := strings.ToLower(f.Rel)
		if strings.HasSuffix(strings.TrimSuffix(lr, ".md"), lt) || strings.HasSuffix(lr, lt) {
			return f
		}
	}
	return cands[0]
}

// try looks up p, then p+".md". p must have spare capacity or be a fresh
// buffer: ".md" is appended to it.
func (v *Vault) try(p []byte) *File {
	if f := v.files[string(p)]; f != nil {
		return f
	}
	return v.files[string(append(p, ".md"...))]
}

// isCleanPath reports whether path.Clean(p) == p for a non-empty p.
func isCleanPath(p []byte) bool {
	if len(p) == 0 {
		return false
	}
	start := 0
	if p[0] == '/' {
		if len(p) == 1 {
			return true
		}
		start = 1
	}
	seg := start
	for i := start; i <= len(p); i++ {
		if i < len(p) && p[i] != '/' {
			continue
		}
		switch n := i - seg; {
		case n == 0:
			return false
		case n == 1 && p[seg] == '.', n == 2 && p[seg] == '.' && p[seg+1] == '.':
			return false
		}
		seg = i + 1
	}
	return true
}

// LinkTargets returns the files f's Links resolve to (nil entries for
// unresolved links), aligned with f.Links. ok is false when f is not the
// vault's current entry for its path.
func (v *Vault) LinkTargets(f *File) (targets []*File, ok bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if f == nil || v.files[f.Rel] != f {
		return nil, false
	}
	out := v.outs[f.Rel]
	targets = make([]*File, len(f.Links))
	for i, rel := range out {
		if rel != "" {
			targets[i] = v.files[rel]
		}
	}
	return targets, true
}

// LinksTo reports whether one of from's links resolves to to. ok is false
// when either file is not the vault's current entry for its path.
func (v *Vault) LinksTo(from, to *File) (linked, ok bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if from == nil || to == nil || v.files[from.Rel] != from || v.files[to.Rel] != to {
		return false, false
	}
	_, linked = slices.BinarySearch(v.backs[to.Rel], from.Rel)
	return linked, true
}

// Props returns the front matter of a note.
func (v *Vault) Props(rel string) map[string]any {
	if f := v.Get(rel); f != nil {
		return f.Props
	}
	return nil
}

// ParseNote fills in front matter, tags, links and embeds from note text.
func ParseNote(f *File, text string) {
	text = strings.TrimPrefix(text, "\ufeff") // byte order mark
	if strings.IndexByte(text, '\r') >= 0 {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	// nextLine splits off the first line of s (without its newline).
	nextLine := func(s string) (line, rest string, ok bool) {
		if s == "" {
			return "", "", false
		}
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			return s[:i], s[i+1:], true
		}
		return s, "", true
	}
	body := text
	if first, rest, _ := nextLine(text); strings.TrimSpace(first) == "---" {
		fmStart := rest
		for {
			line, after, ok := nextLine(rest)
			if !ok {
				break
			}
			if t := strings.TrimSpace(line); t == "---" || t == "..." {
				src := fmStart[:len(fmStart)-len(rest)]
				src = strings.TrimSuffix(src, "\n") // lines[1:j] joined by newlines
				fm, err := yaml.Parse(src)
				if err != nil {
					f.Error = err.Error()
				} else if m, ok := fm.(map[string]any); ok {
					f.Props = m
				}
				body = after
				break
			}
			rest = after
		}
	}
	var tags map[string]bool
	addTag := func(t string) {
		t = strings.TrimPrefix(strings.TrimSpace(t), "#")
		if t == "" {
			return
		}
		if tags == nil {
			tags = map[string]bool{}
		}
		if !tags[t] {
			tags[t] = true
			f.Tags = append(f.Tags, t)
		}
	}
	var tagKeys []string // property names are case-insensitive
	for k := range f.Props {
		if lk := strings.ToLower(k); lk == "tags" || lk == "tag" {
			tagKeys = append(tagKeys, k)
		}
	}
	sort.Strings(tagKeys)
	for _, k := range tagKeys {
		switch t := f.Props[k].(type) {
		case string:
			for _, s := range strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == ' ' }) {
				addTag(s)
			}
		case []any:
			for _, s := range t {
				if s, ok := s.(string); ok {
					addTag(s)
				}
			}
		}
	}
	// Links inside front matter values count as links (Obsidian does this).
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case string:
			if strings.Contains(x, "[[") {
				for _, l := range wikiTargets(x) {
					f.Links = append(f.Links, l.target)
				}
			}
		case []any:
			for _, y := range x {
				walk(y)
			}
		case map[string]any:
			for _, y := range x {
				walk(y)
			}
		}
	}
	walk(f.Props)

	fence := ""
	for rest := body; ; {
		line, after, ok := nextLine(rest)
		if !ok {
			break
		}
		rest = after
		t := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			mark := t[:3]
			if fence == "" {
				fence = mark
			} else if fence == mark {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		// Links need a '[' and tags a '#'; most lines have neither.
		hasBracket := strings.IndexByte(line, '[') >= 0
		hasHash := strings.IndexByte(line, '#') >= 0
		if !hasBracket && !hasHash {
			continue
		}
		line = maskCode(line)
		if hasBracket {
			for _, l := range wikiTargets(line) {
				if l.embed {
					f.Embeds = append(f.Embeds, l.target)
				} else {
					f.Links = append(f.Links, l.target)
				}
			}
			for _, l := range mdTargets(line) {
				if l.embed {
					f.Embeds = append(f.Embeds, l.target)
				} else {
					f.Links = append(f.Links, l.target)
				}
			}
		}
		if hasHash {
			for _, t := range inlineTags(line) {
				addTag(t)
			}
		}
	}
}

type linkRef struct {
	target string
	embed  bool
}

func wikiTargets(s string) []linkRef {
	var out []linkRef
	for {
		i := strings.Index(s, "[[")
		if i < 0 {
			return out
		}
		j := strings.Index(s[i+2:], "]]")
		if j < 0 {
			return out
		}
		inner := s[i+2 : i+2+j]
		if !strings.ContainsAny(inner, "[]") {
			t := inner
			if k := strings.IndexAny(t, "#|"); k >= 0 {
				t = t[:k]
			}
			if t = strings.TrimSpace(t); t != "" {
				out = append(out, linkRef{t, i > 0 && s[i-1] == '!'})
			}
		}
		s = s[i+2+j+2:]
	}
}

func mdTargets(s string) []linkRef {
	var out []linkRef
	for i := 0; i < len(s); i++ {
		if s[i] != ']' || i+1 >= len(s) || s[i+1] != '(' {
			continue
		}
		open := strings.LastIndexByte(s[:i], '[')
		end := strings.IndexByte(s[i+2:], ')')
		if open < 0 || end < 0 {
			continue
		}
		raw := strings.Fields(s[i+2 : i+2+end])
		if len(raw) == 0 || strings.Contains(raw[0], "://") || strings.HasPrefix(raw[0], "mailto:") {
			continue
		}
		t, _, _ := strings.Cut(raw[0], "#")
		t = strings.ReplaceAll(t, "%20", " ")
		if t != "" {
			out = append(out, linkRef{t, open > 0 && s[open-1] == '!'})
		}
	}
	return out
}

func inlineTags(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '#' || i > 0 && s[i-1] != ' ' && s[i-1] != '\t' && s[i-1] != '(' {
			continue
		}
		j := i + 1
		for j < len(s) && isTagByte(s[j]) {
			j++
		}
		t := s[i+1 : j]
		if t != "" && strings.Trim(t, "0123456789") != "" {
			out = append(out, t)
		}
		i = j
	}
	return out
}

func isTagByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '/' || c >= 0x80
}

// maskCode blanks inline `code` spans.
func maskCode(s string) string {
	if strings.IndexByte(s, '`') < 0 {
		return s
	}
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] != '`' {
			continue
		}
		j := strings.IndexByte(s[i+1:], '`')
		if j < 0 {
			break
		}
		for k := i; k <= i+1+j; k++ {
			b[k] = ' '
		}
		i += j + 1
	}
	return string(b)
}
