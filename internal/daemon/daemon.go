// Package daemon runs one background indexer per vault root. Editors talk to
// it over a unix socket with newline-delimited JSON:
//
//	→ {"id":1,"method":"query","params":{...}}
//	← {"id":1,"result":{...}}  or  {"id":1,"error":"..."}
//
// A second daemon for the same root exits immediately (flock), so any number
// of editors can call Ensure and share one process and one cache.
package daemon

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"brain/internal/bases"
	"brain/internal/tex"
	"brain/internal/vault"
)

// RuntimeDir is where sockets and locks live.
func RuntimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "brain")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("brain-%d", os.Getuid()))
}

// CacheDir is where indexes and rendered images are persisted.
func CacheDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "brain")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "brain")
}

// Key identifies a root; the editor computes the same value (sha256, 16 hex chars).
func Key(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return hex.EncodeToString(sum[:])[:16]
}

func SocketPath(root string) string { return filepath.Join(RuntimeDir(), Key(root)+".sock") }

// freshFor is how old the index may be before a request rescans the vault.
var freshFor = 2 * time.Second

type Server struct {
	v       *vault.Vault
	root    string
	idle    time.Duration
	clients atomic.Int64
	lastUse atomic.Int64
	cache   string
	mu      sync.Mutex // serializes renders to the same file

	scanMu   sync.Mutex
	scanning chan struct{} // closed when the rescan in flight finishes
}

// scanIfOlder rescans when the index is older than d. Requests arriving while
// a rescan is in flight wait for it instead of walking the vault again (an
// editor asks for every base of a note at once).
func (s *Server) scanIfOlder(d time.Duration) {
	s.scanMu.Lock()
	if wait := s.scanning; wait != nil {
		s.scanMu.Unlock()
		<-wait
		return
	}
	done := make(chan struct{})
	s.scanning = done
	s.scanMu.Unlock()
	defer func() {
		s.scanMu.Lock()
		s.scanning = nil
		s.scanMu.Unlock()
		close(done)
	}()
	s.v.ScanIfOlder(d)
}

// Run serves root until idle for the given duration with no clients.
// It returns nil without serving when another daemon owns the root.
func Run(root string, idle time.Duration) error {
	root = filepath.Clean(root)
	if err := os.MkdirAll(RuntimeDir(), 0o700); err != nil {
		return err
	}
	sock := SocketPath(root)
	lock, err := os.OpenFile(sock+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil // another daemon serves this root
	}
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	defer os.Remove(sock)
	_ = lock.Truncate(0)
	_, _ = lock.WriteAt([]byte(fmt.Sprintf("%d %s\n", os.Getpid(), root)), 0)

	s := &Server{root: root, idle: idle, cache: CacheDir()}
	s.v = vault.New(root, s.cache)
	s.v.LoadCache()
	s.touch()

	// Index in the background so clients can connect right away.
	ready := make(chan struct{})
	go func() {
		s.v.Scan()
		_ = s.v.SaveCache()
		close(ready)
	}()
	go s.housekeeping(ln, ready)

	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.clients.Add(1)
		s.touch()
		go func() {
			defer s.clients.Add(-1)
			defer s.touch()
			s.serve(c, ready)
		}()
	}
}

func (s *Server) touch() { s.lastUse.Store(time.Now().UnixNano()) }

func (s *Server) housekeeping(ln net.Listener, ready chan struct{}) {
	<-ready
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for range t.C {
		if s.clients.Load() > 0 {
			s.scanIfOlder(8 * time.Second)
		}
		_ = s.v.SaveCache()
		if s.idle > 0 && s.clients.Load() == 0 && time.Since(time.Unix(0, s.lastUse.Load())) > s.idle {
			ln.Close()
			return
		}
	}
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func (s *Server) serve(c net.Conn, ready chan struct{}) {
	defer c.Close()
	r := bufio.NewReaderSize(c, 1<<16)
	var wmu sync.Mutex
	enc := json.NewEncoder(c)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var req request
			if jerr := json.Unmarshal(line, &req); jerr != nil {
				wmu.Lock()
				_ = enc.Encode(response{Error: "bad request: " + jerr.Error()})
				wmu.Unlock()
			} else {
				go func() {
					s.touch()
					res, herr := s.safeHandle(req, ready)
					if len(req.ID) == 0 {
						return
					}
					out := response{ID: req.ID, Result: res}
					if herr != nil {
						out.Error = herr.Error()
						out.Result = nil
					}
					wmu.Lock()
					_ = enc.Encode(out)
					wmu.Unlock()
				}()
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("read: %v", err)
			}
			return
		}
	}
}

