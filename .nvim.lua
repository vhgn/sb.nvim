-- Project-local config (loaded via 'exrc'): sb.nvim from this checkout, running the
-- LSP and frontmatter daemon built from this checkout.
local root = vim.fn.getcwd()

-- Dependencies through vim.pack (installed into the 'core' pack dir on first start).
vim.pack.add({ 'https://github.com/3rd/image.nvim' }, { confirm = false })

-- sb.nvim itself is loaded straight from the working tree (vim.pack installs
-- committed snapshots, which would hide local edits).
vim.opt.rtp:prepend(root)
vim.cmd.runtime('plugin/brain.lua')

local ok, image = pcall(require, 'image')
if ok then
  image.setup({
    backend = 'kitty', -- ghostty/kitty graphics protocol (tmux needs allow-passthrough on)
    processor = 'magick_cli',
    integrations = {
      -- brain renders markdown images itself (incl. ![[wiki]] embeds, mermaid, math)
      markdown = { enabled = false },
    },
    max_height_window_percentage = 50,
    tmux_show_only_in_active_window = true,
    window_overlap_clear_enabled = true,
  })
end

require('brain').setup({
  root = root,
  -- source/bin default to this repo: bin/brain is rebuilt whenever a .go file is newer.
})
