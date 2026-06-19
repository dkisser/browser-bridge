#!/usr/bin/env bash
# Build a runtime tarball + sha256 sidecar for the host or requested architecture.
set -euo pipefail

VERSION="${VERSION:-v0.0.0}"
ARCH="${1:-$(uname -m)}"
case "$ARCH" in
  arm64)  NAME="browser-bridge-macos-arm64-${VERSION}" ;;
  x86_64) NAME="browser-bridge-macos-x64-${VERSION}"   ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"$SCRIPT_DIR/build-binaries.sh" "$ARCH"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/$NAME/bin"
cp "dist/ws-server" "dist/local-proxy" "dist/bridge-cmd" "$STAGE/$NAME/bin/"

OUT_DIR="${OUT_DIR:-.}"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
TAR_PATH="$OUT_DIR/${NAME}.tar.gz"
SHA_PATH="${TAR_PATH}.sha256"

( cd "$STAGE" && tar czf "$TAR_PATH" "$NAME" )
shasum -a 256 "$TAR_PATH" | awk -v f="$(basename "$TAR_PATH")" '{print $1"  "f}' > "$SHA_PATH"

echo "$TAR_PATH"
echo "$SHA_PATH"
