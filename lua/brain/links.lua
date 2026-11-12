-- Dims links to notes that don't exist: [[Missing]] and [x](missing.md) keep
-- their usual link color, mixed halfway toward the background.
local client = require('brain.client')
local colors = require('brain.colors')
local scan = require('brain.scan')

local M = {}

local ns = vim.api.nvim_create_namespace('brain.links')
M.ns = ns

---@type table<integer, integer> buf -> request sequence
local seq = {}

local function fg(name)
  local ok, h = pcall(vim.api.nvim_get_hl, 0, { name = name, link = false })
  return ok and h and h.fg and string.format('#%06x', h.fg) or nil
end

function M.define_highlights()
  local normal = vim.api.nvim_get_hl(0, { name = 'Normal', link = false })
  local bg = normal.bg and string.format('#%06x', normal.bg) or (vim.o.background == 'light' and '#ffffff' or '#000000')
  local c = fg('@markup.link.label') or fg('Underlined') or fg('Normal')
  if c then
    vim.api.nvim_set_hl(0, 'BrainLinkMissing', { fg = colors.blend(c, bg, 0.5), default = true })
  else
    vim.api.nvim_set_hl(0, 'BrainLinkMissing', { link = 'Comment', default = true })
  end
end

--- Rescans buf and dims the links whose target doesn't resolve.
function M.refresh(buf)
  if not vim.api.nvim_buf_is_valid(buf) then
    return
  end
  local root = vim.b[buf].brain_root
  if not root then
    return
  end
  local links = scan.links(vim.api.nvim_buf_get_lines(buf, 0, -1, false))
  seq[buf] = (seq[buf] or 0) + 1
  local my = seq[buf]
  if #links == 0 then
    vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
    return
  end
  local targets = {}
  for i, l in ipairs(links) do
    targets[i] = l.target
  end
  local from = vim.api.nvim_buf_get_name(buf)
  local tick = vim.api.nvim_buf_get_changedtick(buf)
  client.request(root, 'resolve', { targets = targets, from = from }, function(err, res)
    if err or type(res) ~= 'table' or my ~= seq[buf] or not vim.api.nvim_buf_is_valid(buf) then
      return
    end
    if vim.api.nvim_buf_get_changedtick(buf) ~= tick then
      return -- a newer refresh is on its way
    end
    vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
    local dir = vim.fs.dirname(from)
    for i, l in ipairs(links) do
      local p = res[i]
      local missing = p == nil or p == '' or p == vim.NIL
      if missing and not l.wiki then
        -- not indexed yet but on disk
        local f = vim.fs.normalize(dir .. '/' .. l.target)
        missing = not (vim.uv.fs_stat(f) or vim.uv.fs_stat(f .. '.md'))
      end
      if missing then
        vim.api.nvim_buf_set_extmark(buf, ns, l.line, l.col, {
          end_col = l.end_col,
          hl_group = 'BrainLinkMissing',
          priority = 250, -- above treesitter and semantic tokens
          invalidate = true,
          undo_restore = false,
        })
      end
    end
  end)
end

--- Redraws every visible buffer in root (a note was created or removed).
function M.refresh_root(root)
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.b[b].brain_root == root and vim.bo[b].filetype == 'markdown' and vim.fn.bufwinid(b) ~= -1 then
      M.refresh(b)
    end
  end
end

function M.detach(buf)
  seq[buf] = nil
  if vim.api.nvim_buf_is_valid(buf) then
    vim.api.nvim_buf_clear_namespace(buf, ns, 0, -1)
  end
end

return M
