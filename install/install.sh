#!/usr/bin/env bash
# Browser Bridge installer.
set -euo pipefail

ORG="${ORG:-dkisser}"  # substituted at emit time; env override enables testing/mirrors
REPO="${REPO:-browser-bridge}"
BB_VERSION="${BB_VERSION:-}"
BB_HOME="${BB_HOME:-$HOME/.browser-bridge}"
BB_EXTENSION_DIR="${BB_EXTENSION_DIR:-$HOME/Browser-Bridge}"

# Skills installation options (also configurable via environment variables).
BB_SKILLS_TARGET_DIR="${BB_SKILLS_TARGET_DIR:-}"  # destination agent skills directory
BB_WITH_SKILLS="${BB_WITH_SKILLS:-false}"          # deprecated no-op: skills install is the default
BB_NO_SKILLS="${BB_NO_SKILLS:-false}"              # explicitly skip skills installation

# Auto-start options (macOS only).
BB_AUTOSTART="${BB_AUTOSTART:-true}"               # enable login auto-start on macOS by default

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

# The single runtime binary (`bridge`, a Go build — CLI + service lifecycle +
# hidden `serve` control-plane subcommand, ADR-0012/ADR-0013) arrives in the
# runtime tarball. The bash router template and the LaunchAgent plist template
# are gone: `bridge` is the Go binary itself and the plist template is
# embedded in it (go:embed), so install.sh only records the version and
# maintains the PATH symlink.
write_artifacts() {
  local version="$1"
  mkdir -p "$BB_HOME"
  echo "$version" > "$BB_HOME/version"
  mkdir -p "$HOME/.local/bin"
  ln -sf "$BB_HOME/bin/bridge" "$HOME/.local/bin/bridge"
}

print_next_steps() {
  local version="$1" skills_note="" autostart_note=""
  # Always render a line about skills so the user never has to infer the
  # outcome from upstream warnings. SKILLS_STATUS drives which line; empty
  # SKILLS_STATUS (e.g. when sourced in isolation by a BATS test) renders
  # nothing, preserving the existing "omits the skills note" assertion for
  # the unset case.
  case "${SKILLS_STATUS:-}" in
    installed)
      # Newline-separated paths render one per indented line so $HOME with
      # embedded spaces or a path that's a prefix of another doesn't blur
      # the boundary between targets.
      local indented=""
      while IFS= read -r dir; do
        [[ -n "$dir" ]] && indented+="    ${dir}"$'\n'
      done <<< "${SKILLS_INSTALLED_DIRS:-}"
      skills_note=$'  Installed the browser-bridge skill into:\n'"${indented}"$'\n  (available the next time you start your agent).\n'
      ;;
    failed)
      skills_note=$'  Skills installation was attempted but failed; continuing without skills. Re-run with --skills-dir <path> to retry once the issue is resolved.\n'
      ;;
    skipped-opt-out)
      skills_note=$'  Skills: skipped (--no-skills).\n'
      ;;
    skipped-no-target)
      skills_note=$'  Skills: skipped — neither ~/.agents nor ~/.claude exists. Re-run with --skills-dir <path> to install.\n'
      ;;
  esac
  # AUTOSTART gates the user-visible message independently of the host
  # platform; macOS is the only platform that actually enables login
  # auto-start today, but users on other platforms who run with
  # AUTOSTART=true (e.g. the BATS suite under make_fake_uname Linux) still
  # want the post-install summary to surface the setting they chose. The
  # main() guard above is what prevents the actual launchd bootstrap on
  # non-darwin hosts.
  if [[ "${AUTOSTART:-true}" == "true" ]]; then
    autostart_note=$'  Login auto-start is enabled; bridge services will start automatically when you log in.\n'
  fi
  printf '\nBrowser Bridge %s installed.\n%s%s' "$version" "$skills_note" "$autostart_note"
  cat <<EOF
