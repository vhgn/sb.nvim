-- Finds the renderable things in markdown lines: base embeds, ```base
-- blocks, image embeds, mermaid blocks and math. Pure: lines in, items out.
-- Lines and columns are 0-based; end_col is exclusive (bytes).
local M = {}

local IMAGE_EXT = { png = true, jpg = true, jpeg = true, gif = true, webp = true, avif = true, bmp = true, svg = true }

function M.is_image(path)
  local ext = path:match('%.([%w]+)$')
  return ext ~= nil and IMAGE_EXT[ext:lower()] == true
end

--- Blanks inline code spans (`…`, ``…``) so their contents are ignored; keeps byte offsets.
local function mask_code(line)
  local i = line:find('`', 1, true)
  if not i then
    return line
  end
  local out = {}
  local last = 1 -- first byte not yet copied
  while i do
    local e = (line:find('[^`]', i) or #line + 1) - 1
    local tl = e - i + 1
    local ticks = line:sub(i, e)
    local close = line:find(ticks, i + tl, true)
    -- a closing run must be exactly as long
    while close and line:byte(close + tl) == 96 do
      close = line:find(ticks, close + tl + 1, true)
    end
    if close then
      out[#out + 1] = line:sub(last, i - 1)
      out[#out + 1] = string.rep(' ', close + tl - i)
      i = close + tl
      last = i
    else
      i = i + tl
    end
    i = line:find('`', i, true)
  end
  if last == 1 then
    return line
  end
  out[#out + 1] = line:sub(last)
  return table.concat(out)
end
M.mask_code = mask_code

local function fence_open(line)
  if not line:find('^ *[`~]') then
    return nil
  end
  local indent, mark, info = line:match('^( *)(```+)%s*([^`]*)$')
  if not mark then
    indent, mark, info = line:match('^( *)(~~~+)%s*(.*)$')
  end
  if mark and #indent <= 3 then
    return mark, vim.trim(info or '')
  end
end

local function fence_close(line, mark)
  local indent, m = line:match('^( *)([`~]+)%s*$')
  return m ~= nil and #indent <= 3 and m:sub(1, 1) == mark:sub(1, 1) and #m >= #mark
end

--- Parses "![[target#anchor|alias]]" contents.
local function wiki_parts(inner)
  local target, rest = inner:match('^([^#|]*)(.*)$')
  local anchor = rest:match('^#([^|]*)')
  local alias = rest:match('|(.*)$')
  return vim.trim(target), anchor and vim.trim(anchor) or nil, alias and vim.trim(alias) or nil
end

local function url_decode(s)
  return (s:gsub('%%(%x%x)', function(h)
    return string.char(tonumber(h, 16))
  end))
end

--- Parses a markdown image starting at s ("![alt](dest title)") in the masked
--- line. Returns the exclusive end (1-based index of ')') and the destination.
local function md_image(masked, line, s)
  -- alt text: balanced brackets
  local i, depth, n = s + 2, 1, #masked
  while i <= n do
    local c = masked:sub(i, i)
    if c == '\\' then
      i = i + 1
    elseif c == '[' then
      depth = depth + 1
    elseif c == ']' then
      depth = depth - 1
      if depth == 0 then
        break
      end
    end
    i = i + 1
  end
  if depth ~= 0 or masked:sub(i + 1, i + 1) ~= '(' then
    return nil
  end
  i = i + 2
  while masked:sub(i, i):match('%s') do
    i = i + 1
  end
  local dest
  if masked:sub(i, i) == '<' then
    local close = masked:find('>', i + 1, true)
    if not close then
      return nil
    end
    dest = line:sub(i + 1, close - 1)
    i = close + 1
    while masked:sub(i, i):match('%s') do
      i = i + 1
    end
    -- optional title: "…", '…' or (…)
    local q = masked:sub(i, i)
    if q == '"' or q == "'" or q == '(' then
      local tclose = masked:find(q == '(' and ')' or q, i + 1, true)
      if not tclose then
        return nil
      end
      i = tclose + 1
      while masked:sub(i, i):match('%s') do
        i = i + 1
      end
    end
    if masked:sub(i, i) ~= ')' then
      return nil
    end
  else
    -- up to the unbalanced ')'; spaces are allowed (Obsidian accepts them)
    local d0, pd = i, 0
    while i <= n do
      local c = masked:sub(i, i)
      if c == '\\' then
        i = i + 1
      elseif c == '(' then
        pd = pd + 1
      elseif c == ')' then
        if pd == 0 then
          break
        end
        pd = pd - 1
      end
      i = i + 1
    end
    if i > n then
      return nil
    end
    dest = vim.trim(line:sub(d0, i - 1))
    -- strip a title: "…", '…' or (…)
    local last = dest:byte(-1)
    if (last == 34 or last == 39 or last == 41) and dest:find('%s') then
      dest = dest:gsub('%s+"[^"]*"$', ''):gsub("%s+'[^']*'$", ''):gsub('%s+%b()$', '')
    end
  end
  if dest == '' then
    return nil
  end
  return i, dest
end

--- Finds display-math spans on one (masked) line, starting at byte `from`.
--- Returns a list of {start, close_start, close_tok} plus an unclosed opener.
local function display_spans(masked, from, stop_at)
  local spans = {}
  local pos = from
  while true do
    local dstart = masked:find('$$', pos, true)
    local bstart = masked:find('\\[', pos, true)
    if not dstart and not bstart then
      return spans
    end
    local close_tok, start = '$$', dstart
    if not dstart or (bstart and bstart < dstart) then
      close_tok, start = '\\]', bstart
    end
    if stop_at and start >= stop_at then
      return spans
    end
    local close = masked:find(close_tok, start + 2, true)
    if not close then
      return spans, { start = start, tok = close_tok }
    end
    spans[#spans + 1] = { start = start, close = close }
    pos = close + 2
  end
end

local function before(a, b)
  if a.line ~= b.line then
    return a.line < b.line
  end
  return (a.col or 0) < (b.col or 0)
end

--- Sorts items by position. The scan emits them line by line, so when no two
--- share a position the order is unique and sorting each line's few items is
--- enough; otherwise (ties) it is exactly the table.sort it always was.
local function sort_items(items)
  local n = #items
  for k = 2, n do
    if items[k].line < items[k - 1].line then
      return table.sort(items, before)
    end
  end
  -- ties within a line?
  local k = 1
  while k <= n do
    local j = k
    local line = items[k].line
    while j < n and items[j + 1].line == line do
      j = j + 1
    end
    for a = k + 1, j do
      local ca = items[a].col or 0
      for b = k, a - 1 do
        if (items[b].col or 0) == ca then
          return table.sort(items, before)
        end
      end
    end
    k = j + 1
  end
  -- insertion sort per line (lines are already in order)
  k = 1
  while k <= n do
    local j = k
    local line = items[k].line
    while j < n and items[j + 1].line == line do
      j = j + 1
    end
    for a = k + 1, j do
      local it = items[a]
      local c = it.col or 0
      local b = a - 1
      while b >= k and (items[b].col or 0) > c do
        items[b + 1] = items[b]
        b = b - 1
      end
      items[b + 1] = it
    end
    k = j + 1
  end
end

---@param lines string[]
---@return table[] items
function M.scan(lines)
  -- CRLF files read with ff=unix keep a trailing \r; it changes no offsets.
  for k, l in ipairs(lines) do
    if l:byte(-1) == 13 then
      lines = vim.deepcopy(lines)
      for j = k, #lines do
        lines[j] = lines[j]:gsub('\r$', '')
      end
      break
    end
  end
  local items = {}
  local i = 1
  local n = #lines
  -- front matter
  if n > 0 and lines[1]:match('^%-%-%-%s*$') then
    for j = 2, n do
      if lines[j]:match('^%-%-%-%s*$') or lines[j]:match('^%.%.%.%s*$') then
        i = j + 1
        break
      end
    end
  end
  local math_start = nil -- line of an open $$ / \[ block
  local math_close, math_col
  local unclosed = {} -- line -> column of a $$ / \[ that never closes: plain text
  while i <= n or math_start do
    if i > n then
      -- an unclosed display block is not math: rescan after its opener
      unclosed[math_start] = math_col + 1
      i = math_start
      math_start = nil
      for k = #items, 1, -1 do
        if items[k].line >= i - 1 then
          table.remove(items, k)
        end
      end
    end
    local line = lines[i]
    if math_start then
      local close = line:find(math_close, 1, true)
      if close then
        local body = { lines[math_start]:sub(math_col + 3) }
        for k = math_start + 1, i - 1 do
          body[#body + 1] = lines[k]
        end
        body[#body + 1] = line:sub(1, close - 1)
        local text = vim.trim(table.concat(body, '\n'))
        if text ~= '' then
          items[#items + 1] = { kind = 'math', line = math_start - 1, end_line = i - 1, col = math_col, end_col = close + 1, text = text, display = true }
        end
        math_start = nil
      end
      i = i + 1
    else
      local mark, info = fence_open(line)
      if mark then
        local lang = (info:match('^{?%.?([%w_+-]+)') or ''):lower()
        local j = i + 1
        while j <= n and not fence_close(lines[j], mark) do
          j = j + 1
        end
        local body = {}
        for k = i + 1, math.min(j - 1, n) do
          body[#body + 1] = lines[k]
        end
        local item = { line = i - 1, end_line = math.min(j, n) - 1, text = table.concat(body, '\n'), closed = j <= n }
        if lang == 'base' then
          item.kind = 'base'
          item.source = item.text
        elseif lang == 'mermaid' then
          item.kind = 'mermaid'
        elseif lang == 'math' or lang == 'latex' or lang == 'tex' then
          item.kind = 'math'
          item.display = true
        end
        if item.kind and item.closed then
          items[#items + 1] = item
        end
        i = j + 1
      elseif not (line:find('$', 1, true) or line:find('!', 1, true) or line:find('\\', 1, true)) then
        -- no $, ! or \\: nothing on this line can be math, an image or an embed
        i = i + 1
      else
        local masked = mask_code(line)
        -- display math on this line; blank each span for the inline pass
        local spans, open = display_spans(masked, 1, unclosed[i])
        local search = masked
        for _, sp in ipairs(spans) do
          local inner = line:sub(sp.start + 2, sp.close - 1)
          if vim.trim(inner) ~= '' then
            items[#items + 1] = { kind = 'math', line = i - 1, end_line = i - 1, col = sp.start - 1, end_col = sp.close + 1, text = vim.trim(inner), display = true }
          end
          search = search:sub(1, sp.start - 1) .. string.rep(' ', sp.close + 2 - sp.start) .. search:sub(sp.close + 2)
        end
        if open then
          math_start, math_close, math_col = i, open.tok, open.start - 1
          search = search:sub(1, open.start - 1) .. string.rep(' ', #search - open.start + 1)
        end
        -- wiki embeds: ![[…]]
        local pos = 1
        while true do
          -- the first "![[" from pos where the embed pattern matches
          local s, e, inner
          local cand = masked:find('![[', pos, true)
          while cand do
            s, e, inner = masked:find('^!%[%[([^%[%]]-)%]%]', cand)
            if s then
              break
            end
            cand = masked:find('![[', cand + 1, true)
          end
          if not s then
            break
          end
          inner = line:sub(s + 3, e - 2)
          local target, anchor, alias = wiki_parts(inner)
          if anchor == '' then
            anchor = nil
          end
          if target:lower():match('%.base$') then
            items[#items + 1] = { kind = 'base', line = i - 1, end_line = i - 1, col = s - 1, end_col = e, target = target, view = anchor }
          elseif M.is_image(target) then
            items[#items + 1] = { kind = 'image', line = i - 1, end_line = i - 1, col = s - 1, end_col = e, target = target, wiki = true, size = alias }
          end
          pos = e + 1
        end
        -- markdown images: ![alt](path "title")
        pos = 1
        while true do
          local s = masked:find('![', pos, true)
          if not s then
            break
          end
          local e, dest = md_image(masked, line, s)
          if e then
            local http = dest:match('^https?://') ~= nil
            if not http then
              dest = url_decode(dest)
            end
            local clean = dest:gsub('[?#].*$', '')
            if http or M.is_image(clean) then
              items[#items + 1] = { kind = 'image', line = i - 1, end_line = i - 1, col = s - 1, end_col = e, target = dest, wiki = false }
            end
            pos = e + 1
          else
            pos = s + 2
          end
        end
        -- inline math: $…$ (not $$, not \$, no space just inside the dollars)
        pos = 1
        while true do
          local s = search:find('$', pos, true)
          if not s then
            break
          end
          local escaped = s > 1 and search:byte(s - 1) == 92
          local double = search:byte(s + 1) == 36
          if escaped or double then
            pos = s + (double and 2 or 1)
          else
            local e = s + 1
            local found = nil
            while true do
              e = search:find('$', e, true)
              if not e then
                break
              end
              if search:byte(e - 1) ~= 92 then
                found = e
                break
              end
              e = e + 1
            end
            if not found then
              break
            end
            local inner = line:sub(s + 1, found - 1)
            local after = search:sub(found + 1, found + 1)
            if inner ~= '' and not inner:match('^%s') and not inner:match('%s$') and not after:match('%d') then
              items[#items + 1] = { kind = 'math', line = i - 1, end_line = i - 1, col = s - 1, end_col = found, text = inner, display = false }
              pos = found + 1
            else
              pos = s + 1
            end
          end
        end
        i = i + 1
      end
    end
  end
  sort_items(items)
  return items
end

local cache = {} ---@type table<integer, {tick: integer, items: table[]}>

--- Items of a loaded buffer, scanned once per change (base and media both
--- refresh on every debounced edit). The result is shared: don't modify it.
---@param buf integer
---@return table[] items
function M.buf(buf)
  local tick = vim.api.nvim_buf_get_changedtick(buf)
  local c = cache[buf]
  if c and c.tick == tick then
    return c.items
  end
  local items = M.scan(vim.api.nvim_buf_get_lines(buf, 0, -1, false))
  cache[buf] = { tick = tick, items = items }
  return items
end

function M.forget(buf)
  cache[buf] = nil
end

return M
