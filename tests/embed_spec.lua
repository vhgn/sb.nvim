-- Rendering bases inside notes and stepping into them.
local H = require('helpers')
local T = {}

T['view cycling inside the overlay keeps the source file untouched'] = function()
  H.vault({
    ['a.md'] = '---\nstatus: todo\n---\n',
    ['b.md'] = '---\nstatus: done\n---\n',
    ['two.base'] = 'views:\n  - type: table\n    name: First\n  - type: list\n    name: Second\n',
    ['note.md'] = '# Note\n![[two.base]]\ntail\n',
  })
  H.setup()
  local buf = H.open('note.md')
  local before = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
  H.contains(H.virt_text(buf), '▦ First')
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('<CR>')
  H.ok(require('brain.base').overlay(), 'overlay should open on the embed line')
  H.feed('<Tab>')
  H.wait(3000, function()
    local st = require('brain.base').state[buf]
    return st.items[1].result and st.items[1].result.type == 'list'
  end, 'view to switch to list')
  H.contains(table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n'), '• a', 'overlay shows the list view')
  H.eq(before, vim.api.nvim_buf_get_lines(buf, 0, -1, false), 'source must not change')
  H.ok(not vim.bo[buf].modified, 'source must not be modified')
end

T['editing a .base buffer re-renders from unsaved text'] = function()
  H.vault({
    ['a.md'] = '---\nprice: 3\n---\n',
    ['b.md'] = '---\nprice: 7\n---\n',
    ['q.base'] = 'filters: "price > 5"\n',
  })
  H.setup()
  local buf = H.open('q.base')
  H.contains(H.virt_text(buf), 'b.md')
  H.not_contains(H.virt_text(buf), 'a.md')
  vim.api.nvim_buf_set_lines(buf, 0, 1, false, { 'filters: "price < 5"' })
  vim.api.nvim_exec_autocmds('TextChanged', { buffer = buf })
  H.wait(3000, function()
    local t = H.virt_text(buf)
    return t:find('a.md', 1, true) and not t:find('b.md', 1, true)
  end, 'preview to follow unsaved edits')
end

-- ---------------------------------------------------------------------------
-- Edge cases
-- ---------------------------------------------------------------------------

local function B()
  return require('brain.base')
end

-- only markdown notes, so .base files don't show up as rows
local MD = 'filters: \'file.ext == "md"\'\n'
local TWO = MD .. 'views:\n  - type: table\n    name: First\n  - type: list\n    name: Second\n'

local function items(buf)
  return B().state[buf].items
end

local function overlay_lines()
  local o = B().overlay()
  return o and vim.api.nvim_buf_get_lines(o.buf, 0, -1, false)
end

local function cursor(win)
  return vim.api.nvim_win_get_cursor(win or 0)
end

--- Widest virtual line of buf, in display cells.
local function virt_width(buf)
  local w = 0
  for _, v in ipairs(H.virt(buf)) do
    for _, l in ipairs(v.lines) do
      w = math.max(w, vim.api.nvim_strwidth(l))
    end
  end
  return w
end

local function text_cols(win)
  local info = vim.fn.getwininfo(win)[1]
  return info.width - info.textoff
end

local function changed(buf)
  vim.api.nvim_exec_autocmds('TextChanged', { buffer = buf })
end

local function floats()
  local out = {}
  for _, w in ipairs(vim.api.nvim_list_wins()) do
    if vim.api.nvim_win_get_config(w).relative ~= '' then
      out[#out + 1] = w
    end
  end
  return out
end

local function wait_closed()
  H.wait(2000, function()
    return B().overlay() == nil
  end, 'overlay to close')
end

--- 0-based line in the overlay whose text contains needle.
local function overlay_find(needle)
  for i, l in ipairs(overlay_lines()) do
    local s = l:find(needle, 1, true)
    if s then
      return i, s - 1
    end
  end
  error('not in overlay: ' .. needle)
end

--- Setup for tests that split windows. Headless nvim's grid stays 80x24; with
--- a bigger 'columns'/'lines' (H.setup uses 100x40) the statusline redraw that
--- vim.diagnostic triggers on DiagnosticChanged crashes nvim 0.12 once there
--- are splits (grid_line_flush assertion / segfault in grid_clear_line).
local function setup_split(opts)
  H.setup(opts)
  vim.o.columns, vim.o.lines = 80, 24
end

local function current_file()
  return vim.fn.fnamemodify(vim.api.nvim_buf_get_name(0), ':t')
end

T['embeds inside fenced code, tilde fences and inline code are not rendered'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['t.base'] = MD,
    ['note.md'] = table.concat({
      '# Note',
      '```md',
      '![[t.base]]',
      '```',
      '~~~',
      '![[t.base]]',
      '~~~',
      '````',
      '```',
      '![[t.base]]',
      '````',
      'inline `![[t.base]]` and ``a ` ![[t.base]]``',
      '![[t.base]]',
      '',
    }, '\n'),
  })
  H.setup()
  local buf = H.open('note.md')
  H.eq(1, #items(buf), 'only the embed outside code is a base')
  local v = H.virt(buf)
  H.eq(1, #v)
  H.eq(12, v[1].anchor)
  vim.api.nvim_win_set_cursor(0, { 3, 0 })
  H.feed('<CR>')
  H.ok(not B().overlay(), 'no overlay from inside a code fence')
  vim.api.nvim_win_set_cursor(0, { 12, 10 })
  H.feed('<CR>')
  H.ok(not B().overlay(), 'no overlay from an inline-code embed')
end

T['two embeds on one line are independent; <CR> picks the one under the cursor, j/k walk between them'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['two.base'] = TWO,
    ['note.md'] = '# Note\n![[two.base]] and ![[two.base#Second]]\ntail\n',
  })
  H.setup()
  local buf = H.open('note.md')
  local its = items(buf)
  H.eq(2, #its)
  H.eq('table', its[1].result.type)
  H.eq('list', its[2].result.type)
  local all = H.virt_text(buf)
  H.ok(all:find('╭', 1, true) < all:find('• a', 1, true), 'first embed renders first')

  local line = vim.api.nvim_buf_get_lines(buf, 1, 2, false)[1]
  local col2 = line:find('![[two.base#Second', 1, true) + 2
  vim.api.nvim_win_set_cursor(0, { 2, col2 })
  H.feed('<CR>')
  local o = B().overlay()
  H.ok(o and o.item == its[2], '<CR> on the second embed enters the second view')
  -- it sits below the first view's virtual lines, not on top of them
  vim.cmd.redraw()
  local arow = vim.fn.screenpos(o.src_win, 2, 1).row -- 1-based screen row of the anchor
  H.eq(arow + #its[1].rendered.lines, vim.api.nvim_win_get_position(o.win)[1], 'overlay row')
  H.feed('q')

  -- cycling the first leaves the second alone
  local r2 = its[2].result
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('<CR>')
  H.ok(B().overlay().item == its[1])
  H.feed('<Tab>')
  H.wait(3000, function()
    return its[1].result.type == 'list'
  end, 'first view to switch')
  H.ok(rawequal(r2, its[2].result), 'second embed was not re-queried')

  -- j at the bottom of the first view steps into the second, k at its top steps back
  H.feed('G')
  H.feed('j')
  o = B().overlay()
  H.ok(o and o.item == its[2], 'j from the bottom of the first enters the second')
  H.eq(its[2].rendered.rows[1] + 1, cursor()[1])
  H.feed('gg')
  H.feed('k')
  o = B().overlay()
  H.ok(o and o.item == its[1], 'k from the top of the second enters the first')
  H.eq(its[1].rendered.rows[#its[1].rendered.rows] + 1, cursor()[1])
end

T['the same base embedded twice keeps separate views, also across edits above them'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['two.base'] = TWO,
    ['note.md'] = '# Note\n![[two.base]]\nmid\n![[two.base]]\n',
  })
  H.setup()
  local buf = H.open('note.md')
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('j')
  H.ok(B().overlay(), 'j on the anchor enters')
  H.feed('<Tab>')
  H.wait(3000, function()
    return items(buf)[1].result.type == 'list'
  end, 'first view to switch')
  H.feed('q')
  H.eq('table', items(buf)[2].result.type)

  vim.api.nvim_buf_set_lines(buf, 0, 0, false, { 'new first line' })
  changed(buf)
  H.wait(2000, function()
    return items(buf)[1].line == 2
  end, 'rescan')
  H.eq('list', items(buf)[1].result.type)
  H.eq('table', items(buf)[2].result.type)
  local v = H.virt(buf)
  H.eq({ 2, 4 }, { v[1].anchor, v[2].anchor })
  H.contains(table.concat(v[1].lines, '\n'), '• a')
  H.contains(table.concat(v[2].lines, '\n'), '╭')
end

T['embed on the last line (no trailing newline): j at the bottom of the view stays inside'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['t.base'] = MD,
    ['note.md'] = '# Note\n![[t.base]]',
  })
  H.setup()
  local buf = H.open('note.md')
  H.eq(1, H.virt(buf)[1].anchor)
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('j')
  H.ok(B().overlay(), 'j on the last line enters')
  H.feed('G')
  local last = #overlay_lines()
  H.eq(last, cursor()[1])
  H.feed('j')
  H.ok(B().overlay(), 'nothing below the view: stay inside instead of bouncing to the anchor')
  H.eq(last, cursor()[1])
  H.feed('q')
  H.eq({ 2, 0 }, cursor())
end

T['deleting the embed line hides its view at once; undo/redo bring it back and remove it'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['t.base'] = MD,
    ['note.md'] = '# Note\n![[t.base]]\nafter\nmore\n',
  })
  H.setup()
  local buf = H.open('note.md')
  local mark = items(buf)[1].mark
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('dd')
  local m = vim.api.nvim_buf_get_extmark_by_id(buf, B().ns, mark, { details = true })
  H.ok(m[3] == nil or m[3].invalid, 'the virtual lines are hidden before the rescan')
  -- before the debounced rescan: j on the line that took its place must not enter the stale view
  H.eq({ 2, 0 }, cursor())
  H.feed('j')
  H.ok(not B().overlay(), 'stale view entered')
  H.eq(3, cursor()[1])
  vim.api.nvim_win_set_cursor(0, { 3, 0 })
  H.feed('k')
  H.ok(not B().overlay(), 'stale view entered from below')
  changed(buf)
  H.wait(2000, function()
    return #items(buf) == 0
  end, 'rescan')
  H.eq({}, H.virt(buf))

  H.feed('u')
  changed(buf)
  H.wait(3000, function()
    local it = items(buf)[1]
    return it and it.result
  end, 'undo to re-render')
  H.eq(1, #H.virt(buf))
  H.eq(1, H.virt(buf)[1].anchor)
  H.contains(H.virt_text(buf), 'a.md')

  H.feed('<C-r>')
  changed(buf)
  H.wait(2000, function()
    return #items(buf) == 0
  end, 'redo to remove')
  H.eq({}, H.virt(buf))
end

T['missing base shows an error, then renders once it is created and saved from another window'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['note.md'] = '# Note\n![[later.base]]\n',
  })
  setup_split()
  local buf = H.open('note.md')
  H.eq('base not found: later.base', items(buf)[1].err)
  H.contains(H.virt_text(buf), 'base not found: later.base')
  local d = vim.diagnostic.get(buf)
  H.eq(1, #d)
  H.eq(1, d[1].lnum)
  vim.cmd('split ' .. vim.fn.fnameescape(H.root .. '/later.base'))
  vim.api.nvim_buf_set_lines(0, 0, -1, false, { 'filters: \'file.ext == "md"\'' })
  vim.cmd.write()
  H.wait(5000, function()
    return items(buf)[1].result ~= nil
  end, 'note to pick up the new base')
  H.eq(nil, items(buf)[1].err)
  H.eq({}, vim.diagnostic.get(buf))
  H.contains(H.virt_text(buf), 'a.md')
end

T['unknown #view falls back to the first view, reports it, and cycling clears the error'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['two.base'] = TWO,
    ['note.md'] = '# Note\n![[two.base#Nope]]\n',
  })
  H.setup()
  local buf = H.open('note.md')
  local text = H.virt_text(buf)
  H.contains(text, 'no view named "Nope"')
  H.eq(0, items(buf)[1].result.view)
  local d = vim.diagnostic.get(buf)
  H.eq(1, #d)
  H.eq(1, d[1].lnum)
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('<CR><Tab>')
  H.wait(3000, function()
    return items(buf)[1].result.type == 'list'
  end, 'next view')
  H.not_contains(H.virt_text(buf), 'Nope')
  H.eq({}, vim.diagnostic.get(buf))
  H.not_contains(table.concat(overlay_lines(), '\n'), 'Nope', 'overlay follows')
end

T['CJK and emoji cells truncate by display width in a 30-column window'] = function()
  H.vault({
    ['a.md'] = '---\ntitle: 日本語のとても長いタイトルです🚀🚀🚀\n---\n',
    ['b.md'] = '---\ntitle: 🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀\n---\n',
    ['c.md'] = '---\ntitle: plain\n---\n',
    ['t.base'] = MD .. 'views:\n  - type: table\n    order:\n      - file.name\n      - title\n',
    ['note.md'] = '# Note\n![[t.base]]\n',
  })
  setup_split()
  vim.cmd('vsplit')
  vim.api.nvim_win_set_width(0, 30)
  local buf = H.open('note.md')
  local win = vim.api.nvim_get_current_win()
  H.ok(virt_width(buf) <= text_cols(win), ('virtual lines (%d cells) wider than the window (%d)'):format(virt_width(buf), text_cols(win)))
  local v = H.virt(buf)[1].lines
  local table_w
  for i = 2, #v do
    local w = vim.api.nvim_strwidth(v[i])
    table_w = table_w or w
    H.eq(table_w, w, 'table line ' .. i .. ' misaligned: ' .. v[i])
  end
  local text = table.concat(v, '\n')
  H.contains(text, '日本')
  H.contains(text, '…')
  H.contains(text, '🚀')
end

T['a table with more columns than fit drops the overflow instead of spilling past the window'] = function()
  local fm = { '---' }
  local order = { '      - file.name' }
  for i = 1, 9 do
    fm[#fm + 1] = ('prop%d: value%d'):format(i, i)
    order[#order + 1] = ('      - prop%d'):format(i)
  end
  fm[#fm + 1] = '---'
  H.vault({
    ['a.md'] = table.concat(fm, '\n') .. '\n',
    ['t.base'] = MD .. 'views:\n  - type: table\n    order:\n' .. table.concat(order, '\n') .. '\n',
    ['note.md'] = '# Note\n![[t.base]]\n',
  })
  setup_split()
  vim.cmd('vsplit')
  vim.api.nvim_win_set_width(0, 40)
  local buf = H.open('note.md')
  local win = vim.api.nvim_get_current_win()
  H.ok(virt_width(buf) <= text_cols(win), ('virtual lines (%d cells) wider than the window (%d)'):format(virt_width(buf), text_cols(win)))
  local text = H.virt_text(buf)
  H.contains(text, '│ a.md ', 'kept columns stay readable instead of being squeezed to stubs')
  H.contains(text, '│ value1 ')
  H.contains(text, 'more columns')
end

T['narrow window: long view names and long list titles stay within the width'] = function()
  H.vault({
    ['a very long note title that keeps going and going.md'] = '---\nstatus: todo\n---\n',
    ['t.base'] = MD .. 'views:\n  - type: list\n    name: A rather long view name\n    order:\n      - file.name\n      - status\n  - type: table\n    name: Another long view name\n',
    ['note.md'] = '# Note\n![[t.base]]\n',
  })
  setup_split()
  vim.cmd('vsplit')
  vim.api.nvim_win_set_width(0, 30)
  local buf = H.open('note.md')
  local win = vim.api.nvim_get_current_win()
  local v = H.virt(buf)[1].lines
  for _, l in ipairs(v) do
    H.ok(vim.api.nvim_strwidth(l) <= text_cols(win), ('line wider than %d: %q'):format(text_cols(win), l))
  end
  H.contains(v[1], '2 results', 'the count survives')
  H.contains(v[1], '☰', 'the active view is still shown')
  H.contains(table.concat(v, '\n'), '• a very long')
end

T['results beyond max_height: one truncation line, the overlay scrolls over the full result'] = function()
  local files = { ['t.base'] = MD, ['note.md'] = '# Note\n![[t.base]]\nbelow\n' }
  for i = 1, 12 do
    files[('n%02d.md'):format(i)] = 'x\n'
  end
  H.vault(files)
  H.setup({ base = { max_height = 5 } })
  local buf = H.open('note.md')
  local it = items(buf)[1]
  local total = #it.rendered.lines
  local v = H.virt(buf)[1].lines
  H.eq(6, #v)
  H.eq((' … %d more lines (<CR> to browse)'):format(total - 5), v[6])
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('j')
  local o = B().overlay()
  H.eq(total, #overlay_lines(), 'overlay holds every line')
  H.eq(6, vim.api.nvim_win_get_height(o.win), 'overlay covers just the virtual lines')
  H.feed('G')
  H.eq(total, cursor()[1])
  H.feed('[[')
  H.eq(it.rendered.rows[#it.rendered.rows] + 1, cursor()[1])
  H.feed('gg]]')
  H.eq(it.rendered.rows[1] + 1, cursor()[1])
  H.feed('G')
  H.feed('j')
  H.ok(not B().overlay())
  H.eq({ 3, 0 }, cursor())
end

T['entering an embed at the bottom of the window scrolls so the overlay fits under its anchor'] = function()
  local lines = { '# Note' }
  for i = 2, 36 do
    lines[i] = 'line ' .. i
  end
  lines[37] = '![[t.base]]'
  lines[38] = 'after'
  H.vault({ ['a.md'] = 'x\n', ['b.md'] = 'x\n', ['c.md'] = 'x\n', ['t.base'] = MD, ['note.md'] = table.concat(lines, '\n') .. '\n' })
  H.setup()
  H.open('note.md')
  local src = vim.api.nvim_get_current_win()
  vim.api.nvim_win_set_cursor(0, { 37, 0 })
  vim.cmd.redraw()
  H.feed('<CR>')
  local o = B().overlay()
  H.ok(o, 'entered')
  vim.cmd.redraw()
  local wpos, wh = vim.api.nvim_win_get_position(src)[1], vim.api.nvim_win_get_height(src)
  local opos, oh = vim.api.nvim_win_get_position(o.win)[1], vim.api.nvim_win_get_height(o.win)
  H.eq(#o.item.rendered.lines, oh, 'whole result visible')
  H.ok(opos + oh <= wpos + wh, ('overlay rows %d..%d past window end %d'):format(opos, opos + oh, wpos + wh))
  local arow = vim.fn.screenpos(src, 37, 1).row
  H.ok(arow > 0, 'anchor line visible')
  H.eq(arow, opos, 'overlay starts right below the anchor')
end

T['a wrapped anchor line puts the overlay below its last screen row'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['t.base'] = MD,
    ['note.md'] = '# Note\n' .. string.rep('word ', 50) .. '![[t.base]]\nafter\n',
  })
  H.setup()
  H.open('note.md')
  local src = vim.api.nvim_get_current_win()
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('<CR>')
  local o = B().overlay()
  H.ok(o, 'entered')
  vim.cmd.redraw()
  local h = vim.api.nvim_win_text_height(src, { start_row = 1, end_row = 1 })
  local text_rows = h.all - h.fill
  H.ok(text_rows > 1, 'anchor should wrap')
  local arow = vim.fn.screenpos(src, 2, 1).row
  H.eq(arow - 1 + text_rows, vim.api.nvim_win_get_position(o.win)[1])
end

T['leaving the overlay with :q or to another window closes it without stealing focus'] = function()
  H.vault({ ['a.md'] = 'x\n', ['t.base'] = MD, ['note.md'] = '# Note\n![[t.base]]\n' })
  setup_split()
  H.open('note.md')
  local w1 = vim.api.nvim_get_current_win()
  vim.cmd('vsplit ' .. vim.fn.fnameescape(H.root .. '/a.md'))
  local w2 = vim.api.nvim_get_current_win()
  vim.api.nvim_set_current_win(w1)
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('<CR>')
  H.ok(B().overlay())
  vim.cmd('quit')
  wait_closed()
  H.eq(w1, vim.api.nvim_get_current_win())
  H.eq(2, #vim.api.nvim_tabpage_list_wins(0), ':q only closed the overlay')

  H.feed('<CR>')
  H.ok(B().overlay())
  vim.api.nvim_set_current_win(w2)
  wait_closed()
  H.eq({}, floats())
  H.eq(w2, vim.api.nvim_get_current_win(), 'focus must stay where the user went')
end

T['cards: every line of a card (borders too) opens that card; a link value opens its link'] = function()
  H.vault({
    ['a.md'] = '---\nstatus: todo\nref: "[[c]]"\n---\n',
    ['b.md'] = '---\nstatus: done\nref: "[[a]]"\n---\n',
    ['c.md'] = 'x\n',
    ['t.base'] = MD .. 'views:\n  - type: cards\n    order:\n      - file.name\n      - status\n      - ref\n',
    ['note.md'] = '# Note\n![[t.base]]\n',
  })
  H.setup()
  local buf = H.open('note.md')
  local function enter()
    vim.api.nvim_set_current_buf(buf)
    H.wait_bases(buf)
    vim.api.nvim_win_set_cursor(0, { 2, 0 })
    H.feed('<CR>')
    H.ok(B().overlay(), 'entered')
  end
  enter()
  -- padding after "done" on b's second line
  local l, c = overlay_find('status done')
  vim.api.nvim_win_set_cursor(0, { l, c + #'status done' + 2 })
  H.feed('<CR>')
  H.ok(not B().overlay())
  H.eq('b.md', current_file())

  enter()
  -- the bottom border of b's card
  local rows = items(buf)[1].rendered.rows
  local bl = rows[1] + 1 + 3 -- 3 content lines after the first card row's top border: its bottom border (1-based)
  H.contains(overlay_lines()[bl], '╰')
  local bcol = overlay_lines()[bl]:find('╰', 4, true) -- second card's corner
  vim.api.nvim_win_set_cursor(0, { bl, bcol + 3 })
  H.feed('<CR>')
  H.eq('b.md', current_file())

  enter()
  -- link value "c" on a's card
  l, c = overlay_find('ref c')
  vim.api.nvim_win_set_cursor(0, { l, c + 4 })
  H.feed('<CR>')
  H.eq('c.md', current_file())
end

T['table: a link cell opens the link, other cells open the row note, after wide chars'] = function()
  H.vault({
    ['a.md'] = '---\ntitle: 日本語🚀\nref: "[[c]]"\n---\n',
    ['c.md'] = 'x\n',
    ['t.base'] = MD .. 'views:\n  - type: table\n    order:\n      - file.name\n      - title\n      - ref\n',
    ['note.md'] = '# Note\n![[t.base]]\n',
  })
  H.setup()
  local buf = H.open('note.md')
  local function enter()
    vim.api.nvim_set_current_buf(buf)
    H.wait_bases(buf)
    vim.api.nvim_win_set_cursor(0, { 2, 0 })
    H.feed('<CR>')
    H.ok(B().overlay(), 'entered')
  end
  enter()
  local l = overlay_find('日本語🚀')
  local line = overlay_lines()[l]
  local s = line:find('│ c ', #'│ a.md', true)
  vim.api.nvim_win_set_cursor(0, { l, s - 1 + #'│ ' })
  H.feed('<CR>')
  H.eq('c.md', current_file())

  enter()
  vim.api.nvim_win_set_cursor(0, { l, line:find('本', 1, true) - 1 })
  H.feed('<CR>')
  H.eq('a.md', current_file())

  enter()
  -- trailing border of the row still belongs to the row
  vim.api.nvim_win_set_cursor(0, { l, #line - 1 })
  H.feed('gf')
  H.eq('a.md', current_file())
end

T['counted j/k move over bases instead of entering them; counts inside stay inside'] = function()
  H.vault({ ['a.md'] = 'x\n', ['b.md'] = 'x\n', ['t.base'] = MD, ['note.md'] = '# Note\nl2\n![[t.base]]\nl4\nl5\nl6\nl7\n' })
  H.setup()
  local buf = H.open('note.md')
  vim.api.nvim_win_set_cursor(0, { 3, 0 })
  H.feed('3j')
  H.ok(not B().overlay(), '3j entered')
  H.eq(6, cursor()[1])
  vim.api.nvim_win_set_cursor(0, { 4, 0 })
  H.feed('2k')
  H.ok(not B().overlay(), '2k entered')
  H.eq(2, cursor()[1])

  vim.api.nvim_win_set_cursor(0, { 3, 0 })
  H.feed('j')
  local first = items(buf)[1].rendered.rows[1] + 1
  H.eq(first, cursor()[1])
  H.feed('1j')
  H.ok(B().overlay())
  H.eq(first + 1, cursor()[1])
  H.feed('99j')
  H.ok(B().overlay(), '99j must not leave')
  H.eq(#overlay_lines(), cursor()[1])
  H.feed('99k')
  H.ok(B().overlay(), '99k must not leave')
  H.eq(1, cursor()[1])
end

T['resizing re-renders the virtual lines and an open overlay at the new width'] = function()
  H.vault({
    ['a.md'] = '---\ntitle: ' .. string.rep('long title ', 8) .. '\n---\n',
    ['t.base'] = MD .. 'views:\n  - type: table\n    order:\n      - file.name\n      - title\n',
    ['note.md'] = '# Note\n![[t.base]]\n',
  })
  H.setup()
  local buf = H.open('note.md')
  local win = vim.api.nvim_get_current_win()
  H.ok(virt_width(buf) > 60, 'wide at first')
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('<CR>')
  local o = B().overlay()
  vim.o.columns = 60
  H.wait(2000, function()
    return virt_width(buf) <= text_cols(win)
  end, 're-render at 60 columns')
  H.eq(items(buf)[1].rendered.lines, overlay_lines(), 'overlay re-rendered')
  H.eq(text_cols(win), vim.api.nvim_win_get_width(o.win), 'overlay width')
end

T['one buffer in two windows of different widths renders for the narrower one'] = function()
  H.vault({
    ['a.md'] = '---\ntitle: ' .. string.rep('long title ', 8) .. '\n---\n',
    ['t.base'] = MD .. 'views:\n  - type: table\n    order:\n      - file.name\n      - title\n',
    ['note.md'] = '# Note\n![[t.base]]\n',
  })
  setup_split()
  local buf = H.open('note.md')
  local wide = vim.api.nvim_get_current_win()
  vim.cmd('vsplit')
  local narrow = vim.api.nvim_get_current_win()
  vim.api.nvim_win_set_width(narrow, 35)
  vim.api.nvim_set_current_win(wide) -- the wider window is the current one
  H.wait(2000, function()
    -- headless -l has no main loop to fire WinResized
    vim.api.nvim_exec_autocmds('WinResized', {})
    return virt_width(buf) <= text_cols(narrow)
  end, 'render for the narrow window')
  vim.api.nvim_win_close(narrow, true)
  H.wait(2000, function()
    vim.api.nvim_exec_autocmds('WinResized', {})
    return virt_width(buf) > 40
  end, 'wider again after the narrow window closed')
end

T['saving a note refreshes every visible note embedding the base, hidden ones when shown'] = function()
  H.vault({
    ['a.md'] = '---\nstatus: todo\n---\n',
    ['t.base'] = MD .. 'views:\n  - type: table\n    order:\n      - file.name\n      - status\n',
    ['n1.md'] = '![[t.base]]\n',
    ['n2.md'] = '![[t.base]]\n',
    ['n3.md'] = '![[t.base]]\n',
  })
  setup_split()
  local b3 = H.open('n3.md')
  local b1 = H.open('n1.md')
  vim.cmd('split')
  local b2 = H.open('n2.md')
  vim.cmd('split')
  vim.cmd.edit(vim.fn.fnameescape(H.root .. '/a.md'))
  vim.api.nvim_buf_set_lines(0, 1, 2, false, { 'status: done' })
  vim.cmd.write()
  H.wait(5000, function()
    return H.virt_text(b1):find('done', 1, true) and H.virt_text(b2):find('done', 1, true)
  end, 'both visible notes to update')
  vim.api.nvim_set_current_buf(b3)
  H.wait(5000, function()
    return H.virt_text(b3):find('done', 1, true) ~= nil
  end, 'hidden note to update when shown')
end

T['.base buffer: invalid YAML mid-edit shows the error on its line, then recovers'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['q.base'] = MD .. 'views:\n  - type: table\n    name: T\n',
  })
  H.setup()
  local buf = H.open('q.base')
  H.contains(H.virt_text(buf), 'a.md')
  vim.api.nvim_buf_set_lines(buf, 2, 3, false, { '  - type: [table' })
  changed(buf)
  H.wait(3000, function()
    return items(buf)[1].err ~= nil
  end, 'error')
  local err = items(buf)[1].err
  H.contains(H.virt_text(buf), 'yaml')
  H.not_contains(H.virt_text(buf), 'a.md', 'stale result dropped')
  local d = vim.diagnostic.get(buf)
  H.eq(1, #d)
  local n = tonumber(err:match('line (%d+)'))
  H.ok(n, 'error names a line: ' .. err)
  H.eq(n - 1, d[1].lnum, 'diagnostic on the reported line')

  vim.api.nvim_buf_set_lines(buf, 2, 3, false, { '  - type: table' })
  changed(buf)
  H.wait(3000, function()
    return items(buf)[1].result and not items(buf)[1].err
  end, 'recover')
  H.contains(H.virt_text(buf), 'a.md')
  H.eq({}, vim.diagnostic.get(buf))
end

T['.base buffer: expression errors become diagnostics on the offending line'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['q.base'] = '# comment\nviews:\n  - type: table\n    filters: \'price >>> 5\'\n',
  })
  H.setup()
  local buf = H.open('q.base')
  local d = vim.diagnostic.get(buf)
  H.eq(1, #d)
  H.contains(d[1].message, 'price >>> 5')
  H.eq(3, d[1].lnum)
  H.contains(H.virt_text(buf), '⚠')
end

T['deleting the embed while its overlay is open closes the overlay'] = function()
  H.vault({ ['a.md'] = 'x\n', ['t.base'] = MD, ['note.md'] = '# Note\n![[t.base]]\ntail\n' })
  H.setup()
  local buf = H.open('note.md')
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('<CR>')
  H.ok(B().overlay())
  vim.api.nvim_buf_set_lines(buf, 1, 2, false, {})
  changed(buf)
  wait_closed()
  H.eq({}, floats())
  H.eq(buf, vim.api.nvim_get_current_buf())
end

T['rapid <Tab><Tab> moves two views ahead, not one'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['three.base'] = MD .. 'views:\n  - type: table\n    name: One\n  - type: list\n    name: Two\n  - type: cards\n    name: Three\n',
    ['note.md'] = '# Note\n![[three.base]]\n',
  })
  H.setup()
  local buf = H.open('note.md')
  vim.api.nvim_win_set_cursor(0, { 2, 0 })
  H.feed('<CR>')
  H.feed('<Tab><Tab>')
  H.wait(3000, function()
    return items(buf)[1].result.view == 2
  end, 'third view')
  vim.wait(300)
  H.eq(2, items(buf)[1].result.view)
  H.feed('<S-Tab><S-Tab><S-Tab>')
  H.wait(3000, function()
    return items(buf)[1].result.view == 2 and items(buf)[1].view == '2'
  end, 'full cycle back')
end

T['```base block: <CR> anywhere in its source enters; k from below enters at the bottom'] = function()
  H.vault({
    ['a.md'] = 'x\n',
    ['note.md'] = '# Note\n```base\nfilters: \'file.ext == "md"\'\n```\nbelow\n',
  })
  H.setup()
  local buf = H.open('note.md')
  H.eq(3, H.virt(buf)[1].anchor, 'renders below the closing fence')
  vim.api.nvim_win_set_cursor(0, { 3, 5 })
  H.feed('<CR>')
  local o = B().overlay()
  H.ok(o and o.item.kind == 'block', '<CR> inside the block enters')
  H.feed('q')
  H.eq({ 4, 0 }, cursor())
  vim.api.nvim_win_set_cursor(0, { 5, 0 })
  H.feed('k')
  o = B().overlay()
  H.ok(o, 'k from below enters')
  H.eq(o.item.rendered.rows[#o.item.rendered.rows] + 1, cursor()[1])
  H.feed('q')
  -- editing the block's source re-queries it
  vim.api.nvim_buf_set_lines(buf, 2, 3, false, { 'filters: \'file.name == "nothing"\'' })
  changed(buf)
  H.wait(3000, function()
    return H.virt_text(buf):find('No results', 1, true) ~= nil
  end, 'new source rendered')
end

return T
