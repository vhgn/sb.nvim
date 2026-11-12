package lsp

import (
	"fmt"
	"strings"

	"brain/internal/md"
	"brain/internal/workspace"
)

// ---- completion ----

func (s *Server) completion(p TextDocumentPositionParams) CompletionList {
	list := CompletionList{Items: []CompletionItem{}}
	d, col, ok := s.locate(p)
	if !ok {
		return list
	}
	line := d.Doc.Lines[p.Position.Line]
	before, after := line[:col], line[col:]
	rng := func(start int) Range {
		return Range{pos(d, p.Position.Line, start), p.Position}
	}

	if s.cfg.Completion.WikiLinks {
		if i := strings.LastIndex(before, "[["); i >= 0 && !strings.Contains(before[i:], "]]") {
			query := before[i+2:]
			if strings.Contains(query, "|") {
				return list // typing an alias
			}
			closing := "]]"
			if strings.HasPrefix(after, "]]") {
				closing = ""
			}
			if name, _, isHeading := strings.Cut(query, "#"); isHeading {
				target := s.ws.ResolveName(d, name)
				if target == nil {
					return list
				}
				start := i + 2 + len(name) + 1
				for n, h := range target.Doc.Headings {
					list.Items = append(list.Items, CompletionItem{
						Label:      h.Text,
						Kind:       completionKindReference,
						Detail:     strings.Repeat("#", h.Level) + " " + h.Text,
						FilterText: h.Text,
						SortText:   fmt.Sprintf("%05d", n),
						TextEdit:   &TextEdit{rng(start), h.Text + closing},
					})
				}
				return list
			}
			for _, t := range s.ws.Docs() {
				if t == d {
					continue
				}
				short := s.ws.ShortName(t)
				list.Items = append(list.Items, s.noteItem(t, short, short, &TextEdit{rng(i + 2), short + closing}))
			}
			return list
		}
	}

	if s.cfg.Completion.AtLinks {
		if i := strings.LastIndex(before, "@"); i >= 0 && isMentionStart(before, i) {
			query := before[i+1:]
			if strings.ContainsAny(query, " \t[]()`") {
				return list
			}
			for _, t := range s.ws.Docs() {
				if t == d {
					continue
				}
				short := s.ws.ShortName(t)
				// The edit replaces "@query" with a wiki link, so the filter text
				// includes the '@' to match what is already in the buffer.
				//
				// The item is sent as a snippet: clients such as Neovim's built-in
				// completion otherwise use newText ("[[note]]") as the word the
				// popup filters on, which never matches the typed "@q". For
				// snippets they filter on filterText and apply the edit on accept.
				item := s.noteItem(t, short, "@"+short, &TextEdit{rng(i), "[[" + escapeSnippet(short) + "]]"})
				item.InsertTextFormat = insertTextFormatSnippet
				list.Items = append(list.Items, item)
			}
		}
	}
	return list
}

