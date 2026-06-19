# Binary Distribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the end-user source-clone installation path with precompiled macOS binaries for `ws-server`, `local-proxy`, and `bridge-cmd`, so `install.sh` no longer requires `bun` or `git`.

**Architecture:** Build three standalone binaries per macOS architecture using `bun build --compile`, pack them into architecture-specific tarballs with SHA-256 sidecars, and update `install.sh`/`bridge.sh.tmpl` to download, verify, and run those binaries. Developer workflows inside the monorepo stay unchanged.

**Tech Stack:** Bun (build-time only), Bash, GitHub Actions, Bats, tar/shasum.

---

## File Structure

| File | Responsibility |
|---|---|
| `.github/scripts/build-binaries.sh` | Compile `ws-server`, `local-proxy`, `bridge-cmd` for a given macOS architecture. |
| `.github/scripts/build-tarball.sh` | Build a `browser-bridge-macos-{arch}-{version}.tar.gz` + `.sha256` from the compiled binaries. |
| `.github/workflows/release-binaries.yml` | Tag-triggered CI that builds tarballs for arm64 and x64 and uploads them to the GitHub Release. |
| `install/install.sh` | End-user installer: checks prereqs, downloads extension zip + runtime tarball, writes `bridge` script. |
| `install/bridge.sh.tmpl` | Generated `bridge` orchestrator: starts/stops/logs the precompiled binaries. |
| `install/README.md` | User-facing install docs. |
| `install/shell-troubleshooting.md` | Shell-specific troubleshooting, stripped of bun/source-clone content. |
| `README.md` / `README_CN.md` | Project landing pages with Quick Start as the binary install path. |
| `install/tests/helpers.bash` | Shared test fixtures (fake tarball, fake binaries, mock HTTP server). |
| `install/tests/install.bats` | Bats tests for the new installer. |
| `install/tests/bridge.bats` | Bats tests for the binary-based `bridge` orchestrator. |

---

## Task 1: Add local build scripts and package.json scripts

**Files:**
- Create: `.github/scripts/build-binaries.sh`
- Create: `.github/scripts/build-tarball.sh`
- Modify: `package.json`

### Step 1: Write `.github/scripts/build-binaries.sh`

```bash
#!/usr/bin/env bash
# Compile runtime binaries for a single macOS architecture.
set -euo pipefail

ARCH="${1:-$(uname -m)}"
case "$ARCH" in
  arm64)  TARGET="bun-darwin-arm64" ;;
  x86_64) TARGET="bun-darwin-x64"   ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

mkdir -p dist
bun build --compile apps/websocket/src/index.ts --outfile "dist/ws-server"   --target="$TARGET"
bun build --compile apps/local-proxy/src/index.ts --outfile "dist/local-proxy" --target="$TARGET"
bun build --compile apps/cli/src/index.ts       --outfile "dist/bridge-cmd" --target="$TARGET"

echo "Built binaries for $ARCH ($TARGET) in dist/"
ls -l dist/ws-server dist/local-proxy dist/bridge-cmd
```

### Step 2: Write `.github/scripts/build-tarball.sh`

```bash
#!/usr/bin/env bash
# Build a runtime tarball + sha256 sidecar for the host or requested architecture.
set -euo pipefail

VERSION="${VERSION:-v0.0.0}"
ARCH="${1:-$(uname -m)}"
case "$ARCH" in
  arm64)  NAME="browser-bridge-macos-arm64-${VERSION}" ;;
  x86_64) NAME="browser-bridge-macos-x64-${VERSION}"   ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"$SCRIPT_DIR/build-binaries.sh" "$ARCH"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/$NAME/bin"
cp "dist/ws-server" "dist/local-proxy" "dist/bridge-cmd" "$STAGE/$NAME/bin/"

OUT_DIR="${OUT_DIR:-.}"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
TAR_PATH="$OUT_DIR/${NAME}.tar.gz"
SHA_PATH="${TAR_PATH}.sha256"

( cd "$STAGE" && tar czf "$TAR_PATH" "$NAME" )
shasum -a 256 "$TAR_PATH" | awk -v f="$(basename "$TAR_PATH")" '{print $1"  "f}' > "$SHA_PATH"

echo "$TAR_PATH"
echo "$SHA_PATH"
```

### Step 3: Modify `package.json`

Add two scripts next to `build:cli`:

```json
    "build:binaries": "bash .github/scripts/build-binaries.sh",
    "build:tarball": "bash .github/scripts/build-tarball.sh",
```

