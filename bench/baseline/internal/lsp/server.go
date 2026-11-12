// Package lsp implements the language server for a markdown notes workspace.
package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"brain/internal/workspace"
)

type CompletionConfig struct {
	// WikiLinks completes note names (and headings after '#') inside [[ ]].
	WikiLinks bool `json:"wikiLinks"`
	// AtLinks completes note names after '@' and rewrites the mention to [[note]].
	AtLinks bool `json:"atLinks"`
}

type Config struct {
	Completion CompletionConfig `json:"completion"`
	// HoverLines caps the number of lines shown in a hover preview.
	HoverLines int `json:"hoverLines"`
}

func DefaultConfig() Config {
	return Config{Completion: CompletionConfig{WikiLinks: true, AtLinks: true}, HoverLines: 40}
}

// merge overlays the fields present in raw. Settings may be wrapped in {"brain": {...}}.
func (c *Config) merge(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var wrapped struct {
		Brain json.RawMessage `json:"brain"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && len(wrapped.Brain) > 0 {
		raw = wrapped.Brain
	}
	_ = json.Unmarshal(raw, c)
	if c.HoverLines <= 0 {
		c.HoverLines = DefaultConfig().HoverLines
	}
}

type Server struct {
	conn        *conn
	ws          *workspace.Workspace
	cfg         Config
	initialized bool
	shutdown    bool
}

// ErrExit is returned by Run after an "exit" notification that followed "shutdown".
var ErrExit = errors.New("exit")

func NewServer(r io.Reader, w io.Writer) *Server {
	return &Server{conn: &conn{r: bufio.NewReader(r), w: w}, cfg: DefaultConfig(), ws: workspace.New("")}
}

// Run serves requests until the client exits or the stream closes.
func (s *Server) Run() error {
	for {
		m, err := s.conn.read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if m.Method == "" { // response to a server request, or garbage
			continue
		}
		if m.Method == "exit" {
			if s.shutdown {
				return ErrExit
			}
			return errors.New("exit without shutdown")
		}
		result, rerr := s.dispatch(m.Method, m.Params)
		if len(m.ID) > 0 {
			if err := s.conn.reply(m.ID, result, rerr); err != nil {
				return err
			}
		}
	}
}

func (s *Server) dispatch(method string, params json.RawMessage) (any, *responseError) {
	if method == "initialize" {
		return s.initialize(params)
	}
	if !s.initialized {
		return nil, &responseError{codeNotInitialized, "server not initialized"}
	}
	decode := func(v any) *responseError {
		if err := json.Unmarshal(params, v); err != nil {
			return &responseError{codeInvalidParams, err.Error()}
		}
		return nil
	}
	var pos TextDocumentPositionParams
	switch method {
	case "textDocument/completion", "textDocument/definition", "textDocument/hover", "textDocument/documentSymbol":
		if e := decode(&pos); e != nil {
			return nil, e
		}
	}
	switch method {
	case "initialized", "$/setTrace", "$/cancelRequest":
		return nil, nil
	case "shutdown":
		s.shutdown = true
		return nil, nil
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		if decode(&p) == nil {
			s.didOpen(uriToPath(p.TextDocument.URI), p.TextDocument.Text)
		}
	case "textDocument/didChange":
		var p struct {
			TextDocument   TextDocumentIdentifier `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if decode(&p) == nil && len(p.ContentChanges) > 0 {
			if path := uriToPath(p.TextDocument.URI); workspace.IsMarkdown(path) {
				s.ws.Set(path, p.ContentChanges[len(p.ContentChanges)-1].Text, true)
			}
		}
	case "textDocument/didClose":
		var p struct {
			TextDocument TextDocumentIdentifier `json:"textDocument"`
		}
		if decode(&p) == nil {
			path := uriToPath(p.TextDocument.URI)
			if d := s.ws.Get(path); d != nil {
				d.Open = false
				s.ws.Load(path) // drop unsaved edits; removes the doc if it no longer exists
			}
		}
	case "workspace/didChangeWatchedFiles":
		var p struct {
			Changes []struct {
				URI  string `json:"uri"`
				Type int    `json:"type"`
			} `json:"changes"`
		}
		if decode(&p) == nil {
			for _, c := range p.Changes {
				path := uriToPath(c.URI)
				if !workspace.IsMarkdown(path) {
					continue
				}
				if c.Type == 3 { // deleted
					if d := s.ws.Get(path); d == nil || !d.Open {
						s.ws.Remove(path)
					}
				} else {
					s.ws.Load(path)
				}
			}
		}
	case "workspace/didChangeConfiguration":
		var p struct {
			Settings json.RawMessage `json:"settings"`
		}
		if decode(&p) == nil {
			s.cfg.merge(p.Settings)
		}
	case "textDocument/completion":
		return s.completion(pos), nil
	case "textDocument/definition":
		return s.definition(pos), nil
	case "textDocument/hover":
		return s.hover(pos), nil
	case "textDocument/references":
		var p ReferenceParams
		if e := decode(&p); e != nil {
			return nil, e
		}
		return s.references(p), nil
	case "textDocument/documentSymbol":
		return s.documentSymbols(pos.TextDocument.URI), nil
	case "workspace/symbol":
		var p struct {
			Query string `json:"query"`
		}
		if e := decode(&p); e != nil {
			return nil, e
		}
		return s.workspaceSymbols(p.Query), nil
	default:
		if strings.HasPrefix(method, "$/") || strings.HasPrefix(method, "textDocument/did") {
			return nil, nil
		}
		return nil, &responseError{codeMethodNotFound, "method not found: " + method}
	}
	return nil, nil
}