var snippetEscaper = strings.NewReplacer(`\`, `\\`, `$`, `\$`, `}`, `\}`)

func escapeSnippet(s string) string { return snippetEscaper.Replace(s) }

// isMentionStart reports whether the '@' at i starts a mention rather than
// being part of an email address or other word.
func isMentionStart(s string, i int) bool {
	if i == 0 {
		return true
	}
	switch s[i-1] {
	case ' ', '\t', '(', '[', '>', '*', '_', '"', '\'':
		return true
	}
	return false
}

func (s *Server) noteItem(t *workspace.Document, label, filter string, edit *TextEdit) CompletionItem {
	detail := t.Name
	if t.Doc.Title != "" && t.Doc.Title != label {
		detail = t.Doc.Title + " — " + t.Name
	}
	return CompletionItem{
		Label:         label,
		Kind:          completionKindFile,
		Detail:        detail,
		FilterText:    filter,
		TextEdit:      edit,
		Documentation: &MarkupContent{"markdown", s.preview(t, -1, 10)},
	}
}

// ---- navigation ----

func (s *Server) linkAt(p TextDocumentPositionParams) (*workspace.Document, *md.Link) {
	d, col, ok := s.locate(p)
	if !ok {
		return nil, nil
	}
	return d, d.Doc.LinkAt(p.Position.Line, col)
}

func target(t *workspace.Document, heading int) Location {
	r := Range{}
	if heading >= 0 {
		h := t.Doc.Headings[heading]
		r = Range{pos(t, h.Span.Line, h.Span.Start), pos(t, h.Span.Line, h.Span.End)}
	}
	return Location{pathToURI(t.Path), r}
}

func (s *Server) definition(p TextDocumentPositionParams) *Location {
	d, l := s.linkAt(p)
	if l == nil {
		return nil
	}
	t, h := s.ws.Resolve(d, *l)
	if t == nil {
		return nil
	}
	loc := target(t, h)
	return &loc
}

func (s *Server) hover(p TextDocumentPositionParams) *Hover {
	d, l := s.linkAt(p)
	if l == nil {
		return nil
	}
	r := Range{pos(d, l.Span.Line, l.Span.Start), pos(d, l.Span.Line, l.Span.End)}
	t, h := s.ws.Resolve(d, *l)
	var text string
	if t == nil {
		text = fmt.Sprintf("*No note found for* `%s`", l.Target)
	} else {
		title := t.Name
		if h >= 0 {
			title += " › " + t.Doc.Headings[h].Text
		}
		text = "**" + title + "**\n\n---\n\n" + s.preview(t, h, s.cfg.HoverLines)
	}
	return &Hover{MarkupContent{"markdown", text}, &r}
}

// preview returns the document body, or the section under heading, capped at limit lines.
func (s *Server) preview(t *workspace.Document, heading, limit int) string {
	lines := t.Doc.Lines
	start, end := t.Doc.BodyStart, len(lines)-1
	if heading >= 0 {
		start, end = t.Doc.Headings[heading].Span.Line, t.Doc.SectionEnd(heading)
	}
	if start > end {
		return ""
	}
	body := lines[start : end+1]
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	for len(body) > 0 && strings.TrimSpace(body[0]) == "" {
		body = body[1:]
	}
	truncated := len(body) > limit
	if truncated {
		body = body[:limit]
	}
	out := strings.Join(body, "\n")
	if truncated {
		out += "\n\n…"
	}
	return out
}

func (s *Server) references(p ReferenceParams) []Location {
	locs := []Location{}
	d, col, ok := s.locate(p.TextDocumentPositionParams)
	if !ok {
		return locs
	}
	t, h := d, -1
	if l := d.Doc.LinkAt(p.Position.Line, col); l != nil {
		if t, h = s.ws.Resolve(d, *l); t == nil {
			return locs
		}
	} else {
		h = d.Doc.HeadingAt(p.Position.Line)
	}
	if p.Context.IncludeDeclaration {
		locs = append(locs, target(t, h))
	}
	for _, ref := range s.ws.Backlinks(t, h) {
		sp := ref.Link.Span
		locs = append(locs, Location{pathToURI(ref.Doc.Path), Range{pos(ref.Doc, sp.Line, sp.Start), pos(ref.Doc, sp.Line, sp.End)}})
	}
	return locs
}

// ---- symbols ----

func (s *Server) documentSymbols(uri string) []DocumentSymbol {
	d := s.ws.Get(uriToPath(uri))
	if d == nil {
		return []DocumentSymbol{}
	}
	syms, _ := buildSymbols(d, 0, 0)
	if syms == nil {
		syms = []DocumentSymbol{}
	}
	return syms
}

// buildSymbols nests headings by level, consuming those deeper than parentLevel.
func buildSymbols(d *workspace.Document, i, parentLevel int) ([]DocumentSymbol, int) {
	hs := d.Doc.Headings
	var out []DocumentSymbol
	for i < len(hs) && hs[i].Level > parentLevel {
		h, idx := hs[i], i
		var children []DocumentSymbol
		children, i = buildSymbols(d, i+1, h.Level)
		out = append(out, DocumentSymbol{
			Name:           h.Text,
			Detail:         strings.Repeat("#", h.Level),
			Kind:           symbolKindString,
			Range:          Range{Position{h.Span.Line, 0}, lineEnd(d, d.Doc.SectionEnd(idx))},
			SelectionRange: Range{pos(d, h.Span.Line, h.Span.Start), pos(d, h.Span.Line, h.Span.End)},
			Children:       children,
		})
	}
	return out, i
}

func (s *Server) workspaceSymbols(query string) []SymbolInformation {
	out := []SymbolInformation{}
	for _, d := range s.ws.Docs() {
		if fuzzy(d.Name, query) {
			out = append(out, SymbolInformation{Name: d.Name, Kind: symbolKindFile, Location: Location{pathToURI(d.Path), Range{}}})
		}
		for i, h := range d.Doc.Headings {
			if fuzzy(h.Text, query) {
				out = append(out, SymbolInformation{Name: h.Text, Kind: symbolKindString, Location: target(d, i), ContainerName: d.Name})
			}
		}
	}
	return out
}

// fuzzy reports whether the runes of query appear in order in s, ignoring case.
func fuzzy(s, query string) bool {
	s, query = strings.ToLower(s), strings.ToLower(query)
	for _, r := range query {
		i := strings.IndexRune(s, r)
		if i < 0 {
			return false
		}
		s = s[i+len(string(r)):]
	}
	return true
}
