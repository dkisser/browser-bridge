#!/usr/bin/env bash
# Build a self-contained install.sh release asset.
# Reads install/install.sh and embeds install/bridge.sh.tmpl as a heredoc.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
out="${1:-$root/dist/install.sh}"

mkdir -p "$(dirname "$out")"
cp "$root/install/install.sh" "$out"
chmod +x "$out"

{
  echo ""
  echo "# Self-contained release installer template follows."
  echo "exit 0"
  echo "__BB_TEMPLATE_BEGIN__"
  cat "$root/install/bridge.sh.tmpl"
  echo "__BB_TEMPLATE_END__"
} >> "$out"
