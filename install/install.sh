#!/usr/bin/env bash
# Browser Bridge installer.
set -euo pipefail

ORG="${ORG:-dkisser}"  # substituted at emit time; env override enables testing/mirrors
REPO="${REPO:-browser-bridge}"
BB_VERSION="${BB_VERSION:-}"
BB_HOME="${BB_HOME:-$HOME/.browser-bridge}"

die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
info() { printf '%s\n' "$*"; }

# ---- BEGIN PREREQ ----
check_prereqs() {
  command -v bash >/dev/null || die "BB-E000: bash not found"
  (( BASH_VERSINFO[0] >= 4 )) || die "BB-E000: bash >= 4 required"
  command -v curl >/dev/null  || die "BB-E001: curl not found"
  command -v unzip >/dev/null || die "BB-E001: unzip not found"
  command -v shasum >/dev/null || die "BB-E001: shasum not found"
  command -v python3 >/dev/null || die "BB-E001: python3 not found"
  [[ -w "$HOME/.local" || ! -e "$HOME/.local" ]] || die "BB-E001: \$HOME/.local not writable"
}
# ---- END PREREQ ----

detect_arch() {
  if [[ -n "${BB_INSTALL_ARCH:-}" ]]; then
    echo "$BB_INSTALL_ARCH"
    return 0
  fi
  local arch
  arch=$(uname -m)
  case "$arch" in
    arm64)  echo "arm64" ;;
    x86_64) echo "x64"   ;;
    *) die "BB-E033: unsupported architecture '$arch'" ;;
  esac
}

# Path to the bridge template, baked into install.sh via a heredoc at emit time.
BRIDGE_TEMPLATE_PATH="${BRIDGE_TEMPLATE_PATH:-}"

fetch_bridge_template() {
  [[ -n "${BRIDGE_TEMPLATE_PATH:-}" && -f "$BRIDGE_TEMPLATE_PATH" ]] && return 0
  local tmpdir
  tmpdir=$(mktemp -d)
  local url="https://raw.githubusercontent.com/${ORG}/${REPO}/main/install/bridge.sh.tmpl"
  curl -fsSL "$url" -o "${tmpdir}/bridge.sh.tmpl" \
    || die "BB-E021: failed to fetch bridge template"
  BRIDGE_TEMPLATE_PATH="${tmpdir}/bridge.sh.tmpl"
}

write_artifacts() {
  local version="$1"
  fetch_bridge_template
  mkdir -p "$BB_HOME/bin"
  info "Writing bridge to $BB_HOME/bin/bridge"
  local tmp_bridge
  tmp_bridge=$(mktemp "$BB_HOME/bin/bridge.XXXXXX")
  sed -e "s|{{BRIDGE_VERSION}}|${version}|g" -e "s|{{ORG}}|${ORG}|g" -e "s|{{REPO}}|${REPO}|g" "$BRIDGE_TEMPLATE_PATH" > "$tmp_bridge"
  chmod +x "$tmp_bridge"
  mv "$tmp_bridge" "$BB_HOME/bin/bridge"
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

download_runtime() {
  local version="$1" arch="$2" base="$3"
  local tarball="browser-bridge-macos-${arch}-${version}.tar.gz"
  local tmpdir
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' RETURN

  info "Downloading $tarball"
  curl -fsSL "${base}/${tarball}" -o "${tmpdir}/${tarball}" \
    || die "BB-E028: download failed for ${base}/${tarball}"
  curl -fsSL "${base}/${tarball}.sha256" -o "${tmpdir}/${tarball}.sha256" \
    || die "BB-E028: download failed for ${base}/${tarball}.sha256"

  local expected actual
  expected=$(awk '{print $1}' "${tmpdir}/${tarball}.sha256")
  actual=$(shasum -a 256 "${tmpdir}/${tarball}" | awk '{print $1}')
  [[ "$expected" == "$actual" ]] || die "BB-E029: sha256 mismatch (expected $expected, got $actual)"

  info "Extracting runtime"
  mkdir -p "$BB_HOME"
  tar xzf "${tmpdir}/${tarball}" -C "$BB_HOME"

  local extracted="$BB_HOME/browser-bridge-macos-${arch}-${version}"
  [[ -d "$extracted/bin" ]] || die "BB-E032: tarball missing bin/ directory"
  [[ -x "$extracted/bin/ws-server" ]] || die "BB-E032: tarball missing ws-server binary"
  [[ -x "$extracted/bin/local-proxy" ]] || die "BB-E032: tarball missing local-proxy binary"
  [[ -x "$extracted/bin/bridge-cmd" ]] || die "BB-E032: tarball missing bridge-cmd binary"

  mkdir -p "$BB_HOME/bin"
  mv "$extracted/bin/ws-server" "$extracted/bin/local-proxy" "$extracted/bin/bridge-cmd" "$BB_HOME/bin/"
  rm -rf "$extracted"
  trap - RETURN
}

main() {
  check_prereqs
  local version
  version=$(resolve_version)
  info "Installing Browser Bridge ${version}"

  local base
  if [[ "$ORG" =~ ^[0-9a-zA-Z.-]+:[0-9]+$ ]]; then
    base="http://${ORG}"
  else
    base="https://github.com/${ORG}/${REPO}/releases/download/${version}"
  fi

  download_extension "$version"

  local arch
  arch=$(detect_arch)
  download_runtime "$version" "$arch" "$base"

  write_artifacts "$version"
  print_next_steps "$version"
}

main "$@"
