#!/usr/bin/env bash
# Build the skills tarball + sha256 sidecar for a release (ADR-0015).
# The tarball ships the usage skill as a single top-level browser-bridge/
# directory — the layout install.sh's download_skills extracts and installs.
set -euo pipefail

VERSION="${VERSION:-v0.0.0}"
NAME="browser-bridge-skills-${VERSION}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SKILLS_SRC="${SKILLS_SRC:-$root/skills/browser-bridge}"

[[ -f "$SKILLS_SRC/SKILL.md" ]] || { echo "Skill source missing SKILL.md: $SKILLS_SRC" >&2; exit 1; }

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$SKILLS_SRC" "$STAGE/browser-bridge"

OUT_DIR="${OUT_DIR:-.}"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
TAR_PATH="$OUT_DIR/${NAME}.tar.gz"
SHA_PATH="${TAR_PATH}.sha256"

( cd "$STAGE" && tar czf "$TAR_PATH" browser-bridge )
shasum -a 256 "$TAR_PATH" | awk -v f="$(basename "$TAR_PATH")" '{print $1"  "f}' > "$SHA_PATH"

echo "$TAR_PATH"
echo "$SHA_PATH"