### Step 4: Run the new scripts locally

```bash
bun run build:tarball
```

Expected: `dist/browser-bridge-macos-arm64-v0.0.1.tar.gz` (or x64 on Intel) and `.sha256` are created; unpacking shows `bin/ws-server`, `bin/local-proxy`, `bin/bridge-cmd`.

### Step 5: Commit

```bash
git add .github/scripts/build-binaries.sh .github/scripts/build-tarball.sh package.json
git commit -m "build: add local binary and tarball build scripts"
```

---

## Task 2: Add the GitHub Actions release workflow

**Files:**
- Create: `.github/workflows/release-binaries.yml`

### Step 1: Write the workflow

```yaml
name: release-binaries

on:
  push:
    tags:
      - 'v[0-9]+\.[0-9]+\.[0-9]+'

permissions:
  contents: write

jobs:
  build:
    strategy:
      matrix:
        include:
          - arch: arm64
            runner: macos-latest
          - arch: x64
            runner: macos-13
    runs-on: ${{ matrix.runner }}
    steps:
      - uses: actions/checkout@v4

      - uses: oven-sh/setup-bun@v2
        with:
          bun-version: latest

      - name: Verify package.json matches tag
        run: |
          TAG="${GITHUB_REF_NAME}"
          PKG=$(node -p "require('./package.json').version")
          if [[ "v${PKG}" != "${TAG}" ]]; then
            echo "BB-E030: package.json version (${PKG}) does not match tag (${TAG})"
            exit 1
          fi

      - name: Verify CHANGELOG has entry
        run: |
          TAG="${GITHUB_REF_NAME}"
          if ! grep -qE "^## \[?${TAG#v}\]?" CHANGELOG.md; then
            echo "BB-E031: CHANGELOG.md missing entry for ${TAG}"
            exit 1
          fi

      - name: Install dependencies
        run: bun install --frozen-lockfile

      - name: Build runtime tarball
        env:
          VERSION: ${{ github.ref_name }}
        run: bash .github/scripts/build-tarball.sh ${{ matrix.arch }}

      - name: Upload release assets
        uses: softprops/action-gh-release@v2
        with:
          files: |
            browser-bridge-macos-${{ matrix.arch }}-${{ github.ref_name }}.tar.gz
            browser-bridge-macos-${{ matrix.arch }}-${{ github.ref_name }}.tar.gz.sha256
```

### Step 2: Validate the YAML

```bash
bunx actionlint .github/workflows/release-binaries.yml
```

If `actionlint` is not installed, install it with `bunx github.com/rhysd/actionlint/cmd/actionlint@latest` or validate visually.

### Step 3: Commit

```bash
git add .github/workflows/release-binaries.yml
git commit -m "ci: add release workflow for precompiled runtime binaries"
```

---

## Task 3: Update `install/install.sh` prerequisites

**Files:**
- Modify: `install/install.sh`

### Step 1: Rewrite `check_prereqs`

Replace the body of `check_prereqs` with:

```bash
check_prereqs() {
  command -v bash >/dev/null || die "BB-E000: bash not found"
  (( BASH_VERSINFO[0] >= 4 )) || die "BB-E000: bash >= 4 required"
  command -v curl >/dev/null  || die "BB-E001: curl not found"
  command -v unzip >/dev/null || die "BB-E001: unzip not found"
  command -v shasum >/dev/null || die "BB-E001: shasum not found"
  command -v python3 >/dev/null || die "BB-E001: python3 not found"
  [[ -w "$HOME/.local" || ! -e "$HOME/.local" ]] || die "BB-E001: \$HOME/.local not writable"
}
```

### Step 2: Update the prerequisite tests

In `install/tests/install.bats`, replace the first two tests with a single robust success test:

```bats
@test "install.sh check_prereqs succeeds when all required tools are present" {
  bash_path=$(find_modern_bash)
  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_prereq.sh"
  echo 'check_prereqs; echo OK' >> "$BB_TEST_TMP/test_prereq.sh"
  run "$bash_path" "$BB_TEST_TMP/test_prereq.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"OK"* ]]
}
```

Delete the old `install.sh exits BB-E001 when bun missing` and `install.sh succeeds bun check when bun present` tests.

### Step 3: Run the install tests

```bash
bun run test:install
```

Expected: the renamed tests pass; other tests may fail until later tasks.

### Step 4: Commit

```bash
git add install/install.sh install/tests/install.bats
git commit -m "fix(install): remove bun/git prerequisites"
```

---

