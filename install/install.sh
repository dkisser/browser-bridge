#!/usr/bin/env bash
# Browser Bridge installer.
set -euo pipefail

ORG="{{ORG}}"  # substituted at emit time
REPO="browser-bridge"
BB_VERSION="${BB_VERSION:-}"

die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
info() { printf '%s\n' "$*"; }

# ---- BEGIN PREREQ ----
check_prereqs() {
  command -v bash >/dev/null || die "BB-E000: bash not found"
  (( BASH_VERSINFO[0] >= 4 )) || die "BB-E000: bash >= 4 required"
  command -v curl >/dev/null  || die "BB-E001: curl not found"
  command -v unzip >/dev/null || die "BB-E001: unzip not found"
  [[ -w "$HOME/.local" || ! -e "$HOME/.local" ]] || die "BB-E001: \$HOME/.local not writable"
  command -v bun >/dev/null || die "BB-E001: bun not found. Install from https://bun.sh"
  command -v git >/dev/null || die "BB-E001: git not found"
}
# ---- END PREREQ ----

# Stub functions; replaced by later tasks.
resolve_version() { echo "v0.0.0"; }
download_extension() { :; }
clone_source() { :; }
write_artifacts() { :; }
print_next_steps() { :; }

main() {
  check_prereqs
  resolve_version
  download_extension
  clone_source
  write_artifacts
  print_next_steps
}

main "$@"
