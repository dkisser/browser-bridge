#!/usr/bin/env bash
# Build a self-contained install.sh release asset.
# Reads install/install.sh and embeds install/bridge.sh.tmpl and
# install/launchagent.plist.tmpl as heredocs after an "exit 0" line.
# NOTE: Any code in install.sh after "main \"\$@\"" will be unreachable in the
# self-contained build because the appended templates terminate with "exit 0".
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
out="${1:-$root/dist/install.sh}"

mkdir -p "$(dirname "$out")"
cp "$root/install/install.sh" "$out"
chmod +x "$out"

{
  echo ""
  echo "# Self-contained release installer templates follow."
  echo "exit 0"
  echo "__BB_TEMPLATE_BEGIN__"
  cat "$root/install/bridge.sh.tmpl"
  echo "__BB_TEMPLATE_END__"
  echo "__BB_LAUNCHAGENT_BEGIN__"
  cat "$root/install/launchagent.plist.tmpl"
  echo "__BB_LAUNCHAGENT_END__"
} >> "$out"
