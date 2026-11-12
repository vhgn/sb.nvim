#!/usr/bin/env bash
# Runtime micro-benchmarks for brain.nvim + daemon (non-LSP paths).
#
#   tests/bench/run.sh            # all
#   BENCH_ONLY=scan tests/bench/run.sh
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
tests="$(cd "$here/.." && pwd)"
repo="$(cd "$here/../.." && pwd)"
go() { if command -v go >/dev/null; then command go "$@"; else mise exec -- go "$@"; fi; }

work="$(mktemp -d)"
trap 'pkill -f "daemon --root ${BENCH_VAULTS:-$work}" 2>/dev/null || true; rm -rf "$work"' EXIT
export BRAIN_BIN="$work/brain"
(cd "$repo" && go build -o "$BRAIN_BIN" .)

all_base='views:
  - type: table
    name: Table
    order: [file.name, status, priority, due, project]
  - type: cards
    name: Cards
    order: [file.name, status, priority, due]
  - type: list
    name: List
    order: [file.name, status, due]
'
# BENCH_VAULTS=dir reuses (or creates once) the vaults there
vaults="${BENCH_VAULTS:-$work}"
for n in 1000 10000; do
  d="$vaults/v$n"
  if [[ ! -f "$d/all.base" ]]; then
    mkdir -p "$d/.brain"
    (cd "$repo" && BRAIN_BENCH_VAULT="$d" BRAIN_BENCH_N=$n go test -count=1 -run TestWriteBenchVault ./internal/daemon/ >/dev/null)
    printf '%s' "$all_base" >"$d/all.base"
  fi
done
export BENCH_VAULT_1K="$vaults/v1000" BENCH_VAULT_10K="$vaults/v10000"

t="$work/x"
mkdir -p "$t"/{config,data,state,cache,run}
chmod 700 "$t/run"
cd "$tests"
XDG_CONFIG_HOME="$t/config" XDG_DATA_HOME="$t/data" XDG_STATE_HOME="$t/state" \
  XDG_CACHE_HOME="$t/cache" XDG_RUNTIME_DIR="$t/run" LUA_PATH="$tests/?.lua;;" \
  nvim --headless --clean -u NONE -l "$here/runtime_bench.lua" 2>&1
