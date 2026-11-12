-- Test harness loaded by the Go e2e tests inside `nvim --headless`.
-- It starts the server through Neovim's built-in LSP client and exposes small
-- helpers the per-test Lua bodies use.
local H = {}

local config

function H.setup(opts)
  config = {
    name = 'brain',
    cmd = { opts.bin },
    root_dir = opts.root,
    init_options = opts.init_options,
    flags = { debounce_text_changes = 0 },
  }
end

--- Edit a file relative to the workspace root and wait for the client to attach.
function H.open(rel)
  vim.cmd.edit(vim.fn.fnameescape(config.root_dir .. '/' .. rel))
  local bufnr = vim.api.nvim_get_current_buf()
  local id = assert(vim.lsp.start(config, { bufnr = bufnr }), 'vim.lsp.start failed')
  H.client = vim.lsp.get_client_by_id(id)
  assert(
    vim.wait(5000, function()
      return H.client.initialized and vim.lsp.buf_is_attached(bufnr, id)
    end, 10),
    'client did not attach'
  )
  return bufnr
end

--- 0-based line and character of the first plain-text match in the current buffer.
function H.find(needle)
  for i, line in ipairs(vim.api.nvim_buf_get_lines(0, 0, -1, false)) do
    local s = line:find(needle, 1, true)
    if s then
      return i - 1, s - 1
    end
  end
  error('not found in buffer: ' .. needle)
end

function H.params(line, character)
  return {
    textDocument = { uri = vim.uri_from_bufnr(0) },
    position = { line = line, character = character },
  }
end

function H.request(method, params)
  local res, err = H.client:request_sync(method, params, 5000, 0)
  if not res then
    error(method .. ' failed: ' .. tostring(err))
  end
  if res.err then
    error(method .. ' error: ' .. vim.inspect(res.err))
  end
  return res.result
end

--- Replace line `line` with `text`, put the cursor at its end and request completion there.
function H.complete_at_end_of(line, text)
  vim.api.nvim_buf_set_lines(0, line, line + 1, false, { text })
  local result = H.request('textDocument/completion', H.params(line, #text))
  return result.items or result
end

--- Apply a completion item's text edit and return the edited line.
function H.accept(item)
  vim.lsp.util.apply_text_edits({ item.textEdit }, vim.api.nvim_get_current_buf(), 'utf-16')
  return vim.api.nvim_buf_get_lines(0, item.textEdit.range.start.line, item.textEdit.range.start.line + 1, false)[1]
end

function H.find_item(items, label)
  for _, it in ipairs(items) do
    if it.label == label then
      return it
    end
  end
  error('no completion item ' .. label .. ' in ' .. vim.inspect(vim.tbl_map(function(i) return i.label end, items)))
end

function H.labels(items)
  local out = vim.tbl_map(function(i) return i.label end, items)
  table.sort(out)
  return out
end

--- Workspace-relative path for a URI.
function H.rel(uri)
  local p = vim.uri_to_fname(uri)
  return p:sub(#config.root_dir + 2)
end

return H
