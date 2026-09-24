#!/usr/bin/env bash
# Run both extension watchers side by side.
#
# They cannot be a single vite invocation: src/content.ts has to be built as an
# IIFE (Chrome's content_scripts has no `type: "module"` and rejects an ESM
# bundle), while everything else is ESM. They also cannot be chained with `&&`
# -- `vite build --watch` never exits, so the second command would never start
# and dist/content.js would never be produced or rebuilt.
#
# A watcher dying must end the dev session with that watcher's exit status.
# Plain `wait` returns 0 once both children are reaped, so a fatal error in
# one is silently swallowed and the surviving watcher is left running against
# a stale half-built dist -- the exact failure this guards against.
#
# `wait -n` (bash 4.3+) is the natural primitive, but `/usr/bin/env bash`
# resolves to /bin/bash on stock macOS, where bash is 3.2.57 and `wait -n`
# errors with "invalid option". The loop below uses `kill -0` polling, which
# is portable to bash 3.2. The trade-off is that the response time is the
# poll interval (0.2s) instead of immediate.
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

pids=()
for config in vite.config.ts vite.content.config.ts; do
  vite build --config "$config" --watch --mode development &
  pids+=("$!")
done

cleanup() {
  for pid in "${pids[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT INT TERM

# Wait for whichever watcher exits first. Poll both until one of them dies,
# then reap it to retrieve its real exit status.
p1="${pids[0]}"
p2="${pids[1]}"
while kill -0 "$p1" 2>/dev/null && kill -0 "$p2" 2>/dev/null; do
  sleep 0.2
done

if kill -0 "$p1" 2>/dev/null; then
  # p2 is the one that died
  wait "$p2"
  status=$?
  echo "dev-watch: content watcher exited (status $status); stopping the other" >&2
else
  # p1 is the one that died
  wait "$p1"
  status=$?
  echo "dev-watch: extension watcher exited (status $status); stopping the other" >&2
fi
exit "$status"