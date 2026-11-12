// Package e2e drives the server through Neovim's built-in LSP client in
// headless mode. Each test runs a fresh nvim against a copy of
// testdata/workspace and a Lua body that returns a value encoded as JSON.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var binPath string

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("nvim"); err != nil {
		fmt.Println("skipping e2e tests: nvim not found in PATH")
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "brain-e2e")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "brain")
	build := exec.Command("go", "build", "-o", binPath, "..")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Println("building server:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

const wrapper = `
local H = dofile(os.getenv('BRAIN_HARNESS'))
local init = os.getenv('BRAIN_INIT')
H.setup({
  bin = os.getenv('BRAIN_BIN'),
  root = os.getenv('BRAIN_ROOT'),
  init_options = init ~= '' and vim.json.decode(init) or nil,
})
local ok, res = xpcall(function()
%s
end, debug.traceback)
local f = assert(io.open(os.getenv('BRAIN_OUT'), 'w'))
f:write(vim.json.encode({ ok = ok, result = ok and res or nil, error = (not ok) and res or nil }))
f:close()
vim.cmd('qa!')
`

// runNvim executes body (a Lua chunk using the harness H) and decodes its return value into out.
func runNvim(t *testing.T, initOptions, body string, out any) {
	t.Helper()
	root := copyWorkspace(t)
	tmp := t.TempDir()
	script := filepath.Join(tmp, "test.lua")
	outFile := filepath.Join(tmp, "out.json")
	if err := os.WriteFile(script, fmt.Appendf(nil, wrapper, body), 0o644); err != nil {
		t.Fatal(err)
	}
	harness, _ := filepath.Abs("harness.lua")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvim", "--headless", "--clean", "-n", "-l", script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"BRAIN_HARNESS="+harness, "BRAIN_BIN="+binPath, "BRAIN_ROOT="+root,
		"BRAIN_INIT="+initOptions, "BRAIN_OUT="+outFile)
	logs, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("nvim timed out\n%s", logs)
	}
	raw, rerr := os.ReadFile(outFile)
	if rerr != nil {
		t.Fatalf("nvim produced no result (err=%v)\n%s", err, logs)
	}
	var res struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	if !res.OK {
		t.Fatalf("lua error: %s\n%s", res.Error, logs)
	}
	if out != nil {
		if err := json.Unmarshal(res.Result, out); err != nil {
			t.Fatalf("decoding result %s: %v", res.Result, err)
		}
	}
}

func copyWorkspace(t *testing.T) string {
	t.Helper()
	dst, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS("testdata/workspace")); err != nil {
		t.Fatal(err)
	}
	return dst
}

type accepted struct {
	Labels []string `json:"labels"`
	Line   string   `json:"line"`
}

func TestWikiLinkCompletion(t *testing.T) {
	t.Parallel()
	var got accepted
	runNvim(t, "", `
		H.open('index.md')
		local items = H.complete_at_end_of(10, 'x [[al')
		return { labels = H.labels(items), line = H.accept(H.find_item(items, 'alpha')) }`, &got)
	if !slices.Equal(got.Labels, []string{"alpha", "beta", "daily"}) {
		t.Errorf("labels = %v", got.Labels)
	}
	if got.Line != "x [[alpha]]" {
		t.Errorf("line = %q", got.Line)
	}
}

// Note previews are left out of completion lists and filled in on resolve.
func TestCompletionResolveAddsPreview(t *testing.T) {
	t.Parallel()
	var got struct {
		HasDoc     bool   `json:"has_doc"`
		CanResolve bool   `json:"can_resolve"`
		Doc        string `json:"doc"`
	}
	runNvim(t, "", `
		H.open('index.md')
		local item = H.find_item(H.complete_at_end_of(10, 'x [[al'), 'alpha')
		local res = H.request('completionItem/resolve', item)
		return {
			has_doc = item.documentation ~= nil,
			can_resolve = H.client:supports_method('completionItem/resolve'),
			doc = res.documentation.value,
		}`, &got)
	if got.HasDoc || !got.CanResolve {
		t.Errorf("has_doc=%v can_resolve=%v", got.HasDoc, got.CanResolve)
	}
	if !strings.HasPrefix(got.Doc, "# Alpha\n\nAlpha project overview.") {
		t.Errorf("doc = %q", got.Doc)
	}
}

