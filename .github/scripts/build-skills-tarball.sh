#!/usr/bin/env bash
# Build the skills tarball + sha256 sidecar for a release (ADR-0015).
# The tarball ships every skill as a top-level <name>/ directory — the layout
# install.sh extracts and installs, where install_skills walks the extract
# root's children. A shared wrapper directory is *not* the shape: install.sh
# hands install_skills the extract root itself, so a wrapper level would be one
# directory too many and the loop would find nothing.
set -euo pipefail

VERSION="${VERSION:-v0.0.0}"
NAME="browser-bridge-skills-${VERSION}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SKILLS_SRC="${SKILLS_SRC:-$root/skills}"

[[ -d "$SKILLS_SRC" ]] || { echo "Skill source is not a directory: $SKILLS_SRC" >&2; exit 1; }

# Every immediate subdirectory holding a SKILL.md is a shippable skill. The
# SKILL.md test is what keeps a stray directory (a notes file, a shared
# reference) out of the tarball: the installer would create a skill-shaped
# directory for it and agents would find an entry point with no frontmatter.
#
# This used to name one directory, skills/browser-bridge, which meant
# browser-bridge-memory was never released: it existed in the repo and was
# editable, and no user ever received it.
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

shipped=()
for skill_dir in "$SKILLS_SRC"/*/; do
  skill="$(basename "$skill_dir")"
  [[ -f "$skill_dir/SKILL.md" ]] || continue
  cp -R "$skill_dir" "$STAGE/$skill"
  shipped+=("$skill")
done

if [[ ${#shipped[@]} -eq 0 ]]; then
  echo "No skills found under $SKILLS_SRC (expected subdirectories with a SKILL.md)" >&2
  exit 1
fi

OUT_DIR="${OUT_DIR:-.}"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
TAR_PATH="$OUT_DIR/${NAME}.tar.gz"
SHA_PATH="${TAR_PATH}.sha256"

echo "shipping ${#shipped[@]} skill(s): ${shipped[*]}" >&2

# Names are passed bare from inside the staging dir, so members come out as
# browser-bridge/… rather than ./browser-bridge/…. (GNU tar's --anchored would
# say this explicitly; bsdtar on macOS has no such flag, and the bare names
# already give the same result on both.)
( cd "$STAGE" && tar czf "$TAR_PATH" "${shipped[@]}" )
shasum -a 256 "$TAR_PATH" | awk -v f="$(basename "$TAR_PATH")" '{print $1"  "f}' > "$SHA_PATH"

echo "$TAR_PATH"
echo "$SHA_PATH"
