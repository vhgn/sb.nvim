# Runtime (non-LSP) benchmarks: before / after

Lua: `nvim/tests/bench/run.sh` (headless nvim 0.12, notes of 200/2000/20000 mixed-content lines,
vaults of 1k/10k notes from `internal/benchdata`, fake image.nvim). Each number is the median of
3 runs, each the median of 7 samples; "garbage" is Lua heap allocated per call (GC stopped).
Go: `go test -bench . -benchmem ./internal/tex/ ./internal/daemon/`, median of 5 runs.
Before and after were built from one snapshot of the repo (identical vault/bases/yaml) and run
alternately; the machine was heavily loaded by other jobs (load average 11–25), so the speedup
column is the median of the per-round before/after ratios, not the ratio of the medians.
Differences under ~15% are noise.

| Benchmark | before | after | speedup | garbage/mem before | after |
|---|--:|--:|--:|--:|--:|
| scan/200 | 292.7 µs | 168.6 µs | **1.8×** | 45.9 KiB | 39.0 KiB |
| scan/2000 | 1.82 ms | 1.69 ms | 1.1× (≈) | 459.9 KiB | 391.2 KiB |
| scan/20000 | 20.00 ms | 10.27 ms | **2.0×** | 4.61 MiB | 3.94 MiB |
| media.refresh/200 | 2.05 ms | 834.6 µs | **2.6×** | 341.8 KiB | 201.1 KiB |
| media.refresh+inline/200 | 3.19 ms | 888.1 µs | **3.6×** | 508.1 KiB | 167.8 KiB |
| media.cursor_moved/200 | 144.7 µs | 25.5 µs | **5.6×** | 19.5 KiB | 0.3 KiB |
| media.refresh/2000 | 22.45 ms | 14.82 ms | **1.5×** | 3.40 MiB | 1.90 MiB |
| media.refresh+inline/2000 | 25.96 ms | 5.28 ms | **4.9×** | 9.27 MiB | 1.56 MiB |
| media.cursor_moved/2000 | 869.1 µs | 27.9 µs | **31.0×** | 193.9 KiB | 0.3 KiB |
| media.refresh/20000 | 245.36 ms | 98.91 ms | **2.3×** | 33.88 MiB | 19.65 MiB |
| media.refresh+inline/20000 | 333.76 ms | 57.79 ms | **5.8×** | 83.62 MiB | 15.43 MiB |
| media.cursor_moved/20000 | 10.28 ms | 32.5 µs | **317×** | 1.89 MiB | 0.3 KiB |
| client.ping | 35.8 µs | 41.1 µs | 0.9× (≈) | 2.7 KiB | 2.7 KiB |
| client.texUnicode/50 | 356.3 µs | 240.5 µs | **1.5×** | 13.5 KiB | 13.4 KiB |
| client.texUnicode/500 | 3.23 ms | 2.14 ms | **1.5×** | 125.6 KiB | 124.8 KiB |
| client.resolve | 63.6 µs | 71.0 µs | 0.9× (≈) | 3.2 KiB | 3.2 KiB |
| client.tex/cached | 128.4 µs | 68.4 µs | **1.8×** | 3.2 KiB | 3.2 KiB |
| client.query/1k | 2.18 ms | 2.35 ms | 0.9× (≈) | 78.0 KiB | 77.6 KiB |
| client.query-all/1k | 10.52 ms | 7.63 ms | **1.3×** | 3.49 MiB | 1.58 MiB |
| client.query/10k | 11.77 ms | 11.55 ms | 1.0× (≈) | 81.5 KiB | 79.9 KiB |
| client.query-all/10k | 110.25 ms | 75.74 ms | **1.4×** | 168.03 MiB | 21.28 MiB |
| render.table/100 | 5.40 ms | 1.16 ms | **4.8×** | 606.9 KiB | 483.6 KiB |
| render.table/1000 | 36.89 ms | 13.07 ms | **2.8×** | 5.81 MiB | 4.61 MiB |
| render.table/10000 | 386.02 ms | 153.71 ms | **2.6×** | 59.22 MiB | 47.09 MiB |
| render.cards/100 | 4.08 ms | 2.72 ms | **1.5×** | 1.13 MiB | 1.05 MiB |
| render.cards/1000 | 43.23 ms | 28.58 ms | **1.5×** | 11.22 MiB | 10.38 MiB |
| render.cards/10000 | 524.82 ms | 301.60 ms | **1.7×** | 112.18 MiB | 103.72 MiB |
| render.list/100 | 595.1 µs | 856.8 µs | 0.7× (slower) | 314.8 KiB | 314.8 KiB |
| render.list/1000 | 5.97 ms | 8.76 ms | 0.7× (slower) | 3.15 MiB | 3.15 MiB |
| render.list/10000 | 103.05 ms | 101.16 ms | 1.0× (≈) | 32.66 MiB | 32.66 MiB |
| render.map/100 | 394.3 µs | 396.2 µs | 1.0× (≈) | 292.1 KiB | 292.1 KiB |
| render.map/1000 | 2.65 ms | 2.56 ms | 1.0× (≈) | 1.90 MiB | 1.90 MiB |
| render.map/10000 | 26.17 ms | 24.60 ms | 1.1× (≈) | 16.78 MiB | 16.78 MiB |
| base.refresh/4-embeds | 52.62 ms | 2.99 ms | **17.6×** | 14.11 MiB | 437.1 KiB |
| textchanged/4-embeds+media | 86.51 ms | 9.41 ms | **9.4×** | 17.48 MiB | 1.89 MiB |
| base.redraw/same-width | 51.46 ms | 867.3 µs | **59.3×** | 13.64 MiB | 7.5 KiB |
| base.redraw/resized | 51.85 ms | 45.99 ms | 1.1× (≈) | 13.64 MiB | 13.57 MiB |
| base.item_at/j | 377.8 µs | 1.2 µs | **319×** | 169.3 KiB | 0.7 KiB |
| base.item_at/k | 312.5 µs | 1.2 µs | **260×** | 169.3 KiB | 0.7 KiB |
| base.item_at/enter(span) | 3.74 ms | 7.3 µs | **501×** | 646.7 KiB | 1.2 KiB |
| *Go (daemon / tex)* | | | | | |
| SVG | 547.9 µs | 346.0 µs | **1.6×** | 232.4 KiB, 2535 allocs | 204.8 KiB, 1357 allocs |
| Unicode | 16.1 µs | 16.3 µs | 1.0× (≈) | 6.2 KiB, 219 allocs | 6.2 KiB, 219 allocs |
| RoundTrip/ping | 24.1 µs | 23.9 µs | 1.0× (≈) | 1.3 KiB, 30 allocs | 1.3 KiB, 29 allocs |
| RoundTrip/texUnicode50 | 309.8 µs | 194.0 µs | **1.6×** | 88.2 KiB, 2247 allocs | 44.6 KiB, 1226 allocs |
| RoundTrip/resolve | 36.9 µs | 37.6 µs | 1.0× (≈) | 2.4 KiB, 45 allocs | 2.4 KiB, 45 allocs |
| RoundTrip/tex/cached | 107.4 µs | 43.7 µs | **2.5×** | 16.9 KiB, 256 allocs | 3.8 KiB, 79 allocs |
| Query/1k | 2.06 ms | 2.07 ms | 1.0× (≈) | 682.6 KiB, 8942 allocs | 683.2 KiB, 8942 allocs |
| Query/1k/8-concurrent-stale | 6.62 ms | 5.14 ms | **1.3×** | 10.31 MiB, 120530 allocs | 6.05 MiB, 77633 allocs |
| Query/10k | 10.50 ms | 10.48 ms | 1.0× (≈) | 6.15 MiB, 83616 allocs | 6.15 MiB, 83615 allocs |
| Query/10k/8-concurrent-stale | 38.78 ms | 27.52 ms | **1.4×** | 105.67 MiB, 1142709 allocs | 56.31 MiB, 728135 allocs |
| RenderTeX/cached | 59.3 µs | 3.2 µs | **19.4×** | 33.0 KiB, 303 allocs | 0.9 KiB, 14 allocs |
| RenderTeX/cold | 43.06 ms | 43.07 ms | 1.0× (≈) | 68.8 KiB, 434 allocs | 66.5 KiB, 285 allocs |