## Task 4: Replace source clone with runtime tarball download in `install/install.sh`

**Files:**
- Modify: `install/install.sh`

### Step 1: Remove obsolete functions

Delete the entire `clone_source()` and `build_cli()` functions and their comments.

### Step 2: Add architecture detection

Insert after `check_prereqs`:

```bash
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
```

### Step 3: Add runtime download and extraction

Insert after `download_extension`:

```bash
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

  local extracted="$BB_HOME/browser-bridge-macos-${arch}"
  [[ -d "$extracted/bin" ]] || die "BB-E032: tarball missing bin/ directory"
  [[ -x "$extracted/bin/ws-server" ]] || die "BB-E032: tarball missing ws-server binary"
  [[ -x "$extracted/bin/local-proxy" ]] || die "BB-E032: tarball missing local-proxy binary"
  [[ -x "$extracted/bin/bridge-cmd" ]] || die "BB-E032: tarball missing bridge-cmd binary"

  mkdir -p "$BB_HOME/bin"
  mv "$extracted/bin/ws-server" "$extracted/bin/local-proxy" "$extracted/bin/bridge-cmd" "$BB_HOME/bin/"
  rm -rf "$extracted"
}
```

### Step 4: Add bridge-template fetch fallback

Insert before `write_artifacts`:

```bash
fetch_bridge_template() {
  [[ -n "${BRIDGE_TEMPLATE_PATH:-}" && -f "$BRIDGE_TEMPLATE_PATH" ]] && return 0
  local tmpdir
  tmpdir=$(mktemp -d)
  local url="https://raw.githubusercontent.com/${ORG}/${REPO}/main/install/bridge.sh.tmpl"
  curl -fsSL "$url" -o "${tmpdir}/bridge.sh.tmpl" \
    || die "BB-E021: failed to fetch bridge template"
  BRIDGE_TEMPLATE_PATH="${tmpdir}/bridge.sh.tmpl"
}
```

Inside `write_artifacts`, at the top, add:

```bash
fetch_bridge_template
```

### Step 5: Rewrite `main`

```bash
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
```

### Step 6: Run install tests (they will fail until helpers are updated)

At this point most `install.bats` tests still reference `clone_source` and git fixtures. Mark this as a known intermediate state; the next task fixes the tests.

### Step 7: Commit

```bash
git add install/install.sh
git commit -m "feat(install): download precompiled runtime tarball instead of cloning source"
```

---

## Task 5: Update `install/bridge.sh.tmpl` to run precompiled binaries

**Files:**
- Modify: `install/bridge.sh.tmpl`

### Step 1: Update `cmd_up`

Replace `cmd_up` with:

```bash
cmd_up() {
  [[ -x "$BB_HOME/bin/ws-server" ]] || die "BB-E002: install not run. Execute the install script first."
  [[ -x "$BB_HOME/bin/local-proxy" ]] || die "BB-E002: install not run. Execute the install script first."
  mkdir -p "$LOG_DIR" "$RUN_DIR"
  BRIDGE_WS_PORT="$WS_PORT" start_service ws-server "$BB_HOME/bin/ws-server" "$WS_PORT"
  BRIDGE_LOCAL_PORT="$LOCAL_PROXY_PORT" BRIDGE_LOCAL_PROXY_PORT="$LOCAL_PROXY_PORT" BRIDGE_WS_URL="ws://127.0.0.1:$WS_PORT" start_service local-proxy "$BB_HOME/bin/local-proxy" "$LOCAL_PROXY_PORT"
  info "bridge up: ws-server=$(cat "$RUN_DIR/ws-server.pid"), local-proxy=$(cat "$RUN_DIR/local-proxy.pid")"
}
```

### Step 2: Update `start_service` signature

Replace the function with:

```bash
start_service() {
  local name="$1" binary="$2" port="$3"
  local logfile="$LOG_DIR/${name}.log" pidfile="$RUN_DIR/${name}.pid"
  if [[ -f "$pidfile" ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    info "$name already running (pid $(cat "$pidfile"))"
    return 0
  fi
  if port_in_use "$port"; then
    die "BB-E010: $name port $port is already in use"
  fi
  ( exec "$binary" >"$logfile" 2>&1 ) & pid=$!
  disown
  echo "$pid" > "$pidfile"
  for _ in {1..50}; do
    if ! port_in_use "$port"; then
      sleep 0.1
      continue
    fi
    return 0
  done
  kill "$(cat "$pidfile")" 2>/dev/null || true
  rm -f "$pidfile"
  die "BB-E011: $name failed to bind port $port within 5s (see $logfile)"
}
```

