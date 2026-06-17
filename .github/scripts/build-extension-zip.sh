#!/usr/bin/env bash
# Build the Browser Bridge Chrome Extension zip + sha256 sidecar.
set -euo pipefail

VERSION="${VERSION:-v0.0.0}"
NAME="browser-bridge-extension-${VERSION}"
DIST="apps/extension/dist"

if [[ ! -d "$DIST" ]]; then
  echo "Building extension..."
  bun run build:extension
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/$NAME"
cp -R "$DIST/." "$STAGE/$NAME/"

OUT_DIR="${OUT_DIR:-.}"
# Resolve to an absolute path so the subshell `cd "$STAGE"` below
# does not redirect output into the temp staging dir (which is trap-cleaned).
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
ZIP_PATH="$OUT_DIR/${NAME}.zip"
SHA_PATH="${ZIP_PATH}.sha256"

( cd "$STAGE" && zip -qr "$ZIP_PATH" "$NAME" )
( cd "$STAGE" && shasum -a 256 "$ZIP_PATH" | awk -v f="$(basename "$ZIP_PATH")" '{print $1"  "f}' > "$SHA_PATH" )

echo "$ZIP_PATH"
echo "$SHA_PATH"