Next steps:
  1. Ensure ~/.local/bin is on your PATH:
       export PATH="\$HOME/.local/bin:\$PATH"
  2. Open Chrome and load the unpacked extension from:
       $BB_EXTENSION_DIR/extension/
     (chrome://extensions - enable Developer mode - "Load unpacked")
  3. List connected browsers and control the browser:
       bridge browser:list
       bridge --browser <browserId> navigate https://example.com

Bridge services are already running. To stop them: bridge service down
To uninstall later: bridge service uninstall --yes
EOF
}

print_install_help() {
  cat <<'EOF'
Usage: install.sh [options]

Options:
  --skills-dir <dir> Install skills into <dir> only, instead of the default
                     targets (~/.agents/skills/ and ~/.claude/skills/; each is
                     used only when its parent directory already exists).
  --with-skills      Deprecated no-op kept for compatibility: skills are
                     installed by default now.
  --no-skills        Skip installing skills.
  --no-autostart     Do not enable macOS login auto-start (macOS only;
                     equivalent to running 'bridge service disable').
  --force            Reinstall even if the target version is already installed.
  --help, -h         Show this help message.

Environment variables:
  BB_VERSION              Install a specific release version (default: latest).
  BB_HOME                 Installation prefix for runtime internals (default: ~/.browser-bridge).
  BB_EXTENSION_DIR        Visible directory for the Chrome extension symlink (default: ~/Browser-Bridge).
  BB_FORCE                Set to "true" to enable --force.
  BB_AUTOSTART            Set to "false" to disable login auto-start on macOS.
  BB_SKILLS_TARGET_DIR    Same as --skills-dir.
  BB_WITH_SKILLS          Deprecated no-op (skills are installed by default).
  BB_NO_SKILLS            Set to "true" to explicitly skip skills.
  ORG, REPO               GitHub org/repo used for downloads.

Examples:
  Install bridge, extension, and skills (skills are installed by default):
    install.sh

  Install without skills:
    install.sh --no-skills

  Install skills into a custom directory only:
    install.sh --skills-dir ~/.my-agent/skills
EOF
}

resolve_version() {
  # "latest" — also what `bridge [service] update` passes from older
  # releases — means "query the GitHub API", same as an unset BB_VERSION.
  if [[ -n "$BB_VERSION" && "$BB_VERSION" != "latest" ]]; then
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

  mkdir -p "$BB_EXTENSION_DIR"
  if [[ ! -L "$BB_EXTENSION_DIR/extension" ]] || [[ "$(readlink "$BB_EXTENSION_DIR/extension")" != "$BB_HOME/extension" ]]; then
    rm -rf "$BB_EXTENSION_DIR/extension"
    ln -s "$BB_HOME/extension" "$BB_EXTENSION_DIR/extension"
  fi
  info "Extension exposed at $BB_EXTENSION_DIR/extension"

  rm -rf "$tmpdir"
  trap - RETURN
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
  # One Go binary ships (ADR-0013): bridge (CLI + service lifecycle + the
  # hidden `serve` control-plane subcommand). bridge-cmd is retired.
  [[ -x "$extracted/bin/bridge" ]] || die "BB-E032: tarball missing bridge binary"

  # Migration: when upgrading from a pre-merge install, the old ws-server
  # and local-proxy binaries are still in $BB_HOME/bin/ and the old config
  # points at the pre-merge processes. Drop them and force-reinitialize the
  # config so the next bridge-core start produces a fresh browserId, fresh
  # pairing hash, and the user is prompted to re-pair the extension. See
  # ADR-0011.
  rm -f "$BB_HOME/bin/ws-server" "$BB_HOME/bin/local-proxy" 2>/dev/null || true
  # Go rewrite (ADR-0012): the TS bridge-cmd is superseded by the Go `bridge`
  # binary below, and the LaunchAgent plist template moved into the binary
  # (go:embed) — drop both leftovers.
  rm -f "$BB_HOME/bin/bridge-cmd" "$BB_HOME/launchagent.plist.tmpl" 2>/dev/null || true
  # Single-binary merge (ADR-0013): the standalone bridge-core binary from
  # the two-Go-binary era is superseded by `bridge serve`; drop the leftover
  # so a stale daemon binary cannot linger in $BB_HOME/bin.
  rm -f "$BB_HOME/bin/bridge-core" 2>/dev/null || true
  # bridge-core resolves its config dir from BB_HOME, falling back to
  # ~/.browser-bridge. Remove both: the current location, and the default
  # location a pre-merge build always used (which is a different path when
  # BB_HOME is a custom prefix, so the re-pair would otherwise not happen).
  rm -f "$BB_HOME/config.json" "$HOME/.browser-bridge/config.json" 2>/dev/null || true

  mkdir -p "$BB_HOME/bin"
  mv "$extracted/bin/bridge" "$BB_HOME/bin/"
  rm -rf "$extracted"
  trap - RETURN
}

