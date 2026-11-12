-- Locates the brain sources and keeps bin/brain built from them, so the
-- plugin always runs the language server and daemon from the same checkout.
local M = {}

local uv = vim.uv

local function plugin_dir()
  local src = debug.getinfo(1, 'S').source:sub(2)
  return vim.fs.dirname(vim.fs.dirname(vim.fs.dirname(src))) -- repo root
end

function M.source(cfg)
  if cfg.source then
    return vim.fs.normalize(cfg.source)
  end
  return plugin_dir()
end

function M.bin(cfg)
  if cfg.bin then
    return vim.fs.normalize(cfg.bin)
  end
  return M.source(cfg) .. '/bin/brain'
end

local function mtime(path)
  local st = uv.fs_stat(path)
  return st and (st.mtime.sec + st.mtime.nsec / 1e9) or nil
end

--- Newest mtime of the Go sources (and go.mod) under dir.
local function newest_source(dir)
  local newest = mtime(dir .. '/go.mod') or 0
  for name, kind in vim.fs.dir(dir, {
    depth = 6,
    skip = function(n)
      return not (n:match('^%.') or n == 'bin' or n == 'nvim' or n == 'node_modules' or n == 'testdata' or n == 'temp' or n == 'bench')
    end,
  }) do
    if kind == 'file' and name:match('%.go$') and not name:match('_test%.go$') then
      local t = mtime(dir .. '/' .. name)
      if t and t > newest then
        newest = t
      end
    end
  end
  return newest
end

function M.stale(cfg)
  if cfg.bin then
    return false
  end
  local bin = M.bin(cfg)
  local t = mtime(bin)
  if not t then
    return true
  end
  if not cfg.auto_build then
    return false
  end
  return newest_source(M.source(cfg)) > t
end

local function go_cmd(src, out)
  if vim.fn.executable('go') == 1 then
    return { 'go', 'build', '-o', out, '.' }
  end
  if vim.fn.executable('mise') == 1 then
    return { 'mise', 'exec', '--', 'go', 'build', '-o', out, '.' }
  end
  return nil
end

--- Builds the binary. Synchronous when no callback is given.
---@param cb? fun(ok: boolean, err?: string)
function M.build(cfg, cb)
  local src, bin = M.source(cfg), M.bin(cfg)
  local cmd = go_cmd(src, bin)
  if not cmd then
    local err = 'brain: `go` not found; cannot build ' .. bin
    if cb then
      return cb(false, err)
    end
    return false, err
  end
  -- Build to a temp file and rename, so a running daemon/LSP never sees a half-written binary.
  local tmp = bin .. '.tmp'
  cmd[#cmd - 1] = tmp
  local function done(res)
    if res.code == 0 then
      local ok, err = uv.fs_rename(tmp, bin)
      if not ok then
        return false, err
      end
      return true
    end
    pcall(uv.fs_unlink, tmp)
    return false, 'brain build failed:\n' .. (res.stderr or '')
  end
  if cb then
    vim.system(cmd, { cwd = src, text = true }, function(res)
      local ok, err = done(res)
      vim.schedule(function()
        cb(ok, err)
      end)
    end)
    return
  end
  return done(vim.system(cmd, { cwd = src, text = true }):wait())
end

--- Ensures the binary exists and is current; returns its path or nil.
function M.ensure(cfg)
  local bin = M.bin(cfg)
  if M.stale(cfg) then
    local ok, err = M.build(cfg)
    if not ok then
      vim.notify(err, vim.log.levels.ERROR)
      if not mtime(bin) then
        return nil
      end
    end
  end
  return bin
end

return M