// touchable keeps absolute paths of files strictly inside the root: the
// root itself or any directory would otherwise be indexed as a file.
func (s *Server) touchable(paths []string) []string {
	var out []string
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			continue
		}
		rel, err := filepath.Rel(s.root, filepath.Clean(p))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			continue
		}
		out = append(out, p)
	}
	return out
}

// safeHandle turns a panic in one request into an error reply: the daemon
// is shared by every editor on the root and must outlive bad input.
func (s *Server) safeHandle(req request, ready chan struct{}) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in %s: %v\n%s", req.Method, r, debug.Stack())
			res, err = nil, fmt.Errorf("internal error in %s: %v", req.Method, r)
		}
	}()
	return s.handle(req, ready)
}

func (s *Server) handle(req request, ready chan struct{}) (any, error) {
	switch req.Method {
	case "ping":
		return map[string]any{"root": s.root, "pid": os.Getpid()}, nil
	case "shutdown":
		go func() { time.Sleep(50 * time.Millisecond); os.Exit(0) }()
		return true, nil
	case "tex":
		return s.tex(req.Params)
	case "texUnicode":
		var p struct {
			Items []string `json:"items"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		out := make([]unicodeResult, len(p.Items))
		for i, src := range p.Items {
			t, exact, err := tex.UnicodeCheck(src)
			out[i] = unicodeResult{Text: t, Exact: exact}
			if err != nil {
				out[i].Error = err.Error()
			}
		}
		return out, nil
	}

	<-ready
	switch req.Method {
	case "touch":
		var p struct {
			Paths []string `json:"paths"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		s.v.Touch(s.touchable(p.Paths)...)
		return true, nil
	case "rescan":
		return s.v.Scan(), nil
	case "status":
		return map[string]any{"root": s.root, "files": len(s.v.Files()), "pid": os.Getpid(), "clients": s.clients.Load(), "cache": s.v.CachePath}, nil
	case "frontmatter":
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		rel, ok := s.v.RelOf(p.Path)
		if !ok {
			return nil, fmt.Errorf("%s is outside %s", p.Path, s.root)
		}
		f := s.v.Get(rel)
		if f == nil {
			return nil, nil
		}
		return f, nil
	case "resolve":
		var p struct {
			Targets []string `json:"targets"`
			From    string   `json:"from"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		s.scanIfOlder(freshFor)
		from, _ := s.v.RelOf(p.From)
		out := make([]string, len(p.Targets))
		for i, t := range p.Targets {
			if f := s.v.Resolve(t, from); f != nil {
				out[i] = s.v.Abs(f.Rel)
			}
		}
		return out, nil
	case "query":
		return s.query(req.Params)
	}
	return nil, fmt.Errorf("unknown method %q", req.Method)
}

type unicodeResult struct {
	Text  string `json:"text"`
	Exact bool   `json:"exact"`
	Error string `json:"error,omitempty"`
}

type queryParams struct {
	Path   string `json:"path"`   // .base file (absolute)
	Source string `json:"source"` // inline base YAML; overrides reading Path
	View   string `json:"view"`
	This   string `json:"this"` // absolute path of the embedding note
}

func (s *Server) query(raw json.RawMessage) (any, error) {
	var p queryParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	src := p.Source
	if src == "" {
		b, err := os.ReadFile(p.Path)
		if err != nil {
			return nil, err
		}
		src = string(b)
	}
	s.scanIfOlder(freshFor)
	base, err := bases.ParseBase(src)
	if err != nil {
		return nil, err
	}
	this := p.This
	if this == "" {
		this = p.Path
	}
	rel, _ := s.v.RelOf(this)
	return base.Run(s.v, bases.Options{View: p.View, This: rel}), nil
}

type texParams struct {
	Src     string            `json:"src"`
	Display bool              `json:"display"`
	FG      string            `json:"fg"`
	Error   string            `json:"error"`
	Size    float64           `json:"size"`
	Font    string            `json:"font"`
	Palette map[string]string `json:"palette"`
}

func (s *Server) tex(raw json.RawMessage) (any, error) {
	var p texParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return RenderTeX(s.cache, p.Src, tex.Options{Display: p.Display, FG: p.FG, Error: p.Error, Size: p.Size, Font: p.Font, Palette: p.Palette})
}

// pngOf remembers which png each set of inputs rendered to, so a formula
// seen before skips layout and SVG generation (the file name is the hash of
// the SVG, which can only be known by building it).
var pngOf = struct {
	sync.Mutex
	m map[[32]byte]string
}{m: map[[32]byte]string{}}

func texKey(cacheDir, src string, opt tex.Options) [32]byte {
	h := sha256.New()
	names := make([]string, 0, len(opt.Palette))
	for k := range opt.Palette {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Fprintf(h, "%q %q %v %q %q %v %q %v %d\n", cacheDir, src, opt.Display, opt.FG, opt.Error, opt.Size, opt.Font, opt.Scale, len(names))
	for _, k := range names {
		fmt.Fprintf(h, "%q %q\n", k, opt.Palette[k])
	}
	var key [32]byte
	h.Sum(key[:0])
	return key
}

func rememberPNG(key [32]byte, png string) {
	pngOf.Lock()
	if len(pngOf.m) >= 50000 {
		clear(pngOf.m)
	}
	pngOf.m[key] = png
	pngOf.Unlock()
}

// RenderTeX renders math to a cached PNG and returns its path.
func RenderTeX(cacheDir, src string, opt tex.Options) (map[string]any, error) {
	key := texKey(cacheDir, src, opt)
	pngOf.Lock()
	known, ok := pngOf.m[key]
	pngOf.Unlock()
	if ok {
		if _, err := os.Stat(known); err == nil {
			return map[string]any{"png": known}, nil
		}
	}
	svg, err := tex.SVG(src, opt)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(svg))
	dir := filepath.Join(cacheDir, "tex")
	png := filepath.Join(dir, hex.EncodeToString(sum[:12])+".png")
	if _, err := os.Stat(png); err == nil {
		rememberPNG(key, png)
		return map[string]any{"png": png}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	svgPath := strings.TrimSuffix(png, ".png") + ".svg"
	// Concurrent requests may render the same formula: every render works on
	// its own temp files and renames them into place atomically.
	svgTmp, err := writeTemp(dir, "*.svg", []byte(svg))
	if err != nil {
		return nil, err
	}
	defer os.Remove(svgTmp)
	pngTmp, err := writeTemp(dir, "*.png", nil)
	if err != nil {
		return nil, err
	}
	defer os.Remove(pngTmp)
	var cmd *exec.Cmd
	if p, err := exec.LookPath("rsvg-convert"); err == nil {
		cmd = exec.Command(p, "-b", "none", "-o", pngTmp, svgTmp)
	} else if p, err := exec.LookPath("magick"); err == nil {
		cmd = exec.Command(p, "-background", "none", svgTmp, pngTmp)
	} else {
		_ = os.Rename(svgTmp, svgPath)
		return map[string]any{"svg": svgPath}, fmt.Errorf("need rsvg-convert or magick to rasterize math")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s: %v: %s", filepath.Base(cmd.Path), err, bytes.TrimSpace(out))
	}
	if err := os.Rename(svgTmp, svgPath); err != nil {
		return nil, err
	}
	if err := os.Rename(pngTmp, png); err != nil {
		return nil, err
	}
	rememberPNG(key, png)
	return map[string]any{"png": png, "svg": svgPath}, nil
}

func writeTemp(dir, pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	_ = f.Chmod(0o644)
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		os.Remove(f.Name())
		return "", werr
	}
	if cerr != nil {
		os.Remove(f.Name())
		return "", cerr
	}
	return f.Name(), nil
}
