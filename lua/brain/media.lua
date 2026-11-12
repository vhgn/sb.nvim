-- Images in markdown via image.nvim: ![[img.png]], ![](img.png), ```mermaid
-- (through mmdc) and display math (rendered by the brain daemon), all drawn
-- in the terminal's colors. Inline $math$ is concealed as Unicode text.
local client = require('brain.client')
local colors = require('brain.colors')
local config = require('brain.config')
local scan = require('brain.scan')

local M = {}

local ns_inline = vim.api.nvim_create_namespace('brain.math.inline')
local ns_msg = vim.api.nvim_create_namespace('brain.media.messages')

---@type table<integer, {images: table<string, any>, inline: table[], cursor_line?: integer, gen: integer}>
local state = {}
M.state = state

--- Small memo tables (cleared when they grow too big): pure results that
--- every refresh would otherwise recompute or ask the daemon for again.
local function memo()
  return { n = 0, t = {} }
end
local function memo_get(m, k)
  return m.t[k]
end
local function memo_set(m, k, v)
  if m.n >= 20000 then
    m.n, m.t = 0, {}
  end
  if m.t[k] == nil then
    m.n = m.n + 1
  end
  m.t[k] = v
end
local hashes = memo() -- text -> sha256
local unicode = memo() -- inline TeX -> texUnicode answer
local tex_png = memo() -- tex request key -> png path

local function sha256(text)
  local h = memo_get(hashes, text)
  if not h then
    h = vim.fn.sha256(text)
    memo_set(hashes, text, h)
  end
  return h
end

--- Forgets cached daemon answers (e.g. after the binary was rebuilt).
function M.reset_cache()
  hashes, unicode, tex_png = memo(), memo(), memo()
end

local function image_api()
  local ok, api = pcall(require, 'image')
  if not ok then
    return nil
  end
  if api.is_enabled and not api.is_enabled() then
    return nil
  end
  return api
end

M.image_api = image_api

local function cell_size()
  local ok, term = pcall(require, 'image.utils.term')
  if ok and term.get_size then
    local s = term.get_size()
    if s and s.cell_height and s.cell_height > 0 then
      return s.cell_width, s.cell_height
    end
  end
  return 9, 20
end

local function cache_dir(sub)
  local d = vim.fn.stdpath('cache') .. '/brain/' .. sub
  vim.fn.mkdir(d, 'p')
  return d
end

local function st(buf)
  local s = state[buf]
  if not s then
    s = { images = {}, inline = {}, gen = 0 }
    state[buf] = s
  end
  return s
end

local function message(buf, line, text, group)
  pcall(vim.api.nvim_buf_set_extmark, buf, ns_msg, line, 0, {
    virt_lines = { { { '  ' .. text, group or 'DiagnosticWarn' } } },
    undo_restore = false,
    invalidate = true,
  })
end

-- ---- producers: item -> png path (async) ----

local function resolve_image(buf, item, cb, batch)
  local target = item.target
  if target:match('^https?://') then
    return cb(target, true)
  end
  local file = vim.api.nvim_buf_get_name(buf)
  local dir = vim.fs.dirname(file)
  local function local_path()
    local p = target
    if p:sub(1, 1) == '/' then
      local root = vim.b[buf].brain_root
      if root and vim.uv.fs_stat(root .. p) then
        return root .. p
      end
      return p
    end
    p = vim.fs.normalize(dir .. '/' .. p)
    if vim.uv.fs_stat(p) then
      return p
    end
  end
  if not item.wiki then
    return cb(local_path())
  end
  local root = vim.b[buf].brain_root
  if not root then
    return cb(local_path())
  end
  local function done(p)
    cb(p ~= '' and p or local_path())
  end
  if batch then
    -- one resolve request for every wiki image of the refresh
    batch.targets[#batch.targets + 1] = target
    batch.cbs[#batch.cbs + 1] = done
    return
  end
  client.request(root, 'resolve', { targets = { target }, from = file }, function(err, res)
    done(not err and res and res[1] or '')
  end)
end

local mermaid_jobs = {} ---@type table<string, function[]>

local function render_mermaid(item, width_px, cb, ctx)
  local cfg = config.options.mermaid
  ctx = ctx or {}
  if ctx.mmdc == nil then
    ctx.mmdc = vim.fn.executable(cfg.cmd) == 1
  end
  if not ctx.mmdc then
    return cb(nil, '`' .. cfg.cmd .. '` not found: install @mermaid-js/mermaid-cli to render mermaid')
  end
  if not ctx.theme then
    ctx.theme = colors.mermaid(ctx.palette)
    ctx.theme_json = vim.json.encode(ctx.theme)
  end
  local theme = ctx.theme
  local cfg_json = ctx.theme_json
  local key = sha256(item.text .. cfg_json .. width_px .. cfg.scale)
  local dir = cache_dir('mermaid')
  local png = dir .. '/' .. key:sub(1, 24) .. '.png'
  if vim.uv.fs_stat(png) then
    return cb(png)
  end
  -- one run per diagram, however many blocks/refreshes ask for it
  local waiting = mermaid_jobs[png]
  if waiting then
    table.insert(waiting, cb)
    return
  end
  mermaid_jobs[png] = { cb }
  local function finish(path, err)
    local cbs = mermaid_jobs[png] or {}
    mermaid_jobs[png] = nil
    for _, f in ipairs(cbs) do
      f(path, err)
    end
  end
  local input = dir .. '/' .. key:sub(1, 24) .. '.mmd'
  local conf = dir .. '/' .. key:sub(1, 24) .. '.json'
  -- write elsewhere and rename, so a reader never sees a half-written png
  local tmp = ('%s/%s.%d.tmp.png'):format(dir, key:sub(1, 24), vim.uv.os_getpid())
  vim.fn.writefile(vim.split(item.text, '\n'), input)
  vim.fn.writefile({ cfg_json }, conf)
  local bg = theme.themeVariables.background
  local ok = pcall(vim.system, { cfg.cmd, '-q', '-i', input, '-o', tmp, '-c', conf, '-b', bg == 'transparent' and 'transparent' or bg, '-w', tostring(width_px), '-s', tostring(cfg.scale) }, { text = true }, function(res)
    vim.schedule(function()
      if res.code == 0 and vim.uv.fs_stat(tmp) and vim.uv.fs_rename(tmp, png) then
        finish(png)
      else
        os.remove(tmp)
        finish(nil, 'mermaid: ' .. vim.trim((res.stderr or ''):gsub('\n.*', '')))
      end
    end)
  end)
  if not ok then
    finish(nil, '`' .. cfg.cmd .. '` could not be started')
  end
end

local function render_math(buf, item, cb, ctx)
  local root = vim.b[buf].brain_root
  if not root then
    return cb(nil)
  end
  ctx = ctx or {}
  if not ctx.tex then
    local p = ctx.palette or colors.palette()
    local _, ch = cell_size()
    local palette = { red = p.red, green = p.green, blue = p.blue, yellow = p.yellow, magenta = p.magenta, purple = p.magenta, cyan = p.cyan, teal = p.cyan, gray = p.comment, grey = p.comment, orange = p.orange, white = p.white, black = p.black }
    local size = config.options.math.size or math.floor(ch * 1.05)
    ctx.tex = { fg = p.fg, error = p.red, size = size, palette = palette }
    ctx.tex_key = table.concat({ root, p.fg, p.red, size, vim.json.encode(palette) }, '\0')
  end
  local t = ctx.tex
  -- the same formula in the same colors is the same png: skip the daemon
  local key = ctx.tex_key .. '\0' .. item.text
  local png = memo_get(tex_png, key)
  if png and vim.uv.fs_stat(png) then
    return cb(png)
  end
  client.request(root, 'tex', {
    src = item.text,
    display = true,
    fg = t.fg,
    error = t.error,
    size = t.size,
    palette = t.palette,
  }, function(err, res)
    if err then
      return cb(nil, 'math: ' .. err)
    end
    if res and res.png then
      memo_set(tex_png, key, res.png)
    end
    cb(res and res.png)
  end)
end

-- ---- images ----

local function show(buf, win, id, path, line, opts)
  local api = image_api()
  if not api or not path then
    return
  end
  local s = st(buf)
  local existing = s.images[id]
  if existing and existing.path == path then
    if existing.line ~= line then
      existing.line = line
      pcall(function()
        existing.img:render({ x = 0, y = line, width = opts.width, height = opts.height })
      end)
    end
    existing.alive = true
    return
  end
  if existing then
    pcall(function()
      existing.img:clear()
    end)
  end
  local function attach(img)
    if not img then
      return
    end
    s.images[id] = { img = img, path = path, line = line, alive = true }
    pcall(function()
      img:render({ x = 0, y = line, width = opts.width, height = opts.height })
    end)
  end
  local params = {
    id = id,
    window = win,
    buffer = buf,
    with_virtual_padding = true,
    namespace = 'brain',
    max_height_window_percentage = nil,
    max_height = config.options.images.max_height,
  }
  if path:match('^https?://') then
    pcall(api.from_url, path, params, attach)
  else
    local ok, img = pcall(api.from_file, path, params)
    if ok then
      attach(img)
    end
  end
end

-- ---- inline math ----

local function mark_inline(buf, m)
  pcall(vim.api.nvim_buf_set_extmark, buf, ns_inline, m.line, m.col, {
    end_col = m.end_col,
    conceal = '',
    undo_restore = false,
    invalidate = true,
  })
  pcall(vim.api.nvim_buf_set_extmark, buf, ns_inline, m.line, m.col, {
    virt_text = { { m.text, 'BrainMath' } },
    virt_text_pos = 'inline',
    undo_restore = false,
    invalidate = true,
  })
end

local function apply_inline(buf)
  local s = state[buf]
  if not s or not vim.api.nvim_buf_is_valid(buf) then
    return
  end
  vim.api.nvim_buf_clear_namespace(buf, ns_inline, 0, -1)
  local cur
  if vim.api.nvim_get_current_buf() == buf then
    cur = vim.api.nvim_win_get_cursor(0)[1] - 1
  end
  s.cursor_line = cur
  local by_line = {}
  for _, m in ipairs(s.inline) do
    if m.text then
      local l = by_line[m.line]
      if not l then
        l = {}
        by_line[m.line] = l
      end
      l[#l + 1] = m
      if m.line ~= cur then
        mark_inline(buf, m)
      end
    end
  end
  s.by_line = by_line
  s.applied_tick = vim.api.nvim_buf_get_changedtick(buf)
end

function M.cursor_moved(buf)
  local s = state[buf]
  if not s or #s.inline == 0 then
    return
  end
  local line = vim.api.nvim_win_get_cursor(0)[1] - 1
  if line == s.cursor_line then
    return
  end
  -- Unchanged text: only the line left and the line entered differ, so
  -- touch just those two (j/k stays O(1) however much math the note has).
  if s.by_line and s.applied_tick == vim.api.nvim_buf_get_changedtick(buf) and vim.api.nvim_get_current_buf() == buf and vim.api.nvim_buf_is_valid(buf) then
    local old = s.cursor_line
    s.cursor_line = line
    if s.by_line[line] then
      vim.api.nvim_buf_clear_namespace(buf, ns_inline, line, line + 1)
    end
    if old and s.by_line[old] then
      vim.api.nvim_buf_clear_namespace(buf, ns_inline, old, old + 1)
      for _, m in ipairs(s.by_line[old]) do
        mark_inline(buf, m)
      end
    end
    return
  end
  apply_inline(buf)
end

-- ---- refresh ----

--- Collects the wiki images of one refresh into a single resolve request.
local function item_root_batch(buf, root)
  if not root then
    return nil
  end
  return { targets = {}, cbs = {}, from = vim.api.nvim_buf_get_name(buf) }
end


function M.refresh(buf)
  if not vim.api.nvim_buf_is_valid(buf) then
    return
  end
  local win = vim.fn.bufwinid(buf)
  if win == -1 then
    return
  end
  local s = st(buf)
  s.gen = s.gen + 1
  local gen = s.gen
  local items = scan.buf(buf)
  vim.api.nvim_buf_clear_namespace(buf, ns_msg, 0, -1)
  for _, im in pairs(s.images) do
    im.alive = false
  end
  local cw, ch = cell_size()
  -- Ids name the n-th occurrence of the same content, not its line: an image
  -- moved by edits above it is re-rendered in place instead of re-created, and
  -- identical formulas/diagrams on different lines each get their own image.
  local seen = {}
  local function ident(kind, key)
    local k = kind .. ':' .. key
    seen[k] = (seen[k] or 0) + 1
    return ('brain:%d:%s#%d'):format(buf, k, seen[k])
  end
  local info = vim.fn.getwininfo(win)[1]
  local text_cols = info.width - info.textoff
  local inline = {}
  local api = image_api()
  -- computed once per refresh, not per item
  local ctx = {}
  local function palette()
    ctx.palette = ctx.palette or colors.palette()
    return ctx.palette
  end
  local root = vim.b[buf].brain_root
  local batch = item_root_batch(buf, root)
  for _, item in ipairs(items) do
    local anchor = item.end_line
    if item.kind == 'image' and config.options.images.enabled and api then
      local id = ident('img', item.target)
      local width, height
      if item.size then
        local w, h = item.size:match('^%s*(%d+)%s*x?%s*(%d*)%s*$')
        if w then
          width = math.max(1, math.floor(tonumber(w) / cw))
          if h ~= '' then
            height = math.max(1, math.floor(tonumber(h) / ch))
          end
        end
      end
      if s.images[id] then
        s.images[id].alive = true
      end
      resolve_image(buf, item, function(path)
        if gen ~= s.gen then
          return
        end
        if not path then
          message(buf, anchor, '🖼 not found: ' .. item.target)
          return
        end
        show(buf, win, id, path, anchor, { width = width, height = height })
      end, batch)
    elseif item.kind == 'mermaid' and config.options.mermaid.enabled then
      local id = ident('mermaid', sha256(item.text):sub(1, 16))
      if s.images[id] then
        s.images[id].alive = true
      end
      if not api then
        goto continue
      end
      if config.options.mermaid.enabled then
        palette()
      end
      render_mermaid(item, math.max(400, text_cols * cw), function(path, err)
        if gen ~= s.gen then
          return
        end
        if err then
          message(buf, anchor, err)
        end
        show(buf, win, id, path, anchor, {})
      end, ctx)
    elseif item.kind == 'math' and config.options.math.enabled then
      if item.display then
        local id = ident('math', sha256(item.text):sub(1, 16))
        if s.images[id] then
          s.images[id].alive = true
        end
        if not api then
          goto continue
        end
        if root then
          palette()
        end
        render_math(buf, item, function(path, err)
          if gen ~= s.gen then
            return
          end
          if err then
            message(buf, anchor, err, 'DiagnosticError')
          end
          show(buf, win, id, path, anchor, {})
        end, ctx)
      elseif config.options.math.inline then
        inline[#inline + 1] = { line = item.line, col = item.col, end_col = item.end_col, src = item.text }
      end
    end
    ::continue::
  end
  if batch and #batch.targets > 0 then
    client.request(root, 'resolve', { targets = batch.targets, from = batch.from }, function(err, res)
      for i, done in ipairs(batch.cbs) do
        done(not err and type(res) == 'table' and res[i] or '')
      end
    end)
  end
  -- drop images whose source disappeared
  vim.schedule(function()
    for id, im in pairs(s.images) do
      if not im.alive then
        pcall(function()
          im.img:clear()
        end)
        s.images[id] = nil
      end
    end
  end)
  -- inline math → Unicode (answers are cached: only new formulas go to the daemon)
  if #inline > 0 and root then
    local missing, seen_src = {}, {}
    for _, m in ipairs(inline) do
      if memo_get(unicode, m.src) == nil and not seen_src[m.src] then
        seen_src[m.src] = true
        missing[#missing + 1] = m.src
      end
    end
    local function finish()
      for _, m in ipairs(inline) do
        local r = memo_get(unicode, m.src)
        -- invalid TeX stays visible as typed
        m.text = r and not r.error and r.text or nil
      end
      s.inline = inline
      apply_inline(buf)
    end
    if #missing == 0 then
      finish()
    else
      client.request(root, 'texUnicode', { items = missing }, function(err, res)
        if err or gen ~= s.gen or type(res) ~= 'table' then
          return
        end
        for i, src in ipairs(missing) do
          memo_set(unicode, src, type(res[i]) == 'table' and res[i] or false)
        end
        finish()
      end)
    end
  else
    s.inline = {}
    vim.api.nvim_buf_clear_namespace(buf, ns_inline, 0, -1)
  end
end

function M.clear(buf)
  local s = state[buf]
  if not s then
    return
  end
  for _, im in pairs(s.images) do
    pcall(function()
      im.img:clear()
    end)
  end
  s.images = {}
  if vim.api.nvim_buf_is_valid(buf) then
    vim.api.nvim_buf_clear_namespace(buf, ns_inline, 0, -1)
    vim.api.nvim_buf_clear_namespace(buf, ns_msg, 0, -1)
  end
end

function M.detach(buf)
  M.clear(buf)
  state[buf] = nil
  scan.forget(buf)
end

return M