# Default skill targets (ADR-0015): ~/.agents/skills (Kimi Code) and
# ~/.claude/skills (Claude Code). A target is eligible only when its PARENT
# directory (~/.agents resp. ~/.claude) already exists — [[ -d ]] follows
# symlinks on purpose — so the installer never creates a config root for an
# agent the user does not have. The skills/ subdir itself is mkdir -p'd at
# install time. Prints the eligible targets, one per line.
detect_default_skills_dirs() {
  local parent
  for parent in "$HOME/.agents" "$HOME/.claude"; do
    if [[ -d "$parent" ]]; then
      echo "$parent/skills"
    fi
  done
}

install_skills() {
  local src="$1" dest="$2"
  [[ -d "$src" ]] || die "BB-E200: skills source directory not found: $src"
  [[ -n "$dest" ]] || die "BB-E201: skills destination directory not specified"
  # Explicit `|| die` on mkdir/cp: main() runs download_skills (and thus this
  # function) in a subshell as an `if` condition, which suspends errexit, so
  # unguarded commands would otherwise fail silently.
  mkdir -p "$dest" || die "BB-E210: could not create skills destination directory: $dest"

  local installed=0
  if [[ -f "$src/SKILL.md" ]]; then
    local name
    name=$(basename "$src")
    # Defensive: if both $src/SKILL.md and a same-named subdirectory with
    # its own SKILL.md exist, the tarball extracted without its wrapper —
    # the skill files would end up nested inside the wrapper, and
    # basename($src) would point at the mktemp dir ("extract"), so the
    # install would silently copy the skill to $dest/extract/, where no
    # agent would find it. Detect it explicitly.
    local nested_skill_dir=""
    for inner in "$src"/*/; do
      [[ -d "$inner" ]] || continue
      if [[ -f "$inner/SKILL.md" ]]; then
        nested_skill_dir="$(basename "$inner")"
        break
      fi
    done
    if [[ -n "$nested_skill_dir" ]]; then
      die "BB-E211: skills tarball is missing its top-level wrapper directory (saw both $src/SKILL.md and $src/$nested_skill_dir/SKILL.md)."
    fi
    rm -rf "${dest}/${name}"
    cp -R "$src" "${dest}/${name}" || die "BB-E208: failed to install skill $name into $dest"
    info "Installed skill: $name"
    installed=1
  else
    for skill_dir in "$src"/*/; do
      [[ -d "$skill_dir" ]] || continue
      [[ -f "$skill_dir/SKILL.md" ]] || continue
      local name
      name=$(basename "$skill_dir")
      rm -rf "${dest}/${name}"
      cp -R "$skill_dir" "${dest}/${name}" || die "BB-E208: failed to install skill $name into $dest"
      info "Installed skill: $name"
      installed=$((installed + 1))
    done
  fi

  [[ "$installed" -gt 0 ]] || die "BB-E202: no valid skills found in $src"
  info "Skills installed to $dest"
}

