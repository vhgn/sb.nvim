-- Edge cases of the Bases engine (.base YAML, expressions, filters, sorting,
-- grouping, summaries, `this`, links) checked through the daemon's `query`.
local H = require('helpers')
local T = {}

local function q(source, extra)
  return H.query(vim.tbl_extend('force', { source = source }, extra or {}))
end

--- Evaluates formulas against the single file only.md; returns its cells and the result.
local function eval(exprs, files)
  files = files or {}
  files['only.md'] = files['only.md'] or '---\nx: 1\n---\n'
  H.vault(files)
  H.setup()
  local src = { 'filters: file.path == "only.md"', 'formulas:' }
  local order = {}
  for i, e in ipairs(exprs) do
    src[#src + 1] = ('  f%d: |-\n    %s'):format(i, e)
    order[#order + 1] = '      - formula.f' .. i
  end
  src[#src + 1] = 'views:\n  - type: table\n    order:\n' .. table.concat(order, '\n')
  local res = q(table.concat(src, '\n') .. '\n')
  H.eq(1, #H.cells(res), 'only.md should be the single row: ' .. vim.inspect(res.errors))
  return H.cells(res)[1], res
end

local function errors(res)
  return table.concat(res.errors or {}, '\n')
end

--- { {key, {first cells...}} } per group.
local function groups(res)
  local out = {}
  for _, g in ipairs(res.groups) do
    local names = {}
    for _, r in ipairs(g.rows) do
      names[#names + 1] = r.cells[1].t
    end
    out[#out + 1] = { g.key, names }
  end
  return out
end

local function summaries(res)
  local out = {}
  for i, c in ipairs(res.summaries or {}) do
    out[i] = c.t or ''
  end
  return out
end

-- ---------------------------------------------------------------- YAML

T['yaml: a multi-line plain filter continues on indented lines and keeps later keys'] = function()
  H.vault({ ['a.md'] = '---\nprice: 5\n---\n', ['b.md'] = '---\nprice: 60\n---\n', ['c.md'] = '---\nprice: 0\n---\n' })
  H.setup()
  local res = q('filters: price > 1 &&\n  price < 50\nviews:\n  - type: table\n    name: Cont\n    order:\n      - file.name\n')
  H.eq(nil, res.errors)
  H.eq('Cont', res.name, 'views after the multi-line scalar must survive')
  H.eq({ { 'a.md' } }, H.cells(res))
end

T['yaml: a double-quoted string spanning lines folds into one expression'] = function()
  H.vault({ ['a.md'] = '---\nprice: 5\n---\n', ['b.md'] = '---\nprice: 60\n---\n' })
  H.setup()
  local res = q('filters: "price > 1 &&\n  price < 50"\nviews:\n  - type: list\n    name: Q\n')
  H.eq(nil, res.errors)
  H.eq('list', res.type)
  H.eq({ { 'a.md' } }, H.cells(res))
end

T['yaml: CRLF, a block-scalar formula and a flow list spanning lines'] = function()
  H.vault({ ['a.md'] = '---\nprice: 5\n---\n', ['b.md'] = '---\nprice: 60\n---\n' })
  H.setup()
  local src = table.concat({
    'filters: price',
    'formulas:',
    '  double: |',
    '    price *',
    '    2',
    'views:',
    '  - order: [file.name,',
    '      formula.double]',
    '    sort: [file.name]',
    '',
  }, '\r\n')
  local res = q(src)
  H.eq(nil, res.errors)
  H.eq({ 'file.name', 'formula.double' }, vim.tbl_map(function(c)
    return c.id
  end, res.columns))
  H.eq({ { 'a.md', '10' }, { 'b.md', '120' } }, H.cells(res))
end

T['yaml: # inside quotes is not a comment, # after a value is'] = function()
  H.vault({ ['x#y.md'] = 'body', ['z.md'] = 'body' })
  H.setup()
  local res = q([[
filters: 'file.name.contains("#")' # only x#y
formulas:
  t: "#" + file.basename # trailing comment
views:
  - order: [file.name, formula.t]
]])
  H.eq(nil, res.errors)
  H.eq({ { 'x#y.md', '#x#y' } }, H.cells(res))
end

T['yaml: a comment-only base lists every vault file except dot folders in a default Table'] = function()
  H.vault({
    ['a.md'] = 'x',
    ['pic.png'] = 'x',
    ['README'] = 'x',
    ['q.base'] = '# nothing',
    ['.obsidian/app.md'] = 'x',
    ['.hidden.md'] = 'x',
  })
  H.setup()
  local res = q('# nothing but a comment\n')
  H.eq(nil, res.errors)
  H.eq('Table', res.name)
  H.eq('table', res.type)
  H.eq({ { id = 'file.name', name = 'file name' } }, res.columns)
  H.eq({ { 'README' }, { 'a.md' }, { 'pic.png' }, { 'q.base' } }, H.cells(res))
end

T['yaml: views: [] and a null views key both fall back to one Table view'] = function()
  H.vault({ ['a.md'] = 'x', ['b.txt'] = 'x' })
  H.setup()
  for _, src in ipairs({ 'filters: file.ext == "md"\nviews: []\n', 'filters: file.ext == "md"\nviews:\n' }) do
    local res = q(src)
    H.eq(nil, res.errors, src)
    H.eq({ { name = 'Table', type = 'table' } }, res.views, src)
    H.eq({ { 'a.md' } }, H.cells(res), src)
  end
end

T['yaml: leading-zero numbers are decimal and +inf/nan stay strings'] = function()
  local row = eval({ 'zip + 1', 'neg.isType("string")', 'nan.isType("string")', 'ver', 'hex' }, {
    ['only.md'] = '---\nzip: 012\nneg: +inf\nnan: nan\nver: 1.10\nhex: 0x1F\n---\n',
  })
  H.eq({ '13', '☑', '☑', '1.1', '31' }, row)
end

T['yaml: quoted formula names with spaces are addressable as columns'] = function()
  H.vault({ ['a.md'] = '---\nprice: 2\n---\n' })
  H.setup()
  local res = q([[
formulas:
  "my f": price * 3
  'it''s': '"q" + price'
views:
  - order:
      - formula.my f
      - formula.it's
]])
  H.eq(nil, res.errors)
  H.eq({ { '6', 'q2' } }, H.cells(res))
end

-- ---------------------------------------------------------------- front matter & tags

T['front matter: a UTF-8 byte order mark does not hide the front matter'] = function()
  H.vault({ ['bom.md'] = '\239\187\191---\nprice: 7\ntags: [x]\n---\n', ['other.md'] = 'x' })
  H.setup()
  H.eq({ { 'bom.md' } }, H.cells(q('filters: price == 7 && file.hasTag("x")\n')))
end

T['front matter: an unclosed block is body text, so its #tags count and its keys do not'] = function()
  H.vault({ ['open.md'] = '---\nprice: 7\n#body\n' })
  H.setup()
  local res = q('views:\n  - order: [file.name, price, file.tags]\n')
  H.eq({ { 'open.md', '', '#body' } }, H.cells(res))
end

T['front matter: broken YAML leaves the note queryable without properties'] = function()
  H.vault({ ['bad.md'] = '---\ntitle: "unterminated\n---\n#after\n', ['ok.md'] = '---\nprice: 1\n---\n' })
  H.setup()
  local res = q('filters: file.ext == "md"\nviews:\n  - order: [file.name, title, file.tags, price]\n')
  H.eq({ { 'bad.md', '', '#after', '' }, { 'ok.md', '', '', '1' } }, H.cells(res))
end

T['front matter: a capitalised Tags key and nested tag matching in hasTag'] = function()
  local row = eval({
    'file.hasTag("proj")',
    'file.hasTag("PROJ/SUB")',
    'file.hasTag("pro")',
    'file.hasTag("proj/sub/deep")',
    'file.hasTag("#area")',
    'file.hasTag("inline")',
    'file.hasTag("nope", "inline")',
    'file.hasTag("Inline/De")',
  }, { ['only.md'] = '---\nTags: [Proj/Sub, "#Area"]\n---\ntext #Inline/Deep\n' })
  H.eq({ '☑', '☑', '☐', '☐', '☑', '☑', '☑', '☐' }, row)
end

T['front matter: property names are case-insensitive; spaces and hyphens via note[...]'] = function()
  local row = eval({
    'status',
    'note.STATUS',
    'file.hasProperty("status")',
    'note["my prop"]',
    'note["my-prop"]',
    'file.properties["my prop"]',
  }, { ['only.md'] = '---\nStatus: Done\nmy prop: spaced\nmy-prop: dashed\n---\n' })
  H.eq({ 'Done', 'Done', '☑', 'spaced', 'dashed', 'spaced' }, row)
end

T['tags: numeric, mid-word, code and fenced #tags are not tags'] = function()
  local row = eval({ 'file.tags.join(" ")' }, {
    ['only.md'] = 'text #ok #123 x#no (#paren) `#code` [[n#heading]] #a/b\n```\n#fenced\n```\n# Heading\n',
  })
  H.eq({ '#ok #paren #a/b' }, row)
end

-- ---------------------------------------------------------------- expressions

T['expr: operator precedence and associativity follow JavaScript'] = function()
  local row = eval({
    '1 + 2 * 3',
    '10 - 4 - 3',
    '64 / 4 / 2',
    '!false == true',
    '"a" + 1 + 2',
    '1 + 2 + "a"',
    '-7 % 3',
    '2 * -3',
    'true || false && false',
    'missing ?? 5',
    '1 < 2 == true',
    'x > 0 ? "pos" : "neg"',
  })
  H.eq({ '7', '3', '8', '☑', 'a12', '3a', '-1', '-6', '☑', '5', '☑', 'pos' }, row)
end

T['expr: regex literals versus division'] = function()
  local row = eval({
    'x / 2 / 3',
    '(x)/2',
    '"a/b/c".split(/\\//).length',
    '[x].map(value / 4)',
    '"AbA".replace(/a/gi, "x")',
    '/a/.matches("cat")',
    '!/z/.matches("cat")',
  }, { ['only.md'] = '---\nx: 12\n---\n' })
  H.eq({ '2', '6', '3', '3', 'xbx', '☑', '☑' }, row)
end

T['expr: replace() replaces all string matches but only the first regex match without /g'] = function()
  local row = eval({
    '"a:b:c".replace(":", "-")',
    '"a:b:c".replace(/:/, "-")',
    '"a:b:c".replace(/:/g, "-")',
    '"John Smith".replace(/(\\w+) (\\w+)/, "$2, $1")',
    '"John Smith".replace(/(\\w+) (\\w+)/, "$2_$1")',
    '"cost".replace(/o/, "$$")',
    '"ab".replace(/b/, "[$&]")',
  })
  H.eq({ 'a-b-c', 'a-b:c', 'a-b-c', 'Smith, John', 'Smith_John', 'c$st', 'a[b]' }, row)
end

T['expr: negative and inverted slice bounds, split limits, string indexing'] = function()
  local row = eval({
    '"hello".slice(-3)',
    '"hello".slice(-3, -1)',
    '"hello".slice(3, 1).isEmpty()',
    '[1,2,3,4].slice(-2).join("|")',
    '"a,b,,c".split(",").length',
    '"a,b,c".split(",", 0).length',
    '"a,b,c".split(",", 2).join("|")',
    '"abc"[1]',
    '"héllo".length',
    '"héllo".reverse()',
  })
  H.eq({ 'llo', 'll', '☑', '3|4', '4', '0', 'a|b', 'b', '5', 'olléh' }, row)
end

T['expr: repeat() with a NaN count reports an error instead of killing the daemon'] = function()
  local row, res = eval({ '"ab".repeat(number("x"))', '"ab".repeat(2)' })
  H.eq({ '', 'abab' }, row)
  H.contains(errors(res), 'repeat')
  -- the daemon is still alive
  H.eq({ { 'only.md' } }, H.cells(q('filters: file.path == "only.md"\n')))
end

T['expr: flat() flattens exactly one level'] = function()
  local row = eval({ '[1,[2,[3,4]]].flat().length', '[[1],[2]].flat().length', '[1,[2]].flat().contains(2)' })
  H.eq({ '3', '2', '☑' }, row)
end

T['expr: Infinity and NaN behave like JavaScript numbers'] = function()
  local row = eval({ '1/0', '-1/0', 'number("inf")', 'number("Infinity")', '"inf" > 5', 'number("nan").isEmpty()' })
  H.eq({ 'Infinity', '-Infinity', 'NaN', 'Infinity', '☐', '☑' }, row)
end

T['expr: list lambdas shadow properties and nest'] = function()
  local row = eval({
    'value',
    '[1].map(value).join("")',
    '[[1,2],[3]].map(value.map(value * 10).join("+")).join(",")',
    '[].reduce(acc + value, 0)',
    '[1,2,3].reduce(acc + value * index, 0)',
    '[1,2,3].filter(index > 0).length',
    '[3,1,2].sort().reverse().join(",")',
  }, { ['only.md'] = '---\nvalue: 99\n---\n' })
  H.eq({ '99', '1', '10+20,30', '0', '8', '2', '3,2,1' }, row)
end

-- ---------------------------------------------------------------- dates & durations

T['dates: month arithmetic clamps to the end of the month'] = function()
  vim.env.TZ = 'UTC'
  local row = eval({
    'date("2024-01-31") + "1M"',
    'date("2023-01-31") + "1 month"',
    'date("2024-02-29") + "1y"',
    'date("2024-03-31") - "1 month"',
    'date("2024-01-31") + "1M 1d"',
    'date("2024-12-31") + "1d"',
  })
  H.eq({ '2024-02-29', '2023-02-28', '2025-02-28', '2024-02-29', '2024-03-01', '2025-01-01' }, row)
end

T['dates: date minus date is a duration; dates compare with date strings'] = function()
  vim.env.TZ = 'UTC'
  local row = eval({
    'due - date("2024-01-01")',
    '(due - date("2024-01-01")).days',
    '(due - date("2024-01-01")) / 86400000',
    'due > "2024-01-04"',
    'due == "2024-01-05"',
    'due < "2024-01-05 10:00"',
    'date("2024-01-05 10:00") > due',
    'due.month',
    'due.isType("date")',
  }, { ['only.md'] = '---\ndue: 2024-01-05\n---\n' })
  H.eq({ '4 days', '4', '4', '☑', '☑', '☑', '☑', '1', '☑' }, row)
end

T['dates: day arithmetic keeps wall-clock time across DST, hour arithmetic does not'] = function()
  vim.env.TZ = 'America/New_York'
  local row = eval({
    'date("2024-03-09 12:00") + "1d"',
    'date("2024-03-09 12:00") + "24h"',
    '(date("2024-03-11") - date("2024-03-10")).hours',
    'date("2024-11-02 12:00") + "1 day"',
  })
  H.eq({ '2024-03-10 12:00', '2024-03-10 13:00', '23', '2024-11-03 12:00' }, row)
end

T['durations: fractional, compound, scaled and case-sensitive units'] = function()
  vim.env.TZ = 'UTC'
  local row, res = eval({
    'duration("1.5d").hours',
    'duration("1h30m").minutes',
    '(duration("5h") * 2).hours',
    'date("2024-01-01") + "1M"',
    'date("2024-01-01") + "1m"',
    'duration("2 weeks").days',
    'date("2024-01-01") + (duration("1d") * 2)',
    'duration("abc")',
  })
  H.eq({ '36', '90', '10', '2024-02-01', '2024-01-01 00:01', '14', '2024-01-03', '' }, row)
  H.contains(errors(res), 'invalid duration')
end

T['dates: moment format ordinals and escaped text'] = function()
  local row = eval({
    'date("2024-01-11").format("Do")',
    'date("2024-01-22").format("Do")',
    'date("2024-01-03").format("Do")',
    'date("2024-01-05").format("[Week] W, dddd")',
    'date("2024-01-05 14:07").format("hh:mm A")',
  })
  H.eq({ '11th', '22nd', '3rd', 'Week 1, Friday', '02:07 PM' }, row)
end

-- ---------------------------------------------------------------- nulls, sort, group, limit

local null_vault = {
  ['a.md'] = '---\nprice: 3\n---\n',
  ['b.md'] = '---\nprice: 10\n---\n',
  ['c.md'] = 'no front matter',
  ['d.md'] = '---\nprice:\n---\n',
}

T['nulls: missing values sort last in both directions'] = function()
  H.vault(null_vault)
  H.setup()
  local src = [[
views:
  - name: up
    order: [file.name, price]
    sort:
      - property: price
        direction: ASC
  - name: down
    order: [file.name, price]
    sort:
      - property: price
        direction: DESC
]]
  H.eq({ { 'a.md', '3' }, { 'b.md', '10' }, { 'c.md', '' }, { 'd.md', '' } }, H.cells(q(src, { view = 'up' })))
  H.eq({ { 'b.md', '10' }, { 'a.md', '3' }, { 'c.md', '' }, { 'd.md', '' } }, H.cells(q(src, { view = 'down' })))
end

T['nulls: comparisons with a missing value are false, so negating them matches'] = function()
  H.vault(null_vault)
  H.setup()
  H.eq({ { 'b.md' } }, H.cells(q('filters: price > 5\n')))
  H.eq({ { 'a.md' }, { 'c.md' }, { 'd.md' } }, H.cells(q('filters: "!(price > 5)"\n')))
  H.eq({ { 'c.md' }, { 'd.md' } }, H.cells(q('filters: price == null\n')))
end

T['groupBy: list values group by the whole list; empty lists join the empty group, last both ways'] = function()
  H.vault({
    ['a.md'] = '---\ncats: [x, y]\n---\n',
    ['b.md'] = '---\ncats: [x, y]\n---\n',
    ['c.md'] = '---\ncats: [x]\n---\n',
    ['d.md'] = '---\ncats: []\n---\n',
    ['e.md'] = 'none',
  })
  H.setup()
  local src = [[
views:
  - name: asc
    groupBy:
      property: cats
      direction: ASC
  - name: desc
    groupBy:
      property: cats
      direction: DESC
]]
  local asc = q(src, { view = 'asc' })
  H.eq({ { 'x', { 'c.md' } }, { 'x, y', { 'a.md', 'b.md' } }, { '(empty)', { 'd.md', 'e.md' } } }, groups(asc))
  H.ok(asc.groups[1].hasKey, 'groups carry hasKey')
  H.eq({ { 'x, y', { 'a.md', 'b.md' } }, { 'x', { 'c.md' } }, { '(empty)', { 'd.md', 'e.md' } } }, groups(q(src, { view = 'desc' })))
end

T['limit applies to sorted rows before grouping; total counts all matches'] = function()
  local files = {}
  for i = 1, 5 do
    files[('p%d.md'):format(i)] = ('---\nprio: %d\nparity: %s\n---\n'):format(i, i % 2 == 0 and 'even' or 'odd')
  end
  H.vault(files)
  H.setup()
  local res = q([[
views:
  - limit: 3
    sort: [prio]
    groupBy:
      property: parity
      direction: ASC
]])
  H.eq(5, res.total)
  H.eq(3, res.shown)
  H.eq({ { 'even', { 'p2.md' } }, { 'odd', { 'p1.md', 'p3.md' } } }, groups(res))
end

-- ---------------------------------------------------------------- summaries

T['summaries: numeric summaries skip non-numbers; Empty/Filled/Unique count everything'] = function()
  H.vault({
    ['a.md'] = '---\nprice: 3\n---\n',
    ['b.md'] = '---\nprice: abc\n---\n',
    ['c.md'] = '---\nprice: 5\n---\n',
    ['d.md'] = 'none',
    ['e.md'] = '---\nprice: 4\n---\n',
  })
  H.setup()
  local res = q([[
formulas:
  a: price
  b: price
  c: price
  d: price
views:
  - order: [note.price, formula.a, formula.b, formula.c, formula.d]
    summaries:
      price: Sum
      formula.a: Average
      formula.b: Empty
      formula.c: Filled
      formula.d: Unique
]])
  H.eq(nil, res.errors)
  H.eq({ '12', '4', '1', '4', '4' }, summaries(res))
end

T['summaries: date Range is a duration; Earliest/Latest ignore non-dates'] = function()
  vim.env.TZ = 'UTC'
  H.vault({
    ['a.md'] = '---\ndue: 2024-01-15\n---\n',
    ['b.md'] = '---\ndue: 2024-01-31\n---\n',
    ['c.md'] = '---\ndue: 2024-01-01\n---\n',
    ['d.md'] = '---\ndue: not a date\n---\n',
  })
  H.setup()
  local res = q([[
formulas:
  e: due
  l: due
views:
  - order: [due, formula.e, formula.l]
    summaries:
      due: Range
      formula.e: Earliest
      formula.l: Latest
]])
  H.eq({ '30 days', '2024-01-01', '2024-01-31' }, summaries(res))
end

T['summaries: custom formulas get values; unknown names report an error cell'] = function()
  H.vault({ ['a.md'] = '---\nn: 2\n---\n', ['b.md'] = '---\nn: x\n---\n', ['c.md'] = '---\nn: 7\n---\n' })
  H.setup()
  local res = q([[
summaries:
  numbers: 'values.filter(value.isType("number")).length'
  biggest: 'values.filter(value.isType("number")).reduce(if(acc == null || value > acc, value, acc), null)'
formulas:
  m: n
  z: n
views:
  - order: [n, formula.m, formula.z]
    summaries:
      n: numbers
      formula.m: biggest
      formula.z: Bogus
]])
  H.eq({ '2', '7', 'error' }, summaries(res))
  H.contains(errors(res), 'unknown summary "Bogus"')
end

-- ---------------------------------------------------------------- formulas & errors

T['formulas: forward references work and cycles are reported per formula, not fatal'] = function()
  H.vault({ ['a.md'] = '---\nprice: 3\n---\n' })
  H.setup()
  local res = q([[
formulas:
  total: formula.sub * 2
  sub: price + 1
  loop1: formula.loop2 + 1
  loop2: formula.loop1 + 1
  self: formula.self
views:
  - order: [file.name, formula.total, formula.loop1, formula.self]
]])
  H.eq({ { 'a.md', '8', '', '' } }, H.cells(res))
  H.contains(errors(res), 'refers to itself')
end

T['errors: unknown functions and methods land in result.errors and rows still render'] = function()
  H.vault({ ['a.md'] = '---\nprice: 3\n---\n' })
  H.setup()
  local res = q([[
formulas:
  bad: nosuch(1)
  meth: price.nosuch()
  good: price + 1
views:
  - order: [file.name, formula.bad, formula.meth, formula.good]
]])
  H.eq({ { 'a.md', '', '', '4' } }, H.cells(res))
  H.contains(errors(res), 'unknown function nosuch()')
  H.contains(errors(res), 'nosuch()')
end

T['errors: a filter that fails to parse matches nothing and is reported'] = function()
  H.vault({ ['a.md'] = '---\nprice: 3\n---\n', ['b.md'] = 'x' })
  H.setup()
  local res = q('filters:\n  and:\n    - file.ext == "md"\n    - "price >"\n')
  H.eq({}, H.cells(res))
  H.eq(0, res.total)
  H.contains(errors(res), 'price >')
  local res2 = q('filters: nosuch(file.name)\n')
  H.eq({}, H.cells(res2))
  H.contains(errors(res2), 'unknown function nosuch()')
end

T['errors: an unknown view name is reported and the first view is used'] = function()
  H.vault({ ['a.md'] = 'x' })
  H.setup()
  local src = 'views:\n  - name: V1\n  - type: list\n    name: V2\n'
  local res = q(src, { view = 'nope' })
  H.eq('V1', res.name)
  H.contains(errors(res), 'no view named "nope"')
  local res2 = q(src, { view = '1' })
  H.eq('V2', res2.name)
  H.eq('list', res2.type)
  H.eq(nil, res2.errors)
end

-- ---------------------------------------------------------------- filters

local tag_vault = {
  ['a.md'] = '---\ntags: [x]\n---\n',
  ['b.md'] = '---\ntags: [y]\n---\n',
  ['c.md'] = '---\ntags: [x, y]\n---\n',
  ['d.md'] = 'none',
  ['e.txt'] = 'not markdown',
}

T['filters: not is "none of", combined with nested and/or and view filters'] = function()
  H.vault(tag_vault)
  H.setup()
  local res = q([[
filters:
  and:
    - file.ext == "md"
    - not:
        - file.hasTag("x")
        - file.hasTag("y")
]])
  H.eq({ { 'd.md' } }, H.cells(res))

  local src = [[
filters:
  and:
    - file.ext == "md"
    - or:
        - not:
            - file.hasTag("x")
        - and:
            - file.hasTag("x")
            - file.hasTag("y")
views:
  - name: all
  - name: noy
    filters:
      not: file.hasTag("y")
]]
  H.eq({ { 'b.md' }, { 'c.md' }, { 'd.md' } }, H.cells(q(src, { view = 'all' })))
  H.eq({ { 'd.md' } }, H.cells(q(src, { view = 'noy' })), 'view filters AND with the global filter')
end

T['filters: inFolder matches whole path segments only'] = function()
  H.vault({ ['notes/a.md'] = 'x', ['notes2/b.md'] = 'x', ['notes/sub/c.md'] = 'x', ['root.md'] = 'x' })
  H.setup()
  H.eq({ { 'a.md' }, { 'c.md' } }, H.cells(q('filters: file.inFolder("notes/")\n')))
  H.eq({ { 'c.md' } }, H.cells(q('filters: file.inFolder("notes/sub")\n')))
end

T['files: non-markdown and extensionless files expose name, basename, ext, folder'] = function()
  H.vault({ ['img/Photo.JPG'] = 'x', ['doc.pdf'] = 'x', ['a.md'] = 'x', ['archive.tar.gz'] = 'x', ['Makefile'] = 'x' })
  H.setup()
  local res = q([[
filters: file.ext == "JPG" || file.ext == "gz" || file.ext == ""
views:
  - order: [file.name, file.basename, file.ext, file.folder]
]])
  H.eq({
    { 'Makefile', 'Makefile', '', '' },
    { 'archive.tar.gz', 'archive.tar', 'gz', '' },
    { 'Photo.JPG', 'Photo', 'JPG', 'img' },
  }, H.cells(res))
end

-- ---------------------------------------------------------------- this

T['this: standalone is the base file, embedded is the embedding note, absent is null'] = function()
  H.vault({
    ['q.base'] = 'filters: file.name == "a.md"\nformulas:\n  t: this.file.name\n  s: this.status\nviews:\n  - order: [formula.t, formula.s]\n',
    ['note.md'] = '---\nstatus: open\n---\n![[q.base]]\n',
    ['a.md'] = 'x',
  })
  H.setup()
  local path = H.root .. '/q.base'
  H.eq({ { 'q.base', '' } }, H.cells(H.query({ path = path })))
  H.eq({ { 'note.md', 'open' } }, H.cells(H.query({ path = path, this = H.root .. '/note.md' })))
  H.eq({ { '', '' } }, H.cells(q('filters: file.name == "a.md"\nformulas:\n  t: this.file.name\n  s: this.status\nviews:\n  - order: [formula.t, formula.s]\n')))
end

T['this: file.hasLink(this.file) rebuilds backlinks, including front matter links'] = function()
  H.vault({
    ['note.md'] = 'x',
    ['a.md'] = 'see [[note]]',
    ['b.md'] = '---\nup: "[[note]]"\n---\n',
    ['c.md'] = 'see [[other]]',
    ['d.md'] = '`[[note]]` in code',
  })
  H.setup()
  H.eq({ { 'a.md' }, { 'b.md' } }, H.cells(q('filters: file.hasLink(this.file)\n', { this = H.root .. '/note.md' })))
end

-- ---------------------------------------------------------------- links

T['links: resolution by root path, then the source folder, then shortest basename match'] = function()
  H.vault({
    ['a.md'] = '[[b]] [[sub/c]] [[a.b]] [[PIC.png]] [[missing]] [x](sub/c.md) [[b#h|alias]]',
    ['b.md'] = 'root b',
    ['dir/b.md'] = 'dir b',
    ['zdir/x.md'] = '[[b]] [[c]]',
    ['zdir/c.md'] = 'near',
    ['aa/c.md'] = 'shorter path',
    ['sub/c.md'] = 'x',
    ['a.b.md'] = 'x',
    ['pic.png'] = 'x',
  })
  H.setup()
  local res = q([[
filters: file.name == "a.md" || file.name == "x.md"
formulas:
  targets: file.links.map(if(value.asFile(), value.asFile().path, "?")).join("|")
views:
  - order: [file.path, formula.targets]
]])
  H.eq({
    { 'a.md', 'b.md|sub/c.md|a.b.md|pic.png|?' },
    -- [[b]] is the root b.md (a vault path); [[c]] prefers the source folder
    { 'zdir/x.md', 'b.md|zdir/c.md' },
  }, H.cells(res))
end

T['links: front matter links equal links, names and files that resolve the same'] = function()
  local row = eval({
    'up == link("b")',
    'up == "b"',
    'up == "[[b]]"',
    'up == link("c")',
    'up.asFile().path',
    'rel.contains(link("sub/c"))',
    'rel.length',
    'file.hasLink("b")',
    'file.hasLink(file("sub/c.md"))',
  }, {
    ['only.md'] = '---\nup: "[[b]]"\nrel: ["[[b]]", "[[c]]"]\n---\n',
    ['b.md'] = 'x',
    ['sub/c.md'] = 'x',
  })
  H.eq({ '☑', '☑', '☑', '☐', 'b.md', '☑', '2', '☑', '☑' }, row)
end

T['links: backlinks are unique per source and ignore unresolved targets'] = function()
  H.vault({
    ['a.md'] = '[[b]] and again [[B]] and [x](b.md)',
    ['b.md'] = 'target',
    ['c.md'] = '---\nup: "[[b]]"\n---\n[[nowhere]]',
    ['d.md'] = 'no links',
  })
  H.setup()
  local res = q([[
filters: file.name == "b.md"
formulas:
  back: file.backlinks.map(value.asFile().name).join("|")
views:
  - order: [formula.back]
]])
  H.eq({ { 'a.md|c.md' } }, H.cells(res))
end

T['unicode: names, case-insensitive links and string functions on non-ASCII files'] = function()
  H.vault({
    ['Ünï cødé.md'] = '---\nprice: 1\n---\n',
    ['src.md'] = 'see [[ünï CØDÉ]]',
  })
  H.setup()
  local res = q([[
filters: file.basename.startsWith("Ü") || file.hasLink("Ünï cødé")
formulas:
  len: file.basename.length
  up: file.basename.upper()
views:
  - order: [file.name, formula.len, formula.up]
]])
  H.eq({ { 'src.md', '3', 'SRC' }, { 'Ünï cødé.md', '8', 'ÜNÏ CØDÉ' } }, H.cells(res))
  local back = q('filters: file.hasLink(file("Ünï cødé.md"))\n')
  H.eq({ { 'src.md' } }, H.cells(back), 'differently-cased unicode link must resolve')
end

return T
