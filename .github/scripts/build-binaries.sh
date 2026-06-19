#!/usr/bin/env bash
# Compile runtime binaries for a single macOS architecture.
set -euo pipefail

ARCH="${1:-$(uname -m)}"
case "$ARCH" in
  arm64)  TARGET="bun-darwin-arm64" ;;
  x86_64) TARGET="bun-darwin-x64"   ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

mkdir -p dist
bun build --compile apps/websocket/src/index.ts --outfile "dist/ws-server"   --target="$TARGET"
bun build --compile apps/local-proxy/src/index.ts --outfile "dist/local-proxy" --target="$TARGET"
bun build --compile apps/cli/src/index.ts       --outfile "dist/bridge-cmd" --target="$TARGET"

echo "Built binaries for $ARCH ($TARGET) in dist/"
ls -l dist/ws-server dist/local-proxy dist/bridge-cmd
