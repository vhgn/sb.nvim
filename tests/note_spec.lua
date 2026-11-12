-- Rendering note embeds (![[Note]]) inside notes.
local H = require('helpers')
local T = {}

--- Virtual lines of note embeds in buf, as plain strings.
local function lines(buf)
  local out = {}
  local marks = vim.api.nvim_buf_get_extmarks(buf, require('brain.note').ns, 0, -1, { details = true })
  for _, m in ipairs(marks) do
    for _, vl in ipairs(m[4].virt_lines or {}) do
      local s = {}
      for _, ch in ipairs(vl) do
        s[#s + 1] = ch[1]
      end
      out[#out + 1] = (table.concat(s):gsub('^│ (.-)%s*│$', '%1'))
    end
  end
  return out
end

local function wait_text(buf, needle)
  H.wait(5000, function()
    return table.concat(lines(buf), '\n'):find(needle, 1, true) ~= nil
  end, 'note embed to show ' .. needle)
  return lines(buf)
end

T['a note embed shows the note in a frame without a title'] = function()
  H.vault({
    ['Road map.md'] = '---\ntags: [x]\n---\n# Road map\n\n- [[A]] — first\n',
    ['index.md'] = '![[Road map]]\ntail\n',
  })
  H.setup()
  local buf = H.open('index.md')
  local l = wait_text(buf, 'first')
  H.eq('╭╮', (l[1]:gsub('─', '')), 'top of the box has no title: ' .. l[1])
  H.eq('# Road map', l[2])
  H.eq('', l[3])
  H.eq('- [[A]] — first', l[4])
  H.eq('╰╯', (l[#l]:gsub('─', '')), 'bottom of the box: ' .. l[#l])
  H.not_contains(table.concat(l, '\n'), 'tags:', 'front matter is hidden')
  local marks_d = vim.api.nvim_buf_get_extmarks(buf, require('brain.note').ns, 0, -1, { details = true })
  local widths = {}
  for _, vl in ipairs(marks_d[1][4].virt_lines) do
    local w = 0
    for _, ch in ipairs(vl) do
      w = w + vim.fn.strdisplaywidth(ch[1])
    end
    widths[w] = true
  end
  H.eq(1, vim.tbl_count(widths), 'every line of the box is as wide: ' .. vim.inspect(widths))
  local marks = vim.api.nvim_buf_get_extmarks(buf, require('brain.note').ns, 0, -1, {})
  H.eq(0, marks[1][2], 'anchored on the embed line')
end

T['a heading anchor embeds only that section'] = function()
  H.vault({
    ['n.md'] = '# Top\nintro\n## Keep\nkept\n### Sub\nsub text\n## Next\ndropped\n',
    ['index.md'] = '![[n#Keep]]\n',
  })
  H.setup()
  local buf = H.open('index.md')
  local l = wait_text(buf, 'kept')
  H.eq('╭╮', (l[1]:gsub('─', '')), 'top of the box has no title: ' .. l[1])
  local text = table.concat(l, '\n')
  H.contains(text, 'sub text')
  H.not_contains(text, 'intro')
  H.not_contains(text, 'dropped')
end

T['a block anchor embeds its paragraph without the id'] = function()
  H.vault({
    ['n.md'] = 'other\n\nline one\nline two ^abc\n\nafter\n',
    ['index.md'] = '![[n#^abc]]\n',
  })
  H.setup()
  local buf = H.open('index.md')
  local l = wait_text(buf, 'line one')
  local text = table.concat(vim.list_slice(l, 2), '\n') -- the body, without the title
  H.contains(text, 'line two')
  H.not_contains(text, '^abc')
  H.not_contains(text, 'other')
  H.not_contains(text, 'after')
end

T['missing notes and self embeds say so; images and bases are not notes'] = function()
  H.vault({
    ['index.md'] = '![[nope]]\n![[index]]\n![[pic.png]]\n',
  })
  H.setup()
  local buf = H.open('index.md')
  local l = wait_text(buf, 'note not found: nope')
  H.contains(table.concat(l, '\n'), 'cannot embed itself')
  H.eq(2, #vim.api.nvim_buf_get_extmarks(buf, require('brain.note').ns, 0, -1, {}))
end

T['long lines wrap to the window and saving the embedded note re-renders'] = function()
  H.vault({
    ['n.md'] = '- ' .. string.rep('word ', 40) .. '\n',
    ['index.md'] = '![[n]]\n',
  })
  H.setup()
  local buf = H.open('index.md')
  local l = wait_text(buf, 'word')
  H.ok(#l > 4, 'wrapped into several lines')
  for _, s in ipairs(l) do
    H.ok(vim.fn.strdisplaywidth(s) <= 100, 'fits the window: ' .. s)
  end
  H.eq('  word', l[3]:sub(1, 6), 'continuations are indented under the bullet')
  vim.cmd.edit(vim.fn.fnameescape(H.root .. '/n.md'))
  vim.api.nvim_buf_set_lines(0, 0, -1, false, { 'changed text' })
  vim.cmd.write()
  vim.cmd.buffer(buf)
  wait_text(buf, 'changed text')
end

T['gf on a note embed opens the note at its heading'] = function()
  H.vault({
    ['n.md'] = '# A\none\n## B\ntwo\n',
    ['index.md'] = 'x ![[n#B]]\n',
  })
  H.setup()
  H.open('index.md')
  vim.api.nvim_win_set_cursor(0, { 1, 4 })
  require('brain').follow()
  H.eq(H.root .. '/n.md', vim.api.nvim_buf_get_name(0))
  H.eq(3, vim.api.nvim_win_get_cursor(0)[1])
end

T['embedded text wraps between words at 80 columns'] = function()
  local words = {}
  for k = 1, 60 do
    words[k] = 'word' .. k
  end
  local para = table.concat(words, ' ')
  H.vault({ ['long.md'] = para .. '\n', ['index.md'] = '![[long]]\n' })
  H.setup()
  vim.o.columns = 200
  local buf = H.open('index.md')
  local l = wait_text(buf, 'word60')
  local body = vim.list_slice(l, 2, #l - 1)
  H.ok(#body > 1, 'wrapped: ' .. vim.inspect(body))
  for _, b in ipairs(body) do
    H.ok(vim.fn.strdisplaywidth(b) <= 80, 'at most 80 columns: ' .. b)
  end
  H.eq(para, table.concat(body, ' '), 'no word is split')
  H.ok(vim.fn.strdisplaywidth(l[1]) <= 84, 'box fits the text: ' .. vim.fn.strdisplaywidth(l[1]))
end

return T
