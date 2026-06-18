#!/usr/bin/env bash
# Browser Bridge installer.
set -euo pipefail

ORG="dkisser"  # substituted at emit time
REPO="browser-bridge"
BB_VERSION="${BB_VERSION:-}"
BB_HOME="${BB_HOME:-$HOME/.browser-bridge}"
REPO_DIR="$BB_HOME/repo"

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

clone_source() {
  local version="$1"
  local src="${BB_GIT_REMOTE:-https://github.com/${ORG}/${REPO}.git}"
  if [[ -d "$BB_HOME/repo/.git" ]]; then
    info "Updating existing repo at $BB_HOME/repo"
    git -C "$BB_HOME/repo" fetch --depth 1 origin "$version" \
      || die "BB-E023: failed to fetch $version from $src"
    git -C "$BB_HOME/repo" reset --hard FETCH_HEAD \
      || die "BB-E024: failed to reset to $version"
  else
    info "Cloning $src (tag $version) into $BB_HOME/repo"
    git clone --depth 1 --branch "$version" "$src" "$BB_HOME/repo" \
      || die "BB-E024: clone failed"
  fi
  info "Installing dependencies"
  ( cd "$BB_HOME/repo" && bun install --frozen-lockfile ) \
    || die "BB-E025: bun install failed"
}
# Path to the bridge template, baked into install.sh via a heredoc at emit time.
BRIDGE_TEMPLATE_PATH="${BRIDGE_TEMPLATE_PATH:-$REPO_DIR/install/bridge.sh.tmpl}"

build_cli() {
  info "Building bridge command binary"
  ( cd "$REPO_DIR" && bun run build:cli ) \
    || die "BB-E026: failed to build bridge CLI"
  mkdir -p "$BB_HOME/bin"
  cp "$REPO_DIR/dist/bridge" "$BB_HOME/bin/bridge-cmd" \
    || die "BB-E027: failed to copy bridge command binary"
  chmod +x "$BB_HOME/bin/bridge-cmd"
}

write_artifacts() {
  local version="$1"
  mkdir -p "$BB_HOME/bin"
  info "Writing bridge to $BB_HOME/bin/bridge"
  sed -e "s|{{BRIDGE_VERSION}}|${version}|g" -e "s|{{ORG}}|${ORG}|g" "$BRIDGE_TEMPLATE_PATH" > "$BB_HOME/bin/bridge"
  chmod +x "$BB_HOME/bin/bridge"
  echo "$version" > "$BB_HOME/version"
  mkdir -p "$HOME/.local/bin"
  ln -sf "$BB_HOME/bin/bridge" "$HOME/.local/bin/bridge"
}

print_next_steps() {
  local version="$1"
  cat <<EOF

Browser Bridge ${version} installed.

Next steps:
  1. Ensure ~/.local/bin is on your PATH:
       export PATH="\$HOME/.local/bin:\$PATH"
  2. Open Chrome and load the unpacked extension from:
       $BB_HOME/extension/
     (chrome://extensions - enable Developer mode - "Load unpacked")
  3. Start the bridge:
       bridge up
  4. List connected browsers and control the browser:
       bridge browser:list
       bridge --browser <browserId> navigate https://example.com

To uninstall later: bridge uninstall --yes
EOF
}

resolve_version() {
  if [[ -n "$BB_VERSION" ]]; then
    [[ "$BB_VERSION" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "BB-E022: invalid version '$BB_VERSION'"
    # Normalize to always include the leading 'v' (GitHub tags always have it).
    echo "${BB_VERSION#v}" | awk '{print "v"$0}'
    return
  fi
  local url="https://api.github.com/repos/${ORG}/${REPO}/releases/latest"
  local tag
  tag=$(curl -fsSL "$url" | python3 -c "import sys,json; print(json.load(sys.stdin)['tag_name'])") \
    || die "BB-E021: failed to query latest release from $url"
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "BB-E022: latest tag '$tag' is not a valid version"
  echo "$tag"
}

download_extension() {
  local version="${1:-$(resolve_version)}"
  local base zipname
  if [[ "$ORG" =~ ^[0-9a-zA-Z.-]+:[0-9]+$ ]]; then
    base="http://${ORG}"
  else
    base="https://github.com/${ORG}/${REPO}/releases/download/${version}"
  fi
  zipname="browser-bridge-extension-${version}.zip"
  local tmpdir
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' RETURN
  info "Downloading $zipname"
  curl -fsSL "${base}/${zipname}" -o "${tmpdir}/${zipname}" \
    || die "BB-E021: download failed for ${base}/${zipname}"
  curl -fsSL "${base}/${zipname}.sha256" -o "${tmpdir}/${zipname}.sha256" \
    || die "BB-E021: download failed for ${base}/${zipname}.sha256"
  local expected actual
  expected=$(awk '{print $1}' "${tmpdir}/${zipname}.sha256")
  actual=$(shasum -a 256 "${tmpdir}/${zipname}" | awk '{print $1}')
  [[ "$expected" == "$actual" ]] || die "BB-E020: sha256 mismatch (expected $expected, got $actual)"
  if [[ -d "$BB_HOME/extension" ]] && [[ -n "$(ls -A "$BB_HOME/extension" 2>/dev/null)" ]]; then
    mv "$BB_HOME/extension" "$BB_HOME/extension.bak.$(date +%s)"
  fi
  mkdir -p "$BB_HOME/extension"
  unzip -q "${tmpdir}/${zipname}" -d "$BB_HOME/extension"
  rm -rf "$tmpdir"
  trap - RETURN
  info "Extension installed to $BB_HOME/extension"
}

main() {
  check_prereqs
  local version
  version=$(resolve_version)
  info "Installing Browser Bridge ${version}"
  download_extension "$version"
  clone_source "$version"
  build_cli
  write_artifacts "$version"
  print_next_steps "$version"
}

main "$@"