func TestWikiLinkCompletionKeepsExistingClosingBrackets(t *testing.T) {
	t.Parallel()
	var line string
	runNvim(t, "", `
		H.open('index.md')
		vim.api.nvim_buf_set_lines(0, 10, 11, false, { '[[be]] tail' })
		local res = H.request('textDocument/completion', H.params(10, 4))
		return H.accept(H.find_item(res.items, 'beta'))`, &line)
	if line != "[[beta]] tail" {
		t.Errorf("line = %q", line)
	}
}

func TestAtCompletionBecomesWikiLink(t *testing.T) {
	t.Parallel()
	var got struct {
		accepted
		Filter string `json:"filter"`
	}
	runNvim(t, "", `
		H.open('index.md')
		local items = H.complete_at_end_of(10, 'ping @be')
		local item = H.find_item(items, 'beta')
		return { labels = H.labels(items), filter = item.filterText, line = H.accept(item) }`, &got)
	if got.Line != "ping [[beta]]" {
		t.Errorf("line = %q", got.Line)
	}
	if got.Filter != "@beta" {
		t.Errorf("filterText = %q", got.Filter)
	}
}

// Regression: Neovim's built-in completion filters the popup by matching the
// typed text (from the edit's start) against each item's "word". For plain
// text edits the word is newText ("[[beta]]"), which never matches "@b", so
// typing after "@" stopped filtering. This runs the same conversion the
// built-in client uses and checks the words line up with the typed text.
func TestAtCompletionFiltersInBuiltinClient(t *testing.T) {
	t.Parallel()
	var got map[string]struct {
		Typed string   `json:"typed"`
		Words []string `json:"words"`
	}
	runNvim(t, "", `
		H.open('index.md')
		local out = {}
		for _, text in ipairs({ 'x @', 'x @b', 'x @al' }) do
			vim.api.nvim_buf_set_lines(0, 10, 11, false, { text })
			local res = H.request('textDocument/completion', H.params(10, #text))
			local boundary = vim.fn.match(text, '\\k*$')
			local matches, server_boundary = vim.lsp.completion._convert_results(
				text, 10, #text, H.client.id, boundary, nil, res, 'utf-16')
			local words = vim.tbl_map(function(m) return m.word end, matches)
			table.sort(words)
			out[text] = { typed = text:sub((server_boundary or boundary) + 1), words = words }
		end
		return out`, &got)
	want := map[string][]string{
		"x @":   {"@alpha", "@beta", "@daily"},
		"x @b":  {"@beta"},
		"x @al": {"@alpha"},
	}
	for text, w := range want {
		g := got[text]
		if !slices.Equal(g.Words, w) {
			t.Errorf("%q: words = %v, want %v", text, g.Words, w)
		}
		for _, word := range g.Words {
			if !strings.HasPrefix(word, g.Typed) {
				t.Errorf("%q: popup word %q does not start with typed %q, so the popup cannot filter", text, word, g.Typed)
			}
		}
	}
}

func TestHeadingCompletion(t *testing.T) {
	t.Parallel()
	var got accepted
	runNvim(t, "", `
		H.open('index.md')
		local items = H.complete_at_end_of(10, 'see [[beta#Go')
		return { labels = H.labels(items), line = H.accept(H.find_item(items, 'Goals')) }`, &got)
	if !slices.Equal(got.Labels, []string{"Beta", "Goals", "Risks"}) {
		t.Errorf("labels = %v", got.Labels)
	}
	if got.Line != "see [[beta#Goals]]" {
		t.Errorf("line = %q", got.Line)
	}
}

func TestCompletionHandlesUTF16Columns(t *testing.T) {
	t.Parallel()
	var line string
	runNvim(t, "", `
		H.open('index.md')
		local text = 'ü 😀 [[be'
		vim.api.nvim_buf_set_lines(0, 10, 11, false, { text })
		local col = vim.str_utfindex(text, 'utf-16')
		local res = H.request('textDocument/completion', H.params(10, col))
		return H.accept(H.find_item(res.items, 'beta'))`, &line)
	if line != "ü 😀 [[beta]]" {
		t.Errorf("line = %q", line)
	}
}

