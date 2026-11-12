-- Renders note embeds as virtual lines below the embed: ![[Note]],
-- ![[Note#Heading]] (that section) and ![[Note#^block]] (that paragraph).
-- The text sits in a thin box.
local client = require('brain.client')
local config = require('brain.config')
local scan = require('brain.scan')

local M = {}

local ns = vim.api.nvim_create_namespace('brain.note')
M.ns = ns

---@type table<integer, {seq: integer}>
local state = {}

function M.define_highlights()
  local set = vim.api.nvim_set_hl
  -- the frame takes only the foreground of FloatBorder: its background
  -- would turn every rule into a solid bar
  local border = vim.api.nvim_get_hl(0, { name = 'FloatBorder', link = false })
  local fg = border.fg or vim.api.nvim_get_hl(0, { name = 'WinSeparator', link = false }).fg
  set(0, 'BrainNoteRule', fg and { fg = fg, default = true } or { link = 'NonText', default = true })
  set(0, 'BrainNoteText', { link = 'Normal', default = true })
  set(0, 'BrainNoteHeading', { link = '@markup.heading', default = true })
  set(0, 'BrainNoteLink', { link = '@markup.link.label', default = true })
  set(0, 'BrainNoteMeta', { link = 'Comment', default = true })
end

--- Text width for virtual lines: the narrowest window showing buf.
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

--- Lines of the note at path: an open buffer's (unsaved) text, else the file.
local function note_lines(path)
  local b = vim.fn.bufnr(path)
  if b ~= -1 and vim.api.nvim_buf_is_loaded(b) then
    return vim.api.nvim_buf_get_lines(b, 0, -1, false)
  end
  local ok, lines = pcall(vim.fn.readfile, path)
  return ok and lines or nil
end

local function strip_frontmatter(lines)
  if lines[1] ~= '---' then
    return lines
  end
  for i = 2, #lines do
    if lines[i] == '---' or lines[i] == '...' then
      return vim.list_slice(lines, i + 1)
    end
  end
  return lines
end

local function heading(line)
  local hashes, text = line:match('^(#+)%s+(.-)%s*#*%s*$')
  if hashes and #hashes <= 6 then
    return #hashes, text
  end
end

local function norm(s)
  return vim.trim(s):lower():gsub('%s+', ' ')
end

--- The part of lines an anchor names, or nil and an error.
local function section(lines, anchor)
  if not anchor then
    return lines
  end
  local block = anchor:match('^%^(.+)$')
  if block then
    for i, l in ipairs(lines) do
      if l:match('%s%^' .. vim.pesc(block) .. '%s*$') or l:match('^%^' .. vim.pesc(block) .. '%s*$') then
        local s, e = i, i
        -- a ^id alone on a line marks the paragraph above it
        if vim.trim(l):sub(1, 1) == '^' then
          e = i - 1
          s = e
        end
        while s > 1 and vim.trim(lines[s - 1]) ~= '' do
          s = s - 1
        end
        local out = vim.list_slice(lines, s, e)
        if #out > 0 then
          out[#out] = out[#out]:gsub('%s*%^' .. vim.pesc(block) .. '%s*$', '')
        end
        return out
      end
    end
    return nil, 'block not found: ^' .. block
  end
  -- Note#A#B: the last heading names the section
  local want = norm(anchor:match('([^#]*)$'))
  for i, l in ipairs(lines) do
    local level, text = heading(l)
    if level and norm(text) == want then
      local e = #lines
      for j = i + 1, #lines do
        local lv = heading(lines[j])
        if lv and lv <= level then
          e = j - 1
          break
        end
      end
      return vim.list_slice(lines, i, e)
    end
  end
  return nil, 'heading not found: ' .. anchor
end

--- Splits line into pieces of at most width display cells, at spaces when
--- it can; continuation pieces are indented like the line's text.
local function wrap(line, width)
  if vim.fn.strdisplaywidth(line) <= width then
    return { line }
  end
  local indent = #(line:match('^%s*[-*+]%s+') or line:match('^%s*%d+[.)]%s+') or line:match('^%s*'))
  if indent > width / 2 then
    indent = 0
  end
  local pad = string.rep(' ', indent)
  local out, rest, first = {}, line, true
  while rest ~= '' do
    local avail = first and width or width - indent
    if vim.fn.strdisplaywidth(rest) <= avail then
      out[#out + 1] = (first and '' or pad) .. rest
      break
    end
    -- longest prefix that fits, in characters
    local nchars = vim.fn.strchars(rest)
    local cut = 0
    for k = 1, nchars do
      if vim.fn.strdisplaywidth(vim.fn.strcharpart(rest, 0, k)) > avail then
        break
      end
      cut = k
    end
    cut = math.max(cut, 1)
    local piece = vim.fn.strcharpart(rest, 0, cut)
    local sp = piece:match('^.*()%s')
    if sp and sp > 1 and (not first or sp > indent + 1) then
      piece = piece:sub(1, sp - 1)
    end
    out[#out + 1] = (first and '' or pad) .. piece
    rest = rest:sub(#piece + 1):gsub('^%s+', '')
    first = false
  end
  return out
end

--- Highlighted chunks for one wrapped piece of a note line.
local function chunks(piece, is_heading)
  if is_heading then
    return { { piece, 'BrainNoteHeading' } }
  end
  local out, pos = {}, 1
  while true do
    local s, e = piece:find('%[%[[^%[%]]-%]%]', pos)
    if not s then
      break
    end
    if s > pos then
      out[#out + 1] = { piece:sub(pos, s - 1), 'BrainNoteText' }
    end
    out[#out + 1] = { piece:sub(s, e), 'BrainNoteLink' }
    pos = e + 1
  end
  if pos <= #piece then
    out[#out + 1] = { piece:sub(pos), 'BrainNoteText' }
  end
  if #out == 0 then
    out[1] = { '' }
  end
  return out
end

--- Virtual lines for an embed: the text in a thin box,
---   ╭──────╮
---   │ text │
---   ╰──────╯
--- The box carries no title: the embed line above already names the note.
local function build(lines, err, width)
  -- '│ ' and ' │' take 4 columns; the text is at most notes.max_width wide
  local inner = math.max(8, math.min(width - 4, config.options.notes.max_width or 80))
  width = inner + 4
  local virt = { { { '╭' .. string.rep('─', width - 2) .. '╮', 'BrainNoteRule' } } }
  local function add(chunks_)
    local w = 0
    for _, c in ipairs(chunks_) do
      w = w + vim.fn.strdisplaywidth(c[1])
    end
    table.insert(chunks_, 1, { '│ ', 'BrainNoteRule' })
    chunks_[#chunks_ + 1] = { string.rep(' ', math.max(0, inner - w)) .. ' │', 'BrainNoteRule' }
    virt[#virt + 1] = chunks_
  end
  width = inner
  if err then
    add({ { err, 'BrainNoteMeta' } })
  else
    -- no blank lines at the edges
    local s, e = 1, #lines
    while s <= e and vim.trim(lines[s]) == '' do
      s = s + 1
    end
    while e >= s and vim.trim(lines[e]) == '' do
      e = e - 1
    end
    local max_h = config.options.notes.max_height
    local body, in_fence = {}, false
    for i = s, e do
      local l = lines[i]:gsub('\t', string.rep(' ', vim.bo.tabstop > 0 and vim.bo.tabstop or 4))
      if l:match('^%s*```') or l:match('^%s*~~~') then
        in_fence = not in_fence
      end
      local is_heading = not in_fence and heading(l) ~= nil
      for _, piece in ipairs(wrap(l, width)) do
        body[#body + 1] = chunks(piece, is_heading)
      end
    end
    if #body == 0 then
      body[1] = { { '(empty)', 'BrainNoteMeta' } }
    end
    for i, b in ipairs(body) do
      if i > max_h then
        add({ { string.format('… %d more lines (gf to open)', #body - max_h), 'BrainNoteMeta' } })
        break
      end
      add(b)
    end
  end
  virt[#virt + 1] = { { '╰' .. string.rep('─', inner + 2) .. '╯', 'BrainNoteRule' } }
  return virt
end

--- Path for a target the daemon hasn't indexed yet: next to the note.
local function fallback(from, target)
  local p = vim.fs.normalize(vim.fs.dirname(from) .. '/' .. target)
  for _, c in ipairs({ p, p .. '.md' }) do
    local st = vim.uv.fs_stat(c)
    if st and st.type == 'file' then
      return c
    end
  end
end

--- Rescans buf and redraws every note embed in it.
function M.refresh(buf)
  if not config.options.notes.enabled or not vim.api.nvim_buf_is_valid(buf) then
    return
  end
  local root = vim.b[buf].brain_root
  if not root then
    return
  end
  local items = {}
  for _, it in ipairs(scan.buf(buf)) do
    if it.kind == 'note' then
      items[#items + 1] = it
    end
  end
  local st = state[buf] or { seq = 0 }
  state[buf] = st
  st.seq = st.seq + 1
  local seq = st.seq
  if #items == 0 then
    vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
    return
  end
  local targets = {}
  for i, it in ipairs(items) do
    targets[i] = it.target
  end
  local from = vim.api.nvim_buf_get_name(buf)
  client.request(root, 'resolve', { targets = targets, from = from }, function(err, res)
    if seq ~= st.seq or not vim.api.nvim_buf_is_valid(buf) then
      return
    end
    local self = vim.fs.normalize(vim.fn.fnamemodify(from, ':p'))
    local width = text_width(buf)
    vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
    local n = vim.api.nvim_buf_line_count(buf)
    for i, it in ipairs(items) do
      local path = not err and type(res) == 'table' and res[i] or ''
      if path == '' or path == vim.NIL then
        path = fallback(from, it.target)
      end
      local lines, msg
      if not path then
        msg = 'note not found: ' .. it.target
      elseif vim.fs.normalize(path) == self and not it.anchor then
        msg = 'a note cannot embed itself'
      else
        lines = note_lines(path)
        if not lines then
          msg = 'cannot read ' .. path
        else
          lines, msg = section(strip_frontmatter(lines), it.anchor)
        end
      end
      if it.line < n then
        vim.api.nvim_buf_set_extmark(buf, ns, it.line, math.min(it.col, #(vim.api.nvim_buf_get_lines(buf, it.line, it.line + 1, false)[1] or '')), {
          virt_lines = build(lines or {}, msg, width),
          undo_restore = false,
          invalidate = true,
        })
      end
    end
  end)
end

--- Redraws the note embeds of every visible buffer in root (after a save).
function M.refresh_root(root)
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.b[b].brain_root == root and vim.bo[b].filetype == 'markdown' and vim.fn.bufwinid(b) ~= -1 then
      M.refresh(b)
    end
  end
end

function M.detach(buf)
  state[buf] = nil
  if vim.api.nvim_buf_is_valid(buf) then
    vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
  end
end

--- Opens the note an embed points at, at its heading/block when it has one.
function M.open(path, anchor)
  vim.cmd.edit(vim.fn.fnameescape(path))
  if not anchor then
    return
  end
  local lines = vim.api.nvim_buf_get_lines(0, 0, -1, false)
  local block = anchor:match('^%^(.+)$')
  local want = not block and norm(anchor:match('([^#]*)$'))
  for i, l in ipairs(lines) do
    local _, text = heading(l)
    if (block and l:find('^' .. block, 1, true)) or (want and text and norm(text) == want) then
      vim.api.nvim_win_set_cursor(0, { i, 0 })
      return
    end
  end
end

return M