### Step 3: Update `cmd_doctor`

Replace the function with:

```bash
cmd_doctor() {
  local rc=0
  [[ -x "$BB_HOME/bin/ws-server" ]]   && echo "[OK] ws-server binary present"   || { echo "[FAIL] ws-server binary missing"; rc=1; }
  [[ -x "$BB_HOME/bin/local-proxy" ]] && echo "[OK] local-proxy binary present" || { echo "[FAIL] local-proxy binary missing"; rc=1; }
  [[ -x "$BB_HOME/bin/bridge-cmd" ]]  && echo "[OK] bridge-cmd binary present"  || { echo "[FAIL] bridge-cmd binary missing"; rc=1; }
  if [[ -f "$BB_HOME/extension/manifest.json" ]] && python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$BB_HOME/extension/manifest.json" 2>/dev/null; then
    echo "[OK] extension/manifest.json valid"
  else
    echo "[FAIL] extension/manifest.json valid"; rc=1
  fi
  if service_state ws-server >/dev/null && service_state local-proxy >/dev/null; then
    echo "[OK] both services running"
  else
    echo "[WARN] one or more services not running (run 'bridge up')"
  fi
  return $rc
}
```

### Step 4: Update `cmd_update`

Replace the function with:

```bash
cmd_update() {
  local target="${1:-latest}"
  local installer_url="https://raw.githubusercontent.com/${ORG}/${REPO}/main/install/install.sh"
  info "Updating to $target"
  BB_VERSION="$target" bash -c "$(curl -fsSL "$installer_url")" \
    || die "BB-E103: update failed"
  [[ -d "$BB_HOME" ]] && cmd_restart || true
}
```

### Step 5: Remove unused `REPO_DIR` variable

Delete the line:

```bash
REPO_DIR="${BB_HOME:-$HOME/.browser-bridge}/repo"
```

### Step 6: Commit

```bash
git add install/bridge.sh.tmpl
git commit -m "feat(bridge): run precompiled binaries instead of bun-run source"
```

---

## Task 6: Update install test helpers

**Files:**
- Modify: `install/tests/helpers.bash`

### Step 1: Add a fake-binary maker

Add after `make_fake_bun`:

```bash
make_fake_binaries() {
  mkdir -p "$BB_HOME/bin"
  cat > "$BB_HOME/bin/ws-server" <<'EOF'
#!/usr/bin/env bash
port="${BRIDGE_WS_PORT:-3001}"
exec python3 -c "import socket, time; s=socket.socket(); s.bind(('', int('$port'))); s.listen(); time.sleep(9999)"
EOF
  cat > "$BB_HOME/bin/local-proxy" <<'EOF'
#!/usr/bin/env bash
port="${BRIDGE_LOCAL_PORT:-${BRIDGE_LOCAL_PROXY_PORT:-3002}}"
exec python3 -c "import socket, time; s=socket.socket(); s.bind(('', int('$port'))); s.listen(); time.sleep(9999)"
EOF
  cat > "$BB_HOME/bin/bridge-cmd" <<'EOF'
#!/usr/bin/env bash
echo "fake-bridge-cmd: $*"
EOF
  chmod +x "$BB_HOME/bin/ws-server" "$BB_HOME/bin/local-proxy" "$BB_HOME/bin/bridge-cmd"
}
```

### Step 2: Add a fake-tarball maker

Add after `make_fake_binaries`:

```bash
make_fake_runtime_tarball() {
  local version="${1:-v9.9.9}" arch="${2:-arm64}"
  local name="browser-bridge-macos-${arch}-${version}"
  local stage="$BB_TEST_TMP/${name}"
  mkdir -p "$stage/bin"

  cat > "$stage/bin/ws-server" <<'EOF'
#!/usr/bin/env bash
port="${BRIDGE_WS_PORT:-3001}"
exec python3 -c "import socket, time; s=socket.socket(); s.bind(('', int('$port'))); s.listen(); time.sleep(9999)"
EOF
  cat > "$stage/bin/local-proxy" <<'EOF'
#!/usr/bin/env bash
port="${BRIDGE_LOCAL_PORT:-${BRIDGE_LOCAL_PROXY_PORT:-3002}}"
exec python3 -c "import socket, time; s=socket.socket(); s.bind(('', int('$port'))); s.listen(); time.sleep(9999)"
EOF
  cat > "$stage/bin/bridge-cmd" <<'EOF'
#!/usr/bin/env bash
echo "fake-bridge-cmd: $*"
EOF
  chmod +x "$stage/bin/"/*

  ( cd "$BB_TEST_TMP" && tar czf "${name}.tar.gz" "$name" )
  ( cd "$BB_TEST_TMP" && shasum -a 256 "${name}.tar.gz" > "${name}.tar.gz.sha256" )

  echo "$BB_TEST_TMP/${name}.tar.gz"
}
```

