-- Client for the per-vault `brain daemon`. One connection per root per
-- editor; the daemon itself is shared between editors (it exits when another
-- one already serves the root), so every Neovim gets the same index.
local M = {}

local uv = vim.uv

---@class brain.Conn
---@field root string
---@field pipe uv.uv_pipe_t?
---@field state 'idle'|'connecting'|'connected'
---@field pending table<integer, fun(err: string?, result: any)>
---@field queue string[]
---@field parts string[] bytes of an incomplete reply line
---@field attempts integer

---@type table<string, brain.Conn>
local conns = {}
local next_id = 0
M.bin = nil ---@type string?
M.idle = '30m'

function M.runtime_dir()
  local xdg = os.getenv('XDG_RUNTIME_DIR')
  if xdg and xdg ~= '' then
    return xdg .. '/brain'
  end
  local tmp = os.getenv('TMPDIR')
  if not tmp or tmp == '' then
    tmp = '/tmp'
  end
  return tmp:gsub('/$', '') .. '/brain-' .. uv.getuid()
end

--- Must match daemon.Key in Go: first 16 hex chars of sha256(root).
function M.socket_path(root)
  return M.runtime_dir() .. '/' .. vim.fn.sha256(root):sub(1, 16) .. '.sock'
end

local function normalize(root)
  root = vim.fs.normalize(vim.fn.fnamemodify(root, ':p'))
  if #root > 1 then
    root = root:gsub('/+$', '')
  end
  return root
end
M.normalize = normalize

local function fail_all(c, err)
  local pending = c.pending
  c.pending = {}
  for _, cb in pairs(pending) do
    vim.schedule(function()
      cb(err, nil)
    end)
  end
end

local function on_line(c, line)
  if line == '' then
    return
  end
  local ok, msg = pcall(vim.json.decode, line, { luanil = { object = true, array = true } })
  if not ok or type(msg) ~= 'table' then
    return
  end
  local cb = msg.id and c.pending[msg.id]
  if cb then
    c.pending[msg.id] = nil
    vim.schedule(function()
      cb(msg.error, msg.result)
    end)
  end
end

local function spawn(root)
  if not M.bin then
    return false
  end
  local handle = uv.spawn(M.bin, {
    args = { 'daemon', '--root', root, '--idle', M.idle },
    detached = true,
    stdio = { nil, nil, nil },
  }, function() end)
  if handle then
    handle:unref()
    return true
  end
  return false
end

local connect

local function flush(c)
  if c.state ~= 'connected' or not c.pipe then
    return
  end
  local q = c.queue
  if #q == 0 then
    return
  end
  c.queue = {}
  c.pipe:write(#q == 1 and q[1] or q) -- several at once: one writev
end

local function disconnect(c, err)
  if c.pipe and not c.pipe:is_closing() then
    c.pipe:read_stop()
    c.pipe:close()
  end
  c.pipe = nil
  c.state = 'idle'
  c.parts = {}
  fail_all(c, err or 'disconnected')
end

connect = function(c)
  if c.state ~= 'idle' then
    return
  end
  c.state = 'connecting'
  local path = M.socket_path(c.root)
  local function try()
    local pipe = uv.new_pipe(false)
    pipe:connect(path, function(err)
      if err then
        pipe:close()
        c.attempts = c.attempts + 1
        if c.attempts == 1 then
          vim.schedule(function()
            spawn(c.root)
          end)
        end
        if c.attempts > 60 then -- ~3s
          c.state = 'idle'
          c.attempts = 0
          c.queue = {}
          fail_all(c, 'cannot reach brain daemon at ' .. path .. ': ' .. err)
          return
        end
        local t = uv.new_timer()
        t:start(50, 0, function()
          t:close()
          try()
        end)
        return
      end
      c.pipe = pipe
      c.state = 'connected'
      c.attempts = 0
      pipe:read_start(function(rerr, data)
        if rerr or not data then
          disconnect(c, rerr or 'daemon closed the connection')
          return
        end
        -- split into lines without re-copying what's buffered (big query
        -- answers arrive in many chunks)
        local nl = data:find('\n', 1, true)
        if not nl then
          c.parts[#c.parts + 1] = data
          return
        end
        local line = data:sub(1, nl - 1)
        if #c.parts > 0 then
          c.parts[#c.parts + 1] = line
          line = table.concat(c.parts)
          c.parts = {}
        end
        on_line(c, line)
        local start = nl + 1
        while true do
          nl = data:find('\n', start, true)
          if not nl then
            break
          end
          on_line(c, data:sub(start, nl - 1))
          start = nl + 1
        end
        if start <= #data then
          c.parts[1] = data:sub(start)
        end
      end)
      flush(c)
    end)
  end
  try()
end

local function conn(root)
  root = normalize(root)
  local c = conns[root]
  if not c then
    c = { root = root, state = 'idle', pending = {}, queue = {}, parts = {}, attempts = 0 }
    conns[root] = c
  end
  return c
end

--- Sends a request. cb(err, result) runs on the main loop.
---@param root string
---@param method string
---@param params table?
---@param cb fun(err: string?, result: any)?
function M.request(root, method, params, cb)
  local c = conn(root)
  local msg = { method = method, params = params or vim.empty_dict() }
  if cb then
    next_id = next_id + 1
    msg.id = next_id
    c.pending[next_id] = cb
  end
  table.insert(c.queue, vim.json.encode(msg) .. '\n')
  if c.state == 'idle' then
    connect(c)
  else
    flush(c)
  end
end

--- Synchronous request (for tests and commands). Returns err, result.
function M.request_sync(root, method, params, timeout)
  local done, err, res = false, nil, nil
  M.request(root, method, params, function(e, r)
    done, err, res = true, e, r
  end)
  vim.wait(timeout or 5000, function()
    return done
  end, 10)
  if not done then
    return 'timeout', nil
  end
  return err, res
end

function M.notify(root, method, params)
  M.request(root, method, params, nil)
end

function M.ensure(root)
  connect(conn(root))
end

function M.connected(root)
  local c = conns[normalize(root)]
  return c ~= nil and c.state == 'connected'
end

function M.close_all()
  for _, c in pairs(conns) do
    disconnect(c, 'closed')
  end
end

return M
