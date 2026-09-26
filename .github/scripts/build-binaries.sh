#!/usr/bin/env bash
# Compile the runtime binary for a single macOS architecture.
# One Go binary ships (ADR-0013): `bridge` — cobra CLI, the `service`
# lifecycle tree, and the hidden `serve` subcommand that runs the control
# plane. Plain `go build` output — the bun-compiled TS sources are gone.
# The link-time version (main.version) comes from the root package.json
# version field; VERSION=vX.Y.Z overrides it (release workflow passes the
# tag). Must be run from the repo root.
set -euo pipefail

ARCH="${1:-$(uname -m)}"
case "$ARCH" in
  arm64)  GOARCH="arm64" ;;
  x86_64) GOARCH="amd64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

VERSION="${VERSION:-$(node -p "require('./package.json').version")}"
VERSION="v${VERSION#v}"

mkdir -p dist
CGO_ENABLED=0 GOOS=darwin GOARCH="$GOARCH" go -C apps/bridge-core build \
  -ldflags "-X main.version=${VERSION}" \
  -o ../../dist/bridge ./cmd/bridge

echo "Built dist/bridge ${VERSION} for darwin/$GOARCH"
ls -l dist/bridge
