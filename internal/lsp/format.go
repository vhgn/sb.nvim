package lsp

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"brain/internal/workspace"
)

// Formatting hard-wraps prose lines longer than the configured width. It only
// ever splits lines: short lines are never joined, words are never cut, and
// links, inline code and inline math are kept on one line. Front matter,
// fenced and indented code, math blocks, headings, tables, HTML and link
// reference definitions are left alone.

var (
	// leading blockquote markers, indentation, list marker and task box
	prefixRe  = regexp.MustCompile(`^((?:[ \t]*>[ \t]?)*)([ \t]*)((?:[-+*]|\d{1,9}[.)])[ \t]+(?:\[[ xX]\][ \t]+)?)?`)
	headingRe = regexp.MustCompile(`^#{1,6}(\s|$)`)
	linkDefRe = regexp.MustCompile(`^\[[^\]]+\]:`)
	ruleRe    = regexp.MustCompile(`^([-*_])(\s*[-*_]){2,}\s*$`)
	// a word that would start a new block if it began a line
	blockStartRe = regexp.MustCompile(`^([-+*]|\d{1,9}[.)]|#{1,6}|>.*|\|.*|=+|-+)$`)
)

func cells(s string) int { return utf8.RuneCountInString(s) }

// formatLines returns the lines of text wrapped at width. changed[i] lists
// the lines replacing input line i, or is nil when the line is unchanged.
func formatLines(lines []string, width int) (changed map[int][]string) {
	changed = map[int][]string{}
	if width <= 0 {
		return changed
	}
	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for j := 1; j < len(lines); j++ {
			if t := strings.TrimSpace(lines[j]); t == "---" || t == "..." {
				i = j + 1
				break
			}
		}
	}
	fence := ""     // open ``` or ~~~ run
	inMath := false // inside a $$ block
	prevBlank := true
	inIndentCode := false
	for ; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		blank := trimmed == ""
		wasBlank := prevBlank
		prevBlank = blank
		if fence != "" {
			if strings.HasPrefix(trimmed, fence[:1]) && strings.Trim(trimmed, fence[:1]) == "" && len(trimmed) >= len(fence) {
				fence = ""
			}
			continue
		}
		if inMath {
			if strings.Contains(line, "$$") {
				inMath = false
			}
			continue
		}
		if f := fenceRun(trimmed); f != "" && indentOf(line) <= 3 {
			fence = f
			continue
		}
		if strings.HasPrefix(trimmed, "$$") {
			if strings.Count(trimmed, "$$") == 1 {
				inMath = true
			}
			continue
		}
		if blank {
			inIndentCode = false
			continue
		}
		m := prefixRe.FindStringSubmatch(line)
		listItem := m[3] != ""
		if !listItem && m[1] == "" && indentOf(line) >= 4 && (wasBlank || inIndentCode) {
			inIndentCode = true
			continue
		}
		inIndentCode = false
		if cells(line) <= width {
			continue
		}
		body := line[len(m[0]):]
		bt := strings.TrimSpace(body)
		if headingRe.MatchString(bt) || strings.HasPrefix(bt, "|") || strings.HasPrefix(bt, "<") ||
			linkDefRe.MatchString(bt) || ruleRe.MatchString(bt) {
			continue
		}
		cont := m[1] + strings.Repeat(" ", cells(m[2])+cells(m[3]))
		if out := wrapLine(m[0], cont, body, width); len(out) > 1 {
			changed[i] = out
		}
	}
	return changed
}

func indentOf(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

func fenceRun(trimmed string) string {
	for _, c := range []string{"`", "~"} {
		if strings.HasPrefix(trimmed, c+c+c) {
			n := len(trimmed) - len(strings.TrimLeft(trimmed, c))
			if c == "`" && strings.Contains(trimmed[n:], "`") {
				return "" // not a fence: ``` with backticks in the info string
			}
			return trimmed[:n]
		}
	}
	return ""
}

// words splits body at spaces outside links, inline code and inline math.
func words(body string) []string {
	var out []string
	start := -1
	for i := 0; i < len(body); {
		c := body[i]
		if c == ' ' || c == '\t' {
			if start >= 0 {
				out = append(out, body[start:i])
				start = -1
			}
			i++
			continue
		}
		if start < 0 {
			start = i
		}
		i = skipSpan(body, i)
	}
	if start >= 0 {
		out = append(out, body[start:])
	}
	return out
}

// skipSpan returns the index after the unbreakable span starting at i, or
// i+1 when none starts there.
func skipSpan(s string, i int) int {
	switch s[i] {
	case '`':
		n := len(s[i:]) - len(strings.TrimLeft(s[i:], "`"))
		ticks := s[i : i+n]
		if j := strings.Index(s[i+n:], ticks); j >= 0 {
			return i + n + j + n
		}
		return i + n
	case '$':
		if j := strings.IndexByte(s[i+1:], '$'); j > 0 {
			return i + 1 + j + 1
		}
	case '[':
		if strings.HasPrefix(s[i:], "[[") {
			if j := strings.Index(s[i+2:], "]]"); j >= 0 {
				return i + 2 + j + 2
			}
			return i + 2
		}
		// [text](dest) or [text][ref]
		if j := strings.IndexByte(s[i+1:], ']'); j >= 0 {
			k := i + 1 + j + 1
			if k < len(s) && (s[k] == '(' || s[k] == '[') {
				closer := byte(')')
				if s[k] == '[' {
					closer = ']'
				}
				if e := strings.IndexByte(s[k+1:], closer); e >= 0 {
					return k + 1 + e + 1
				}
			}
		}
	case '<':
		if j := strings.IndexByte(s[i+1:], '>'); j > 0 && !strings.ContainsAny(s[i+1:i+1+j], " \t") {
			return i + 1 + j + 1
		}
	}
	return i + 1
}

// wrapLine greedily fills lines of at most width cells. A word longer than
// the width gets a line of its own; a line never starts with a word that
// would turn it into a list item, heading, quote or table row.
func wrapLine(first, cont, body string, width int) []string {
	ws := words(body)
	if len(ws) == 0 {
		return nil
	}
	var out []string
	cur := first + ws[0]
	for _, w := range ws[1:] {
		if cells(cur)+1+cells(w) > width && cells(cur) > cells(cont) && !blockStartRe.MatchString(w) {
			out = append(out, cur)
			cur = cont + w
		} else {
			cur += " " + w
		}
	}
	if strings.HasSuffix(body, "  ") { // keep a trailing hard line break
		cur += "  "
	}
	return append(out, cur)
}

func (s *Server) formatting(uri string) []TextEdit {
	edits := []TextEdit{}
	d := s.ws.Get(uriToPath(uri))
	if d == nil {
		return edits
	}
	changed := formatLines(d.Doc.Lines, s.cfg.Format.Width)
	for i := range d.Doc.Lines {
		if out, ok := changed[i]; ok {
			edits = append(edits, lineEdit(d, i, strings.Join(out, "\n")))
		}
	}
	return edits
}

func lineEdit(d *workspace.Document, i int, text string) TextEdit {
	return TextEdit{Range{Position{i, 0}, pos(d, i, len(d.Doc.Lines[i]))}, text}
}
