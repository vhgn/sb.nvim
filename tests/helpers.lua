-- Helpers for brain.nvim end-to-end specs. Each test runs in its own
-- `nvim --headless --clean`, with its own vault, XDG dirs and daemon (see run.sh).
local H = {}

local plugin = vim.fs.dirname(vim.fs.dirname(debug.getinfo(1, 'S').source:sub(2)))
H.plugin = plugin

--- Records image.nvim calls instead of drawing (headless has no graphics).
H.images = {}
function H.fake_image_nvim()
  H.images = {}
  package.loaded['image'] = nil
  package.preload['image'] = function()
    local api = { _enabled = true }
    function api.is_enabled()
      return api._enabled
    end
    local function make(path, opts)
      local img = { path = path, opts = opts, renders = {}, cleared = false }
      function img:render(geom)
        table.insert(self.renders, geom)
        self.cleared = false
        self.geometry = geom
      end
      function img:clear()
        self.cleared = true
      end
      table.insert(H.images, img)
      return img
    end
    function api.from_file(path, opts)
      return make(path, opts)
    end
    function api.from_url(url, opts, cb)
      cb(make(url, opts))
    end
    return api
  end
  package.preload['image.utils.term'] = function()
    return {
      get_size = function()
        return { cell_width = 10, cell_height = 20 }
      end,
    }
  end
end

--- Live (rendered, not cleared) fake images.
function H.live_images()
  local out = {}
  for _, img in ipairs(H.images) do
    if not img.cleared and #img.renders > 0 then
      out[#out + 1] = img
    end
  end
  return out
end

--- Creates a vault from { ['rel/path.md'] = 'content' }; returns its absolute path.
function H.vault(files)
  local dir = vim.fn.resolve(vim.fn.tempname())
  vim.fn.mkdir(dir, 'p')
  for rel, content in pairs(files or {}) do
    local p = dir .. '/' .. rel
    vim.fn.mkdir(vim.fs.dirname(p), 'p')
    local f = assert(io.open(p, 'wb'))
    f:write(content)
    f:close()
  end
  H.root = dir
  return dir
end

function H.write(rel, content)
  local p = H.root .. '/' .. rel
  vim.fn.mkdir(vim.fs.dirname(p), 'p')
  local f = assert(io.open(p, 'wb'))
  f:write(content)
  f:close()
  return p
end

--- Loads the plugin and runs setup against H.root.
function H.setup(opts)
  vim.opt.rtp:prepend(plugin)
  vim.cmd.runtime('plugin/brain.lua')
  H.fake_image_nvim()
  opts = vim.tbl_deep_extend('force', {
    root = H.root,
    bin = os.getenv('BRAIN_BIN'),
    lsp = { enabled = false },
    daemon = { idle = '20s' },
    mermaid = { cmd = 'mmdc-not-installed' },
  }, opts or {})
  require('brain').setup(opts)
  vim.o.columns, vim.o.lines = 100, 40
end

function H.wait(ms, cond, msg)
  if not vim.wait(ms or 3000, cond, 10) then
    error('timeout: ' .. (msg or 'condition'), 2)
  end
end

--- Waits until every base in buf has a result or an error.
function H.wait_bases(buf)
  buf = buf or vim.api.nvim_get_current_buf()
  H.wait(5000, function()
    local st = require('brain.base').state[buf]
    if not st then
      return false
    end
    for _, it in ipairs(st.items) do
      if not (it.result or it.err) then
        return false
      end
    end
    return true
  end, 'bases to render')
end

--- Edits a vault file and waits for its bases.
function H.open(rel)
  vim.cmd.edit(vim.fn.fnameescape(H.root .. '/' .. rel))
  local buf = vim.api.nvim_get_current_buf()
  H.wait_bases(buf)
  return buf
end

--- Virtual lines of rendered bases: { {anchor = line, lines = {...}} } sorted by anchor.
function H.virt(buf)
  buf = buf or vim.api.nvim_get_current_buf()
  local out = {}
  local marks = vim.api.nvim_buf_get_extmarks(buf, require('brain.base').ns, 0, -1, { details = true })
  for _, m in ipairs(marks) do
    local lines = {}
    for _, vl in ipairs(m[4].virt_lines or {}) do
      local s = {}
      for _, ch in ipairs(vl) do
        s[#s + 1] = ch[1]
      end
      lines[#lines + 1] = table.concat(s)
    end
    out[#out + 1] = { anchor = m[2], lines = lines }
  end
  table.sort(out, function(a, b)
    return a.anchor < b.anchor
  end)
  return out
end

function H.virt_text(buf)
  local parts = {}
  for _, v in ipairs(H.virt(buf)) do
    parts[#parts + 1] = table.concat(v.lines, '\n')
  end
  return table.concat(parts, '\n')
end

--- Base query through the daemon.
function H.query(params)
  local err, res = require('brain.client').request_sync(H.root, 'query', params, 5000)
  assert(not err, err)
  return res
end

--- Table cells of a query result: { {'a.md', '12'}, ... } across groups.
function H.cells(res)
  local out = {}
  for _, g in ipairs(res.groups) do
    for _, row in ipairs(g.rows) do
      local r = {}
      for i, c in ipairs(row.cells) do
        r[i] = c.t
      end
      out[#out + 1] = r
    end
  end
  return out
end

function H.feed(keys)
  vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes(keys, true, false, true), 'xt', false)
end

function H.eq(want, got, msg)
  if not vim.deep_equal(want, got) then
    error(('%s\nwant: %s\n got: %s'):format(msg or 'not equal', vim.inspect(want), vim.inspect(got)), 2)
  end
end

function H.ok(cond, msg)
  if not cond then
    error(msg or 'assertion failed', 2)
  end
end

function H.contains(haystack, needle, msg)
  if not tostring(haystack):find(needle, 1, true) then
    error(('%s\nexpected to find: %s\nin:\n%s'):format(msg or 'missing text', needle, haystack), 2)
  end
end

function H.not_contains(haystack, needle, msg)
  if tostring(haystack):find(needle, 1, true) then
    error(('%s\ndid not expect: %s\nin:\n%s'):format(msg or 'unexpected text', needle, haystack), 2)
  end
end

return H
