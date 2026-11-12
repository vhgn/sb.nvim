-- Terminal-matched colors for rendered images (math, mermaid). Prefers the
-- colorscheme's terminal palette (g:terminal_color_N), then highlight groups.
local M = {}

local function hex(n)
  return n and string.format('#%06x', n) or nil
end

local function hl(name, attr)
  local ok, h = pcall(vim.api.nvim_get_hl, 0, { name = name, link = false })
  if ok and h then
    return hex(h[attr])
  end
end

local function rgb(c)
  c = c:gsub('#', '')
  return tonumber(c:sub(1, 2), 16), tonumber(c:sub(3, 4), 16), tonumber(c:sub(5, 6), 16)
end

--- Mixes a toward b by t (0..1).
function M.blend(a, b, t)
  local ar, ag, ab = rgb(a)
  local br, bg, bb = rgb(b)
  local function mix(x, y)
    return math.floor(x + (y - x) * t + 0.5)
  end
  return string.format('#%02x%02x%02x', mix(ar, br), mix(ag, bg), mix(ab, bb))
end

local function term(n)
  local c = vim.g['terminal_color_' .. n]
  if type(c) == 'string' and c:match('^#%x%x%x%x%x%x$') then
    return c:lower()
  end
end

---@return table palette fg, bg, and named ANSI colors
function M.palette()
  local dark = vim.o.background == 'dark'
  local fg = hl('Normal', 'fg') or term(15) or term(7) or (dark and '#d0d0d0' or '#303030')
  local bg = hl('Normal', 'bg') or term(0) or (dark and '#1c1c1c' or '#fafafa')
  local function pick(n, group, attr, fallback)
    return term(n) or hl(group, attr) or fallback
  end
  local p = {
    fg = fg,
    bg = bg,
    -- Normal bg may be unset (transparent terminals): images then stay transparent.
    transparent = hl('Normal', 'bg') == nil,
    black = pick(0, 'NonText', 'fg', '#555555'),
    red = pick(1, 'DiagnosticError', 'fg', '#e06c75'),
    green = pick(2, 'String', 'fg', '#98c379'),
    yellow = pick(3, 'DiagnosticWarn', 'fg', '#e5c07b'),
    blue = pick(4, 'Function', 'fg', '#61afef'),
    magenta = pick(5, 'Keyword', 'fg', '#c678dd'),
    cyan = pick(6, 'Special', 'fg', '#56b6c2'),
    white = pick(7, 'Normal', 'fg', '#cccccc'),
    comment = hl('Comment', 'fg') or M.blend(fg, bg, 0.45),
  }
  p.purple, p.gray, p.grey, p.orange = p.magenta, p.comment, p.comment, hl('Number', 'fg') or p.yellow
  return p
end

--- Mermaid theme derived from the palette.
function M.mermaid(p)
  p = p or M.palette()
  local surface = M.blend(p.bg, p.blue, 0.22)
  return {
    theme = 'base',
    themeVariables = {
      darkMode = vim.o.background == 'dark',
      background = p.transparent and 'transparent' or p.bg,
      fontFamily = 'monospace',
      primaryColor = surface,
      primaryTextColor = p.fg,
      primaryBorderColor = p.blue,
      secondaryColor = M.blend(p.bg, p.magenta, 0.22),
      secondaryTextColor = p.fg,
      secondaryBorderColor = p.magenta,
      tertiaryColor = M.blend(p.bg, p.cyan, 0.18),
      tertiaryTextColor = p.fg,
      tertiaryBorderColor = p.cyan,
      lineColor = M.blend(p.fg, p.bg, 0.3),
      textColor = p.fg,
      mainBkg = surface,
      nodeBorder = p.blue,
      clusterBkg = M.blend(p.bg, p.magenta, 0.12),
      clusterBorder = p.magenta,
      titleColor = p.fg,
      edgeLabelBackground = p.transparent and M.blend(p.bg, p.fg, 0.05) or p.bg,
      noteBkgColor = M.blend(p.bg, p.yellow, 0.25),
      noteTextColor = p.fg,
      noteBorderColor = p.yellow,
      actorBkg = surface,
      actorBorder = p.blue,
      actorTextColor = p.fg,
      actorLineColor = p.comment,
      signalColor = p.fg,
      signalTextColor = p.fg,
      labelBoxBkgColor = surface,
      labelTextColor = p.fg,
      loopTextColor = p.fg,
      activationBkgColor = M.blend(p.bg, p.cyan, 0.3),
      sequenceNumberColor = p.bg,
      sectionBkgColor = M.blend(p.bg, p.blue, 0.15),
      altSectionBkgColor = M.blend(p.bg, p.magenta, 0.1),
      taskBkgColor = M.blend(p.bg, p.blue, 0.4),
      taskTextColor = p.fg,
      taskBorderColor = p.blue,
      doneTaskBkgColor = M.blend(p.bg, p.green, 0.35),
      critBkgColor = M.blend(p.bg, p.red, 0.4),
      todayLineColor = p.red,
      pie1 = p.blue, pie2 = p.magenta, pie3 = p.cyan, pie4 = p.green, pie5 = p.yellow, pie6 = p.red,
      pieTitleTextColor = p.fg,
      pieSectionTextColor = p.bg,
      pieLegendTextColor = p.fg,
      pieStrokeColor = p.bg,
      git0 = p.blue, git1 = p.magenta, git2 = p.cyan, git3 = p.green, git4 = p.yellow, git5 = p.red,
      errorBkgColor = p.red,
      errorTextColor = p.fg,
    },
  }
end

return M