Notes on the rows:
- `media.refresh/N`: synchronous cost of one refresh after an edit (note re-scanned); `+inline`: until
  inline math is concealed again (before: a texUnicode round trip every time; after: cached answers,
  applied in the same tick). `media.cursor_moved`: one j/k step.
- `base.refresh/4-embeds`, `textchanged/…`: a 2000-line note with 4 embedded bases (~3000 result rows),
  results already present — what every debounced TextChanged ran. `base.item_at/*`: the per-keypress
  cost of the j/k/<CR> maps. `base.redraw/resized`: the text width changed, so all bases re-render.
- `render.list/100,1000` show up slower in the harness but not in an interleaved in-process A/B
  (`render.list/1000`: 5.7 → 5.3 ms, 5.8 → 5.6 ms); the list code path is unchanged apart from
  ASCII fast paths; read it as "≈".
- `client.query*` and `Query/*` are dominated by the bases engine (optimized separately); `query-all`
  returns every note, so the client's line assembly and JSON matter.
- `Query/*/8-concurrent-stale`: 8 queries arrive together with the index stale (freshFor = 0), as when
  a note with 8 embeds is opened after >2s idle.

## What changed and why

- **scan.lua**: lines without `$`, `!` or `\` are skipped right away (three plain `find`s); `mask_code`
  copies runs between backticks instead of one `line:sub` per byte; `![[` is located with a plain find
  before the anchored embed pattern (the unanchored lazy pattern was the hot spot); the image-title
  `gsub`s only run when the destination ends in a quote/paren; the final sort is a per-line insertion
  sort when no two items share a position (order is then unique), else the original `table.sort`.
  New `scan.buf(buf)` caches the items per `changedtick`, so base.refresh, media.refresh and `item_at`
  (span) share one scan per edit instead of two-three. Verified identical on 30k fuzzed notes.
- **media.lua**: `cursor_moved` only touches the line left and the line entered when the text is
  unchanged since the marks were applied (falls back to the full re-apply otherwise) — O(1) instead of
  O(n) per j/k. Per refresh: palette, mermaid theme/`executable()` computed once instead of per item;
  sha256 memoized; all wiki images resolved in one `resolve` request; texUnicode answers and display-math
  png paths (keyed by formula+colors+size, verified with `fs_stat`) are cached, so a refresh only asks
  the daemon about new formulas. `reset_cache()` is called from `require("brain").rebuild()` (init.lua).
- **base/init.lua**: `draw` reuses the rendered view and virtual lines when result, width, title and
  max_height are unchanged (every debounced edit and every WinResized re-rendered all bases); virtual
  lines bucket highlights by line in one pass instead of scanning all highlights for each of the 40
  lines; `locate` reads extmarks without `details` (which copied every virtual line on each j/k —
  marks use `undo_restore = false`, so invalidated ones are deleted and need no details to detect).
- **base/render.lua**: `trunc` and widths take an ASCII fast path (no `vim.fn.split(s, '\zs')` / API
  call); `Builder:add` avoids `table.insert`. Output identical on 12k fuzzed results (incl. CJK, emoji,
  combining marks, tabs, control chars).
- **client.lua**: reply lines are assembled from chunk lists instead of `buf = buf .. data` plus
  `buf:sub` per line (quadratic for multi-MB query answers: 168 MiB → 21 MiB garbage on 10k notes);
  queued requests go out in one `writev`.
- **daemon.go**: `RenderTeX` remembers which png a set of inputs produced, so a known formula skips
  layout + SVG + hashing (59 µs → 3 µs; still `stat`s the png). `texUnicode` parses each formula once
  (`tex.UnicodeCheck`) and encodes a struct instead of maps. Rescans triggered by requests are
  single-flight: requests arriving while one is in flight wait for it instead of walking the vault again.
  `serve` no longer copies each line to a string to trim it.
- **tex/layout.go**: SVG is appended with `strconv` instead of `fmt.Fprintf`, and path coordinates are
  parsed with `strconv.ParseFloat` instead of `fmt.Sscanf` (falling back to it for anything else).
  Byte-identical output on 4000 generated formulas × 3 option sets.

## What didn't help / not done

- `RenderTeX/cold` is the rsvg-convert spawn (~43 ms); building the SVG first costs <1% of it.
- `client.ping/resolve/query` latency is the socket + scheduling floor (~25–60 µs); nothing to win there.
- `render.list`/`render.map` were already cheap per row; `render.cards` remains allocation-heavy
  (segment tables per card line) — further gains need a different data layout of `hls`/`targets`.
- `base.redraw/resized` must re-render (width changed); it got only the render.* speedups.
- Caching parsed `.base` sources in the daemon: ParseBase is ~13 µs, not worth the risk.
