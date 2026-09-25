#!/usr/bin/env bash
# Compile runtime binaries for a single macOS architecture.
# After the bridge-core merge (ADR-0011) only two binaries ship:
# `bridge-core` (control plane + MCP + extension bridge) and `bridge-cmd`
# (stateless CLI). Since the Go rewrite (ADR-0012) both are plain
# `go build` outputs — the bun-compiled TS sources are gone.
set -euo pipefail

ARCH="${1:-$(uname -m)}"
case "$ARCH" in
  arm64)  GOARCH="arm64" ;;
  x86_64) GOARCH="amd64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

mkdir -p dist
CGO_ENABLED=0 GOOS=darwin GOARCH="$GOARCH" go -C apps/bridge-core build -o ../../dist/bridge-core ./cmd/bridge-core
CGO_ENABLED=0 GOOS=darwin GOARCH="$GOARCH" go -C apps/bridge-core build -o ../../dist/bridge-cmd  ./cmd/bridge

echo "Built binaries for darwin/$GOARCH in dist/"
ls -l dist/bridge-core dist/bridge-cmd
