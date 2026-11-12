-- Dimming links to notes that don't exist.
local H = require('helpers')
local T = {}

vim.opt.rtp:prepend(H.plugin)

T['scan: finds note links, skipping embeds, code, URLs and images'] = function()
  local got = require('brain.scan').links({
    '---',
    'title: "[[Front]]"',
    '---',
    'See [[A]], [[b/C#h|alias]] and [x](d.md#top), [y](e%20f).',
    '![[Embed]] ![i](p.png) [w](https://x.org) [i2](pic.png) `[[Code]]`',
    '```',
    '[[Fenced]]',
    '```',
  })
  local short = {}
  for _, l in ipairs(got) do
    short[#short + 1] = string.format('%d:%d-%d %s %s', l.line, l.col, l.end_col, l.target, l.wiki and 'wiki' or 'md')
  end
  H.eq({
    '3:4-9 A wiki',
    '3:11-26 b/C wiki',
    '3:31-44 d.md md',
    '3:46-56 e f md',
  }, short)
end

--- Spans of BrainLinkMissing extmarks in buf, as the text they cover.
local function dimmed(buf)
  local out = {}
  for _, m in ipairs(vim.api.nvim_buf_get_extmarks(buf, require('brain.links').ns, 0, -1, { details = true })) do
    if m[4].hl_group == 'BrainLinkMissing' then
      local line = vim.api.nvim_buf_get_lines(buf, m[2], m[2] + 1, false)[1]
      out[#out + 1] = line:sub(m[3] + 1, m[4].end_col)
    end
  end
  table.sort(out)
  return out
end

T['links to missing notes are dimmed, existing ones are not'] = function()
  H.vault({
    ['A.md'] = '# A\n',
    ['sub/B.md'] = '# B\n',
    ['index.md'] = '[[A]] [[Nope]] [[B#x]] [ok](sub/B.md) [bad](sub/zzz.md)\n',
  })
  H.setup()
  local buf = H.open('index.md')
  H.wait(5000, function()
    return #dimmed(buf) > 0
  end, 'dimmed links')
  H.eq({ '[[Nope]]', '[bad](sub/zzz.md)' }, dimmed(buf))
  local hl = vim.api.nvim_get_hl(0, { name = 'BrainLinkMissing', link = false })
  H.ok(hl.fg or hl.link, 'BrainLinkMissing is defined')
end

T['creating the missing note undims its links'] = function()
  H.vault({ ['index.md'] = '[[Later]]\n' })
  H.setup()
  local buf = H.open('index.md')
  H.wait(5000, function()
    return #dimmed(buf) == 1
  end, 'link dimmed')
  vim.cmd.split(vim.fn.fnameescape(H.root .. '/Later.md'))
  vim.api.nvim_buf_set_lines(0, 0, -1, false, { '# Later' })
  vim.cmd.write()
  H.wait(5000, function()
    return #dimmed(buf) == 0
  end, 'link undimmed after the note was created')
end

return T