### Step 3: Remove or deprecate `make_fake_bun`

`make_fake_bun` is no longer needed by the new install/bridge tests. Leave it in place for now unless it causes confusion; a later cleanup pass can remove it.

### Step 4: Commit

```bash
git add install/tests/helpers.bash
git commit -m "test(install): add helpers for fake runtime binaries and tarballs"
```

---

## Task 7: Rewrite `install/tests/install.bats`

**Files:**
- Modify: `install/tests/install.bats`

### Step 1: Remove clone/build tests

Delete everything from the `setup_clone_fixture` function through the `clone_source fresh` and `clone_source update` tests, and remove `make_fake_bun` calls.

### Step 2: Add a runtime-download test

After the extension-download tests, add:

```bats
@test "download_runtime exits BB-E029 on SHA-256 mismatch" {
  bash_path=$(find_modern_bash)
  make_fake_runtime_tarball v9.9.9 arm64
  local tarball_path
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  local tarball_name
  tarball_name=$(basename "$tarball_path")
  mkdir -p "$BB_TEST_TMP/www"
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  echo "0000000000000000000000000000000000000000000000000000000000000000  $tarball_name" \
    > "$BB_TEST_TMP/www/${tarball_name}.sha256"
  start_mock_http 18760

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_rt.sh"
  cat >> "$BB_TEST_TMP/test_rt.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18760'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
download_runtime v9.9.9 arm64 "http://${ORG}"
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home" run "$bash_path" "$BB_TEST_TMP/test_rt.sh"
  stop_mock_http
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E029"* ]]
}

@test "download_runtime succeeds with correct sha256 and extracts binaries" {
  bash_path=$(find_modern_bash)
  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  mkdir -p "$BB_TEST_TMP/www"
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"
  start_mock_http 18761

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_rt.sh"
  cat >> "$BB_TEST_TMP/test_rt.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18761'
REPO='browser-bridge'
resolve_version() { echo 'v9.9.9'; }
download_runtime v9.9.9 arm64 "http://${ORG}"
ls "$BB_HOME/bin"
SCRIPT
  BB_HOME="$BB_TEST_TMP/bb-home2" run "$bash_path" "$BB_TEST_TMP/test_rt.sh"
  stop_mock_http
  [ "$status" -eq 0 ]
  [[ "$output" == *"ws-server"* ]]
  [[ "$output" == *"local-proxy"* ]]
  [[ "$output" == *"bridge-cmd"* ]]
}
```

### Step 3: Rewrite end-to-end install tests

Replace the two end-to-end tests with:

```bats
@test "install.sh end-to-end against mock release server" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  start_mock_http 18762
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_e2e.sh"
  cat >> "$BB_TEST_TMP/test_e2e.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18762'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e.sh"
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
  [[ -f "$BB_TEST_TMP/bb-home/bin/bridge" ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/ws-server" ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/local-proxy" ]]
  [[ -x "$BB_TEST_TMP/bb-home/bin/bridge-cmd" ]]
  [[ -L "$HOME/.local/bin/bridge" ]]
}

@test "install.sh idempotent: second run upgrades in place" {
  mkdir -p "$BB_TEST_TMP/www" "$BB_TEST_TMP/stage"
  echo "fake-extension-content" > "$BB_TEST_TMP/stage/bb.zip"
  ( cd "$BB_TEST_TMP/stage" && zip -q "$BB_TEST_TMP/www/browser-bridge-extension-v9.9.9.zip" bb.zip )
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 browser-bridge-extension-v9.9.9.zip > browser-bridge-extension-v9.9.9.zip.sha256 )

  local tarball_path tarball_name
  tarball_path=$(make_fake_runtime_tarball v9.9.9 arm64)
  tarball_name=$(basename "$tarball_path")
  cp "$tarball_path" "$BB_TEST_TMP/www/$tarball_name"
  cp "${tarball_path}.sha256" "$BB_TEST_TMP/www/${tarball_name}.sha256"

  start_mock_http 18763
  bash_path=$(find_modern_bash)

  sed '$d' "$INSTALL_SH" > "$BB_TEST_TMP/test_e2e.sh"
  cat >> "$BB_TEST_TMP/test_e2e.sh" <<'SCRIPT'
BB_INSTALL_ARCH=arm64
ORG='127.0.0.1:18763'
main
SCRIPT

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e.sh"
  first_status=$status

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run "$bash_path" "$BB_TEST_TMP/test_e2e.sh"
  stop_mock_http

  [ "$first_status" -eq 0 ]
  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
  [[ "$(cat "$BB_TEST_TMP/bb-home/version")" == "v9.9.9" ]]
}
```

