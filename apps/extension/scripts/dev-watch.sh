#!/usr/bin/env bash
# Run both extension watchers side by side.
#
# They cannot be a single vite invocation: src/content.ts has to be built as an
# IIFE (Chrome's content_scripts has no `type: "module"` and rejects an ESM
# bundle), while everything else is ESM. They also cannot be chained with `&&`
# — `vite build --watch` never exits, so the second command would never start
# and dist/content.js would never be produced or rebuilt.
#
# `wait -n` is the point of this script existing at all: it returns as soon as
# either watcher exits, so a fatal error in one tears down the session with a
# non-zero status instead of leaving a half-built dist behind a still-running
# sibling. Plain `wait` returns 0 once the children are reaped, whatever their
# exit status, which would swallow exactly the failure this guards against.
# macOS /bin/sh is bash 3.2 in POSIX mode and has no `wait -n`, hence the
# explicit bash shebang and the package.json hop through this file.
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

wait -n
status=$?
echo "dev-watch: a watcher exited (status $status); stopping the other" >&2
exit "$status"
