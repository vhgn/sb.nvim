-- Turns a base query result into text lines plus highlights and navigation
-- targets. Pure apart from string width functions; no buffer access.
local M = {}

local strw = vim.api.nvim_strwidth

--- Truncates s to at most w display cells, adding … when cut.
function M.trunc(s, w)
  s = s or ''
  if w <= 0 then
    return ''
  end
  -- printable ASCII: one byte is one cell
  if not s:find('[^\32-\126]') then
    if #s <= w then
      return s
    end
    return s:sub(1, w - 1) .. '…'
  end
  s = s:gsub('[\n\r\t]', ' ')
  if strw(s) <= w then
    return s
  end
  local out, used = {}, 0
  for _, ch in ipairs(vim.fn.split(s, '\\zs')) do
    local cw = strw(ch)
    if used + cw > w - 1 then
      break
    end
    out[#out + 1] = ch
    used = used + cw
  end
  return table.concat(out) .. '…'
end

--- Display width; printable ASCII needs no API call.
local function cellw(s)
  if not s:find('[^\32-\126]') then
    return #s
  end
  return strw(s)
end
M.width = cellw

local function pad(s, w, align)
  local gap = w - cellw(s)
  if gap <= 0 then
    return s
  end
  if align == 'right' then
    return string.rep(' ', gap) .. s
  elseif align == 'center' then
    local l = math.floor(gap / 2)
    return string.rep(' ', l) .. s .. string.rep(' ', gap - l)
  end
  return s .. string.rep(' ', gap)
end

local KIND_HL = {
  link = 'BrainBaseLink',
  date = 'BrainBaseDate',
  number = 'BrainBaseNumber',
  bool = 'BrainBaseBool',
  tag = 'BrainBaseTag',
  list = 'BrainBaseList',
  image = 'BrainBaseLink',
  error = 'DiagnosticError',
}

function M.define_highlights()
  local set = function(name, link)
    vim.api.nvim_set_hl(0, name, { link = link, default = true })
  end
  set('BrainBaseBorder', 'FloatBorder')
  set('BrainBaseHeader', 'Title')
  set('BrainBaseTitle', 'Title')
  set('BrainBaseViewActive', 'TabLineSel')
  set('BrainBaseView', 'TabLine')
  set('BrainBaseMeta', 'Comment')
  set('BrainBaseLink', 'Underlined')
  set('BrainBaseDate', 'Constant')
  set('BrainBaseNumber', 'Number')
  set('BrainBaseBool', 'Boolean')
  set('BrainBaseTag', 'Label')
  set('BrainBaseList', 'Normal')
  set('BrainBaseGroup', 'Special')
  set('BrainBaseSummary', 'Comment')
  set('BrainBaseLabel', 'Comment')
  set('BrainBaseError', 'DiagnosticError')
  set('BrainBaseMarker', 'DiagnosticWarn')
end

---@class brain.Rendered
---@field lines string[]
---@field hls {[1]: integer, [2]: integer, [3]: integer, [4]: string}[] line, col_start, col_end (bytes), group
---@field targets table<integer, {s: integer, e: integer, path: string}[]> per line (0-based) navigation targets
---@field rows integer[] lines that start a result row (0-based), for row-wise motions

local Builder = {}
Builder.__index = Builder

local function builder()
  return setmetatable({ lines = {}, hls = {}, targets = {}, rows = {} }, Builder)
end

--- Appends a line made of segments { text, hl?, target? }.
function Builder:add(segments, row_path)
  local parts, col = {}, 0
  local ln = #self.lines
  local hls = self.hls
  for k = 1, #segments do
    local seg = segments[k]
    local text = seg[1]
    local len = #text
    if seg[2] and len > 0 then
      hls[#hls + 1] = { ln, col, col + len, seg[2] }
    end
    if seg[3] then
      local t = self.targets[ln]
      if not t then
        t = {}
        self.targets[ln] = t
      end
      t[#t + 1] = { s = col, e = col + len, path = seg[3] }
    end
    parts[k] = text
    col = col + len
  end
  if row_path then
    self.targets[ln] = self.targets[ln] or {}
    table.insert(self.targets[ln], { s = 0, e = col, path = row_path, row = true })
    table.insert(self.rows, ln)
  end
  table.insert(self.lines, table.concat(parts))
end

local function title_of(row)
  local name = row.rel:match('([^/]+)$') or row.rel
  return (name:gsub('%.md$', ''))
end

local ICONS = { table = '▦', cards = '▤', list = '☰', map = '⌖' }

local function segs_width(segs)
  local w = 0
  for _, s in ipairs(segs) do
    w = w + cellw(s[1])
  end
  return w
end

local function header(b, res, width, opts)
  local count = res.total == res.shown and string.format('%d results', res.total) or string.format('%d of %d results', res.shown, res.total)
  -- title and view tabs; `names` = show inactive view names, `active` = override the active name
  local function build(title, names, active)
    local segs = { { ' ', nil } }
    if title then
      table.insert(segs, { title .. ' ', 'BrainBaseTitle' })
    end
    for i, v in ipairs(res.views or {}) do
      local is_active = (i - 1) == res.view
      local name = is_active and (active or v.name) or (names and v.name or '')
      local icon = ICONS[v.type] or '▦'
      table.insert(segs, { ' ' .. icon .. (name ~= '' and (' ' .. name) or '') .. ' ', is_active and 'BrainBaseViewActive' or 'BrainBaseView' })
      table.insert(segs, { ' ' })
    end
    return segs
  end
  -- narrow windows: drop inactive view names, then shorten the active one, then the title
  local budget = width - strw(count) - 2
  local segs = build(opts.title, true)
  if segs_width(segs) > budget then
    segs = build(opts.title, false)
  end
  if segs_width(segs) > budget then
    local cur = (res.views or {})[(res.view or 0) + 1]
    for _, title in ipairs({ opts.title or false, false }) do
      title = title or nil
      local fixed = segs_width(build(title, false, ''))
      segs = build(title, false, cur and M.trunc(cur.name, budget - fixed - 1) or '')
      if segs_width(segs) <= budget then
        break
      end
    end
  end
  local used = segs_width(segs)
  local gap = math.max(1, width - used - strw(count) - 1)
  table.insert(segs, { string.rep(' ', gap) })
  table.insert(segs, { count, 'BrainBaseMeta' })
  b:add(segs)
  for _, e in ipairs(res.errors or {}) do
    b:add({ { ' ⚠ ' .. M.trunc(e, width - 4), 'BrainBaseError' } })
  end
end

-- ---- table ----

local MIN_COL = 6

--- Widths of the first n columns; second result: whether they fit in width.
local function col_widths(res, width, n)
  local cols = res.columns
  local nat = {}
  for i, c in ipairs(cols) do
    nat[i] = cellw(c.name)
  end
  for _, g in ipairs(res.groups) do
    for _, row in ipairs(g.rows) do
      for i, cell in ipairs(row.cells) do
        nat[i] = math.max(nat[i] or 0, cellw(cell.t or ''))
      end
    end
    for i, s in ipairs(g.summaries or {}) do
      nat[i] = math.max(nat[i] or 0, cellw((s.t or '')) + 2)
    end
  end
  for i, s in ipairs(res.summaries or {}) do
    nat[i] = math.max(nat[i] or 0, cellw(s.t or '') + 2)
  end
  for i = 1, n do
    nat[i] = math.min(math.max(nat[i], 1), 60)
  end
  -- borders: "│ " + " │ " between + " │"
  local avail = width - (3 * n + 1)
  local total = 0
  for i = 1, n do
    total = total + nat[i]
  end
  while total > avail do
    local wi, wv = 1, -1
    for i = 1, n do
      if nat[i] > wv then
        wi, wv = i, nat[i]
      end
    end
    -- below this, columns become stubs: better to drop some (see render_table)
    if wv <= MIN_COL then
      break
    end
    nat[wi] = nat[wi] - 1
    total = total - 1
  end
  for i = n + 1, #nat do
    nat[i] = nil
  end
  return nat, total <= avail
end

local function rule(widths, l, m, r)
  local parts = {}
  for i, w in ipairs(widths) do
    parts[i] = string.rep('─', w + 2)
  end
  return l .. table.concat(parts, m) .. r
end

local function render_table(b, res, width)
  local cols = res.columns
  if #cols == 0 then
    return
  end
  -- too many columns to fit even when squeezed: drop them from the right
  local n, w = #cols, nil
  local hidden_note = ''
  while true do
    local fits
    w, fits = col_widths(res, width, n)
    if fits or n == 1 then
      break
    end
    n = n - 1
    hidden_note = string.format(' … %d more column%s', #cols - n, #cols - n == 1 and '' or 's')
  end
  local align = {}
  local function cell_segs(cells, hl_override)
    local segs = { { '│', 'BrainBaseBorder' } }
    for i = 1, n do
      local cell = cells[i] or { t = '' }
      local text = M.trunc(cell.t or '', w[i])
      local a = cell.k == 'number' and 'right' or cell.k == 'bool' and 'center' or 'left'
      if cell.k == 'number' then
        align[i] = 'right'
      end
      table.insert(segs, { ' ' })
      local group = hl_override or KIND_HL[cell.k]
      local padded = pad(text, w[i], a)
      if group and text ~= '' then
        local lead = padded:match('^ *')
        table.insert(segs, { lead })
        table.insert(segs, { text, group, cell.l })
        table.insert(segs, { padded:sub(#lead + #text + 1) })
      else
        table.insert(segs, { padded })
      end
      table.insert(segs, { ' │', 'BrainBaseBorder' })
    end
    return segs
  end
  b:add({ { rule(w, '╭', '┬', '╮'), 'BrainBaseBorder' } })
  local head = {}
  for i = 1, n do
    head[i] = { t = cols[i].name }
  end
  b:add(cell_segs(head, 'BrainBaseHeader'))
  b:add({ { rule(w, '├', '┼', '┤'), 'BrainBaseBorder' } })
  local function summary_line(sums)
    local cells = {}
    for i = 1, n do
      local s = sums[i]
      cells[i] = { t = (s and s.t and s.t ~= '') and ('Σ ' .. s.t) or '' }
    end
    b:add(cell_segs(cells, 'BrainBaseSummary'))
  end
  for gi, g in ipairs(res.groups) do
    if g.hasKey then
      if gi > 1 then
        b:add({ { rule(w, '├', '┼', '┤'), 'BrainBaseBorder' } })
      end
      local inner = width - 4
      local label = M.trunc('▸ ' .. g.key .. '  (' .. #g.rows .. ')', inner)
      b:add({ { '│ ', 'BrainBaseBorder' }, { label, 'BrainBaseGroup' }, { string.rep(' ', math.max(0, (#rule(w, '', '', '') > 0 and strw(rule(w, '╭', '┬', '╮')) or width) - 4 - strw(label))) }, { ' │', 'BrainBaseBorder' } })
    end
    for _, row in ipairs(g.rows) do
      b:add(cell_segs(row.cells), row.path)
    end
    if g.hasKey and g.summaries and #res.groups > 1 then
      summary_line(g.summaries)
    end
  end
  if #res.groups == 1 and #res.groups[1].rows == 0 then
    local inner = strw(rule(w, '╭', '┬', '╮')) - 4
    b:add({ { '│ ', 'BrainBaseBorder' }, { pad('No results', inner), 'BrainBaseMeta' }, { ' │', 'BrainBaseBorder' } })
  end
  if res.summaries and #res.summaries > 0 then
    b:add({ { rule(w, '├', '┼', '┤'), 'BrainBaseBorder' } })
    summary_line(res.summaries)
  end
  b:add({ { rule(w, '╰', '┴', '╯'), 'BrainBaseBorder' } })
  if hidden_note ~= '' then
    b:add({ { hidden_note, 'BrainBaseMeta' } })
  end
end

-- ---- cards ----

local function render_cards(b, res, width)
  local size = tonumber((res.options or {}).cardSize) or 200
  local cw = math.max(18, math.min(48, math.floor(size / 7)))
  cw = math.min(cw, width)
  local per = math.max(1, math.floor((width + 1) / (cw + 1)))
  local cols = res.columns
  local title_col = cols[1] and cols[1].id == 'file.name' and 1 or nil

  for _, g in ipairs(res.groups) do
    if g.hasKey then
      b:add({ { ' ▸ ' .. M.trunc(g.key, width - 10) .. '  (' .. #g.rows .. ')', 'BrainBaseGroup' } })
    end
    local rows = g.rows
    for start = 1, #rows, per do
      local batch = {}
      for k = start, math.min(start + per - 1, #rows) do
        batch[#batch + 1] = rows[k]
      end
      -- build each card's content lines as segment lists
      local cards, height = {}, 0
      for ci, row in ipairs(batch) do
        local inner = cw - 4
        local content = {}
        local title = title_col and (row.cells[1].t or ''):gsub('%.md$', '') or title_of(row)
        table.insert(content, { { M.trunc(title, inner), 'BrainBaseTitle', row.path } })
        if row.image and row.image ~= '' then
          table.insert(content, { { M.trunc('▣ ' .. vim.fn.fnamemodify(row.image, ':t'), inner), 'BrainBaseMeta', row.image } })
        end
        for i, c in ipairs(cols) do
          if i ~= title_col then
            local cell = row.cells[i] or { t = '' }
            if cell.t and cell.t ~= '' then
              local label = M.trunc(c.name, math.floor(inner / 2))
              local value = M.trunc(cell.t, inner - cellw(label) - 1)
              table.insert(content, { { label .. ' ', 'BrainBaseLabel' }, { value, KIND_HL[cell.k], cell.l } })
            end
          end
        end
        cards[ci] = content
        height = math.max(height, #content)
      end
      local function line_for(fn)
        local segs = {}
        for ci = 1, #batch do
          if ci > 1 then
            table.insert(segs, { ' ' })
          end
          for _, s in ipairs(fn(ci)) do
            table.insert(segs, s)
          end
        end
        return segs
      end
      -- border lines open their card too (each border is 3 bytes per cell)
      local function border(l, r)
        local ln = #b.lines
        b:add(line_for(function()
          return { { l .. string.rep('─', cw - 2) .. r, 'BrainBaseBorder' } }
        end))
        b.targets[ln] = {}
        for ci, row in ipairs(batch) do
          local s = (ci - 1) * (3 * cw + 1)
          table.insert(b.targets[ln], { s = s, e = s + 3 * cw, path = row.path, row = true })
        end
      end
      border('╭', '╮')
      for li = 1, height do
        local segs = line_for(function(ci)
          local content = cards[ci][li]
          local out = { { '│ ', 'BrainBaseBorder' } }
          local used = 0
          if content then
            for _, s in ipairs(content) do
              table.insert(out, s)
              used = used + cellw(s[1])
            end
          end
          table.insert(out, { string.rep(' ', cw - 4 - used) })
          table.insert(out, { ' │', 'BrainBaseBorder' })
          return out
        end)
        -- every content line of a card navigates to that card's note
        local ln = #b.lines
        b:add(segs)
        local col = 0
        b.targets[ln] = b.targets[ln] or {}
        for ci, row in ipairs(batch) do
          local cwb = 0
          -- compute byte width of this card's segments on the line
          local first = ci == 1 and 0 or 1
          col = col + first
          local seglen = 0
          local content = cards[ci][li]
          seglen = #'│ ' + #' │' + (cw - 4)
          if content then
            local used = 0
            for _, s in ipairs(content) do
              seglen = seglen + #s[1]
              used = used + cellw(s[1])
            end
            seglen = seglen - used
          end
          cwb = seglen
          table.insert(b.targets[ln], { s = col, e = col + cwb, path = row.path, row = true })
          col = col + cwb
        end
        if li == 1 then
          table.insert(b.rows, ln)
        end
      end
      border('╰', '╯')
    end
  end
  if #res.groups == 1 and #res.groups[1].rows == 0 then
    b:add({ { ' No results', 'BrainBaseMeta' } })
  end
end

-- ---- list ----

local function render_list(b, res, width)
  local cols = res.columns
  local markers = (res.options or {}).markers or 'bullet'
  local title_col = cols[1] and cols[1].id == 'file.name' and 1 or nil
  local n = 0
  for _, g in ipairs(res.groups) do
    if g.hasKey then
      b:add({ { ' ▸ ' .. M.trunc(g.key, width - 10) .. '  (' .. #g.rows .. ')', 'BrainBaseGroup' } })
    end
    for _, row in ipairs(g.rows) do
      n = n + 1
      local marker = markers == 'number' and (n .. '. ') or markers == 'none' and '' or '• '
      local indent = g.hasKey and '   ' or ' '
      local title = title_col and (row.cells[1].t or ''):gsub('%.md$', '') or title_of(row)
      local more = false
      for i, cell in ipairs(row.cells) do
        if i ~= title_col and cell.t and cell.t ~= '' then
          more = true
        end
      end
      -- leave room for the " …" that marks fields cut off
      title = M.trunc(title, width - strw(indent .. marker) - (more and 2 or 0))
      local segs = { { indent }, { marker, 'BrainBaseMarker' }, { title, 'BrainBaseLink', row.path } }
      local used = strw(indent .. marker .. title)
      for i, c in ipairs(cols) do
        local cell = row.cells[i]
        if i ~= title_col and cell and cell.t and cell.t ~= '' then
          local sep = '  · '
          local room = width - used - strw(sep)
          if room < 4 then
            table.insert(segs, { ' …', 'BrainBaseMeta' })
            break
          end
          local text = M.trunc(cell.t, room)
          table.insert(segs, { sep, 'BrainBaseMeta' })
          table.insert(segs, { text, KIND_HL[cell.k], cell.l })
          used = used + strw(sep) + cellw(text)
          _ = c
        end
      end
      b:add(segs, row.path)
    end
  end
  if n == 0 then
    b:add({ { ' No results', 'BrainBaseMeta' } })
  end
end

-- ---- map ----

local MARKS = '123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ'

local function render_map(b, res, width)
  local pts = {}
  for _, g in ipairs(res.groups) do
    for _, row in ipairs(g.rows) do
      if row.lat and row.lng then
        pts[#pts + 1] = row
      end
    end
  end
  local inner_w = math.max(20, width - 2)
  local inner_h = math.max(6, math.min(16, math.floor(inner_w / 3.2)))
  local minlat, maxlat, minlng, maxlng = 90, -90, 180, -180
  for _, p in ipairs(pts) do
    minlat, maxlat = math.min(minlat, p.lat), math.max(maxlat, p.lat)
    minlng, maxlng = math.min(minlng, p.lng), math.max(maxlng, p.lng)
  end
  if #pts == 0 then
    minlat, maxlat, minlng, maxlng = -60, 75, -180, 180
  end
  -- pad the bounds and keep a sensible minimum span
  local function span(lo, hi, min_span, lim)
    local mid = (lo + hi) / 2
    local s = math.max(hi - lo, min_span) * 1.25
    lo, hi = mid - s / 2, mid + s / 2
    if lo < -lim then
      lo, hi = -lim, -lim + s
    end
    if hi > lim then
      lo, hi = lim - s, lim
    end
    return math.max(lo, -lim), math.min(hi, lim)
  end
  minlat, maxlat = span(minlat, maxlat, 0.02, 90)
  minlng, maxlng = span(minlng, maxlng, 0.04, 180)

  local grid = {}
  for y = 1, inner_h do
    grid[y] = {}
    for x = 1, inner_w do
      grid[y][x] = (y % 4 == 0 and x % 2 == 0) and '·' or ' '
    end
  end
  local placed = {}
  for i, p in ipairs(pts) do
    local x = math.floor((p.lng - minlng) / (maxlng - minlng) * (inner_w - 1) + 1.5)
    local y = math.floor((maxlat - p.lat) / (maxlat - minlat) * (inner_h - 1) + 1.5)
    x, y = math.max(1, math.min(inner_w, x)), math.max(1, math.min(inner_h, y))
    local mark = i <= #MARKS and MARKS:sub(i, i) or '+'
    if grid[y][x] ~= ' ' and grid[y][x] ~= '·' then
      mark = '*' -- overlapping markers
    end
    grid[y][x] = mark
    placed[i] = mark
  end
  local function fmt(v)
    return string.format('%.2f', v)
  end
  local top = string.format('╭─ %s°N ', fmt(maxlat))
  b:add({ { top .. string.rep('─', math.max(0, inner_w + 1 - strw(top))) .. '╮', 'BrainBaseBorder' } })
  for y = 1, inner_h do
    local segs = { { '│', 'BrainBaseBorder' } }
    local run = {}
    for x = 1, inner_w do
      local ch = grid[y][x]
      if ch ~= ' ' and ch ~= '·' then
        if #run > 0 then
          table.insert(segs, { table.concat(run), 'BrainBaseMeta' })
          run = {}
        end
        table.insert(segs, { ch, 'BrainBaseMarker' })
      else
        run[#run + 1] = ch
      end
    end
    if #run > 0 then
      table.insert(segs, { table.concat(run), 'BrainBaseMeta' })
    end
    table.insert(segs, { '│', 'BrainBaseBorder' })
    b:add(segs)
  end
  local bottom = string.format(' %s°N  %s°E … %s°E ─╯', fmt(minlat), fmt(minlng), fmt(maxlng))
  b:add({ { '╰' .. string.rep('─', math.max(0, inner_w + 2 - strw(bottom) - 1)) .. bottom, 'BrainBaseBorder' } })
  for i, p in ipairs(pts) do
    local title = p.cells[1] and p.cells[1].t ~= '' and p.cells[1].t:gsub('%.md$', '') or title_of(p)
    b:add({
      { ' ' },
      { placed[i], 'BrainBaseMarker' },
      { ' ' },
      { M.trunc(title, math.max(8, width - 30)), 'BrainBaseLink', p.path },
      { string.format('  %s, %s', fmt(p.lat), fmt(p.lng)) .. (p.marker and p.marker ~= '' and ('  ' .. p.marker) or ''), 'BrainBaseMeta' },
    }, p.path)
  end
  local missing = 0
  for _, g in ipairs(res.groups) do
    missing = missing + #g.rows
  end
  missing = missing - #pts
  if missing > 0 then
    b:add({ { string.format(' %d result%s without coordinates', missing, missing == 1 and '' or 's'), 'BrainBaseMeta' } })
  end
end

---@param res table query result
---@param width integer available display width
---@param opts? {title?: string, error?: string}
---@return brain.Rendered
function M.render(res, width, opts)
  opts = opts or {}
  local b = builder()
  width = math.max(width, 24)
  if opts.error then
    b:add({ { ' ⚠ ' .. M.trunc(opts.error, width - 4), 'BrainBaseError' } })
    return b
  end
  header(b, res, width, opts)
  local kind = res.type
  if kind == 'cards' then
    render_cards(b, res, width)
  elseif kind == 'list' then
    render_list(b, res, width)
  elseif kind == 'map' then
    render_map(b, res, width)
  else
    render_table(b, res, width)
  end
  return b
end

return M
