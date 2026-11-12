-- brain.nvim: the brain language server, a shared per-vault frontmatter
-- daemon, Obsidian Bases (.base) rendering, and images/mermaid/math in notes.
local M = {}

local config = require('brain.config')

local group = vim.api.nvim_create_augroup('brain', { clear = true })
M.did_setup = false

--- Vault root for a buffer.
function M.root_for(buf)
  local cfg = config.options
  local root = cfg.root
  if type(root) == 'function' then
    root = root(buf)
  end
  if not root then
    local name = vim.api.nvim_buf_get_name(buf)
    local start = name ~= '' and vim.fs.dirname(name) or vim.fn.getcwd()
    local found = vim.fs.root(start, cfg.root_markers)
    root = found or vim.fn.getcwd()
  end
  return require('brain.client').normalize(root)
end

local function in_root(path, root)
  return path:sub(1, #root + 1) == root .. '/'
end

local function debounce(ms, fn)
  local timers = {}
  return function(buf, ...)
    local args = { ... }
    local t = timers[buf]
    if t then
      t:stop()
    else
      t = vim.uv.new_timer()
      timers[buf] = t
    end
    t:start(ms, 0, function()
      t:stop()
      vim.schedule(function()
        fn(buf, unpack(args))
      end)
    end)
  end
end

local function setup_lsp(bin, cfg)
  if not cfg.lsp.enabled then
    return
  end
  vim.lsp.config('brain', {
    cmd = { bin },
    filetypes = { 'markdown' },
    root_dir = function(buf, on_dir)
      on_dir(M.root_for(buf))
    end,
    init_options = cfg.lsp.init_options,
  })
  vim.lsp.enable('brain')
end

local function attach(buf)
  local cfg = config.options
  if vim.b[buf].brain_attached then
    return
  end
  local name = vim.api.nvim_buf_get_name(buf)
  if name == '' or vim.bo[buf].buftype ~= '' then
    return
  end
  vim.b[buf].brain_attached = true
  vim.b[buf].brain_root = M.root_for(buf)
  require('brain.client').ensure(vim.b[buf].brain_root)
  local base = require('brain.base')
  if cfg.base.enabled then
    base.attach_keys(buf)
    base.refresh(buf)
  end
  if vim.bo[buf].filetype == 'markdown' then
    if cfg.math.enabled and cfg.math.inline then
      for _, win in ipairs(vim.fn.win_findbuf(buf)) do
        if vim.wo[win].conceallevel == 0 then
          vim.wo[win][0].conceallevel = 2
        end
      end
    end
    require('brain.media').refresh(buf)
    require('brain.note').refresh(buf)
    vim.keymap.set('n', 'gf', function()
      M.follow()
    end, { buffer = buf, desc = 'brain: open embed/link under cursor' })
  end
end

--- gf on ![[x.base]] / ![[img.png]] / ![[Note]] opens the target; otherwise falls back to gf.
function M.follow()
  local buf = vim.api.nvim_get_current_buf()
  local line = vim.api.nvim_get_current_line()
  local col = vim.api.nvim_win_get_cursor(0)[2]
  for _, it in ipairs(require('brain.scan').scan({ line })) do
    if (it.kind == 'base' or it.kind == 'image' or it.kind == 'note') and it.target and col >= it.col and col < it.end_col then
      local err, res = require('brain.client').request_sync(vim.b[buf].brain_root, 'resolve', { targets = { it.target }, from = vim.api.nvim_buf_get_name(buf) }, 2000)
      local path = not err and res and res[1] ~= '' and res[1] or nil
      if path then
        if it.kind == 'note' then
          require('brain.note').open(path, it.anchor)
        else
          vim.cmd.edit(vim.fn.fnameescape(path))
        end
        return
      end
    end
  end
  vim.cmd('normal! gf')
end

function M.setup(opts)
  local cfg = config.set(opts)
  M.did_setup = true
  local build = require('brain.build')
  local client = require('brain.client')
  local bin = build.ensure(cfg)
  if not bin then
    return
  end
  M.bin = bin
  client.bin = bin
  client.idle = cfg.daemon.idle

  require('brain.base.render').define_highlights()
  vim.api.nvim_set_hl(0, 'BrainBaseText', { link = 'Normal', default = true })
  vim.api.nvim_set_hl(0, 'BrainMath', { link = '@markup.math', default = true })
  require('brain.note').define_highlights()

  setup_lsp(bin, cfg)

  local base = require('brain.base')
  local media = require('brain.media')
  local note = require('brain.note')
  local refresh_text = debounce(cfg.base.debounce, function(buf)
    if not vim.api.nvim_buf_is_valid(buf) then
      return
    end
    base.refresh(buf, { requery = vim.bo[buf].filetype == 'base' })
    if vim.bo[buf].filetype == 'markdown' then
      media.refresh(buf)
      note.refresh(buf)
    end
  end)

  vim.api.nvim_create_autocmd('FileType', {
    group = group,
    pattern = { 'markdown', 'base' },
    callback = function(ev)
      attach(ev.buf)
    end,
  })
  vim.api.nvim_create_autocmd({ 'TextChanged', 'TextChangedI' }, {
    group = group,
    callback = function(ev)
      if vim.b[ev.buf].brain_attached then
        refresh_text(ev.buf)
      end
    end,
  })
  vim.api.nvim_create_autocmd('BufWinEnter', {
    group = group,
    callback = function(ev)
      if vim.b[ev.buf].brain_attached then
        base.refresh(ev.buf)
        if vim.bo[ev.buf].filetype == 'markdown' then
          media.refresh(ev.buf)
          note.refresh(ev.buf)
        end
      end
    end,
  })
  vim.api.nvim_create_autocmd('BufWinLeave', {
    group = group,
    callback = function(ev)
      if vim.b[ev.buf].brain_attached then
        media.clear(ev.buf)
      end
    end,
  })
  vim.api.nvim_create_autocmd({ 'CursorMoved', 'CursorMovedI' }, {
    group = group,
    callback = function(ev)
      if vim.b[ev.buf].brain_attached then
        media.cursor_moved(ev.buf)
      end
    end,
  })
  vim.api.nvim_create_autocmd({ 'WinResized', 'VimResized' }, {
    group = group,
    callback = function()
      for _, buf in ipairs(vim.api.nvim_list_bufs()) do
        if vim.b[buf].brain_attached and vim.fn.bufwinid(buf) ~= -1 then
          base.redraw(buf)
          if vim.bo[buf].filetype == 'markdown' then
            note.refresh(buf)
          end
        end
      end
    end,
  })
  -- A note opened from an unresolved link may live in a directory that does
  -- not exist yet; create it on first save.
  vim.api.nvim_create_autocmd('BufWritePre', {
    group = group,
    pattern = { '*.md', '*.markdown' },
    callback = function(ev)
      if not vim.b[ev.buf].brain_attached then
        return
      end
      local dir = vim.fs.dirname(vim.fn.fnamemodify(vim.api.nvim_buf_get_name(ev.buf), ':p'))
      if dir and not vim.uv.fs_stat(dir) then
        vim.fn.mkdir(dir, 'p')
      end
    end,
  })
  -- Saving any file in a vault updates the daemon's index, then every visible base.
  vim.api.nvim_create_autocmd({ 'BufWritePost', 'BufDelete' }, {
    group = group,
    callback = function(ev)
      local path = vim.api.nvim_buf_get_name(ev.buf)
      if path == '' then
        return
      end
      path = vim.fs.normalize(vim.fn.fnamemodify(path, ':p'))
      local roots = {}
      for _, buf in ipairs(vim.api.nvim_list_bufs()) do
        local r = vim.b[buf].brain_root
        if r and in_root(path, r) then
          roots[r] = true
        end
      end
      for root in pairs(roots) do
        client.request(root, 'touch', { paths = { path } }, function()
          for _, buf in ipairs(vim.api.nvim_list_bufs()) do
            if vim.b[buf].brain_root == root and vim.fn.bufwinid(buf) ~= -1 then
              base.refresh(buf)
            end
          end
          -- the saved note may be embedded elsewhere
          note.refresh_root(root)
        end)
      end
    end,
  })
  vim.api.nvim_create_autocmd('ColorScheme', {
    group = group,
    callback = function()
      require('brain.base.render').define_highlights()
      note.define_highlights()
      for _, buf in ipairs(vim.api.nvim_list_bufs()) do
        if vim.b[buf].brain_attached and vim.bo[buf].filetype == 'markdown' then
          media.clear(buf)
          media.refresh(buf)
        end
      end
    end,
  })
  vim.api.nvim_create_autocmd('BufUnload', {
    group = group,
    callback = function(ev)
      if vim.b[ev.buf].brain_attached then
        base.detach(ev.buf)
        media.detach(ev.buf)
        note.detach(ev.buf)
      end
    end,
  })

  -- Attach to buffers that were opened before setup ran.
  for _, buf in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(buf) and (vim.bo[buf].filetype == 'markdown' or vim.bo[buf].filetype == 'base') then
      attach(buf)
    end
  end
end

--- Rebuilds the binary, restarts the LSP and the daemon(s).
function M.rebuild()
  local cfg = config.options
  local ok, err = require('brain.build').build(cfg)
  if not ok then
    vim.notify(err, vim.log.levels.ERROR)
    return
  end
  local client = require('brain.client')
  local roots = {}
  for _, buf in ipairs(vim.api.nvim_list_bufs()) do
    if vim.b[buf].brain_root then
      roots[vim.b[buf].brain_root] = true
    end
  end
  for root in pairs(roots) do
    client.request_sync(root, 'shutdown', {}, 1000)
  end
  client.close_all()
  -- the new binary may render math differently
  require('brain.media').reset_cache()
  for _, c in ipairs(vim.lsp.get_clients({ name = 'brain' })) do
    c:stop()
  end
  vim.defer_fn(function()
    for _, buf in ipairs(vim.api.nvim_list_bufs()) do
      if vim.b[buf].brain_attached then
        vim.b[buf].brain_attached = false
        if vim.api.nvim_buf_is_loaded(buf) then
          attach(buf)
          if vim.bo[buf].filetype == 'markdown' then
            vim.api.nvim_buf_call(buf, function()
              vim.cmd('doautocmd <nomodeline> FileType markdown')
            end)
          end
        end
      end
    end
    vim.notify('brain rebuilt and restarted')
  end, 300)
end

function M.status()
  local buf = vim.api.nvim_get_current_buf()
  local root = vim.b[buf].brain_root or M.root_for(buf)
  local err, res = require('brain.client').request_sync(root, 'status', {}, 3000)
  if err then
    vim.notify('brain daemon: ' .. err, vim.log.levels.ERROR)
  else
    vim.notify(('brain daemon pid %d · %d files · %d editors\nroot  %s\ncache %s\nbin   %s'):format(res.pid, res.files, res.clients, res.root, res.cache, M.bin or '?'))
  end
end

return M
