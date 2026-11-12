-- Edge cases of the per-vault daemon and the editor's client.
local H = require('helpers')
local T = {}

vim.opt.rtp:prepend(H.plugin)
local client = require('brain.client')
client.bin = os.getenv('BRAIN_BIN')
client.idle = '20s'

local uv = vim.uv
local extra_roots = {} -- daemons besides H.root, shut down after each test

local function sync(root, method, params, timeout)
  return client.request_sync(root, method, params or {}, timeout or 5000)
end

local function pid_of(root)
  local err, res = sync(root, 'status')
  assert(not err, 'status failed: ' .. tostring(err))
  return res.pid
end

local function alive(pid)
  return uv.kill(pid, 0) == 0
end

--- Daemon processes serving root (by command line).
local function daemons(root)
  local res = vim.system({ 'pgrep', '-a', '-f', 'daemon --root' }, { text = true }):wait()
  local out = {}
  for line in (res.stdout or ''):gmatch('[^\n]+') do
    if line:find(' daemon --root ' .. root .. ' ', 1, true) then
      out[#out + 1] = tonumber(line:match('^%d+'))
    end
  end
  return out
end

--- Starts another headless Neovim acting as a second editor on root; it
--- prints the daemon pid it was served by. Returns the vim.system handle.
local function other_editor(root)
  local script = vim.fn.tempname() .. '.lua'
  vim.fn.writefile({
    ('vim.opt.rtp:prepend(%q)'):format(H.plugin),
    'local c = require("brain.client")',
    ('c.bin = %q'):format(client.bin),
    'c.idle = "20s"',
    ('local err, res = c.request_sync(%q, "status", {}, 8000)'):format(root),
    'io.stdout:write(err and ("ERR " .. err) or tostring(res.pid))',
    'vim.cmd("qa!")',
  }, script)
  return vim.system({ 'nvim', '--headless', '--clean', '-u', 'NONE', '-l', script }, { text = true })
end

--- Raw unix-socket connection to the daemon of root (no client logic).
local function raw(root)
  local pipe = uv.new_pipe(false)
  local conn = { lines = {}, closed = false }
  local done, cerr = false, nil
  pipe:connect(client.socket_path(client.normalize(root)), function(err)
    cerr, done = err, true
  end)
  H.wait(3000, function()
    return done
  end, 'raw connect')
  assert(not cerr, cerr)
  local buf = ''
  pipe:read_start(function(err, data)
    if err or not data then
      conn.closed = true
      return
    end
    buf = buf .. data
    while true do
      local nl = buf:find('\n', 1, true)
      if not nl then
        break
      end
      table.insert(conn.lines, buf:sub(1, nl - 1))
      buf = buf:sub(nl + 1)
    end
  end)
  function conn.send(s)
    pipe:write(s)
  end
  function conn.close()
    pipe:read_stop()
    pipe:close()
  end
  return conn
end

local function wrap(fn)
  return function()
    local ok, err = xpcall(fn, debug.traceback)
    for _, r in ipairs(extra_roots) do
      pcall(sync, r, 'shutdown', {}, 500)
    end
    if not ok then
      error(err, 0)
    end
  end
end

local function test(name, fn)
  T[name] = wrap(fn)
end

-- ---------------------------------------------------------------- sharing

test('two editors share one daemon process', function()
  local root = H.vault({ ['a.md'] = '# a\n' })
  local pid = pid_of(root)
  local procs = {}
  for i = 1, 3 do
    procs[i] = other_editor(root)
  end
  for i = 1, 3 do
    local r = procs[i]:wait(15000)
    H.eq(tostring(pid), r.stdout, 'editor ' .. i .. ' must use the same daemon (stderr: ' .. (r.stderr or '') .. ')')
  end
  H.eq({ pid }, daemons(root))
end)

test('editors starting at the same moment on a cold root spawn one daemon', function()
  local root = H.vault({ ['a.md'] = '# a\n' })
  H.eq({}, daemons(root))
  local procs = {}
  for i = 1, 4 do
    procs[i] = other_editor(root)
  end
  local mine = pid_of(root)
  for i = 1, 4 do
    local r = procs[i]:wait(15000)
    H.eq(tostring(mine), r.stdout, 'editor ' .. i .. ' (stderr: ' .. (r.stderr or '') .. ')')
  end
  -- losers of the flock race exit right away
  H.wait(3000, function()
    return #daemons(root) == 1
  end, 'one daemon left: ' .. vim.inspect(daemons(root)))
  H.eq({ mine }, daemons(root))
end)

test('status counts each connected editor as a client', function()
  local root = H.vault({})
  local _, a = sync(root, 'status')
  local conn = raw(root)
  local _, b = sync(root, 'status')
  H.eq(a.clients + 1, b.clients)
  conn.close()
  H.wait(3000, function()
    local _, c = sync(root, 'status')
    return c.clients == a.clients
  end, 'client count to drop after disconnect')
end)

test('trailing slash and .. in the root reach the same daemon', function()
  local root = H.vault({ ['sub/x.md'] = '' })
  local pid = pid_of(root)
  H.eq(pid, pid_of(root .. '/'))
  H.eq(pid, pid_of(root .. '//'))
  H.eq(pid, pid_of(root .. '/sub/..'))
  H.eq(pid, pid_of(root .. '/./'))
  H.eq({ pid }, daemons(root))
end)

test('root with spaces and non-ASCII characters', function()
  H.vault({})
  local root = H.root .. '/my vault é ü'
  vim.fn.mkdir(root, 'p')
  H.write('my vault é ü/n.md', '# n\n')
  table.insert(extra_roots, root)
  local err, res = sync(root, 'status')
  H.eq(nil, err)
  H.eq(root, res.root)
  H.eq(1, res.files)
end)

-- ---------------------------------------------------------------- lifecycle

test('stale socket file and lock without a process are replaced', function()
  local root = H.vault({ ['a.md'] = '' })
  local sock = client.socket_path(root)
  vim.fn.mkdir(vim.fs.dirname(sock), 'p')
  vim.fn.writefile({ 'not a socket' }, sock)
  vim.fn.writefile({ '999999 ' .. root }, sock .. '.lock')
  local pid = pid_of(root)
  H.ok(alive(pid))
  H.eq('socket', uv.fs_stat(sock).type)
end)

test('stale unix socket left by a SIGKILLed daemon is replaced', function()
  local root = H.vault({ ['a.md'] = '' })
  local pid = pid_of(root)
  uv.kill(pid, 'sigkill')
  H.wait(3000, function()
    return not client.connected(root)
  end, 'client to notice the disconnect')
  H.eq('socket', uv.fs_stat(client.socket_path(root)).type, 'killed daemon leaves its socket behind')
  local pid2 = pid_of(root)
  H.ok(pid2 ~= pid and alive(pid2), 'a new daemon serves the root')
end)

test('daemon killed with a request in flight: error, then respawn', function()
  local root = H.vault({ ['a.md'] = '' })
  local pid = pid_of(root)
  -- a request the daemon is still working on when it dies
  local done, rerr
  local t0 = uv.hrtime()
  client.request(root, 'tex', { src = string.rep('\\frac{', 60) .. 'x' .. string.rep('}{y}', 60), size = 200 }, function(e)
    done, rerr = true, e
  end)
  uv.kill(pid, 'sigkill')
  H.wait(3000, function()
    return done
  end, 'in-flight request to finish')
  H.ok(rerr ~= nil, 'in-flight request must fail, not hang')
  H.ok((uv.hrtime() - t0) / 1e6 < 2500, 'and fail fast')
  local pid2 = pid_of(root)
  H.ok(pid2 ~= pid and alive(pid2))
  H.eq({ pid2 }, daemons(root))
end)

test('requests queued right after a kill are answered by the new daemon', function()
  local root = H.vault({ ['a.md'] = '' })
  local pid = pid_of(root)
  uv.kill(pid, 'sigkill')
  H.wait(3000, function()
    return not alive(pid) and not client.connected(root)
  end, 'daemon dead')
  local results = {}
  for i = 1, 5 do
    client.request(root, 'texUnicode', { items = { 'x^' .. i } }, function(e, r)
      results[i] = e or r[1].text
    end)
  end
  H.wait(6000, function()
    return #vim.tbl_keys(results) == 5
  end, 'all answers')
  H.eq({ 'x¹', 'x²', 'x³', 'x⁴', 'x⁵' }, results)
end)

test('shutdown leaves a stale socket; the next request respawns', function()
  local root = H.vault({})
  local pid = pid_of(root)
  local err, res = sync(root, 'shutdown')
  H.eq(nil, err)
  H.eq(true, res)
  H.wait(3000, function()
    return not alive(pid)
  end, 'daemon to exit')
  local pid2 = pid_of(root)
  H.ok(pid2 ~= pid)
end)

test('unreachable daemon (bad binary) fails within the retry budget, then recovers', function()
  local root = H.vault({})
  local bin = client.bin
  client.bin = '/nonexistent/brain'
  local t0 = uv.hrtime()
  local err = sync(root, 'ping', {}, 10000)
  local ms = (uv.hrtime() - t0) / 1e6
  H.ok(err and err:find('cannot reach brain daemon', 1, true), 'got: ' .. tostring(err))
  H.ok(ms < 6000, 'gave up after ' .. ms .. 'ms')
  client.bin = bin
  local e2, res = sync(root, 'ping')
  H.eq(nil, e2)
  H.eq(root, res.root)
end)

test('root that does not exist: served empty, not created', function()
  H.vault({})
  local root = H.root .. '/missing/deeper'
  table.insert(extra_roots, root)
  local err, res = sync(root, 'status')
  H.eq(nil, err)
  H.eq({ root, 0 }, { res.root, res.files })
  H.eq(nil, uv.fs_stat(H.root .. '/missing'), 'daemon must not create the root')
  local e2, r2 = sync(root, 'resolve', { targets = { 'x' }, from = root .. '/a.md' })
  H.eq(nil, e2)
  H.eq({ '' }, r2)
end)

-- ---------------------------------------------------------------- protocol

test('concurrent requests are matched to their own ids', function()
  local root = H.vault({ ['a.md'] = '' })
  pid_of(root)
  local order, got = {}, {}
  -- slow first: replies arrive out of order
  client.request(root, 'tex', { src = string.rep('\\sqrt{', 40) .. 'x' .. string.rep('}', 40), size = 60 }, function(e, r)
    got.slow = e or r.png
    table.insert(order, 'slow')
  end)
  for i = 1, 40 do
    if i % 2 == 0 then
      client.request(root, 'texUnicode', { items = { '\\alpha_' .. i % 10, 'y' } }, function(e, r)
        got[i] = e or (r[1].text .. '|' .. r[2].text)
        table.insert(order, i)
      end)
    else
      client.request(root, 'frontmatter', { path = root .. '/a.md' }, function(e, r)
        got[i] = e or vim.inspect(r)
        table.insert(order, i)
      end)
    end
  end
  H.wait(15000, function()
    return #order == 41
  end, 'all replies')
  for i = 2, 40, 2 do
    H.eq('α' .. ({ '₀', '₁', '₂', '₃', '₄', '₅', '₆', '₇', '₈', '₉' })[i % 10 + 1] .. '|y', got[i], 'reply ' .. i)
  end
  local first_fm = got[1]
  for i = 1, 39, 2 do
    H.eq(first_fm, got[i], 'frontmatter reply ' .. i)
  end
  H.ok(type(got.slow) == 'string' and got.slow:match('%.png$'), 'slow tex: ' .. tostring(got.slow))
  H.ok(order[1] ~= 'slow', 'fast replies must not wait for the slow one')
end)

test('malformed lines get an error without an id; the connection survives', function()
  local root = H.vault({})
  pid_of(root)
  local c = raw(root)
  c.send('this is not json\n')
  c.send('\n   \n')
  c.send('{"id":"str-id","method":"ping","params":{}}\n')
  c.send('{"method":"ping"}\n') -- notification: no reply
  c.send('{"id":7,"method":"nope","params":{}}\n')
  c.send('{"id":8,"method":"tex","params":null}\n')
  c.send('{"id":9,"method":"texUnicode","params":{"items":"notalist"}}\n')
  H.wait(3000, function()
    return #c.lines >= 5
  end, 'replies: ' .. vim.inspect(c.lines))
  vim.wait(200)
  local msgs = vim.tbl_map(vim.json.decode, c.lines)
  H.eq(5, #msgs, vim.inspect(c.lines))
  H.eq(nil, msgs[1].id)
  H.contains(msgs[1].error, 'bad request')
  local by = {}
  for _, m in ipairs(msgs) do
    if m.id then
      by[tostring(m.id)] = m
    end
  end
  H.eq(root, by['str-id'].result.root)
  H.eq('unknown method "nope"', by['7'].error)
  H.ok(by['8'].error or by['8'].result, 'tex with null params gets a reply')
  H.ok(by['9'].error, 'texUnicode with bad items is an error')
  H.ok(not c.closed, 'connection must stay open')
  c.close()
end)

test('a reply split across reads and a huge request line', function()
  local root = H.vault({})
  pid_of(root)
  local c = raw(root)
  local msg = vim.json.encode({ id = 1, method = 'texUnicode', params = { items = { string.rep('x', 300000) } } }) .. '\n'
  -- dribble the request in pieces
  for i = 1, #msg, 65536 do
    c.send(msg:sub(i, i + 65535))
  end
  H.wait(5000, function()
    return #c.lines == 1
  end, 'reply')
  local m = vim.json.decode(c.lines[1])
  H.eq(1, m.id)
  H.ok(m.result[1].error and m.result[1].error:find('too long', 1, true), 'oversized formula is an error: ' .. vim.inspect(m.result[1].error))
  c.close()
  -- the editor's client reassembles a reply spread over many reads
  local err, res = sync(root, 'texUnicode', { items = { string.rep('y', 300000), 'z' } })
  H.eq(nil, err)
  H.eq(300000, #res[1].text)
  H.eq('z', res[2].text)
end)

test('[BUG fixed] touch of paths outside the root (or the root itself) is ignored', function()
  local root = H.vault({ ['a.md'] = '' })
  local sibling = root .. '2'
  vim.fn.mkdir(sibling, 'p')
  vim.fn.writefile({ '# s' }, sibling .. '/s.md')
  local _, before = sync(root, 'status')
  local err, res = sync(root, 'touch', { paths = { '/etc/passwd', root .. '/../x.md', sibling .. '/s.md', 'relative.md', '', root } })
  H.eq(nil, err)
  H.eq(true, res)
  local _, after = sync(root, 'status')
  H.eq(before.files, after.files)
  H.eq(before.pid, after.pid)
  local fe = sync(root, 'frontmatter', { path = sibling .. '/s.md' })
  H.contains(fe, 'is outside')
  -- contrast: a new file inside the root is picked up by touch
  H.write('new.md', '---\nk: 1\n---\n')
  sync(root, 'touch', { paths = { root .. '/new.md' } })
  local _, later = sync(root, 'status')
  H.eq(before.files + 1, later.files)
end)

test('touch of a deleted file drops it from the index', function()
  local root = H.vault({ ['a.md'] = '', ['b.md'] = '' })
  local _, before = sync(root, 'status')
  os.remove(root .. '/b.md')
  sync(root, 'touch', { paths = { root .. '/b.md' } })
  local _, after = sync(root, 'status')
  H.eq(before.files - 1, after.files)
  local err, fm = sync(root, 'frontmatter', { path = root .. '/b.md' })
  H.eq(nil, err)
  H.eq(nil, fm)
end)

-- ---------------------------------------------------------------- tex

local function tex(root, src, extra)
  local t0 = uv.hrtime()
  local err, res = sync(root, 'tex', vim.tbl_extend('force', { src = src, display = true, size = 20 }, extra or {}), 20000)
  return err, res, (uv.hrtime() - t0) / 1e6
end

test('tex: broken input gives precise errors', function()
  local root = H.vault({})
  local cases = {
    ['\\frac'] = 'missing argument',
    ['\\frac{a}'] = 'missing argument',
    ['x^'] = 'missing argument',
    ['{'] = 'missing }',
    ['}'] = 'unexpected }',
    ['\\left( x'] = 'missing \\right',
    ['\\begin{pmatrix} a'] = 'missing \\end{pmatrix}',
    ['\\sqrt[3'] = 'missing argument',
  }
  for src, want in pairs(cases) do
    local err = tex(root, src)
    H.eq(want, err, src)
  end
end)

test('tex: unknown commands and odd input still render', function()
  local root = H.vault({})
  for _, src in ipairs({ '\\unknowncmd{x}', '\\', '&&', '\\frac{}{}', 'x\0y', '\\text{<b>&amp;"</b>}', '' }) do
    local err, res = tex(root, src)
    H.ok(err or (res and res.png and uv.fs_stat(res.png)), ('%q: neither error nor png'):format(src))
  end
  H.eq({ pid_of(root) }, daemons(root))
end)

test('tex: pathological input is bounded and never kills the daemon', function()
  local root = H.vault({})
  local pid = pid_of(root)
  local cases = {
    deep_frac = string.rep('\\frac{', 2000) .. 'x' .. string.rep('}{y}', 2000),
    deep_sqrt = string.rep('\\sqrt{', 3000) .. 'x' .. string.rep('}', 3000),
    deep_scripts = string.rep('x^{', 3000) .. 'y' .. string.rep('}', 3000),
    deep_left = string.rep('\\left(', 2000) .. 'x' .. string.rep('\\right)', 2000),
    deep_opt = string.rep('\\sqrt[', 200) .. 'x' .. string.rep(']{y}', 200),
    open_braces = string.rep('{', 100000),
    long_sum = string.rep('a+', 9000),
    huge = string.rep('x', 300000),
    over_chain = string.rep('a \\over ', 500) .. 'b',
  }
  for name, src in pairs(cases) do
    local err, res, ms = tex(root, src)
    H.ok(err or (res and res.png), name .. ': no answer')
    H.ok(ms < 3000, ('%s took %dms'):format(name, ms))
  end
  local err, _, ms = tex(root, 'x', { size = 1e9 })
  H.ok(ms < 3000, 'huge size took ' .. ms .. 'ms: ' .. tostring(err))
  local e2, r2 = tex(root, string.rep('x', 9000))
  H.ok(e2 and e2:find('too large', 1, true), 'over-wide formula is an error, got ' .. vim.inspect({ e2, r2 }))
  H.eq(pid, pid_of(root), 'same daemon after all of it')
end)

test('tex: same formula rendered concurrently by many requests', function()
  local root = H.vault({})
  pid_of(root)
  local res = {}
  for i = 1, 12 do
    client.request(root, 'tex', { src = '\\int_0^1 x^2\\,dx', display = true, size = 20 }, function(e, r)
      res[i] = e or r.png
    end)
  end
  H.wait(10000, function()
    return #vim.tbl_keys(res) == 12
  end, 'all renders')
  for i = 2, 12 do
    H.eq(res[1], res[i], 'render ' .. i)
  end
  H.ok(res[1]:match('%.png$') and uv.fs_stat(res[1]).size > 0, 'png: ' .. res[1])
  local leftovers = vim.fn.glob(vim.fs.dirname(res[1]) .. '/*.tmp*', false, true)
  H.eq({}, leftovers)
end)

test('texUnicode: errors are flagged, approximations are not errors', function()
  local root = H.vault({})
  local err, res = sync(root, 'texUnicode', { items = { '\\frac{a}', '\\frac{a+b}{c}', '', '\\pi' } })
  H.eq(nil, err)
  H.eq('missing argument', res[1].error)
  H.eq(nil, res[2].error)
  H.eq(false, res[2].exact)
  H.eq(nil, res[3].error)
  H.eq({ 'π', true }, { res[4].text, res[4].exact })
end)

return T
