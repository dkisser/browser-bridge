#!/usr/bin/env bash
set -euo pipefail

file=$(jq -r '.tool_response.filePath // .tool_input.file_path')

echo "$(date '+%Y-%m-%d %H:%M:%S') format hook: $file" >> /tmp/claude-hook-check.txt

case "$file" in
  *.ts|*.tsx|*.js|*.jsx|*.json|*.jsonc)
    ./node_modules/.bin/biome format --write "$file" >/dev/null 2>&1 || true
    ;;
  *)
    # Nothing to format
    ;;
esac
