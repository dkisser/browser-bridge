#!/usr/bin/env bash
# Build the install.sh release asset.
# Since ADR-0012 the installer needs no embedded templates: `bridge` is a Go
# binary from the runtime tarball and the LaunchAgent plist template is
# embedded in it (go:embed). This script is a straight copy kept so the
# release workflow has a stable entry point.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
out="${1:-$root/dist/install.sh}"

mkdir -p "$(dirname "$out")"
cp "$root/install/install.sh" "$out"
chmod +x "$out"
