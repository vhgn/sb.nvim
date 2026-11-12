local M = {}

---@class brain.Config
M.defaults = {
  -- Vault root: a path, or a function(bufnr) -> path. Default: nearest
  -- directory with .obsidian/.brain/.git, else the current directory.
  root = nil,
  root_markers = { '.obsidian', '.brain', '.git' },
  -- Directory holding the brain Go sources (defaults to the plugin repo root).
  -- The binary is built into <source>/bin/brain.
  source = nil,
  -- Use this binary instead of building one.
  bin = nil,
  -- Rebuild the binary when any .go file is newer than it.
  auto_build = true,
  lsp = {
    enabled = true,
    init_options = {
      completion = { wikiLinks = true, atLinks = true },
      hoverLines = 40,
    },
  },
  daemon = {
    idle = '30m', -- the shared daemon exits after this long without editors
  },
  base = {
    enabled = true,
    -- In .base buffers, render the result below the YAML.
    inline_preview = true,
    max_height = 40, -- rows of an embed; longer results are cut (navigate to see more)
    debounce = 250,
  },
  images = {
    enabled = true, -- needs image.nvim
    max_height = 20, -- in terminal rows
  },
  mermaid = {
    enabled = true,
    cmd = 'mmdc', -- @mermaid-js/mermaid-cli; not installed automatically
    scale = 2,
  },
  math = {
    enabled = true,
    -- Display math ($$…$$, \[…\], ```math) becomes an image; inline $…$ is concealed as Unicode.
    inline = true,
    size = nil, -- font size in px; default: terminal cell height
  },
  keys = {
    enter = '<CR>', -- on an embed line: step into the rendered view
    next_view = '<Tab>',
    prev_view = '<S-Tab>',
    refresh = 'R',
    close = 'q',
  },
}

M.options = vim.deepcopy(M.defaults)

function M.set(opts)
  M.options = vim.tbl_deep_extend('force', vim.deepcopy(M.defaults), opts or {})
  return M.options
end

return M