func (s *Server) initialize(params json.RawMessage) (any, *responseError) {
	var p struct {
		RootURI               string          `json:"rootUri"`
		RootPath              string          `json:"rootPath"`
		InitializationOptions json.RawMessage `json:"initializationOptions"`
		WorkspaceFolders      []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &responseError{codeInvalidParams, err.Error()}
	}
	root := p.RootPath
	if p.RootURI != "" {
		root = uriToPath(p.RootURI)
	} else if len(p.WorkspaceFolders) > 0 {
		root = uriToPath(p.WorkspaceFolders[0].URI)
	}
	s.cfg.merge(p.InitializationOptions)
	s.ws = workspace.New(root)
	_ = s.ws.Scan()
	s.initialized = true

	var triggers []string
	if s.cfg.Completion.WikiLinks {
		triggers = append(triggers, "[", "#")
	}
	if s.cfg.Completion.AtLinks {
		triggers = append(triggers, "@")
	}
	caps := map[string]any{
		"textDocumentSync": map[string]any{
			"openClose": true,
			"change":    1, // full
			"save":      false,
		},
		"definitionProvider":      true,
		"hoverProvider":           true,
		"referencesProvider":      true,
		"documentSymbolProvider":  true,
		"workspaceSymbolProvider": true,
	}
	if len(triggers) > 0 {
		caps["completionProvider"] = map[string]any{"triggerCharacters": triggers}
	}
	return map[string]any{
		"capabilities": caps,
		"serverInfo":   map[string]string{"name": "brain"},
	}, nil
}

func (s *Server) didOpen(path, text string) {
	if !workspace.IsMarkdown(path) {
		return
	}
	if s.ws.Root == "" { // no workspace given: index the file's directory
		s.ws = workspace.New(filepath.Dir(path))
		_ = s.ws.Scan()
	}
	s.ws.Set(path, text, true)
}

// locate returns the document and the byte column for a request position.
func (s *Server) locate(p TextDocumentPositionParams) (*workspace.Document, int, bool) {
	d := s.ws.Get(uriToPath(p.TextDocument.URI))
	if d == nil || p.Position.Line < 0 || p.Position.Line >= len(d.Doc.Lines) {
		return nil, 0, false
	}
	return d, utf16ToByte(d.Doc.Lines[p.Position.Line], p.Position.Character), true
}

func pos(d *workspace.Document, line, col int) Position {
	if line >= len(d.Doc.Lines) {
		return Position{Line: line}
	}
	return Position{line, byteToUTF16(d.Doc.Lines[line], col)}
}

func lineEnd(d *workspace.Document, line int) Position {
	if line >= len(d.Doc.Lines) || line < 0 {
		return Position{Line: max(line, 0)}
	}
	return pos(d, line, len(d.Doc.Lines[line]))
}
