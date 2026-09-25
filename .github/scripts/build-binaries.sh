#!/usr/bin/env bash
# Compile runtime binaries for a single macOS architecture.
# After the bridge-core merge (ADR-0011) only two binaries ship:
# `bridge-core` (control plane + MCP + extension bridge) and `bridge-cmd`
# (stateless CLI). The pre-merge trio `ws-server` + `local-proxy` +
# `bridge-cmd` is replaced by these two.
set -euo pipefail

ARCH="${1:-$(uname -m)}"
case "$ARCH" in
  arm64)  TARGET="bun-darwin-arm64" ;;
  x86_64) TARGET="bun-darwin-x64"   ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

mkdir -p dist
bun build --compile apps/bridge-core/src/index.ts --outfile "dist/bridge-core" --target="$TARGET"
bun build --compile apps/cli/src/index.ts         --outfile "dist/bridge-cmd" --target="$TARGET"

echo "Built binaries for $ARCH ($TARGET) in dist/"
ls -l dist/bridge-core dist/bridge-cmd