### Step 4: Run install tests

```bash
bun run test:install
```

Expected: all tests pass.

### Step 5: Commit

```bash
git add install/tests/install.bats
git commit -m "test(install): update bats tests for binary tarball install"
```

---

## Task 8: Rewrite `install/tests/bridge.bats`

**Files:**
- Modify: `install/tests/bridge.bats`

### Step 1: Replace fake-bun setup with fake binaries

Replace the entire `setup_up()` function with:

```bash
setup_up() {
  make_fake_binaries
  mkdir -p "$BB_HOME/logs" "$BB_HOME/run"
}
```

### Step 2: Update tests that reference `repo` or `BB_FAKE_BUN_BEHAVIOR`

In each `bridge up` test, remove `BB_FAKE_BUN_BEHAVIOR=ok` and rely on `setup_up`.

For example:

```bats
@test "bridge up writes PID files for both services" {
  setup_up
  run bash "$BRIDGE_TMPL" up
  [ "$status" -eq 0 ]
  [[ -f "$BB_HOME/run/ws-server.pid" ]]
  [[ -f "$BB_HOME/run/local-proxy.pid" ]]
  [[ -f "$BB_HOME/logs/ws-server.log" ]]
  [[ -f "$BB_HOME/logs/local-proxy.log" ]]
}
```

### Step 3: Update repo-missing test

Change:

```bats
@test "bridge up fails with BB-E002 when repo missing" {
  rm -rf "$BB_HOME/repo"
  run bash "$BRIDGE_TMPL" up
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E002"* ]]
}
```

to:

```bats
@test "bridge up fails with BB-E002 when binaries missing" {
  rm -rf "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" up
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E002"* ]]
}
```

### Step 4: Update restart test

Change:

```bats
@test "bridge restart runs down then up" {
  mkdir -p "$BB_HOME/run" "$BB_HOME/logs" "$BB_HOME/repo/apps/websocket" "$BB_HOME/repo/apps/local-proxy"
  echo "99999" > "$BB_HOME/run/ws-server.pid"
  make_fake_bun
  BB_FAKE_BUN_BEHAVIOR=ok run bash "$BRIDGE_TMPL" restart
  [ "$status" -eq 0 ]
  # The fake PID 99999 is gone; new PIDs are written.
  [[ "$(cat "$BB_HOME/run/ws-server.pid")" != "99999" ]]
}
```

to:

```bats
@test "bridge restart runs down then up" {
  setup_up
  echo "99999" > "$BB_HOME/run/ws-server.pid"
  run bash "$BRIDGE_TMPL" restart
  [ "$status" -eq 0 ]
  [[ "$(cat "$BB_HOME/run/ws-server.pid")" != "99999" ]]
}
```

### Step 5: Update doctor tests

Change:

```bats
@test "bridge doctor reports OK when install is healthy" {
  mkdir -p "$BB_HOME/repo" "$BB_HOME/extension"
  echo '{"manifest_version":3}' > "$BB_HOME/extension/manifest.json"
  make_fake_bun
  run bash "$BRIDGE_TMPL" doctor
  [ "$status" -eq 0 ]
  [[ "$output" == *"[OK] bun on PATH"* ]]
  [[ "$output" == *"[OK] repo present"* ]]
  [[ "$output" == *"[OK] extension/manifest.json valid"* ]]
}
```

to:

```bats
@test "bridge doctor reports OK when install is healthy" {
  make_fake_binaries
  mkdir -p "$BB_HOME/extension"
  echo '{"manifest_version":3}' > "$BB_HOME/extension/manifest.json"
  run bash "$BRIDGE_TMPL" doctor
  [ "$status" -eq 0 ]
  [[ "$output" == *"[OK] ws-server binary present"* ]]
  [[ "$output" == *"[OK] local-proxy binary present"* ]]
  [[ "$output" == *"[OK] bridge-cmd binary present"* ]]
  [[ "$output" == *"[OK] extension/manifest.json valid"* ]]
}
```