func TestAtCompletionIgnoresEmailAddresses(t *testing.T) {
	t.Parallel()
	var n int
	runNvim(t, "", `
		H.open('index.md')
		return #H.complete_at_end_of(10, 'mail bob@be')`, &n)
	if n != 0 {
		t.Errorf("got %d items for an email address", n)
	}
}

func TestCompletionTriggersAreConfigurable(t *testing.T) {
	t.Parallel()
	var got struct {
		Triggers []string `json:"triggers"`
		At       int      `json:"at"`
		Wiki     int      `json:"wiki"`
	}
	runNvim(t, `{"completion":{"atLinks":false}}`, `
		H.open('index.md')
		return {
			triggers = H.client.server_capabilities.completionProvider.triggerCharacters,
			at = #H.complete_at_end_of(10, '@be'),
			wiki = #H.complete_at_end_of(10, '[[be'),
		}`, &got)
	if slices.Contains(got.Triggers, "@") || got.At != 0 {
		t.Errorf("@ completion should be disabled: %+v", got)
	}
	if got.Wiki == 0 {
		t.Errorf("[[ completion should still work: %+v", got)
	}

	var wikiOff struct {
		At   int `json:"at"`
		Wiki int `json:"wiki"`
	}
	runNvim(t, `{"completion":{"wikiLinks":false}}`, `
		H.open('index.md')
		return { at = #H.complete_at_end_of(10, '@be'), wiki = #H.complete_at_end_of(10, '[[be') }`, &wikiOff)
	if wikiOff.Wiki != 0 || wikiOff.At == 0 {
		t.Errorf("wikiLinks=false: %+v", wikiOff)
	}
}

func TestDefinitionJumps(t *testing.T) {
	t.Parallel()
	type jump struct {
		File   string `json:"file"`
		Cursor []int  `json:"cursor"`
	}
	var got map[string]jump
	runNvim(t, "", `
		local out = {}
		local function jump(name, needle, offset)
			H.open('index.md')
			local l, c = H.find(needle)
			vim.api.nvim_win_set_cursor(0, { l + 1, c + offset })
			local before = vim.api.nvim_get_current_buf()
			vim.lsp.buf.definition()
			assert(vim.wait(5000, function() return vim.api.nvim_get_current_buf() ~= before end), name .. ': no jump')
			out[name] = { file = H.rel(vim.uri_from_bufnr(0)), cursor = vim.api.nvim_win_get_cursor(0) }
		end
		jump('wiki', '[[projects/alpha]]', 4)
		jump('anchor', '[[beta#Goals]]', 9)
		jump('markdown', '[markdown link]', 2)
		return out`, &got)
	want := map[string]jump{
		"wiki":     {"projects/alpha.md", []int{1, 0}},
		"anchor":   {"beta.md", []int{3, 3}},
		"markdown": {"projects/alpha.md", []int{5, 3}},
	}
	for k, w := range want {
		g := got[k]
		if g.File != w.File || !slices.Equal(g.Cursor, w.Cursor) {
			t.Errorf("%s: got %+v, want %+v", k, g, w)
		}
	}
}

func TestDefinitionOutsideLinkIsEmpty(t *testing.T) {
	t.Parallel()
	var empty bool
	runNvim(t, "", `
		H.open('index.md')
		return H.request('textDocument/definition', H.params(10, 2)) == nil`, &empty)
	if !empty {
		t.Error("expected no definition outside a link")
	}
}

