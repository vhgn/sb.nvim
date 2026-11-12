#!/usr/bin/env bash
# End-to-end tests for brain.nvim. Every test runs in a fresh
# `nvim --headless --clean` with isolated XDG dirs and its own daemon.
#
#   tests/run.sh                 # all specs
#   tests/run.sh embed_spec      # one spec
#   tests/run.sh embed_spec 'name filter'
set -uo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/.." && pwd)"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

export BRAIN_BIN="$work/brain"
( cd "$repo" && if command -v go >/dev/null; then go build -o "$BRAIN_BIN" .; else mise exec -- go build -o "$BRAIN_BIN" .; fi ) || { echo "build failed"; exit 1; }

specs=()
if [[ $# -ge 1 ]]; then
  specs=("$here/${1%.lua}.lua")
else
  for f in "$here"/*_spec.lua; do specs+=("$f"); done
fi
filter="${2:-}"

pass=0 fail=0 failed=()
for spec in "${specs[@]}"; do
  sname="$(basename "$spec" .lua)"
  while IFS= read -r t; do
    [[ -z "$t" ]] && continue
    [[ -n "$filter" && "$t" != *"$filter"* ]] && continue
    tdir="$work/t$pass$fail"
    mkdir -p "$tdir"/{config,data,state,cache,run}
    chmod 700 "$tdir/run"
    out="$(cd "$here" && XDG_CONFIG_HOME="$tdir/config" XDG_DATA_HOME="$tdir/data" XDG_STATE_HOME="$tdir/state" \
      XDG_CACHE_HOME="$tdir/cache" XDG_RUNTIME_DIR="$tdir/run" LUA_PATH="$here/?.lua;;" \
      timeout 60 nvim --headless --clean -u NONE -l "$here/runner.lua" "$spec" "$t" 2>&1)"
    code=$?
    if [[ $code -eq 0 ]]; then
      pass=$((pass + 1)); echo "ok   $sname: $t"
    else
      fail=$((fail + 1)); failed+=("$sname: $t"); echo "FAIL $sname: $t"; echo "$out" | sed 's/^/     /'
    fi
    # make sure no daemon outlives its test
    pkill -f "daemon --root .*${tdir}" 2>/dev/null || true
  done < <(cd "$here" && LUA_PATH="$here/?.lua;;" nvim --headless --clean -u NONE -l "$here/runner.lua" "$spec" 2>/dev/null)
done

echo
echo "$pass passed, $fail failed"
for f in "${failed[@]}"; do echo "  - $f"; done
[[ $fail -eq 0 ]]
