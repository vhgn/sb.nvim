if vim.g.loaded_brain then
  return
end
vim.g.loaded_brain = true

-- Obsidian Bases: YAML with their own filetype so they can be rendered.
vim.filetype.add({ extension = { base = 'base' } })
pcall(vim.treesitter.language.register, 'yaml', 'base')
vim.api.nvim_create_autocmd('FileType', {
  pattern = 'base',
  callback = function(ev)
    vim.bo[ev.buf].commentstring = '# %s'
    vim.bo[ev.buf].expandtab = true
    vim.bo[ev.buf].shiftwidth = 2
    vim.bo[ev.buf].tabstop = 2
    if not pcall(vim.treesitter.start, ev.buf, 'yaml') then
      vim.bo[ev.buf].syntax = 'yaml'
    end
  end,
})

local function brain()
  return require('brain')
end

vim.api.nvim_create_user_command('BrainRebuild', function()
  brain().rebuild()
end, { desc = 'Rebuild brain and restart the LSP and daemon' })
vim.api.nvim_create_user_command('BrainStatus', function()
  brain().status()
end, { desc = 'Show the brain daemon status' })
vim.api.nvim_create_user_command('BrainRefresh', function()
  local buf = vim.api.nvim_get_current_buf()
  require('brain.base').refresh(buf)
  require('brain.media').clear(buf)
  require('brain.media').refresh(buf)
end, { desc = 'Re-render bases, images and math in this buffer' })
