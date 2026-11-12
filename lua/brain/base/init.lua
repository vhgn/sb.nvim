-- Renders bases as virtual lines: below `![[x.base]]` / ```base blocks in
-- markdown, and below the YAML of .base buffers (live, from unsaved text).
-- The rendered view can't be edited, but you can step into it: a read-only
-- overlay window takes its place, where rows and links can be opened.
local client = require('brain.client')
local config = require('brain.config')
local render = require('brain.base.render')
local scan = require('brain.scan')

local M = {}

local ns = vim.api.nvim_create_namespace('brain.base')
local diag_ns = vim.api.nvim_create_namespace('brain.base.diagnostics')
M.ns = ns

---@class brain.Embed
---@field key string
---@field kind 'embed'|'block'|'file'
---@field line integer anchor line (0-based); virtual lines go below it
---@field target? string
---@field path? string resolved .base path
---@field source? string inline YAML
---@field view? string
---@field result? table
---@field err? string
---@field rendered? brain.Rendered
---@field mark? integer
---@field col? integer byte column of the embed on its anchor line
---@field virt_n? integer number of virtual lines drawn
---@field seq integer

---@type table<integer, {items: brain.Embed[], root: string, seq: integer}>
local state = {}
M.state = state

local overlay = nil ---@type {win: integer, buf: integer, src_win: integer, src_buf: integer, item: brain.Embed}?

local function buf_root(buf)
  return vim.b[buf].brain_root
end

--- Text width available to virtual lines: virt_lines belong to the buffer, so
--- with several windows on it, render for the narrowest one.
local function text_width(buf)
  local w
  for _, win in ipairs(vim.fn.win_findbuf(buf)) do
    if vim.api.nvim_win_get_config(win).relative == '' then
      local info = vim.fn.getwininfo(win)[1]
      if info then
        local tw = info.width - info.textoff - 1
        w = w and math.min(w, tw) or tw
      end
    end
  end
  return math.max(24, w or 80)
end

--- Virtual lines (highlighted chunks) for the first max_h lines of r.
local function virt_lines(r, max_h)
  -- highlights by line, in one pass (they come in line order from the builder)
  local by_line = {}
  for _, h in ipairs(r.hls) do
    local ln = h[1]
    if ln < max_h then
      local l = by_line[ln]
      if not l then
        l = {}
        by_line[ln] = l
      end
      l[#l + 1] = h
    end
  end
  local virt = {}
  for i, line in ipairs(r.lines) do
    if i > max_h then
      table.insert(virt, { { string.format(' … %d more lines (%s to browse)', #r.lines - max_h, config.options.keys.enter), 'BrainBaseMeta' } })
      break
    end
    -- split the line into highlighted chunks
    local chunks, col = {}, 0
    local hls = by_line[i - 1] or {}
    if #hls > 1 then
      table.sort(hls, function(a, b)
        return a[2] < b[2]
      end)
    end
    for _, h in ipairs(hls) do
      if h[2] > col then
        chunks[#chunks + 1] = { line:sub(col + 1, h[2]), 'BrainBaseText' }
      end
      if h[3] > math.max(col, h[2]) then
        chunks[#chunks + 1] = { line:sub(math.max(col, h[2]) + 1, h[3]), h[4] }
      end
      col = math.max(col, h[3])
    end
    if col < #line then
      chunks[#chunks + 1] = { line:sub(col + 1), 'BrainBaseText' }
    end
    if #chunks == 0 then
      chunks = { { '' } }
    end
    virt[#virt + 1] = chunks
  end
  return virt
end

local function draw(buf, item)
  if not vim.api.nvim_buf_is_valid(buf) then
    return
  end
  local width = text_width(buf)
  local title
  if item.kind == 'embed' then
    title = vim.fn.fnamemodify(item.target or '', ':t:r')
  end
  local max_h = config.options.base.max_height
  local virt
  -- the same answer at the same width renders the same: reuse it (every
  -- debounced edit and every resize redraws all bases)
  local c = item.drawn
  if c and (item.result or item.err) and c.result == item.result and c.err == item.err and c.width == width and c.title == title and c.max_h == max_h and c.enter == config.options.keys.enter and item.rendered == c.rendered then
    virt = c.virt
  else
    if item.result or item.err then
      item.rendered = render.render(item.result or {}, width, { title = title, error = item.err })
    else
      item.rendered = { lines = { ' … loading base' }, hls = { { 0, 0, 16, 'BrainBaseMeta' } }, targets = {}, rows = {} }
    end
    virt = virt_lines(item.rendered, max_h)
    item.drawn = { result = item.result, err = item.err, width = width, title = title, max_h = max_h, enter = config.options.keys.enter, rendered = item.rendered, virt = virt }
  end
  item.virt_n = #virt
  local line_count = vim.api.nvim_buf_line_count(buf)
  local anchor = math.min(item.line, line_count - 1)
  -- several embeds on one line: anchoring at their columns keeps their
  -- virtual lines in the same order as the embeds
  local text = vim.api.nvim_buf_get_lines(buf, anchor, anchor + 1, false)[1] or ''
  local col = math.min(item.col or 0, #text)
  local ok, id = pcall(vim.api.nvim_buf_set_extmark, buf, ns, anchor, col, {
    id = item.mark,
    virt_lines = virt,
    virt_lines_leftcol = false,
    undo_restore = false,
    invalidate = true,
  })
  if ok then
    item.mark = id
  end
end

--- Best line (0-based) in a .base buffer for an error message: the line a YAML
--- error names, else the line holding the quoted expression, else the first.
local function error_line(buf, msg)
  local n = vim.api.nvim_buf_line_count(buf)
  local ln = tonumber(msg:match('^yaml: line (%d+)') or msg:match('^line (%d+):'))
  if ln then
    return math.max(0, math.min(ln - 1, n - 1))
  end
  local expr = msg:match('"(.-)"')
  if expr and expr ~= '' then
    for i, l in ipairs(vim.api.nvim_buf_get_lines(buf, 0, -1, false)) do
      if l:find(expr, 1, true) then
        return i - 1
      end
    end
  end
  return 0
end

local function set_diagnostics(buf)
  local st = state[buf]
  if not st then
    return
  end
  local diags = {}
  for _, item in ipairs(st.items) do
    local msgs = {}
    if item.err then
      msgs[1] = item.err
    end
    for _, e in ipairs(item.result and item.result.errors or {}) do
      msgs[#msgs + 1] = e
    end
    for _, m in ipairs(msgs) do
      diags[#diags + 1] = {
        lnum = item.kind == 'file' and error_line(buf, m) or item.line,
        col = 0,
        severity = vim.diagnostic.severity.WARN,
        source = 'brain',
        message = m,
      }
    end
  end
  -- skip no-op updates (every query answer lands here)
  if vim.deep_equal(st.diags or {}, diags) then
    return
  end
  st.diags = diags
  vim.diagnostic.set(diag_ns, buf, diags)
end

local function query(buf, item)
  local st = state[buf]
  local root = st.root
  item.seq = (item.seq or 0) + 1
  local seq = item.seq
  local params = {
    path = item.path,
    source = item.source,
    view = item.view or '',
    this = vim.api.nvim_buf_get_name(buf),
  }
  client.request(root, 'query', params, function(err, res)
    if seq ~= item.seq or not vim.api.nvim_buf_is_valid(buf) then
      return
    end
    item.err, item.result = err, res
    if res == vim.NIL then
      item.result = nil
    end
    draw(buf, item)
    set_diagnostics(buf)
    M.sync_overlay(buf, item)
  end)
end

local function resolve_and_query(buf, item)
  if item.kind ~= 'embed' then
    return query(buf, item)
  end
  local from = vim.api.nvim_buf_get_name(buf)
  client.request(state[buf].root, 'resolve', { targets = { item.target }, from = from }, function(err, res)
    if not vim.api.nvim_buf_is_valid(buf) then
      return
    end
    local path = not err and res and res[1] or ''
    if path == '' then
      -- not indexed (yet): try relative to the note
      local rel = vim.fs.normalize(vim.fs.dirname(from) .. '/' .. item.target)
      if vim.uv.fs_stat(rel) then
        path = rel
      end
    end
    if path == '' then
      item.err = 'base not found: ' .. item.target
      item.result = nil
      draw(buf, item)
      set_diagnostics(buf)
      return
    end
    item.path = path
    query(buf, item)
  end)
end

--- Rescans the buffer and refreshes every base in it.
---@param opts? {requery?: boolean}
function M.refresh(buf, opts)
  opts = opts or {}
  if not vim.api.nvim_buf_is_valid(buf) or not buf_root(buf) then
    return
  end
  local st = state[buf]
  if not st then
    st = { items = {}, root = buf_root(buf), seq = 0 }
    state[buf] = st
  end
  local old = {}
  for _, it in ipairs(st.items) do
    old[it.key] = old[it.key] or {}
    table.insert(old[it.key], it)
  end
  local items = {}
  local is_base_file = vim.bo[buf].filetype == 'base'
  if is_base_file then
    if config.options.base.inline_preview then
      local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
      local it = { key = 'file', kind = 'file', line = #lines - 1, source = table.concat(lines, '\n') }
      items[1] = it
    end
  else
    for _, s in ipairs(scan.buf(buf)) do
      if s.kind == 'base' then
        if s.source then
          items[#items + 1] = { key = 'block:' .. s.source, kind = 'block', line = s.end_line, col = 0, source = s.source }
        else
          items[#items + 1] = { key = 'embed:' .. s.target .. '#' .. (s.view or ''), kind = 'embed', line = s.end_line, col = s.col, end_col = s.end_col, target = s.target, view = s.view }
        end
      end
    end
  end
  -- carry over state from matching items, so views and results survive edits
  local keep = {}
  for _, it in ipairs(items) do
    local prev = old[it.key] and table.remove(old[it.key], 1)
    if prev then
      prev.line = it.line
      prev.col, prev.end_col = it.col, it.end_col
      prev.source = it.source
      it = prev
    end
    keep[#keep + 1] = it
  end
  for _, list in pairs(old) do
    for _, it in ipairs(list) do
      if it.mark then
        pcall(vim.api.nvim_buf_del_extmark, buf, ns, it.mark)
      end
      -- its view is gone: so is the overlay stepping through it
      if overlay and overlay.item == it then
        M.close_overlay()
      end
    end
  end
  st.items = keep
  for _, it in ipairs(keep) do
    if opts.requery ~= false or not it.result then
      draw(buf, it)
      resolve_and_query(buf, it)
    else
      draw(buf, it)
    end
  end
  set_diagnostics(buf)
end

--- Redraws with cached results (after a resize).
function M.redraw(buf)
  local st = state[buf]
  if st then
    for _, it in ipairs(st.items) do
      draw(buf, it)
      M.sync_overlay(buf, it)
    end
  end
end

function M.detach(buf)
  if overlay and overlay.src_buf == buf then
    M.close_overlay()
  end
  state[buf] = nil
  scan.forget(buf)
  if vim.api.nvim_buf_is_valid(buf) then
    vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
    vim.diagnostic.reset(diag_ns, buf)
  end
end

--- Updates it.line from its extmark. False when the mark was invalidated
--- (its line deleted) and the buffer hasn't been rescanned yet.
local function locate(buf, it)
  if not it.mark then
    return true
  end
  -- marks are set with undo_restore = false: an invalidated one is deleted,
  -- so no details (which copy all virtual lines) are needed to tell
  local pos = vim.api.nvim_buf_get_extmark_by_id(buf, ns, it.mark, {})
  if not pos[1] then
    return false
  end
  it.line = pos[1]
  return true
end

--- Items anchored on `line` (0-based), in display order.
local function items_on(buf, line)
  local out = {}
  for _, it in ipairs(state[buf] and state[buf].items or {}) do
    if locate(buf, it) and it.line == line then
      out[#out + 1] = it
    end
  end
  table.sort(out, function(a, b)
    return (a.col or 0) < (b.col or 0)
  end)
  return out
end

--- The base whose anchor is on `line` (0-based), if any. With several on one
--- line, `pick` chooses: 'last', or a byte column (the embed under it);
--- otherwise the first.
---@param span? boolean also accept any line of an embed/block's source
---@param pick? 'last'|integer
function M.item_at(buf, line, span, pick)
  local st = state[buf]
  if not st or #st.items == 0 then
    return nil
  end
  local on = items_on(buf, line)
  if #on > 0 then
    if pick == 'last' then
      return on[#on]
    elseif type(pick) == 'number' then
      for _, it in ipairs(on) do
        if it.end_col and pick >= (it.col or 0) and pick < it.end_col then
          return it
        end
      end
    end
    return on[1]
  end
  if span and vim.bo[buf].filetype ~= 'base' then
    for _, s in ipairs(scan.buf(buf)) do
      if s.kind == 'base' and line >= s.line and line <= s.end_line then
        for _, it in ipairs(st.items) do
          if locate(buf, it) and it.line == s.end_line then
            return it
          end
        end
      end
    end
  end
end

--- Cycles the view of an item (does not modify the file).
function M.cycle_view(buf, item, delta)
  local views = item.result and item.result.views or {}
  if #views < 2 then
    return
  end
  -- count a switch that's still in flight, so <Tab><Tab> moves two views
  local cur = tonumber(item.view or '') or item.result.view or 0
  item.view = tostring((cur + delta) % #views)
  query(buf, item)
end

-- ---- overlay: step into a rendered base ----

--- Screen rows from the top of item's anchor line to its first virtual line:
--- the anchor's (wrapped) text plus the views of embeds before it on the line.
local function rows_above(win, buf, item)
  local rows = 1
  local ok, h = pcall(vim.api.nvim_win_text_height, win, { start_row = item.line, end_row = item.line })
  if ok and h then
    rows = math.max(1, h.all - h.fill)
  end
  for _, it in ipairs(items_on(buf, item.line)) do
    if it == item then
      break
    end
    rows = rows + (it.virt_n or 0)
  end
  return rows
end

--- Overlay height: the virtual lines it covers (they stop at max_height; the
--- overlay scrolls through the rest), within the window.
local function overlay_height(item, win_height, above)
  local n = #item.rendered.lines
  return math.max(1, math.min(n, item.virt_n or n, win_height - above))
end

local function open_target(path, src_win)
  if not path or path == '' then
    return
  end
  M.close_overlay()
  if vim.api.nvim_win_is_valid(src_win) then
    vim.api.nvim_set_current_win(src_win)
  end
  vim.cmd.edit(vim.fn.fnameescape(path))
end

local function target_under_cursor(r, win)
  local pos = vim.api.nvim_win_get_cursor(win)
  local line, col = pos[1] - 1, pos[2]
  local best
  for _, t in ipairs(r.targets[line] or {}) do
    if col >= t.s and col < t.e then
      if not t.row then
        return t.path
      end
      best = best or t.path
    end
  end
  if best then
    return best
  end
  -- between targets (e.g. the gap between two cards): the first row on the line
  local ts = r.targets[line]
  return ts and ts[1] and ts[1].path or nil
end

local function fill(obuf, r)
  vim.bo[obuf].modifiable = true
  vim.api.nvim_buf_set_lines(obuf, 0, -1, false, r.lines)
  vim.api.nvim_buf_clear_namespace(obuf, ns, 0, -1)
  for _, h in ipairs(r.hls) do
    pcall(vim.api.nvim_buf_set_extmark, obuf, ns, h[1], h[2], { end_col = h[3], hl_group = h[4] })
  end
  vim.bo[obuf].modifiable = false
  vim.bo[obuf].modified = false
end

--- Closes the overlay. Focus goes back to the source window only when the
--- overlay had it (or was just closed with :q), never away from a window the
--- user switched to.
---@param move? integer put the cursor this many lines below the anchor
function M.close_overlay(move)
  local o = overlay
  if not o then
    return
  end
  overlay = nil
  local had_focus = not vim.api.nvim_win_is_valid(o.win) or vim.api.nvim_get_current_win() == o.win
  if vim.api.nvim_win_is_valid(o.win) then
    vim.api.nvim_win_close(o.win, true)
  end
  if had_focus and vim.api.nvim_win_is_valid(o.src_win) then
    vim.api.nvim_set_current_win(o.src_win)
    if move and vim.api.nvim_buf_is_valid(o.src_buf) and vim.api.nvim_win_get_buf(o.src_win) == o.src_buf then
      locate(o.src_buf, o.item)
      local lc = vim.api.nvim_buf_line_count(o.src_buf)
      local target = math.max(1, math.min(lc, o.item.line + 1 + move))
      vim.api.nvim_win_set_cursor(o.src_win, { target, 0 })
    end
  end
end

function M.sync_overlay(buf, item)
  local o = overlay
  if not (o and o.src_buf == buf and o.item == item and item.rendered) then
    return
  end
  if not (vim.api.nvim_buf_is_valid(o.buf) and vim.api.nvim_win_is_valid(o.win) and vim.api.nvim_win_is_valid(o.src_win)) then
    return
  end
  local cur = vim.api.nvim_win_get_cursor(o.win)
  fill(o.buf, item.rendered)
  local info = vim.fn.getwininfo(o.src_win)[1]
  if info and locate(buf, item) then
    local above = rows_above(o.src_win, buf, item)
    pcall(vim.api.nvim_win_set_config, o.win, {
      relative = 'win',
      win = o.src_win,
      bufpos = { item.line, 0 },
      row = above,
      col = 0,
      width = math.max(1, info.width - info.textoff),
      height = overlay_height(item, info.height, above),
    })
  end
  pcall(vim.api.nvim_win_set_cursor, o.win, { math.min(cur[1], #item.rendered.lines), cur[2] })
end

--- Opens the read-only overlay for item over its virtual lines.
---@param where? 'top'|'bottom'
function M.enter(buf, item, where)
  if not item or not item.rendered or not locate(buf, item) then
    return false
  end
  M.close_overlay()
  local src_win = vim.api.nvim_get_current_win()
  local r = item.rendered
  local wininfo = vim.fn.getwininfo(src_win)[1]
  local above = rows_above(src_win, buf, item)
  local height = overlay_height(item, wininfo.height, above)
  -- make sure the anchor line and the overlay fit on screen
  local anchor_row = vim.fn.screenpos(src_win, item.line + 1, 1).row - wininfo.winrow
  if anchor_row < 0 or anchor_row + above + height > wininfo.height then
    vim.api.nvim_win_call(src_win, function()
      vim.fn.winrestview({ topline = item.line + 1, topfill = 0 })
    end)
  end
  local obuf = vim.api.nvim_create_buf(false, true)
  vim.bo[obuf].bufhidden = 'wipe'
  vim.bo[obuf].filetype = 'brainbase'
  fill(obuf, r)
  local win = vim.api.nvim_open_win(obuf, true, {
    relative = 'win',
    win = src_win,
    bufpos = { item.line, 0 },
    row = above,
    col = 0,
    width = math.max(1, wininfo.width - wininfo.textoff),
    height = height,
    style = 'minimal',
    border = 'none',
    zindex = 40,
    noautocmd = false,
  })
  vim.wo[win].winhighlight = 'Normal:Normal,NormalFloat:Normal'
  vim.wo[win].cursorline = true
  vim.wo[win].wrap = false
  overlay = { win = win, buf = obuf, src_win = src_win, src_buf = buf, item = item }

  local first = r.rows[1] and r.rows[1] + 1 or 1
  local last = r.rows[#r.rows] and r.rows[#r.rows] + 1 or #r.lines
  vim.api.nvim_win_set_cursor(win, { where == 'bottom' and last or first, 0 })

  -- the embed before/after this one on the same anchor line
  local function sibling(delta)
    local on = items_on(buf, item.line)
    for i, it in ipairs(on) do
      if it == item then
        return on[i + delta]
      end
    end
  end

  local keys = config.options.keys
  local function map(lhs, fn, desc)
    vim.keymap.set('n', lhs, fn, { buffer = obuf, nowait = true, desc = desc })
  end
  local function open()
    open_target(target_under_cursor(item.rendered, win), src_win)
  end
  map(keys.enter, open, 'Open note under cursor')
  map('gf', open, 'Open note under cursor')
  map('gd', open, 'Open note under cursor')
  map('<2-LeftMouse>', open, 'Open note under cursor')
  map(keys.close, function()
    M.close_overlay(0)
  end, 'Leave base view')
  map('<Esc>', function()
    M.close_overlay(0)
  end, 'Leave base view')
  map('j', function()
    local l = vim.api.nvim_win_get_cursor(win)[1]
    if l < vim.api.nvim_buf_line_count(obuf) then
      return vim.cmd('normal! ' .. vim.v.count1 .. 'j')
    end
    local nxt = sibling(1)
    if nxt then
      M.close_overlay()
      M.enter(buf, nxt, 'top')
    elseif locate(buf, item) and item.line < vim.api.nvim_buf_line_count(buf) - 1 then
      M.close_overlay(1)
    end
    -- else: nothing below the view; stay
  end, 'Down (leaves at the bottom)')
  map('k', function()
    local l = vim.api.nvim_win_get_cursor(win)[1]
    if l > 1 then
      return vim.cmd('normal! ' .. vim.v.count1 .. 'k')
    end
    local prev = sibling(-1)
    if prev then
      M.close_overlay()
      M.enter(buf, prev, 'bottom')
    else
      M.close_overlay(0)
    end
  end, 'Up (leaves at the top)')
  map(']]', function()
    local l = vim.api.nvim_win_get_cursor(win)[1] - 1
    for _, rl in ipairs(item.rendered.rows) do
      if rl > l then
        return vim.api.nvim_win_set_cursor(win, { rl + 1, 0 })
      end
    end
  end, 'Next row')
  map('[[', function()
    local l = vim.api.nvim_win_get_cursor(win)[1] - 1
    for i = #item.rendered.rows, 1, -1 do
      if item.rendered.rows[i] < l then
        return vim.api.nvim_win_set_cursor(win, { item.rendered.rows[i] + 1, 0 })
      end
    end
  end, 'Previous row')
  map(keys.next_view, function()
    M.cycle_view(buf, item, 1)
  end, 'Next view')
  map(keys.prev_view, function()
    M.cycle_view(buf, item, -1)
  end, 'Previous view')
  map(keys.refresh, function()
    M.refresh(buf)
  end, 'Refresh')
  map('o', function()
    if item.path then
      open_target(item.path, src_win)
    end
  end, 'Open the .base file')
  vim.api.nvim_create_autocmd('WinLeave', {
    buffer = obuf,
    once = true,
    callback = function()
      vim.schedule(function()
        if overlay and overlay.buf == obuf then
          M.close_overlay()
        end
      end)
    end,
  })
  return true
end

function M.overlay()
  return overlay
end

--- Buffer-local keys in markdown/.base buffers to step into rendered bases.
function M.attach_keys(buf)
  local keys = config.options.keys
  local function cursor_line()
    return vim.api.nvim_win_get_cursor(0)[1] - 1
  end
  vim.keymap.set('n', keys.enter, function()
    local it = M.item_at(buf, cursor_line(), true, vim.api.nvim_win_get_cursor(0)[2])
    if it and M.enter(buf, it, 'top') then
      return
    end
    local cr = vim.api.nvim_replace_termcodes(keys.enter, true, false, true)
    vim.api.nvim_feedkeys(cr, 'n', false)
  end, { buffer = buf, desc = 'brain: step into the base on this line' })
  -- j on an anchor line steps into the view below it; k just below steps in from the bottom.
  -- With a count they are plain motions.
  vim.keymap.set('n', 'j', function()
    if vim.v.count <= 1 then
      local it = M.item_at(buf, cursor_line())
      if it and M.enter(buf, it, 'top') then
        return
      end
    end
    vim.cmd('normal! ' .. vim.v.count1 .. 'j')
  end, { buffer = buf, desc = 'brain: down, into rendered bases' })
  vim.keymap.set('n', 'k', function()
    if vim.v.count <= 1 then
      local line = cursor_line() - 1
      local it = line >= 0 and M.item_at(buf, line, false, 'last')
      if it and M.enter(buf, it, 'bottom') then
        return
      end
    end
    vim.cmd('normal! ' .. vim.v.count1 .. 'k')
  end, { buffer = buf, desc = 'brain: up, into rendered bases' })
end

return M
