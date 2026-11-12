# sb.nvim

Second brain in Neovim: a markdown-notes language server (links, backlinks,
hover, completion), Obsidian Bases, and inline images, mermaid and math.

Requires Neovim 0.12 and Go (the server is built into `bin/brain` on first
start). Optional: [image.nvim](https://github.com/3rd/image.nvim), `mmdc`,
`rsvg-convert`.

## Install

```lua
vim.pack.add({ 'https://github.com/vhgn/sb.nvim' })
require('brain').setup()
```

Commands: `:BrainStatus`, `:BrainRefresh`, `:BrainRebuild`.