func TestHoverPreviewsLinkedNote(t *testing.T) {
	t.Parallel()
	var got map[string]string
	runNvim(t, "", `
		H.open('index.md')
		local function hover(needle)
			local l, c = H.find(needle)
			return H.request('textDocument/hover', H.params(l, c + 2)).contents.value
		end
		return {
			note = hover('[[projects/alpha]]'),
			section = hover('[[beta#Goals]]'),
			markdown = hover('[markdown link]'),
		}`, &got)
	check := func(name string, want, notWant []string) {
		for _, w := range want {
			if !strings.Contains(got[name], w) {
				t.Errorf("%s hover missing %q:\n%s", name, w, got[name])
			}
		}
		for _, w := range notWant {
			if strings.Contains(got[name], w) {
				t.Errorf("%s hover should not contain %q:\n%s", name, w, got[name])
			}
		}
	}
	check("note", []string{"**projects/alpha**", "Alpha project overview.", "Nothing yet."}, nil)
	check("section", []string{"**beta › Goals**", "Ship it."}, []string{"Risks"})
	check("markdown", []string{"Tasks", "write lsp"}, []string{"Nothing yet."})
}

func TestReferences(t *testing.T) {
	t.Parallel()
	var got map[string][]string
	runNvim(t, "", `
		local function refs(params, include)
			params.context = { includeDeclaration = include or false }
			local out = {}
			for _, loc in ipairs(H.request('textDocument/references', params)) do
				table.insert(out, H.rel(loc.uri) .. ':' .. loc.range.start.line)
			end
			table.sort(out)
			return out
		end
		H.open('index.md')
		local l, c = H.find('[[projects/alpha]]')
		local link = refs(H.params(l, c + 3))
		local withDecl = refs(H.params(l, c + 3), true)
		H.open('beta.md')
		return {
			link = link,
			with_declaration = withDecl,
			heading = refs(H.params(2, 4)),
			backlinks = refs(H.params(4, 1)),
		}`, &got)
	want := map[string][]string{
		"link":             {"index.md:5", "index.md:6", "notes/daily.md:2"},
		"with_declaration": {"index.md:5", "index.md:6", "notes/daily.md:2", "projects/alpha.md:0"},
		"heading":          {"index.md:5"},
		"backlinks":        {"index.md:5", "projects/alpha.md:7"},
	}
	for k, w := range want {
		if !slices.Equal(got[k], w) {
			t.Errorf("%s: got %v, want %v", k, got[k], w)
		}
	}
}

type symbol struct {
	Name     string   `json:"name"`
	Children []symbol `json:"children"`
	Range    struct {
		Start struct{ Line int } `json:"start"`
		End   struct{ Line int } `json:"end"`
	} `json:"range"`
}

func TestDocumentSymbolsAreTOC(t *testing.T) {
	t.Parallel()
	var got []symbol
	runNvim(t, "", `
		H.open('projects/alpha.md')
		return H.request('textDocument/documentSymbol', { textDocument = { uri = vim.uri_from_bufnr(0) } })`, &got)
	if len(got) != 1 || got[0].Name != "Alpha" || len(got[0].Children) != 2 ||
		got[0].Children[0].Name != "Tasks" || got[0].Children[1].Name != "Notes" {
		t.Fatalf("symbols = %+v", got)
	}
	if tasks := got[0].Children[0]; tasks.Range.Start.Line != 4 || tasks.Range.End.Line != 8 {
		t.Errorf("Tasks range = %+v", tasks.Range)
	}
}

func TestWorkspaceSymbols(t *testing.T) {
	t.Parallel()
	var got []string
	runNvim(t, "", `
		H.open('index.md')
		local out = {}
		for _, s in ipairs(H.request('workspace/symbol', { query = 'goal' })) do
			table.insert(out, (s.containerName or '') .. '/' .. s.name)
		end
		return out`, &got)
	if !slices.Equal(got, []string{"beta/Goals"}) {
		t.Errorf("symbols = %v", got)
	}
}

func TestUnsavedEditsAreVisibleAcrossDocuments(t *testing.T) {
	t.Parallel()
	var got accepted
	runNvim(t, "", `
		H.open('beta.md')
		vim.api.nvim_buf_set_lines(0, -1, -1, false, { '', '## Launch plan' })
		H.open('index.md')
		local items = H.complete_at_end_of(10, '[[beta#La')
		return { labels = H.labels(items), line = H.accept(H.find_item(items, 'Launch plan')) }`, &got)
	if got.Line != "[[beta#Launch plan]]" {
		t.Errorf("line = %q (labels %v)", got.Line, got.Labels)
	}
}
