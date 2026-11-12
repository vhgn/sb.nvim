-- Micro-benchmarks for the non-LSP runtime paths of brain.nvim.
-- Run through nvim/tests/bench/run.sh (it builds the binary, writes the
-- vaults and isolates XDG dirs). Prints one line per benchmark:
--   name  median-time-per-call  garbage-per-call
local here = vim.fs.dirname(debug.getinfo(1, 'S').source:sub(2))
package.path = vim.fs.dirname(here) .. '/?.lua;' .. package.path
local H = require('helpers')
vim.opt.rtp:prepend(H.plugin)

local only = os.getenv('BENCH_ONLY') -- substring filter
local SAMPLES = tonumber(os.getenv('BENCH_SAMPLES') or '7')
local hr = vim.uv.hrtime

local function fmt_time(ns)
  if ns >= 1e6 then
    return ('%.2f ms'):format(ns / 1e6)
  elseif ns >= 1e3 then
    return ('%.1f µs'):format(ns / 1e3)
  end
  return ('%d ns'):format(ns)
end

local function fmt_kb(kb)
  if kb >= 1024 then
    return ('%.2f MiB'):format(kb / 1024)
  end
  return ('%.1f KiB'):format(kb)
end

local results = {}

--- Times fn (called `iters` times per sample), median over SAMPLES samples;
--- garbage is measured in a separate run with the GC stopped.
local function bench(name, iters, fn, setup)
  if only and not name:find(only, 1, true) then
    return
  end
  iters = iters or 1
  if setup then
    setup()
  end
  fn() -- warm up
  local times = {}
  for s = 1, SAMPLES do
    collectgarbage('collect')
    local t0 = hr()
    for _ = 1, iters do
      fn()
    end
    times[s] = (hr() - t0) / iters
  end
  local prof = os.getenv('BENCH_PROFILE')
  if prof and name:find(prof, 1, true) then
    local jp = require('jit.p')
    jp.start(os.getenv('BENCH_PROFILE_MODE') or 'Fl', os.getenv('BENCH_PROFILE_OUT') or '/dev/stderr')
    for _ = 1, iters * 3 do
      fn()
    end
    jp.stop()
  end
  table.sort(times)
  local med = times[math.floor((#times + 1) / 2)]
  collectgarbage('collect')
  collectgarbage('stop')
  local k0 = collectgarbage('count')
  for _ = 1, iters do
    fn()
  end
  local kb = (collectgarbage('count') - k0) / iters
  collectgarbage('restart')
  collectgarbage('collect')
  local line = ('%-40s %12s %12s'):format(name, fmt_time(med), fmt_kb(kb))
  results[#results + 1] = line
  io.stdout:write(line .. '\n')
  io.stdout:flush()
end

local function wait(ms, cond, msg)
  if not vim.wait(ms, cond, 1) then
    error('timeout: ' .. msg)
  end
end

-- ------------------------------------------------------------ notes

--- A 20-line block of mixed markdown: prose, prices, inline/display math,
--- images, embeds, fences.
local function block(k)
  return {
    ('## Section %d'):format(k),
    ('Some prose with $x^%d$ inline math and a price of $5 and $10 for it.'):format(k % 9),
    'Another line with `inline $code$` and $\\alpha_i + \\beta$ here, then ``a`b`` text.',
    ('![[img%d.png]] and a link [[note-%d]] in the middle of the text'):format(k, k),
    ('![alt text](images/pic%d.png "a title") then more words after the image'):format(k),
    '- list item with **bold**, _emphasis_ and [[some link|alias]] at the end',
    '$$',
    ('\\sum_{i=1}^{%d} i = \\frac{n(n+1)}{2}'):format(k),
    '$$',
    '```lua',
    'local x = "$not math$" -- ![[not-an-embed.png]]',
    '```',
    'Plain prose line without anything special, long enough to be realistic text in notes.',
    ('Inline $a_{ij}$ and $\\vec{v}$, cost $3.50 total, %d items.'):format(k),
    ('$$E = mc^%d$$ and \\[ x + y \\] on one line'):format(k % 4 + 1),
    '> a quote that mentions ![[other-note]] and an escaped \\$ sign',
    '```mermaid',
    ('graph TD; A%d-->B'):format(k),
    '```',
    'Closing prose for the section, with a trailing URL https://example.com/x?y=1.',
  }
end

local function note(n)
  local lines = { '---', 'title: Bench', 'tags: [bench]', '---' }
  local k = 0
  while #lines < n do
    k = k + 1
    vim.list_extend(lines, block(k))
  end
  while #lines > n do
    lines[#lines] = nil
  end
  -- never cut a fence open: close it
  return lines
end

local NOTES = {}
for _, n in ipairs({ 200, 2000, 20000 }) do
  NOTES[n] = note(n)
end

-- ------------------------------------------------------------ setup

local vault1k = assert(os.getenv('BENCH_VAULT_1K'), 'BENCH_VAULT_1K')
local vault10k = os.getenv('BENCH_VAULT_10K')
H.root = vault1k
H.setup({ base = { debounce = 1e7 }, images = { enabled = true } })
vim.o.columns, vim.o.lines = 160, 50
local client = require('brain.client')
local scan = require('brain.scan')
local media = require('brain.media')
local base = require('brain.base')
local render = require('brain.base.render')

-- ------------------------------------------------------------ 1. scan

for _, n in ipairs({ 200, 2000, 20000 }) do
  local lines = NOTES[n]
  bench(('scan/%d'):format(n), n >= 20000 and 3 or n >= 2000 and 20 or 200, function()
    scan.scan(lines)
  end)
end

-- ------------------------------------------------------------ 2. media

--- A scratch markdown buffer in the current window, attached to vault1k.
local function md_buf(lines)
  local path = vault1k .. '/bench-' .. #lines .. '.md'
  vim.fn.writefile(lines, path)
  vim.cmd('silent! %bwipeout!')
  vim.cmd.edit(vim.fn.fnameescape(path))
  local buf = vim.api.nvim_get_current_buf()
  -- markdown starts treesitter, whose highlighter would dominate cursor moves
  pcall(vim.treesitter.stop, buf)
  return buf
end

local function settle(buf)
  -- wait for every outstanding daemon reply (tex, resolve, texUnicode)
  vim.wait(200, function()
    return false
  end, 5)
  local st = media.state[buf]
  return st
end

for _, n in ipairs({ 200, 2000, 20000 }) do
  if only and not (('media.refresh/%d'):format(n)):find(only, 1, true) and not (('media.cursor_moved/%d'):format(n)):find(only, 1, true) then
    goto continue
  end
  do
    local buf = md_buf(NOTES[n])
    media.refresh(buf)
    wait(10000, function()
      return #media.state[buf].inline > 0
    end, 'inline math')
    settle(buf)
    -- synchronous cost of one refresh (scan, ids, requests sent, extmarks)
    bench(('media.refresh/%d'):format(n), n >= 20000 and 2 or 5, function()
      if scan.forget then
        scan.forget(buf) -- as after an edit: the note is scanned again
      end
      media.refresh(buf)
    end)
    settle(buf)
    -- refresh until the inline math is concealed again (texUnicode round trip)
    bench(('media.refresh+inline/%d'):format(n), 1, function()
      local s = media.state[buf]
      s.inline = {}
      media.refresh(buf)
      local gen = s.gen
      wait(10000, function()
        return #s.inline > 0 and s.gen == gen
      end, 'inline')
    end)
    settle(buf)
    -- j/k: cursor moves one line at a time through the note
    local nlines = vim.api.nvim_buf_line_count(buf)
    local cur = 1
    bench(('media.cursor_moved/%d'):format(n), 200, function()
      cur = cur % nlines + 1
      vim.api.nvim_win_set_cursor(0, { cur, 0 })
      media.cursor_moved(buf)
    end)
    media.detach(buf)
  end
  ::continue::
end

-- ------------------------------------------------------------ 3. daemon round trips

local function rt(root, method, params)
  local done, err = false, nil
  client.request(root, method, params, function(e)
    done, err = true, e
  end)
  vim.wait(10000, function()
    return done
  end, 0)
  assert(done and not err, method .. ': ' .. tostring(err))
end

local inline_src = {
  '\\alpha^2 + \\beta_i', 'x \\leq \\sqrt{y}', '\\mathbb{R}^n', 'e^{i\\pi} = -1', '\\frac{1}{2}',
  'O(n \\log n)', 'a_{ij}', '\\sum_k x_k', "f'(x)", '\\vec{v} \\cdot \\vec{w}',
}
local items50 = {}
for i = 1, 50 do
  items50[i] = inline_src[(i - 1) % #inline_src + 1]
end
local items500 = {}
for i = 1, 500 do
  items500[i] = inline_src[(i - 1) % #inline_src + 1] .. ' + ' .. i
end

bench('client.ping', 200, function()
  rt(vault1k, 'ping', {})
end)
bench('client.texUnicode/50', 50, function()
  rt(vault1k, 'texUnicode', { items = items50 })
end)
bench('client.texUnicode/500', 10, function()
  rt(vault1k, 'texUnicode', { items = items500 })
end)
bench('client.resolve', 100, function()
  rt(vault1k, 'resolve', { targets = { 'note-3', 'missing.png' }, from = vault1k .. '/note-1.md' })
end)
local pal = require('brain.colors').palette()
bench('client.tex/cached', 50, function()
  rt(vault1k, 'tex', { src = '\\frac{a}{b} + \\sqrt{x}', display = true, fg = pal.fg, error = pal.red, size = 21, palette = { red = pal.red, blue = pal.blue } })
end)
for _, v in ipairs({ { '1k', vault1k }, { '10k', vault10k } }) do
  if v[2] then
    rt(v[2], 'ping', {})
    bench(('client.query/%s'):format(v[1]), v[1] == '1k' and 20 or 5, function()
      rt(v[2], 'query', { path = v[2] .. '/bench.base', this = v[2] .. '/note-5.md' })
    end)
    bench(('client.query-all/%s'):format(v[1]), v[1] == '1k' and 5 or 2, function()
      rt(v[2], 'query', { path = v[2] .. '/all.base', this = v[2] .. '/note-5.md' })
    end)
  end
end

-- ------------------------------------------------------------ 4. base rendering

--- A query result of n rows for a view type, shaped like the daemon's.
local function result(n, kind)
  local rows = {}
  for i = 1, n do
    local cells = {
      { t = ('note-%d'):format(i), k = 'link', l = ('/v/note-%d.md'):format(i) },
      { t = ({ 'todo', 'doing', 'done' })[i % 3 + 1] },
      { t = tostring(i % 5 + 1), k = 'number' },
      { t = ('2024-%02d-%02d'):format(i % 12 + 1, i % 28 + 1), k = 'date' },
      { t = 'A longer text property value that will need truncating in narrow windows ' .. i },
      { t = 'bench, n' .. (i % 7), k = 'list' },
    }
    rows[i] = { path = ('/v/note-%d.md'):format(i), rel = ('note-%d.md'):format(i), cells = cells, lat = 40 + (i % 50) / 10, lng = -70 + (i % 37) / 10 }
  end
  return {
    views = { { name = 'Table', type = 'table' }, { name = 'Cards', type = 'cards' }, { name = 'List', type = 'list' }, { name = 'Map', type = 'map' } },
    view = 0,
    type = kind,
    name = kind,
    columns = { { id = 'file.name', name = 'Name' }, { id = 'status', name = 'Status' }, { id = 'priority', name = 'Priority' }, { id = 'due', name = 'Due' }, { id = 'summary', name = 'Summary' }, { id = 'file.tags', name = 'Tags' } },
    groups = { { key = '', hasKey = false, rows = rows } },
    summaries = { {}, {}, { t = '3.0' } },
    total = n,
    shown = n,
  }
end

for _, kind in ipairs({ 'table', 'cards', 'list', 'map' }) do
  for _, n in ipairs({ 100, 1000, 10000 }) do
    local res = result(n, kind)
    bench(('render.%s/%d'):format(kind, n), n >= 10000 and 1 or n >= 1000 and 3 or 20, function()
      render.render(res, 140, { title = 'bench' })
    end)
  end
end

-- base.refresh in a note with several embeds (results present: the
-- TextChanged path), item_at per j/k, and redraw on resize.
do
  local embeds = { '![[all.base]]', '![[bench.base]]', '![[all.base#Cards]]', '![[all.base#List]]' }
  local lines = vim.deepcopy(NOTES[2000])
  for i, e in ipairs(embeds) do
    table.insert(lines, i * 400, e)
  end
  local buf = md_buf(lines)
  base.refresh(buf)
  H.wait_bases(buf)
  local nrows = 0
  for _, it in ipairs(base.state[buf].items) do
    nrows = nrows + #((it.result and it.result.groups and it.result.groups[1] or {}).rows or {})
  end
  io.stdout:write(('# base: %d embeds, %d rows total\n'):format(#base.state[buf].items, nrows))
  bench('base.refresh/4-embeds', 5, function()
    if scan.forget then
      scan.forget(buf)
    end
    base.refresh(buf, { requery = false })
  end)
  -- what a debounced TextChanged runs in a note: bases, then media
  bench('textchanged/4-embeds+media', 5, function()
    if scan.forget then
      scan.forget(buf)
    end
    base.refresh(buf, { requery = false })
    media.refresh(buf)
  end)
  bench('base.redraw/same-width', 5, function()
    base.redraw(buf)
  end)
  -- headless windows can't be resized reliably: toggling 'number' changes
  -- the text width just the same
  local w = 0
  bench('base.redraw/resized', 5, function()
    w = (w + 1) % 2
    vim.wo.number = w == 1
    base.redraw(buf)
  end)
  vim.wo.number = false
  local nl = vim.api.nvim_buf_line_count(buf)
  local l = 0
  bench('base.item_at/j', 1000, function()
    l = (l + 1) % nl
    base.item_at(buf, l)
  end)
  bench('base.item_at/k', 1000, function()
    l = (l + 1) % nl
    base.item_at(buf, l, false, 'last')
  end)
  bench('base.item_at/enter(span)', 20, function()
    l = (l + 1) % nl
    base.item_at(buf, l, true, 0)
  end)
end

io.stdout:write('# done\n')
vim.cmd('qa!')
