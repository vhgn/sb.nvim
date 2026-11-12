# Bases engine: before / after

`go test -run '^$' -bench . -count=5 -benchmem ./internal/yaml/ ./internal/vault/ ./internal/bases/`, medians of 5 runs (raw output in `engine-before.txt` / `engine-after.txt`). Intel Core Ultra 7 155H, 22 threads, Go 1.25.14. Vaults are `benchdata.WriteRich` (front matter with tags, status, numbers, dates, link properties; ~20 links per note). Other agents were using the machine (load average 10–40 during the after run; the before run was not monitored), so the parallel paths are understated and differences under ~10% are noise.

| Benchmark | before time/op | after time/op | speedup | before allocs/op | after allocs/op |
|---|--:|--:|--:|--:|--:|
| ParseFrontMatter | 7.5 µs | 5.4 µs | **1.4×** | 69 | 41 |
| ParseBaseYAML | 9.4 µs | 8.1 µs | **1.1×** | 72 | 55 |
| ColdStart/1k/nocache | 30.09 ms | 24.04 ms | **1.3×** | 152262 | 117276 |
| ColdStart/1k/warmcache | 35.50 ms | 20.58 ms | **1.7×** | 134519 | 130060 |
| ColdStart/10k/nocache | 211.57 ms | 119.51 ms | **1.8×** | 1532885 | 1171153 |
| ColdStart/10k/warmcache | 329.31 ms | 94.38 ms | **3.5×** | 1347188 | 1299641 |
| SaveCache/1k | 6.64 ms | 5.87 ms | **1.1×** | 25514 | 24548 |
| SaveCache/10k | 68.29 ms | 18.58 ms | **3.7×** | 255018 | 245293 |
| ScanUnchanged/1k | 1.93 ms | 899.5 µs | **2.1×** | 5158 | 6122 |
| ScanUnchanged/10k | 23.58 ms | 4.17 ms | **5.7×** | 50226 | 59215 |
| Touch/1k | 10.93 ms | 60.0 µs | **182.2×** | 25029 | 114 |
| Touch/10k | 120.43 ms | 67.6 µs | **1782.0×** | 251663 | 103 |
| ParseNote | 30.5 µs | 24.9 µs | **1.2×** | 133 | 105 |
| Resolve | 1.3 µs | 656 ns | **2.0×** | 3 | 0 |
| Run/table/1k | 9.20 ms | 4.13 ms | **2.2×** | 54549 | 29218 |
| Run/links/1k | 7.65 ms | 1.10 ms | **6.9×** | 20538 | 6185 |
| Run/sorted/1k | 3.80 ms | 2.51 ms | **1.5×** | 29712 | 9236 |
| Run/table/10k | 95.58 ms | 25.52 ms | **3.7×** | 557518 | 290078 |
| Run/links/10k | 86.91 ms | 4.99 ms | **17.4×** | 196942 | 51531 |
| Run/sorted/10k | 44.18 ms | 11.21 ms | **3.9×** | 287505 | 85256 |
| ParseBase | 9.4 µs | 9.0 µs | 1.04× | 96 | 76 |
| NaturalCompare | 462 ns | 187 ns | **2.5×** | 6 | 0 |

Benchmarks: `ColdStart` = `New` + `LoadCache` + `Scan` (no cache file vs. a saved cache); `ScanUnchanged` = the stat walk `ScanIfOlder` does when nothing changed; `Touch` = re-reading one edited note; `Run/*` = `ParseBase` + `Run` per query (`table`: tag filter, three formulas, two-key sort, groupBy, three summaries; `links`: `file.hasLink(this.file)` / `project == this` with `file.backlinks` per row; `sorted`: sort by a text and a link property).

## What changed and why

- **Incremental `Touch`/`Scan` (vault.go `apply`/`update`)**: resolution depends only on the set of paths, so when files are only edited the edited files are swapped into `sorted`/`byName` and only their outgoing links are re-resolved, with `backs` patched by diff. Adds/removes still do a full `reindex`. Was rebuilding everything (sort, byName, ~200k link resolutions) on every save.
- **Parallel `reindex` + cached link targets (vault.go)**: link resolution fans out over all cores; the per-file resolved targets are kept (`outs`) and exposed as `LinkTargets`/`LinksTo`.
- **Allocation-free `resolve`**: lookups use a stack buffer (`p+".md"`, `dir/target` joins) instead of `path.Join`/concatenation.
- **Parallel stat walk (vault.go `walk`)**: directories are read and their files stat'ed concurrently; the "gone" set is only built when a file count says something disappeared. Root symlinks fall back to the old `WalkDir` path.
- **JSON-lines cache, encoded/decoded in parallel (`cacheVersion` 3)**: the single large `map` document was decoded on one core and was slower than re-parsing every note. Old caches are ignored (one full scan).
- **`ParseNote`/yaml**: no line splitting / front matter re-joining, lines without `[` or `#` skip link/tag scanning, quoted YAML scalars without escapes are substrings, date-like scalars skip `strconv`.
- **Bases rows evaluated in parallel (base.go `pool`)**: filtering, sort keys and cells run on per-worker evaluators; errors are collected per row and replayed in row order, so `Errors` is identical. Sorting uses a copy of `sort.SliceStable`'s exact algorithm with disjoint ranges merged in parallel (sort.go), so ties and mixed-type keys order exactly as before.
- **Per-row costs**: converted property values are memoized per row (dates/links were re-parsed on every access); formula memo/cycle tracking use small slices instead of three maps per row; `parseDate` and the `[[link]]` check use hand-written matchers equivalent to the regexps; `parseDate` skips zone layouts for zone-less strings (their failures allocated errors); `naturalCompare` walks the strings instead of converting to `[]rune`.
- **`file.hasLink(f)`** answers from the vault's backlink index (one binary search) instead of re-resolving every link of every row; **`file.links`** uses the cached targets.

Behaviour was checked against the original code with golden outputs (full vault index, backlinks and resolution after scans/touches/edits/adds/deletes; Bases results with errors, cycles, groups and summaries over 3,000 notes) and differential fuzzing of the old and new `yaml.Parse`, `ParseNote`, `naturalCompare`, `parseDate`, `wikiOnly`, the stable sort and whole `Base.Run` results; plus `go test -race` and `nvim/tests/run.sh bases_spec` (47/47).