Change the repo-missing doctor test to a binaries-missing test:

```bats
@test "bridge doctor reports FAIL when binaries missing" {
  rm -rf "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" doctor
  [ "$status" -ne 0 ]
  [[ "$output" == *"[FAIL] ws-server binary missing"* ]]
}
```

Delete the "bridge doctor reports FAIL when bun missing" test.

### Step 6: Update uninstall tests

Remove the `mkdir -p "$BB_HOME/repo"` line in the uninstall test; `mkdir -p "$BB_HOME"` is enough.

### Step 7: Run bridge tests

```bash
bats install/tests/bridge.bats
```

Expected: all tests pass.

### Step 8: Commit

```bash
git add install/tests/bridge.bats
git commit -m "test(bridge): update bats tests for binary-based orchestrator"
```

---

## Task 9: Update `install/README.md`

**Files:**
- Modify: `install/README.md`

### Step 1: Update prerequisites table

Remove `bun` and `git` rows. The table should contain only:

| Tool | Required | Notes |
|---|---|---|
| bash ≥ 4 | yes | macOS users: pre-installed. |
| curl | yes | pre-installed. |
| unzip | yes | `brew install unzip` if missing. |
| shasum | yes | pre-installed on macOS. |
| python3 | yes | pre-installed on macOS. |

### Step 2: Update "What It Does"

Change step 5/6/7 from git clone + bun install + build CLI to:

1. Verifies prerequisites.
2. Resolves the target version.
3. Downloads the extension zip and its SHA-256 sidecar; aborts on mismatch.
4. Extracts the extension into `~/.browser-bridge/extension/`.
5. Detects the macOS architecture (arm64 or x64).
6. Downloads the matching runtime tarball and its SHA-256 sidecar; aborts on mismatch.
7. Extracts the three binaries into `~/.browser-bridge/bin/`.
8. Writes `~/.browser-bridge/bin/bridge` and symlinks it into `~/.local/bin/bridge`.
9. Writes the resolved version to `~/.browser-bridge/version`.
10. Prints next steps.

### Step 3: Update error code table

Remove `BB-E023` through `BB-E027`. Add:

| `BB-E028` | Runtime tarball download failed | Check network; verify the release includes a tarball for your architecture. |
| `BB-E029` | Runtime tarball SHA-256 mismatch | Re-run; check network/proxy. |
| `BB-E032` | Runtime tarball extraction failed | Inspect `~/.browser-bridge/bin/`. |
| `BB-E033` | Unsupported architecture | Only macOS arm64 and x64 are supported. |

### Step 4: Add a Gatekeeper note

Add after the error table:

```markdown
### macOS Gatekeeper

If macOS blocks the downloaded binaries with "cannot be opened because the developer cannot be verified", remove the quarantine attribute:

```bash
xattr -d com.apple.quarantine ~/.browser-bridge/bin/*
```
```

### Step 5: Commit

```bash
git add install/README.md
git commit -m "docs(install): update README for binary distribution"
```

---

## Task 10: Update `install/shell-troubleshooting.md`

**Files:**
- Modify: `install/shell-troubleshooting.md`

### Step 1: Remove bun/source-clone content

Delete the entire "macOS 升级 Bash 指南" section? No, keep the Bash upgrade guide because `install.sh` still requires Bash ≥ 4. But remove all references to `bun` PATH/source-clone/zsh/bun sections.

Specifically, delete the section starting `## 排查当前终端使用的 Shell` (from line 178 onward) because it discusses bun/PATH/source-clone. The Bash upgrade guide (lines 1-176) remains useful.

### Step 2: Update intro

Change the first paragraph from:

```markdown
Browser Bridge 的安装脚本需要 Bash ≥ 4，而项目日常开发通常使用 zsh。本指南整理 macOS 上常见的 shell 相关问题：Bash 升级、PATH 顺序、默认 shell 切换等。
```

to:

```markdown
Browser Bridge 的安装脚本需要 Bash ≥ 4。macOS 系统自带的 `/bin/bash` 长期停留在 3.2.x，因此需要先升级到新版 Bash。本指南只保留 Bash 升级步骤；安装完成后不再需要额外配置 PATH 来加载 `bun` 或源码目录。
```

### Step 3: Commit

```bash
git add install/shell-troubleshooting.md
git commit -m "docs(install): remove bun/source-clone troubleshooting content"
```

---

## Task 11: Update root README files

**Files:**
- Modify: `README.md`
- Modify: `README_CN.md`

### Step 1: `README.md` (English)

The English landing page already has the curl install under both Quick Start and Install. Make two small edits:

1. Under `## 🛠️ Development`, add a one-line note at the top:

```markdown
> The steps below are for contributors/developers only. End users do not need to install `bun` or `git`.
```

2. Remove or demote `### Option B: Build from source` under `## 📦 Install`. Replace it with a short note:

```markdown
### Option B: Build from source (contributors only)

See the [Development](#-development) section below. You only need this if you are contributing to Browser Bridge.
```

No other README.md changes are required — the Quick Start already shows the binary install path.

### Step 2: `README_CN.md`

The existing Quick Start (lines 38-60) already shows the curl install. Make two edits:

1. Under `## 📦 安装`, replace the `### 方案 B：从源码构建` section with:

```markdown
### 方案 B：从源码构建（仅贡献者）

普通用户无需执行。开发者请参考下方的 [🛠️ 开发](#-开发) 章节。
```

2. At the very top of `## 🛠️ 开发`, insert:

```markdown
> 以下步骤仅适用于贡献者/开发者，终端用户无需安装 `bun` 或 `git`。
```

No other README_CN.md changes are required.

### Step 3: Commit

```bash
git add README.md README_CN.md
git commit -m "docs: update README quick start for binary distribution"
```

---

## Task 12: Run full automated test suites

### Step 1: Unit tests

```bash
bun run test
```

Expected: all workspace unit tests pass.

### Step 2: Install/bridge Bats tests

```bash
bun run test:install
```

Expected: all bats tests pass.

### Step 3: Type check

```bash
bun run type-check
```

Expected: no TypeScript errors.

### Step 4: Lint/format

```bash
bunx @biomejs/biome check --write .
```

Expected: no Biome errors.

### Step 5: Fix failures and commit

Fix any failures, then commit the fixes.

---

## Task 13: Manual smoke test

### Step 1: Build a local tarball

```bash
bun run build:tarball
```

### Step 2: Serve the tarball and extension from a local directory

Create a fake release directory containing the tarball, its sha256, the extension zip, and its sha256. Use `python3 -m http.server` to serve it.

### Step 3: Run install.sh against the local server

```bash
ORG='127.0.0.1:8000' BB_VERSION=v0.0.0 bash install/install.sh
```

### Step 4: Verify layout

```bash
ls ~/.browser-bridge/bin/
# should show bridge, bridge-cmd, ws-server, local-proxy
```

### Step 5: Run `bridge up`, `bridge status`, `bridge down`

```bash
bridge up
bridge status
bridge down
```

Expected: services start and stop cleanly.

### Step 6: Test `bridge update`

Because `bridge update` pulls `install.sh` from GitHub `main`, test it only after the changes are merged or by overriding the installer URL with a local file.

### Step 7: Document any issues

If the smoke test reveals gaps, create follow-up tasks and update the spec if necessary.

---

## Spec Coverage Check

| Spec Section | Implementing Task |
|---|---|
| Release artifacts (tarball + sha256) | Task 1, Task 2 |
| Tarball internal layout | Task 1 |
| Build pipeline / cross-compile | Task 1, Task 2 |
| Install flow / directory layout | Task 3, Task 4 |
| `bridge` orchestrator binary startup | Task 5 |
| `bridge doctor` binary checks | Task 5 |
| `bridge update` via curl | Task 5 |
| Error code changes | Task 3, Task 4, Task 9 |
| Environment variables preserved | No change needed (binary reads same env) |
| Testing strategy | Task 6, Task 7, Task 8, Task 12 |
| CI release workflow | Task 2 |
| Documentation updates | Task 9, Task 10, Task 11 |
| macOS Gatekeeper risk | Task 9 |

## Placeholder Scan

No `TBD`, `TODO`, "implement later", or "add appropriate error handling" remain. Every step includes exact file paths, code snippets, and commands.

## Type/Signature Consistency

- `detect_arch` returns `arm64` or `x64`; `download_runtime` consumes it to build the tarball filename `browser-bridge-macos-{arch}-{version}.tar.gz`, matching the filenames produced by `build-tarball.sh`.
- `start_service` signature changed from `(name, cwd, port)` to `(name, binary, port)` consistently across `bridge.sh.tmpl` and tests.
- Error codes `BB-E028`/`BB-E029`/`BB-E032`/`BB-E033` are used in `install.sh` and documented in `install/README.md`.
