package lsp

import (
	"net/url"
	"path/filepath"
	"unicode/utf8"
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type ReferenceParams struct {
	TextDocumentPositionParams
	Context struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type CompletionItem struct {
	Label         string         `json:"label"`
	Kind          int            `json:"kind,omitempty"`
	Detail        string         `json:"detail,omitempty"`
	Documentation *MarkupContent `json:"documentation,omitempty"`
	FilterText    string         `json:"filterText,omitempty"`
	SortText      string         `json:"sortText,omitempty"`
	TextEdit      *TextEdit      `json:"textEdit,omitempty"`
	// InsertTextFormat 2 marks TextEdit.NewText as a snippet.
	InsertTextFormat int `json:"insertTextFormat,omitempty"`
	// Data is echoed back by completionItem/resolve.
	Data *CompletionData `json:"data,omitempty"`
}

// CompletionData identifies the note whose preview completionItem/resolve fills in.
type CompletionData struct {
	Path string `json:"path"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

const (
	insertTextFormatSnippet = 2
	completionKindFile      = 17
	completionKindReference = 18
	symbolKindFile          = 1
	symbolKindString        = 15
)

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(u.Path))
}

func pathToURI(p string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(p)}).String()
}

// LSP positions count UTF-16 code units; the parser works in bytes.

func utf16ToByte(s string, u int) int {
	n := 0
	for i, r := range s {
		if n >= u {
			return i
		}
		n += utf16Len(r)
	}
	return len(s)
}

func byteToUTF16(s string, b int) int {
	if b > len(s) {
		b = len(s)
	}
	n := 0
	for _, r := range s[:b] {
		n += utf16Len(r)
	}
	return n
}

func utf16Len(r rune) int {
	if r >= 0x10000 && r != utf8.RuneError {
		return 2
	}
	return 1
}
