-- nvim --headless --clean -u NONE -l runner.lua <spec.lua> [test name]
-- Without a test name, prints the spec's test names (one per line).
-- With one, runs it and exits 0 on success, 1 on failure.
local spec_path, name = arg[1], arg[2]
local tests = dofile(spec_path)

if not name then
  local names = {}
  for k in pairs(tests) do
    names[#names + 1] = k
  end
  table.sort(names)
  io.stdout:write(table.concat(names, '\n') .. '\n')
  vim.cmd('qa!')
  return
end

local fn = tests[name]
if not fn then
  io.stderr:write('no such test: ' .. name .. '\n')
  vim.cmd('cq 1')
  return
end

local ok, err = xpcall(fn, debug.traceback)

-- stop this test's daemon
pcall(function()
  local H = require('helpers')
  if H.root then
    require('brain.client').request_sync(H.root, 'shutdown', {}, 500)
  end
end)

if ok then
  vim.cmd('qa!')
else
  io.stderr:write(tostring(err) .. '\n')
  vim.cmd('cq 1')
end
