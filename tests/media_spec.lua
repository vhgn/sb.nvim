-- Edge cases of the markdown scanner and of image / mermaid / math rendering.
local H = require('helpers')
local T = {}

vim.opt.rtp:prepend(H.plugin)

--- Scans text (or a list of lines) and returns compact items.
local function S(src)
  local lines = type(src) == 'table' and src or vim.split(src, '\n', { plain = true })
  local out = {}
  for _, it in ipairs(require('brain.scan').scan(lines)) do
    out[#out + 1] = {
      it.kind,
      it.line,
      it.col,
      it.end_col,
      it.target or it.text,
      display = it.display,
      size = it.size,
      view = it.view,
    }
  end
  return out
end

local function kinds(src)
  local out = {}
  for _, it in ipairs(S(src)) do
    out[#out + 1] = it[1] .. ':' .. tostring(it[5])
  end
  return out
end

-- ---------------------------------------------------------------- scanner

T['scan: dollar prices are not math'] = function()
  H.eq({}, S('It costs $5 and $6, or $10.\nPay $3.50 now, $4 later'))
  H.eq({}, S('From $ x$ to $y $ and $x$5'))
end

T['scan: a price before real inline math does not shift or swallow it'] = function()
  H.eq({ { 'math', 0, 14, 19, 'x^2', display = false } }, S('Costs $5, and $x^2$ more'))
end

T['scan: escaped dollars never open or close inline math'] = function()
  H.eq({}, S([[\$x$ and \$y\$]]))
  H.eq({ { 'math', 0, 0, 6, [[a\$b]], display = false } }, S([[$a\$b$ $]]))
end

T['scan: $$ and $ inside inline code are ignored'] = function()
  H.eq({ 'math:y' }, kinds('`$$` and `$x$` then $y$'))
  H.eq({ 'math:y' }, kinds('``a`$x$`b`` $y$'))
  -- unclosed backtick run is literal: the math after it still counts
  H.eq({ 'math:z' }, kinds('a ` b $z$'))
end

T['[BUG fixed] scan: unclosed $$ does not swallow the rest of the note'] = function()
  H.eq({ 'image:a.png', 'math:z' }, kinds('I paid $$ for it\n![[a.png]]\n$z$'))
end

T['[BUG fixed] scan: multi-line $$ whose opener line has $$ in inline code'] = function()
  local it = S('`$$` and $$\nx + y\n$$')
  H.eq(1, #it)
  H.eq({ 'math', 0, 9, 2, 'x + y', display = true }, it[1])
end

T['[BUG fixed] scan: \\[ before $$ on one line keeps inline columns right'] = function()
  local line = [[\[a\] and $b$ and $$c$$]]
  H.eq({
    { 'math', 0, 0, 5, 'a', display = true },
    { 'math', 0, 10, 13, 'b', display = false },
    { 'math', 0, 18, 23, 'c', display = true },
  }, S(line))
end

T['[BUG fixed] scan: two display formulas on one line'] = function()
  H.eq({ 'math:a', 'math:b' }, kinds('$$a$$ and $$b$$'))
end

T['scan: empty display math produces nothing'] = function()
  H.eq({}, S('$$  $$\n$$\n\n$$\n\\[\\]'))
end

T['scan: math and embeds inside fenced code are ignored'] = function()
  H.eq({}, S('```\n$x$ $$y$$ ![[a.png]] ![[q.base]]\n```'))
  H.eq({}, S('~~~python\nprice = "$5" + "$6"\n$$z$$\n~~~'))
end

T['scan: a longer fence hides an inner ```math block'] = function()
  local src = '````md\n```math\nx\n```\n````\n$after$'
  H.eq({ 'math:after' }, kinds(src))
end

T['scan: a ~~~ fence is not closed by ``` and vice versa'] = function()
  H.eq({ 'math:y' }, kinds('~~~\n$x$\n```\n~~~\n$y$'))
  H.eq({ 'math:y' }, kinds('```\n$x$\n~~~\n```\n$y$'))
end

T['scan: a fence line with an info string does not close a fence'] = function()
  H.eq({ 'math:x\n```js' }, kinds('```math\nx\n```js\n```'))
end

T['scan: ~~~math, ```latex and ``` {.tex} are display math'] = function()
  local items = S('~~~math\na\n~~~\n```latex\nb\n```\n``` {.tex}\nc\n```')
  H.eq(3, #items)
  for i, t in ipairs({ 'a', 'b', 'c' }) do
    H.eq('math', items[i][1])
    H.eq(t, items[i][5])
    H.eq(true, items[i].display)
  end
end

T['scan: unclosed ```mermaid is not rendered and hides what follows'] = function()
  -- CommonMark: an unclosed fence runs to the end of the document
  H.eq({}, S('```mermaid\ngraph TD\n![[a.png]]\n$x$'))
end

T['scan: embeds and dollars in front matter are ignored'] = function()
  H.eq({ 'image:b.png' }, kinds('---\nimg: "![[a.png]]"\nprice: $5 and $6$\n---\n![[b.png]]'))
  H.eq({ 'image:b.png' }, kinds('---\nx: ![[a.png]]\n...\n![[b.png]]'))
end

T['scan: an unclosed front matter block is ordinary text'] = function()
  H.eq({ 'image:a.png' }, kinds('---\n![[a.png]]'))
  -- only the first line can open front matter
  H.eq({ 'image:a.png' }, kinds('\n---\n![[a.png]]\n---'))
end

T['[BUG fixed] scan: CRLF line endings'] = function()
  local lines = { '---\r', 'a: 1\r', '---\r', '![[a.png|300]]\r', '$$\r', 'x\r', '$$\r', 'and $y$\r', '```math\r', 'z\r', '```\r' }
  H.eq({
    { 'image', 3, 0, 14, 'a.png', size = '300' },
    { 'math', 4, 0, 2, 'x', display = true },
    { 'math', 7, 4, 7, 'y', display = false },
  }, vim.list_slice(S(lines), 1, 3))
  H.eq('z', S(lines)[4][5], 'fenced math body must not keep the \\r')
end

T['scan: wiki image sizes, spaces and parentheses'] = function()
  local items = S('![[img.png|300x200]] ![[my image (1).PNG]] ![[a.jpg|caption]]')
  H.eq('300x200', items[1].size)
  H.eq('my image (1).PNG', items[2][5])
  H.eq('caption', items[3].size)
  -- not images: notes (their own kind) and other files (nothing)
  H.eq({ { 'note', 0, 0, 9, 'note' }, { 'note', 0, 23, 36, 'a.png.md' } }, S('![[note]] ![[doc.pdf]] ![[a.png.md]]'))
end

T['scan: base embed with an empty or aliased view'] = function()
  H.eq({ { 'base', 0, 0, 22, 'x.base', view = 'View' } }, S('![[x.base#View|alias]]'))
  H.eq(nil, S('![[x.base#]]')[1].view, 'empty #anchor means no view')
end

T['[BUG fixed] scan: markdown image destinations'] = function()
  H.eq({ 'image:my image.png' }, kinds('![](my%20image.png)'))
  H.eq({ 'image:img(1).png' }, kinds('![](img%281%29.png)'))
  H.eq({ 'image:img(1).png' }, kinds('![alt](img(1).png)'))
  H.eq({ 'image:my image.png' }, kinds('![x](<my image.png> "title")'))
  H.eq({ 'image:a.png' }, kinds("![x](a.png 'title')"))
  H.eq({ 'image:a.png' }, kinds('![x](a.png (title))'))
  H.eq({ 'image:https://x.org/a%20b.png' }, kinds('![](https://x.org/a%20b.png)'))
  H.eq({ 'image:https://x.org/img?id=3' }, kinds('![](https://x.org/img?id=3)'))
  H.eq({}, kinds('![](notes.md) [link](a.png) ![]()'))
end

T['scan: column of an image after multibyte text is a byte offset'] = function()
  local it = S('αβ ![[a.png]]')[1]
  H.eq({ 5, 15 }, { it[3], it[4] })
end

-- ---------------------------------------------------------------- rendering

local ns_msg = function()
  return vim.api.nvim_get_namespaces()['brain.media.messages']
end
local ns_inline = function()
  return vim.api.nvim_get_namespaces()['brain.math.inline']
end

--- Message virt lines: { {line, text, group} }.
local function messages(buf)
  local out = {}
  for _, m in ipairs(vim.api.nvim_buf_get_extmarks(buf, ns_msg(), 0, -1, { details = true })) do
    for _, vl in ipairs(m[4].virt_lines or {}) do
      out[#out + 1] = { m[2], vim.trim(vl[1][1]), vl[1][2] }
    end
  end
  return out
end

--- Live images sorted by line: { {path, y} }.
local function live()
  local out = {}
  for _, img in ipairs(H.live_images()) do
    out[#out + 1] = { img.path, img.geometry.y }
  end
  table.sort(out, function(a, b)
    if a[2] ~= b[2] then
      return a[2] < b[2]
    end
    return a[1] < b[1]
  end)
  return out
end

local function png(rel)
  return H.write(rel, '\137PNG\r\n\26\n')
end

--- Waits until no async refresh work is pending (resolve / tex / mermaid).
local function settle(ms)
  vim.wait(ms or 300, function()
    return false
  end, 10)
end

local function edit(buf, start, stop, lines)
  vim.api.nvim_buf_set_lines(buf, start, stop, false, lines)
  vim.api.nvim_exec_autocmds('TextChanged', { buffer = buf })
end

T['image: moves when lines are inserted above and is not re-created'] = function()
  H.vault({ ['note.md'] = 'top\n![[a.png]]\n![](b.png)\n' })
  local a, b = png('a.png'), png('b.png')
  H.setup()
  local buf = H.open('note.md')
  H.wait(3000, function()
    return #live() == 2
  end, 'two images')
  H.eq({ { a, 1 }, { b, 2 } }, live())
  local created = #H.images
  edit(buf, 0, 0, { 'new 1', 'new 2' })
  H.wait(3000, function()
    local l = live()
    return #l == 2 and l[1][2] == 3 and l[2][2] == 4
  end, 'images to follow their lines')
  settle()
  H.eq({ { a, 3 }, { b, 4 } }, live())
  H.eq(created, #H.images, 'moving an image must re-render it, not create another')
end

T['image: cleared when its embed line is deleted, others stay'] = function()
  H.vault({ ['note.md'] = '![[a.png]]\n![[b.png]]\n' })
  png('a.png')
  local b = png('b.png')
  H.setup()
  local buf = H.open('note.md')
  H.wait(3000, function()
    return #live() == 2
  end, 'two images')
  edit(buf, 0, 1, {})
  H.wait(3000, function()
    return vim.deep_equal({ { b, 0 } }, live())
  end, 'deleted embed to clear and b to move up')
  settle()
  H.eq({ { b, 0 } }, live())
  H.eq(2, #H.images, 'b moved, not re-created')
end

T['image: repeated refreshes never duplicate images'] = function()
  H.vault({ ['note.md'] = '![[a.png]]\n![](a.png)\n$$x$$\n' })
  png('a.png')
  H.setup()
  local buf = H.open('note.md')
  H.wait(5000, function()
    return #live() == 3
  end, 'three images')
  local media = require('brain.media')
  for _ = 1, 10 do
    media.refresh(buf)
  end
  vim.api.nvim_exec_autocmds('BufWinEnter', { buffer = buf })
  settle(500)
  H.eq(3, #live())
  H.eq(3, #H.images, 'no image objects beyond the first three')
end

T['image: cleared on BufWinLeave, restored on BufWinEnter, cleared on wipeout'] = function()
  H.vault({ ['note.md'] = '![[a.png]]\n$y$\n', ['other.md'] = 'x\n' })
  png('a.png')
  H.setup()
  vim.o.hidden = true
  local buf = H.open('note.md')
  H.wait(3000, function()
    return #live() == 1
  end, 'image')
  vim.cmd.edit(vim.fn.fnameescape(H.root .. '/other.md'))
  H.eq(0, #live(), 'hidden buffer keeps no image')
  vim.cmd.buffer(buf)
  H.wait(3000, function()
    return #live() == 1
  end, 'image back after BufWinEnter')
  vim.cmd('bwipeout! ' .. buf)
  H.eq(0, #live(), 'wiped buffer keeps no image')
  H.eq(nil, require('brain.media').state[buf])
end

T['image: still shown while the buffer is visible in another window'] = function()
  H.vault({ ['note.md'] = '![[a.png]]\n', ['other.md'] = 'x\n' })
  png('a.png')
  H.setup()
  vim.o.hidden = true
  H.open('note.md')
  H.wait(3000, function()
    return #live() == 1
  end, 'image')
  vim.cmd.split()
  vim.cmd.edit(vim.fn.fnameescape(H.root .. '/other.md'))
  settle()
  H.eq(1, #live(), 'note is still visible in the first window')
end

T['image: missing file shows a message, appears once it exists'] = function()
  H.vault({ ['note.md'] = 'x\n![[gone.png]]\n![](also gone.png)\n' })
  H.setup()
  local buf = H.open('note.md')
  H.wait(3000, function()
    return #messages(buf) == 2
  end, 'not found messages')
  H.eq({ { 1, '🖼 not found: gone.png', 'DiagnosticWarn' }, { 2, '🖼 not found: also gone.png', 'DiagnosticWarn' } }, messages(buf))
  H.eq(0, #live())
  png('gone.png')
  require('brain.client').request_sync(H.root, 'touch', { paths = { H.root .. '/gone.png' } })
  require('brain.media').refresh(buf)
  H.wait(3000, function()
    return #live() == 1
  end, 'image after it was created')
  H.eq({ { 2, '🖼 not found: also gone.png', 'DiagnosticWarn' } }, messages(buf))
end

T['image: names with spaces, parentheses and %20 resolve'] = function()
  H.vault({ ['notes/n.md'] = '![[my pic (1).png]]\n![](../att/my%20pic%20(2).png)\n' })
  local one = png('att/my pic (1).png')
  local two = png('att/my pic (2).png')
  H.setup()
  H.open('notes/n.md')
  H.wait(3000, function()
    return #live() == 2
  end, 'both images')
  H.eq({ { one, 0 }, { two, 1 } }, live())
end

T['image: http image goes through from_url untouched'] = function()
  H.vault({ ['note.md'] = '![remote](https://example.org/a%20b.png?x=1)\n' })
  H.setup()
  H.open('note.md')
  H.wait(3000, function()
    return #live() == 1
  end, 'remote image')
  H.eq('https://example.org/a%20b.png?x=1', live()[1][1])
end

T['[BUG fixed] image: |WxH sets both width and height in cells'] = function()
  H.vault({ ['note.md'] = '![[a.png|300x200]]\n![[a.png|50]]\n' })
  png('a.png')
  H.setup()
  H.open('note.md')
  H.wait(3000, function()
    return #live() == 2
  end, 'two images')
  local g = {}
  for _, img in ipairs(H.live_images()) do
    g[img.geometry.y + 1] = { img.geometry.width, img.geometry.height }
  end
  -- fake terminal cells are 10x20 px
  H.eq({ { 30, 10 }, { 5, nil } }, g)
end

T['image: two different images on one line both render'] = function()
  H.vault({ ['note.md'] = '![[a.png]] ![[b.png]]\n' })
  local a, b = png('a.png'), png('b.png')
  H.setup()
  H.open('note.md')
  H.wait(3000, function()
    return #live() == 2
  end, 'two images')
  H.eq({ { a, 0 }, { b, 0 } }, live())
end

T['image: image.nvim absent → no errors, inline math still concealed'] = function()
  H.vault({ ['note.md'] = '![[a.png]]\n$$x$$\n```mermaid\ngraph TD\n```\nsee $\\alpha$\n' })
  png('a.png')
  H.setup()
  package.preload['image'] = nil
  package.loaded['image'] = nil
  H.ok(not pcall(require, 'image'), 'image.nvim must be unavailable')
  local errs = {}
  local notify = vim.notify
  vim.notify = function(m, l)
    errs[#errs + 1] = m
    notify(m, l)
  end
  vim.cmd.edit(vim.fn.fnameescape(H.root .. '/note.md'))
  local buf = vim.api.nvim_get_current_buf()
  H.wait(3000, function()
    return #vim.api.nvim_buf_get_extmarks(buf, ns_inline(), 0, -1, {}) > 0
  end, 'inline math conceal')
  H.eq(0, #H.images)
  H.eq({}, errs)
  H.eq({}, messages(buf), 'no mermaid message without image.nvim')
end

T['image: image.nvim present but disabled behaves as absent'] = function()
  H.vault({ ['note.md'] = '![[a.png]]\n$$x$$\n' })
  png('a.png')
  H.setup()
  require('image')._enabled = false
  H.open('note.md')
  settle(500)
  H.eq(0, #H.images)
end

T['mermaid: missing command shows a message, images still render'] = function()
  H.vault({ ['note.md'] = '```mermaid\ngraph TD\n  A-->B\n```\n![[a.png]]\n' })
  local a = png('a.png')
  H.setup({ mermaid = { cmd = 'definitely-not-mmdc' } })
  local buf = H.open('note.md')
  H.wait(3000, function()
    return #messages(buf) == 1 and #live() == 1
  end, 'message and image')
  local m = messages(buf)[1]
  H.eq(3, m[1], 'message sits under the closing fence')
  H.contains(m[2], '`definitely-not-mmdc` not found')
  H.eq({ { a, 4 } }, live())
end

--- A fake mmdc: logs each call and writes a PNG to the -o path.
local function fake_mmdc(exit_code)
  local log = H.root .. '/../mmdc-' .. vim.fn.fnamemodify(H.root, ':t') .. '.log'
  local script = H.root .. '/../mmdc-' .. vim.fn.fnamemodify(H.root, ':t') .. '.sh'
  local f = assert(io.open(script, 'w'))
  f:write(([[#!/bin/sh
echo "$@" >> '%s'
if [ %d -ne 0 ]; then echo "Parse error on line 2" >&2; echo "more" >&2; exit %d; fi
while [ $# -gt 0 ]; do if [ "$1" = "-o" ]; then shift; printf 'PNG' > "$1"; fi; shift; done
]]):format(log, exit_code, exit_code))
  f:close()
  vim.fn.setfperm(script, 'rwxr-xr-x')
  return script, function()
    local lf = io.open(log)
    if not lf then
      return 0
    end
    local n = 0
    for _ in lf:lines() do
      n = n + 1
    end
    lf:close()
    return n
  end
end

T['[BUG fixed] mermaid: identical diagrams twice render twice, command runs once'] = function()
  H.vault({ ['note.md'] = '```mermaid\ngraph TD\n```\nmid\n```mermaid\ngraph TD\n```\n' })
  local script, calls = fake_mmdc(0)
  H.setup({ mermaid = { cmd = script } })
  local buf = H.open('note.md')
  H.wait(5000, function()
    return #live() == 2
  end, 'two diagrams')
  local l = live()
  H.eq({ 2, 6 }, { l[1][2], l[2][2] })
  H.eq(l[1][1], l[2][1], 'same cached png')
  local n = calls()
  H.eq(1, n, 'one mmdc run for both blocks and every overlapping refresh')
  require('brain.media').refresh(buf)
  settle(300)
  H.eq(n, calls(), 'refresh must hit the cache')
end

T['mermaid: a failing command shows its first stderr line'] = function()
  H.vault({ ['note.md'] = '```mermaid\ngraph ???\n```\n' })
  local script = fake_mmdc(3)
  H.setup({ mermaid = { cmd = script } })
  local buf = H.open('note.md')
  H.wait(5000, function()
    return #messages(buf) == 1
  end, 'error message')
  H.eq({ { 2, 'mermaid: Parse error on line 2', 'DiagnosticWarn' } }, messages(buf))
  H.eq(0, #live())
end

T['math: a parse error shows a message and the other formulas still render'] = function()
  H.vault({ ['note.md'] = '$$\\frac{a}$$\n$$x^2$$\n\\[\\left( x\\]\n```math\ny\n```\n' })
  H.setup()
  local buf = H.open('note.md')
  H.wait(8000, function()
    return #messages(buf) == 2 and #live() == 2
  end, 'two errors and two images')
  local m = messages(buf)
  H.eq({ 0, 'math: missing argument', 'DiagnosticError' }, m[1])
  H.eq({ 2, 'math: missing \\right', 'DiagnosticError' }, m[2])
  local l = live()
  H.eq({ 1, 5 }, { l[1][2], l[2][2] })
  for _, it in ipairs(l) do
    H.ok(it[1]:match('%.png$') and vim.uv.fs_stat(it[1]), 'png exists: ' .. it[1])
  end
end

T['[BUG fixed] math: identical formulas on two lines render twice'] = function()
  H.vault({ ['note.md'] = '$$a+b$$\ntext\n$$a+b$$\n' })
  H.setup()
  H.open('note.md')
  H.wait(5000, function()
    return #live() == 2
  end, 'two formulas')
  local l = live()
  H.eq({ 0, 2 }, { l[1][2], l[2][2] })
end

T['math: fixing a broken formula clears its message and renders it'] = function()
  H.vault({ ['note.md'] = '$$\\frac{a}$$\n' })
  H.setup()
  local buf = H.open('note.md')
  H.wait(5000, function()
    return #messages(buf) == 1
  end, 'error')
  edit(buf, 0, 1, { '$$\\frac{a}{b}$$' })
  H.wait(5000, function()
    return #messages(buf) == 0 and #live() == 1
  end, 'image instead of error')
end

T['math: colorscheme change re-renders in the new colors'] = function()
  H.vault({ ['note.md'] = '$$x$$\n![[a.png]]\n' })
  local a = png('a.png')
  H.setup()
  vim.cmd('hi Normal guifg=#112233 guibg=#000000')
  H.open('note.md')
  H.wait(5000, function()
    return #live() == 2
  end, 'formula and image')
  local before
  for _, it in ipairs(live()) do
    if it[1] ~= a then
      before = it[1]
    end
  end
  H.contains(table.concat(vim.fn.readfile(before:gsub('%.png$', '.svg')), '\n'), '#112233')
  vim.cmd('hi Normal guifg=#ddeeff')
  vim.api.nvim_exec_autocmds('ColorScheme', {})
  H.wait(5000, function()
    local l = live()
    return #l == 2 and l[1][1] ~= before
  end, 'formula re-rendered')
  local after = live()[1][1]
  H.contains(table.concat(vim.fn.readfile(after:gsub('%.png$', '.svg')), '\n'), '#ddeeff')
  H.eq(a, live()[2][1])
end

--- Inline conceal marks: { [line] = {texts} }.
local function inline_marks(buf)
  local out = {}
  for _, m in ipairs(vim.api.nvim_buf_get_extmarks(buf, ns_inline(), 0, -1, { details = true })) do
    local d = m[4]
    if d.virt_text then
      out[m[2]] = out[m[2]] or {}
      table.insert(out[m[2]], d.virt_text[1][1])
    end
  end
  return out
end

T['inline math: cursor line shows raw text, follows CursorMoved'] = function()
  H.vault({ ['note.md'] = 'a $\\alpha^2$ b\nc $\\beta_i$ and $x \\leq y$\nplain\n' })
  H.setup()
  local buf = H.open('note.md')
  vim.api.nvim_win_set_cursor(0, { 3, 0 })
  vim.api.nvim_exec_autocmds('CursorMoved', { buffer = buf })
  H.wait(3000, function()
    return inline_marks(buf)[0] ~= nil and inline_marks(buf)[1] ~= nil
  end, 'conceal on both lines')
  H.eq({ [0] = { 'α²' }, [1] = { 'βᵢ', 'x ≤ y' } }, inline_marks(buf))
  vim.api.nvim_win_set_cursor(0, { 2, 3 })
  vim.api.nvim_exec_autocmds('CursorMoved', { buffer = buf })
  H.eq({ [0] = { 'α²' } }, inline_marks(buf), 'cursor line 2 is raw')
  vim.api.nvim_win_set_cursor(0, { 1, 0 })
  vim.api.nvim_exec_autocmds('CursorMoved', { buffer = buf })
  H.eq({ [1] = { 'βᵢ', 'x ≤ y' } }, inline_marks(buf), 'cursor line 1 is raw')
end

T['inline math: conceal spans exactly the $…$ bytes after multibyte text'] = function()
  H.vault({ ['note.md'] = 'αβ $x^2$ é\nend\n' })
  H.setup()
  local buf = H.open('note.md')
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  vim.api.nvim_exec_autocmds('CursorMoved', { buffer = buf })
  H.wait(3000, function()
    return inline_marks(buf)[0] ~= nil
  end, 'conceal')
  local spans = {}
  for _, m in ipairs(vim.api.nvim_buf_get_extmarks(buf, ns_inline(), 0, -1, { details = true })) do
    if m[4].conceal then
      spans[#spans + 1] = { m[3], m[4].end_col }
    end
  end
  H.eq({ { 5, 10 } }, spans)
end

T['inline math: edits re-conceal at the new position; deleting it clears'] = function()
  H.vault({ ['note.md'] = '$x^2$\nend\n' })
  H.setup()
  local buf = H.open('note.md')
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  vim.api.nvim_exec_autocmds('CursorMoved', { buffer = buf })
  H.wait(3000, function()
    return inline_marks(buf)[0] ~= nil
  end, 'conceal')
  edit(buf, 0, 1, { 'now $y^2$ here' })
  H.wait(3000, function()
    local ms = vim.api.nvim_buf_get_extmarks(buf, ns_inline(), 0, -1, { details = true })
    for _, m in ipairs(ms) do
      if m[4].conceal and m[3] == 4 and m[4].end_col == 9 then
        return true
      end
    end
  end, 'conceal moved')
  H.eq({ [0] = { 'y²' } }, inline_marks(buf))
  edit(buf, 0, 1, { 'no math' })
  H.wait(3000, function()
    return #vim.api.nvim_buf_get_extmarks(buf, ns_inline(), 0, -1, {}) == 0
  end, 'conceal cleared')
end

T['inline math: invalid TeX is left as raw text'] = function()
  H.vault({ ['note.md'] = 'bad $\\frac{a}$ ok $\\pi$\nend\n' })
  H.setup()
  local buf = H.open('note.md')
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  vim.api.nvim_exec_autocmds('CursorMoved', { buffer = buf })
  H.wait(3000, function()
    return inline_marks(buf)[0] ~= nil
  end, 'conceal')
  H.eq({ [0] = { 'π' } }, inline_marks(buf))
end

return T