download_skills() {
  local version="$1" dest="$2" base
  if [[ "$ORG" =~ ^[0-9a-zA-Z.-]+:[0-9]+$ ]]; then
    base="http://${ORG}"
  else
    base="https://github.com/${ORG}/${REPO}/releases/download/${version}"
  fi

  local tarball="browser-bridge-skills-${version}.tar.gz"
  local tmpdir
  tmpdir=$(mktemp -d)
  # EXIT (not RETURN) trap: download_skills may die() — e.g. BB-E203 on a
  # 404, BB-E204 on sha256 mismatch, BB-E211 on a malformed tarball — and
  # die() exits the shell, which means a RETURN trap would never fire and
  # the mktemp dir would leak. EXIT fires on every shell exit including
  # die(), and is idempotent (re-cleaning an already-removed tmpdir is a
  # no-op).
  trap 'rm -rf "$tmpdir"' EXIT

  info "Downloading $tarball"
  curl -fsSL "${base}/${tarball}" -o "${tmpdir}/${tarball}" \
    || die "BB-E203: download failed for ${base}/${tarball}"
  curl -fsSL "${base}/${tarball}.sha256" -o "${tmpdir}/${tarball}.sha256" \
    || die "BB-E203: download failed for ${base}/${tarball}.sha256"

  local expected actual
  expected=$(awk '{print $1}' "${tmpdir}/${tarball}.sha256")
  actual=$(shasum -a 256 "${tmpdir}/${tarball}" | awk '{print $1}')
  [[ "$expected" == "$actual" ]] || die "BB-E204: sha256 mismatch (expected $expected, got $actual)"

  mkdir -p "$tmpdir/extract"
  tar xzf "${tmpdir}/${tarball}" -C "$tmpdir/extract"
  # Validate the tarball shape: install_skills expects the extract root to
  # be a collection directory whose only child is the wrapper (e.g.
  # browser-bridge/). A wrapper-less tarball (just SKILL.md at the top,
  # shipped directly without its wrapper dir) would extract as a single
  # file and install_skills would silently copy it to $dest/<basename of
  # the extract root>, where no agent would ever find it. Catch it here
  # where we still know the tarball's URL.
  local -a entries=()
  local entry
  for entry in "$tmpdir/extract"/*; do
    [[ -e "$entry" ]] && entries+=("$entry")
  done
  if [[ ${#entries[@]} -eq 0 ]]; then
    die "BB-E211: skills tarball is empty: ${base}/${tarball}"
  fi
  if [[ ${#entries[@]} -ne 1 || ! -d "${entries[0]}" ]]; then
    die "BB-E211: skills tarball is missing its top-level wrapper directory: ${base}/${tarball} (saw ${#entries[@]} entries at the root; expected exactly one wrapper directory)"
  fi
  install_skills "$tmpdir/extract" "$dest"
  # Done; clean up explicitly. The EXIT trap also fires on normal return
  # for safety, but the rm -rf is idempotent.
  rm -rf "$tmpdir"
  trap - EXIT
}

parse_install_args() {
  SKILLS_TARGET_DIR="${BB_SKILLS_TARGET_DIR:-}"
  NO_SKILLS="${BB_NO_SKILLS:-false}"
  FORCE="${BB_FORCE:-false}"
  AUTOSTART="${BB_AUTOSTART:-true}"
  # SKILLS_STATUS is one of: "installed" / "failed" /
  # "skipped-opt-out" / "skipped-no-target" / "unknown" (the empty default
  # before main() runs). SKILLS_INSTALLED_DIRS is the newline-separated list
  # of skill targets that actually received the browser-bridge skill when
  # SKILLS_STATUS="installed", and empty otherwise. Both are read by
  # print_next_steps to render the post-install summary.
  SKILLS_STATUS=""
  SKILLS_INSTALLED_DIRS=""
  # --with-skills / BB_WITH_SKILLS predate default-on skills (ADR-0015):
  # accepted as no-ops so old scripts / muscle memory keep working. Tracking
  # this state lets us emit a single deprecation note at the end of the arg
  # loop, gated on the user NOT also opting out via --no-skills.
  SAW_DEPRECATED_SKILLS_OPT=0
  [[ "${BB_WITH_SKILLS:-false}" == "true" ]] && SAW_DEPRECATED_SKILLS_OPT=1

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --skills-dir)
        [[ -n "${2:-}" ]] || die "BB-E205: --skills-dir requires a directory argument"
        SKILLS_TARGET_DIR="$2"
        shift 2
        ;;
      --with-skills)
        SAW_DEPRECATED_SKILLS_OPT=1
        shift
        ;;
      --no-skills)
        NO_SKILLS=true
        shift
        ;;
      --no-autostart)
        AUTOSTART=false
        shift
        ;;
      --force)
        FORCE=true
        shift
        ;;
      --help|-h)
        print_install_help
        exit 0
        ;;
      *)
        die "BB-E206: unknown option '$1'. Run 'install.sh --help' for usage."
        ;;
    esac
  done

  # Single deprecation notice at the end of arg parsing, gated on the user
  # NOT opting out via --no-skills. Printing "use --no-skills to opt out"
  # while --no-skills has just been set (e.g. `install.sh --with-skills
  # --no-skills`) reads as "we ignored your opt-out".
  if [[ "$SAW_DEPRECATED_SKILLS_OPT" -eq 1 && "$NO_SKILLS" != "true" ]]; then
    info "Note: --with-skills (and BB_WITH_SKILLS) are deprecated; skills are installed by default (use --no-skills to opt out)."
  fi
}

# Publish the platform the install is running on to the bridge binary so
# `bridge service enable|up` walks the correct path even when the local
# binary was cross-compiled for the other OS (the BATS suite runs Linux-
# compiled binaries under make_fake_uname Darwin, and vice versa).
# BB_TESTING=1 unlocks the BB_GOOS override inside the binary — production
# installs on the native OS leave BB_GOOS unset and pick runtime.GOOS.
publish_platform_env() {
  case "$(uname -s)" in
    Darwin) export BB_GOOS=darwin ;;
    Linux)  export BB_GOOS=linux ;;
  esac
  export BB_TESTING=1
}

# wait_for_supervisor polls up to ~5s for the supervisor's pidfile to land
# under $BB_HOME/run/supervisor.pid. Returns 0 when the file appears; 1
# when the supervisor never came up (port conflict, etc.). The detached
# `bridge service up --foreground` process owns its own lifecycle, so the
# only signal we can observe from install.sh is the pidfile — which is
# also what `bridge service status` and the launchd path check, so this
# keeps the install contract consistent across platforms.
wait_for_supervisor() {
  local home="$1" waited=0
  while (( waited < 50 )); do
    [[ -f "$home/run/supervisor.pid" ]] && return 0
    sleep 0.1
    waited=$(( waited + 1 ))
  done
  return 1
}

main() {
  parse_install_args "$@"
  check_prereqs
  publish_platform_env
  local version
  version=$(resolve_version)
  info "Installing Browser Bridge ${version}"

  local current_version=""
  if [[ -f "$BB_HOME/version" ]]; then
    current_version=$(cat "$BB_HOME/version" 2>/dev/null || true)
  fi
  if [[ "$FORCE" != "true" ]] && [[ -n "$current_version" ]] && [[ "$current_version" == "$version" ]]; then
    info "Browser Bridge ${version} is already installed and up to date."
    exit 0
  fi

  local base
  if [[ "$ORG" =~ ^[0-9a-zA-Z.-]+:[0-9]+$ ]]; then
    base="http://${ORG}"
  else
    base="https://github.com/${ORG}/${REPO}/releases/download/${version}"
  fi

  if [[ -x "$BB_HOME/bin/bridge" ]] && "$BB_HOME/bin/bridge" service status >/dev/null 2>&1; then
    info "Stopping existing bridge services before update..."
    "$BB_HOME/bin/bridge" service down >/dev/null 2>&1 || true
  fi

  # Skills are installed by default (ADR-0015). The step is best-effort and
  # never fails the overall install: without an explicit --skills-dir only
  # agent config roots that already exist are targeted (neither present →
  # skip with a note), and a failed download/install (e.g. a pinned release
  # that predates the skills tarball) degrades to a warning. SKILLS_STATUS
  # summarizes the outcome for print_next_steps.
  SKILLS_INSTALLED_DIRS=""
  if [[ "$NO_SKILLS" == "true" ]]; then
    SKILLS_STATUS="skipped-opt-out"
  elif [[ -n "$SKILLS_TARGET_DIR" ]]; then
    # Explicit --skills-dir: single target, exactly as requested.
    if ( download_skills "$version" "$SKILLS_TARGET_DIR" ); then
      SKILLS_INSTALLED_DIRS="$SKILLS_TARGET_DIR"
      SKILLS_STATUS="installed"
    else
      info "Warning: skills installation failed; continuing without skills."
      SKILLS_STATUS="failed"
    fi
  else
    local dest_dirs=()
    while IFS= read -r line; do
      [[ -n "$line" ]] && dest_dirs+=("$line")
    done < <(detect_default_skills_dirs)
    if [[ ${#dest_dirs[@]} -eq 0 ]]; then
      info "Note: neither ~/.agents nor ~/.claude exists; skipping skills installation (pass --skills-dir to choose a location)."
      SKILLS_STATUS="skipped-no-target"
    else
      local any_installed=0
      local dest_dir
      for dest_dir in "${dest_dirs[@]}"; do
        if ( download_skills "$version" "$dest_dir" ); then
          any_installed=1
          if [[ -z "$SKILLS_INSTALLED_DIRS" ]]; then
            SKILLS_INSTALLED_DIRS="$dest_dir"
          else
            SKILLS_INSTALLED_DIRS="${SKILLS_INSTALLED_DIRS}"$'\n'"${dest_dir}"
          fi
        else
          info "Warning: skills installation into $dest_dir failed; continuing."
        fi
      done
      if [[ "$any_installed" -eq 1 ]]; then
        SKILLS_STATUS="installed"
      else
        SKILLS_STATUS="failed"
      fi
    fi
  fi

  download_extension "$version"

  local arch
  arch=$(detect_arch)
  download_runtime "$version" "$arch" "$base"

  write_artifacts "$version"

  if [[ "$(uname -s)" == "Darwin" ]] && [[ "$AUTOSTART" == "true" ]] && [[ -x "$BB_HOME/bin/bridge" ]]; then
    info "Enabling login auto-start..."
    if "$BB_HOME/bin/bridge" service enable >/dev/null 2>&1; then
      info "Login auto-start enabled."
    else
      info "Could not enable login auto-start. Run 'bridge service enable' manually."
    fi
  fi

  if [[ "$(uname -s)" == "Darwin" ]] && [[ -x "$BB_HOME/bin/bridge" ]]; then
    info "Starting bridge services..."
    if "$BB_HOME/bin/bridge" service up >/dev/null 2>&1; then
      info "Bridge services started."
    else
      info "Bridge services could not auto-start (ports may be in use). Run 'bridge service up' manually."
    fi
  elif [[ -x "$BB_HOME/bin/bridge" ]]; then
    # Non-darwin: the launchd bootstrap path is unavailable, so we run the
    # supervisor in the foreground mode but detach it (nohup + & + disown)
    # so install.sh returns immediately and the supervisor outlives it. The
    # supervisor writes run/supervisor.pid and spawns `bridge serve`, which
    # writes run/bridge-core.pid — the same pidfile pair the launchd path
    # produces, so the post-install contract is identical across platforms.
    info "Starting bridge supervisor..."
    mkdir -p "$BB_HOME/run" "$BB_HOME/logs" 2>/dev/null || true
    nohup "$BB_HOME/bin/bridge" service up --foreground >>"$BB_HOME/logs/supervisor.log" 2>&1 &
    disown || true
    # The supervisor exits immediately with BB-E010 / BB-E011 when the
    # control-plane port is already held; without this poll the user
    # would only see the failure on the next install or via the log.
    if ! wait_for_supervisor "$BB_HOME"; then
      info "Bridge services could not auto-start (ports may be in use). Run 'bridge service up' manually."
    fi
  fi

  print_next_steps "$version"
}

main "$@"
