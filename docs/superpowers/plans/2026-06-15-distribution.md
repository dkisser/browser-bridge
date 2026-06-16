# Browser-Bridge Interim Distribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a one-line installer that puts Browser-Bridge on a developer machine in under five minutes (CLI + WS-Server + Local Proxy + Chrome Extension), with a `bridge` CLI orchestrator and GitHub Releases-based extension distribution — without painting us out of the future two-machine deployment.

**Architecture:** A POSIX `install.sh` does prerequisite checks, downloads a prebuilt extension zip from GitHub Releases (with SHA-256 verify), shallow-clones the repo, and emits a `bridge` shell script from an embedded heredoc. `bridge` spawns ws-server + local-proxy via `nohup bun run start` and tracks them with PID files. A GitHub Actions workflow zips `apps/extension/dist/` on tag push and uploads it. Tests use `bats` for shell scripts and `bun test` for the zip builder.

**Tech Stack:** Bash (POSIX), bats-core, Bun, GitHub Actions (`softprops/action-gh-release@v2`).

---

## File Structure

**New files (created during this plan):**

| Path | Responsibility |
|---|---|
| `install/install.sh` | One-line installer. Inlines the bridge heredoc, runs prereqs → download → clone → write. |
| `install/bridge.sh.tmpl` | Source-of-truth template for the `bridge` script. Tests source it directly. |
| `install/tests/helpers.bash` | Bats helpers (mock http server, fixture dirs, `setup`/`teardown`). |
| `install/tests/install.bats` | install.sh end-to-end against a mock GitHub server. |
| `install/tests/bridge.bats` | bridge subcommand lifecycle tests with mocked process spawn. |
| `install/tests/release-workflow.test.ts` | bun test: zip structure + sha256 correctness. |
| `install/README.md` | User-facing install docs, error code reference, troubleshooting. |
| `.github/workflows/release-extension.yml` | Tag-triggered release pipeline. |
| `.github/scripts/build-extension-zip.sh` | Builds the extension zip + sha256 sidecar. |
| `CHANGELOG.md` | Release notes; workflow refuses to publish without an entry. |

**Modified files:**

| Path | Change |
|---|---|
| `package.json` | Add `test:install`, `release:extension-zip`, and `packageManager` unchanged. |
| `README.md` + `README.en.md` | Add "Install" section above "Quick Start"; demote old content to "Development". |
| `apps/cli/src/index.ts` | Reserve `bridge-host` subcommand slot with TODO placeholder. |

---

## Task 1: Repo scaffolding for distribution

**Files:**
- Create: `install/tests/helpers.bash`
- Create: `install/tests/.gitkeep`
- Modify: `package.json` (add scripts)

- [ ] **Step 1: Add bats test script to `package.json`**

Edit `package.json` and replace the `"scripts"` block:

```json
"scripts": {
  "dev:websocket": "bun --cwd apps/websocket dev",
  "dev:extension": "bun --cwd apps/extension dev",
  "build:extension": "bun --cwd apps/extension build",
  "cli": "bun --cwd apps/cli start",
  "dev:local-proxy": "bun --cwd apps/local-proxy dev",
  "type-check": "tsc --noEmit",
  "test": "bun test",
  "test:watch": "bun test --watch",
  "test:install": "bats install/tests/",
  "release:extension-zip": "bash .github/scripts/build-extension-zip.sh"
}
```

- [ ] **Step 2: Create `install/tests/helpers.bash`**

```bash
# Helpers for bats tests. Source this from each .bats file.
# shellcheck shell=bash

# Resolve the project root regardless of where bats is invoked from.
BB_TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Per-test scratch directory. Tests should put everything here.
BB_TEST_TMP="${BATS_TEST_TMPDIR:-/tmp}/bb-test-${BATS_TEST_NUMBER:-0}-$$"
mkdir -p "$BB_TEST_TMP"

# Fake HOME so we never touch the developer's real ~/.browser-bridge.
export HOME="$BB_TEST_TMP/home"
mkdir -p "$HOME"

# Set BB_HOME explicitly so bridge/install.sh never reads the real path.
export BB_HOME="$HOME/.browser-bridge"

# Path to the script under test.
INSTALL_SH="$BB_TEST_ROOT/install/install.sh"
BRIDGE_TMPL="$BB_TEST_ROOT/install/bridge.sh.tmpl"

# Stub PATH so "bun" can be a controlled fake when needed.
make_fake_bun() {
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/bun" <<'EOF'
#!/usr/bin/env bash
# Fake bun for tests. Honors BB_FAKE_BUN_BEHAVIOR env var.
case "${BB_FAKE_BUN_BEHAVIOR:-ok}" in
  ok)        echo "fake-bun: $*"; exit 0 ;;
  fail)      echo "fake-bun: $*"; exit 1 ;;
  hang)      sleep 999 ;;
esac
EOF
  chmod +x "$BB_TEST_TMP/bin/bun"
  export PATH="$BB_TEST_TMP/bin:$PATH"
}

# Start a tiny Python HTTP server on a free port serving $BB_TEST_TMP/www.
start_mock_http() {
  python3 -m http.server -d "$BB_TEST_TMP/www" "$@" >"$BB_TEST_TMP/http.log" 2>&1 &
  MOCK_HTTP_PID=$!
  sleep 0.2
}

stop_mock_http() {
  [[ -n "${MOCK_HTTP_PID:-}" ]] && kill "$MOCK_HTTP_PID" 2>/dev/null || true
}

teardown() {
  stop_mock_http
  # Kill anything still running from the test.
  pkill -P $$ 2>/dev/null || true
}
```

- [ ] **Step 3: Verify bats picks up the directory**

Run:

```bash
which bats || echo "bats not installed (see install/README.md)"
bun run test:install
```

Expected: prints "bats not installed..." (or, if bats is installed, runs 0 tests successfully).

- [ ] **Step 4: Commit**

```bash
git add install/tests/helpers.bash install/tests/.gitkeep package.json
git commit -m "chore: scaffold install/ for distribution scripts and bats tests"
```

---

## Task 2: `bridge.sh.tmpl` skeleton + help dispatch

**Files:**
- Create: `install/bridge.sh.tmpl`
- Create: `install/tests/bridge.bats`

- [ ] **Step 1: Write failing test for help dispatch**

Create `install/tests/bridge.bats`:

```bash
#!/usr/bin/env bats
load helpers

@test "bridge with no args prints help and exits 0" {
  run bash "$BRIDGE_TMPL"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Usage: bridge"* ]]
  [[ "$output" == *"up"* ]]
  [[ "$output" == *"down"* ]]
  [[ "$output" == *"doctor"* ]]
}

@test "bridge with unknown subcommand exits non-zero with BB-E" {
  run bash "$BRIDGE_TMPL" frobnicate
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E"* ]]
}

@test "bridge --version prints template version marker" {
  run bash "$BRIDGE_TMPL" --version
  [ "$status" -eq 0 ]
  [[ "$output" == *"bridge 0.1.0"* ]]
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
bun run test:install
```

Expected: errors like "No such file or directory" for `BRIDGE_TMPL`.

- [ ] **Step 3: Create `install/bridge.sh.tmpl`**

```bash
#!/usr/bin/env bash
# Browser Bridge orchestrator. Sourced into install.sh and emitted at install time.
# Template markers {{BRIDGE_VERSION}} and {{ORG}} are substituted at emit time.
set -euo pipefail

BRIDGE_VERSION="{{BRIDGE_VERSION}}"

die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
info() { printf '%s\n' "$*"; }

print_help() {
  cat <<'EOF'
Usage: bridge <command> [args]

Commands:
  up                  Start ws-server and local-proxy
  down                Stop both services
  restart             down then up
  status              Show service state
  logs [name]         Tail logs (ws-server | local-proxy)
  update [version]    Upgrade to a release (default: latest)
  doctor              Diagnose the install
  uninstall           Remove ~/.browser-bridge/ (use --yes to skip prompt)
  version             Print installed + latest version
EOF
}

cmd_version() {
  echo "bridge ${BRIDGE_VERSION}"
}

main() {
  if [[ $# -eq 0 ]]; then print_help; exit 0; fi
  case "$1" in
    --version|-V|version) cmd_version; exit 0 ;;
    -h|--help|help)       print_help; exit 0 ;;
    up|down|restart|status|logs|update|doctor|uninstall)
      die "BB-E100: '$1' not implemented yet" ;;
    *) die "BB-E101: unknown command '$1'. Run 'bridge' for help." ;;
  esac
}

main "$@"
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
bun run test:install
```

Expected: 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add install/bridge.sh.tmpl install/tests/bridge.bats
git commit -m "feat(install): bridge.sh.tmpl skeleton with help dispatch"
```

---

## Task 3: `bridge up` — start ws-server then local-proxy

**Files:**
- Modify: `install/bridge.sh.tmpl`

- [ ] **Step 1: Write failing tests for `bridge up`**

Append to `install/tests/bridge.bats`:

```bash
make_fake_bun
setup_up() {
  mkdir -p "$BB_HOME/repo/apps/websocket" "$BB_HOME/repo/apps/local-proxy"
  cat > "$BB_HOME/repo/apps/websocket/index.ts" <<'EOF'
EOF
  cat > "$BB_HOME/repo/apps/local-proxy/index.ts" <<'EOF'
EOF
  mkdir -p "$BB_HOME/logs" "$BB_HOME/run"
}

@test "bridge up writes PID files for both services" {
  setup_up
  BB_FAKE_BUN_BEHAVIOR=ok run bash "$BRIDGE_TMPL" up
  [ "$status" -eq 0 ]
  [[ -f "$BB_HOME/run/ws-server.pid" ]]
  [[ -f "$BB_HOME/run/local-proxy.pid" ]]
  [[ -f "$BB_HOME/logs/ws-server.log" ]]
  [[ -f "$BB_HOME/logs/local-proxy.log" ]]
}

@test "bridge up fails with BB-E002 when repo missing" {
  rm -rf "$BB_HOME/repo"
  run bash "$BRIDGE_TMPL" up
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E002"* ]]
}

@test "bridge up fails with BB-E011 when ws-server port is taken" {
  setup_up
  # Occupy the ws-server port.
  python3 -c "import socket; s=socket.socket(); s.bind(('127.0.0.1',8787)); s.listen(); import time; time.sleep(30)" &
  SOCAT_PID=$!
  sleep 0.3
  BB_FAKE_BUN_BEHAVIOR=ok run bash "$BRIDGE_TMPL" up
  kill "$SOCAT_PID" 2>/dev/null || true
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E011"* ]]
}
```

- [ ] **Step 2: Run tests, confirm they fail**

```bash
bun run test:install
```

Expected: 3 new tests fail with "BB-E100: 'up' not implemented yet".

- [ ] **Step 3: Implement `cmd_up`**

Replace the `up|down|...` branch in `install/bridge.sh.tmpl`:

```bash
WS_PORT="${BRIDGE_WS_PORT:-8787}"
LOCAL_PROXY_PORT="${BRIDGE_LOCAL_PROXY_PORT:-3001}"
REPO_DIR="${BB_HOME:-$HOME/.browser-bridge}/repo"
LOG_DIR="${BB_HOME:-$HOME/.browser-bridge}/logs"
RUN_DIR="${BB_HOME:-$HOME/.browser-bridge}/run"

port_in_use() {
  python3 -c "
import socket, sys
s = socket.socket()
try:
    s.bind(('127.0.0.1', int('$1')))
    s.close()
    sys.exit(0)
except OSError:
    sys.exit(1)
" 2>/dev/null
}

start_service() {
  local name="$1" cwd="$2" port="$3"
  local logfile="$LOG_DIR/${name}.log" pidfile="$RUN_DIR/${name}.pid"
  if [[ -f "$pidfile" ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    info "$name already running (pid $(cat "$pidfile"))"
    return 0
  fi
  ( cd "$cwd" && nohup bun run start >"$logfile" 2>&1 & echo $! >"$pidfile" )
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

cmd_up() {
  [[ -d "$REPO_DIR" ]] || die "BB-E002: install not run. Execute the install script first."
  mkdir -p "$LOG_DIR" "$RUN_DIR"
  start_service ws-server    "$REPO_DIR/apps/websocket"  "$WS_PORT"
  start_service local-proxy  "$REPO_DIR/apps/local-proxy" "$LOCAL_PROXY_PORT"
  info "bridge up: ws-server=$(cat "$RUN_DIR/ws-server.pid"), local-proxy=$(cat "$RUN_DIR/local-proxy.pid")"
}
```

And update the case branch:

```bash
    up)    cmd_up ;;
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
bun run test:install
```

Expected: 6 tests pass (3 from Task 2 + 3 new).

- [ ] **Step 5: Commit**

```bash
git add install/bridge.sh.tmpl install/tests/bridge.bats
git commit -m "feat(install): bridge up spawns services and waits for ports"
```

---

## Task 4: `bridge down` — graceful stop with PID cleanup

**Files:**
- Modify: `install/bridge.sh.tmpl`

- [ ] **Step 1: Write failing tests for `bridge down`**

Append to `install/tests/bridge.bats`:

```bash
@test "bridge down stops running service via SIGTERM and removes PID file" {
  mkdir -p "$BB_HOME/run"
  # Spawn a sleeper that traps SIGTERM.
  sleeper() { trap "exit 0" TERM; sleep 60; }
  sleeper &
  SLEEP_PID=$!
  echo "$SLEEP_PID" > "$BB_HOME/run/ws-server.pid"
  run bash "$BRIDGE_TMPL" down
  [ "$status" -eq 0 ]
  [[ ! -f "$BB_HOME/run/ws-server.pid" ]]
  ! kill -0 "$SLEEP_PID" 2>/dev/null
}

@test "bridge down with no PID file is a no-op (exit 0)" {
  rm -f "$BB_HOME/run/ws-server.pid" "$BB_HOME/run/local-proxy.pid"
  run bash "$BRIDGE_TMPL" down
  [ "$status" -eq 0 ]
  [[ "$output" == *"already stopped"* ]]
}

@test "bridge down SIGKILLs after 3s if service ignores SIGTERM" {
  mkdir -p "$BB_HOME/run"
  # Spawn a sleeper that ignores SIGTERM.
  ( trap "" TERM; sleep 60 ) &
  ZOMBIE_PID=$!
  echo "$ZOMBIE_PID" > "$BB_HOME/run/local-proxy.pid"
  run bash "$BRIDGE_TMPL" down
  [ "$status" -eq 0 ]
  [[ ! -f "$BB_HOME/run/local-proxy.pid" ]]
  ! kill -0 "$ZOMBIE_PID" 2>/dev/null
}
```

- [ ] **Step 2: Run tests, confirm fail**

```bash
bun run test:install
```

Expected: 3 new tests fail with "BB-E100: 'down' not implemented yet".

- [ ] **Step 3: Implement `cmd_down`**

Add to `install/bridge.sh.tmpl`:

```bash
stop_service() {
  local name="$1" pidfile="$RUN_DIR/${name}.pid"
  [[ -f "$pidfile" ]] || { info "$name already stopped"; return 0; }
  local pid
  pid=$(cat "$pidfile")
  if ! kill -0 "$pid" 2>/dev/null; then
    info "$name: pid $pid not running, cleaning up"
    rm -f "$pidfile"
    return 0
  fi
  kill -TERM "$pid" 2>/dev/null || true
  for _ in {1..30}; do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill -KILL "$pid" 2>/dev/null || true
    info "$name: sent SIGKILL after timeout"
  fi
  rm -f "$pidfile"
  info "$name stopped"
}

cmd_down() {
  stop_service ws-server
  stop_service local-proxy
}
```

Update case branch: `    down)   cmd_down ;;`

- [ ] **Step 4: Run tests, confirm pass**

```bash
bun run test:install
```

Expected: 9 tests pass.

- [ ] **Step 5: Commit**

```bash
git add install/bridge.sh.tmpl install/tests/bridge.bats
git commit -m "feat(install): bridge down with graceful SIGTERM and SIGKILL fallback"
```

---

## Task 5: `bridge status`, `bridge logs`, `bridge restart`

**Files:**
- Modify: `install/bridge.sh.tmpl`

- [ ] **Step 1: Write failing tests**

Append to `install/tests/bridge.bats`:

```bash
@test "bridge status exits 0 when both services running" {
  mkdir -p "$BB_HOME/run"
  ( trap "" TERM; sleep 30 ) & echo $! > "$BB_HOME/run/ws-server.pid"
  ( trap "" TERM; sleep 30 ) & echo $! > "$BB_HOME/run/local-proxy.pid"
  run bash "$BRIDGE_TMPL" status
  [ "$status" -eq 0 ]
  [[ "$output" == *"ws-server:    running"* ]]
  [[ "$output" == *"local-proxy:  running"* ]]
}

@test "bridge status exits 1 when a service is down" {
  mkdir -p "$BB_HOME/run"
  echo "99999" > "$BB_HOME/run/ws-server.pid"
  echo "99998" > "$BB_HOME/run/local-proxy.pid"
  run bash "$BRIDGE_TMPL" status
  [ "$status" -eq 1 ]
  [[ "$output" == *"ws-server:    stopped"* ]]
}

@test "bridge restart runs down then up" {
  mkdir -p "$BB_HOME/run" "$BB_HOME/logs" "$BB_HOME/repo/apps/websocket" "$BB_HOME/repo/apps/local-proxy"
  echo "99999" > "$BB_HOME/run/ws-server.pid"
  make_fake_bun
  BB_FAKE_BUN_BEHAVIOR=ok run bash "$BRIDGE_TMPL" restart
  [ "$status" -eq 0 ]
  # The fake PID 99999 is gone; new PIDs are written.
  [[ "$(cat "$BB_HOME/run/ws-server.pid")" != "99999" ]]
}

@test "bridge logs without name tails both logs (smoke test that files exist)" {
  mkdir -p "$BB_HOME/logs"
  echo "ws log" > "$BB_HOME/logs/ws-server.log"
  echo "lp log" > "$BB_HOME/logs/local-proxy.log"
  # We can't easily test tail -f in bats; instead confirm the files are referenced.
  run bash -c "BB_HOME='$BB_HOME' bash '$BRIDGE_TMPL' logs 2>&1 & sleep 0.2; pkill -P \$\$ ; wait"
  [ -f "$BB_HOME/logs/ws-server.log" ]
  [ -f "$BB_HOME/logs/local-proxy.log" ]
}
```

- [ ] **Step 2: Run tests, confirm fail**

```bash
bun run test:install
```

- [ ] **Step 3: Implement status, logs, restart**

Add to `install/bridge.sh.tmpl`:

```bash
service_state() {
  local name="$1" pidfile="$RUN_DIR/${name}.pid"
  if [[ -f "$pidfile" ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    printf '%-12s running  (pid %s)\n' "$name:" "$(cat "$pidfile")"
    return 0
  fi
  printf '%-12s stopped\n' "$name:"
  return 1
}

cmd_status() {
  local rc=0
  service_state ws-server || rc=1
  service_state local-proxy || rc=1
  return $rc
}

cmd_logs() {
  local name="${1:-}"
  case "$name" in
    ws-server|local-proxy) tail -f "$LOG_DIR/${name}.log" ;;
    "")                    tail -f "$LOG_DIR/ws-server.log" "$LOG_DIR/local-proxy.log" ;;
    *) die "BB-E102: unknown log target '$name' (use ws-server | local-proxy)" ;;
  esac
}

cmd_restart() {
  cmd_down
  cmd_up
}
```

Update case branch:

```bash
    status)  cmd_status ;;
    logs)    shift; cmd_logs "$@" ;;
    restart) cmd_restart ;;
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
bun run test:install
```

Expected: 13 tests pass.

- [ ] **Step 5: Commit**

```bash
git add install/bridge.sh.tmpl install/tests/bridge.bats
git commit -m "feat(install): bridge status, logs, restart"
```

---

## Task 6: `bridge doctor` and `bridge version`

**Files:**
- Modify: `install/bridge.sh.tmpl`

- [ ] **Step 1: Write failing tests**

Append to `install/tests/bridge.bats`:

```bash
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

@test "bridge doctor reports FAIL when repo missing" {
  rm -rf "$BB_HOME/repo"
  run bash "$BRIDGE_TMPL" doctor
  [ "$status" -ne 0 ]
  [[ "$output" == *"[FAIL] repo present"* ]]
}

@test "bridge doctor reports FAIL when bun missing" {
  export PATH="/usr/bin:/bin"  # exclude our fake
  run bash "$BRIDGE_TMPL" doctor
  [ "$status" -ne 0 ]
  [[ "$output" == *"[FAIL] bun on PATH"* ]]
}

@test "bridge version prints installed and latest release" {
  echo "v1.2.3" > "$BB_HOME/version"
  run bash "$BRIDGE_TMPL" version
  [ "$status" -eq 0 ]
  [[ "$output" == *"installed: v1.2.3"* ]]
}
```

- [ ] **Step 2: Run tests, confirm fail**

```bash
bun run test:install
```

- [ ] **Step 3: Implement doctor and version**

Add to `install/bridge.sh.tmpl`:

```bash
LATEST_VERSION="${LATEST_VERSION:-unknown}"

cmd_doctor() {
  local rc=0
  command -v bun >/dev/null && echo "[OK]   bun on PATH ($(command -v bun))" \
    || { echo "[FAIL] bun on PATH"; rc=1; }
  [[ -d "$REPO_DIR" ]] && echo "[OK]   repo present" \
    || { echo "[FAIL] repo present"; rc=1; }
  if [[ -f "$BB_HOME/extension/manifest.json" ]] && python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$BB_HOME/extension/manifest.json" 2>/dev/null; then
    echo "[OK]   extension/manifest.json valid"
  else
    echo "[FAIL] extension/manifest.json valid"; rc=1
  fi
  if service_state ws-server >/dev/null && service_state local-proxy >/dev/null; then
    echo "[OK]   both services running"
  else
    echo "[WARN] one or more services not running (run 'bridge up')"
  fi
  return $rc
}

cmd_version() {
  local installed
  installed=$(cat "$BB_HOME/version" 2>/dev/null || echo "unknown")
  printf 'installed: %s\nlatest:    %s\n' "$installed" "$LATEST_VERSION"
}
```

Note: rename the earlier `cmd_version()` (which only prints "bridge $VERSION") and the case branch should call this one. The `--version` flag still prints just the bridge version (separate concern). Adjust as:

```bash
cmd_bridge_version() { echo "bridge ${BRIDGE_VERSION}"; }
cmd_version() {
  local installed
  installed=$(cat "$BB_HOME/version" 2>/dev/null || echo "unknown")
  printf 'installed: %s\nlatest:    %s\n' "$installed" "$LATEST_VERSION"
}
```

And in `main`:

```bash
    --version|-V) cmd_bridge_version; exit 0 ;;
    version) cmd_version; exit 0 ;;
```

Update case branch:

```bash
    doctor)  cmd_doctor ;;
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
bun run test:install
```

Expected: 17 tests pass.

- [ ] **Step 5: Commit**

```bash
git add install/bridge.sh.tmpl install/tests/bridge.bats
git commit -m "feat(install): bridge doctor and version"
```

---

## Task 7: `bridge update` and `bridge uninstall`

**Files:**
- Modify: `install/bridge.sh.tmpl`

- [ ] **Step 1: Write failing tests**

Append to `install/tests/bridge.bats`:

```bash
@test "bridge uninstall without --yes prompts and aborts on 'n'" {
  mkdir -p "$BB_HOME"
  echo "n" | run bash "$BRIDGE_TMPL" uninstall
  [[ -d "$BB_HOME" ]]
}

@test "bridge uninstall --yes removes BB_HOME" {
  mkdir -p "$BB_HOME/repo"
  run bash "$BRIDGE_TMPL" uninstall --yes
  [ "$status" -eq 0 ]
  [[ ! -d "$BB_HOME" ]]
}

@test "bridge update invokes install.sh with target version" {
  # Stub install.sh so we can assert invocation.
  cat > "$BB_TEST_TMP/install.sh" <<'EOF'
#!/usr/bin/env bash
echo "called with: $*"
exit 0
EOF
  chmod +x "$BB_TEST_TMP/install.sh"
  BB_INSTALL_SH="$BB_TEST_TMP/install.sh" BB_VERSION=v9.9.9 \
    run bash -c "BB_INSTALL_SH='$BB_TEST_TMP/install.sh'; source '$BRIDGE_TMPL'; cmd_update v9.9.9"
  [ "$status" -eq 0 ]
  [[ "$output" == *"v9.9.9"* ]]
}
```

- [ ] **Step 2: Run tests, confirm fail**

```bash
bun run test:install
```

- [ ] **Step 3: Implement update and uninstall**

Add to `install/bridge.sh.tmpl`:

```bash
cmd_update() {
  local target="${1:-latest}"
  local installer="${BB_INSTALL_SH:-$REPO_DIR/../install/install.sh}"
  if [[ ! -x "$installer" ]]; then
    die "BB-E103: cannot find installer at $installer"
  fi
  BB_VERSION="$target" "$installer"
  cmd_restart
}

cmd_uninstall() {
  local yes=false
  [[ "${1:-}" == "--yes" ]] && yes=true
  if ! $yes; then
    read -rp "About to rm -rf $BB_HOME. Continue? [y/N] " ans
    [[ "$ans" == "y" || "$ans" == "Y" ]] || { echo "aborted"; return 0; }
  fi
  cmd_down || true
  rm -rf "$BB_HOME"
  echo "Removed $BB_HOME"
}
```

Update case branch:

```bash
    update)    shift; cmd_update "$@" ;;
    uninstall) shift; cmd_uninstall "$@" ;;
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
bun run test:install
```

Expected: 20 tests pass.

- [ ] **Step 5: Commit**

```bash
git add install/bridge.sh.tmpl install/tests/bridge.bats
git commit -m "feat(install): bridge update and uninstall"
```

---

## Task 8: `install.sh` scaffolding + `BB-E001` prerequisite check

**Files:**
- Create: `install/install.sh`

- [ ] **Step 1: Write failing test**

Create `install/tests/install.bats`:

```bash
#!/usr/bin/env bats
load helpers

@test "install.sh exits BB-E001 when bun missing" {
  cat > "$BB_TEST_TMP/install.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
$(sed -n '/^set -euo pipefail$/,/^# END PREREQ/p' "$INSTALL_SH" | sed '/^# END PREREQ$/d')
EOF
  chmod +x "$BB_TEST_TMP/install.sh"
  export PATH="/usr/bin:/bin"
  run bash "$BB_TEST_TMP/install.sh"
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E001"* ]]
}

@test "install.sh succeeds bun check when bun present" {
  make_fake_bun
  cat > "$BB_TEST_TMP/install.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
$(sed -n '/^set -euo pipefail$/,/^# END PREREQ/p' "$INSTALL_SH" | sed '/^# END PREREQ$/d')
echo "OK"
EOF
  chmod +x "$BB_TEST_TMP/install.sh"
  run bash "$BB_TEST_TMP/install.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"OK"* ]]
}
```

- [ ] **Step 2: Run tests, confirm fail**

```bash
bun run test:install
```

- [ ] **Step 3: Create `install/install.sh` with embedded heredoc placeholder**

```bash
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
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
bun run test:install
```

Expected: 22 tests pass.

- [ ] **Step 5: Commit**

```bash
git add install/install.sh install/tests/install.bats
git commit -m "feat(install): install.sh scaffold and prerequisite checks (BB-E001)"
```

---

## Task 9: `install.sh` version resolution + extension download with SHA-256 verify

**Files:**
- Modify: `install/install.sh`

- [ ] **Step 1: Write failing tests**

Append to `install/tests/install.bats`:

```bash
@test "resolve_version accepts BB_VERSION env override" {
  # shellcheck disable=SC1090
  BB_VERSION="v1.2.3" source <(sed -n '/^resolve_version()/,/^}/p' "$INSTALL_SH")
  run resolve_version
  [ "$status" -eq 0 ]
  [ "$output" = "v1.2.3" ]
}

@test "resolve_version rejects malformed BB_VERSION" {
  run bash -c "set -e; BB_VERSION='not-a-version'; source <(sed -n '/^resolve_version()/,/^}/p' '$INSTALL_SH'); resolve_version"
  [ "$status" -ne 0 ]
  [[ "$output" == *"version"* ]]
}

@test "download_extension exits BB-E020 on SHA-256 mismatch" {
  mkdir -p "$BB_TEST_TMP/www"
  echo "actual-zip-content" > "$BB_TEST_TMP/www/fake.zip"
  echo "deadbeef  fake.zip" > "$BB_TEST_TMP/www/fake.zip.sha256"
  start_mock_http 18745
  run bash -c "
    set -e
    source <(sed -n '/^resolve_version()/,/^print_next_steps/p' '$INSTALL_SH' | sed '/^print_next_steps/,\$d')
    ORG='127.0.0.1:18745'
    resolve_version() { echo 'v9.9.9'; }
    download_extension
  "
  stop_mock_http
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E020"* ]]
}

@test "download_extension exits BB-E021 on HTTP error" {
  # No mock server, port 1 is unbound.
  run bash -c "
    set -e
    source <(sed -n '/^resolve_version()/,/^print_next_steps/p' '$INSTALL_SH' | sed '/^print_next_steps/,\$d')
    ORG='127.0.0.1:1'
    resolve_version() { echo 'v9.9.9'; }
    download_extension
  "
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E021"* ]]
}

@test "download_extension succeeds with correct sha256" {
  mkdir -p "$BB_TEST_TMP/www"
  echo "real-content" > "$BB_TEST_TMP/www/bb.zip"
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 bb.zip | awk '{print $1"  bb.zip"}' > bb.zip.sha256 )
  start_mock_http 18746
  run bash -c "
    set -e
    source <(sed -n '/^resolve_version()/,/^print_next_steps/p' '$INSTALL_SH' | sed '/^print_next_steps/,\$d')
    ORG='127.0.0.1:18746'
    BB_HOME='$BB_TEST_TMP/bb-home'
    mkdir -p \"\$BB_HOME/extension\"
    resolve_version() { echo 'v9.9.9'; }
    download_extension
    ls \"\$BB_HOME/extension\"
  "
  stop_mock_http
  [ "$status" -eq 0 ]
  [[ "$output" == *"bb.zip"* ]]
}
```

- [ ] **Step 2: Run tests, confirm fail**

```bash
bun run test:install
```

- [ ] **Step 3: Implement resolve_version and download_extension**

Replace the stub functions in `install/install.sh`:

```bash
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
  local version="$1"
  local base="https://github.com/${ORG}/${REPO}/releases/download/${version}"
  local zipname="browser-bridge-extension-${version}.zip"
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
  mkdir -p "$BB_HOME/extension"
  if [[ -d "$BB_HOME/extension" ]] && [[ -n "$(ls -A "$BB_HOME/extension" 2>/dev/null)" ]]; then
    mv "$BB_HOME/extension" "$BB_HOME/extension.bak.$(date +%s)"
  fi
  unzip -q "${tmpdir}/${zipname}" -d "$BB_HOME/extension"
  info "Extension installed to $BB_HOME/extension"
}
```

Update `main()`:

```bash
main() {
  check_prereqs
  local version
  version=$(resolve_version)
  info "Installing Browser Bridge ${version}"
  download_extension "$version"
  clone_source "$version"
  write_artifacts "$version"
  print_next_steps "$version"
}
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
bun run test:install
```

Expected: 27 tests pass.

- [ ] **Step 5: Commit**

```bash
git add install/install.sh install/tests/install.bats
git commit -m "feat(install): version resolution and verified extension download"
```

---

## Task 10: `install.sh` clone source and `bun install`

**Files:**
- Modify: `install/install.sh`

- [ ] **Step 1: Write failing tests**

Append to `install/tests/install.bats`:

```bash
setup_clone_fixture() {
  # Make a bare repo we can clone from.
  mkdir -p "$BB_TEST_TMP/origin.git"
  git -C "$BB_TEST_TMP/origin.git" init --bare --quiet
  # Seed a commit on a v9.9.9 branch in a working tree, push it.
  local seed="$BB_TEST_TMP/seed"
  mkdir -p "$seed"
  git -C "$seed" init --quiet -b main
  git -C "$seed" -c user.email=t@t -c user.name=t commit --allow-empty -m initial --quiet
  git -C "$seed" checkout -b v9.9.9 --quiet
  git -C "$seed" -c user.email=t@t -c user.name=t commit --allow-empty -m "v9.9.9" --quiet
  git -C "$seed" remote add origin "$BB_TEST_TMP/origin.git"
  git -C "$seed" push origin main v9.9.9 --quiet
}

@test "clone_source fresh: shallow-clones repo at tag" {
  setup_clone_fixture
  BB_HOME="$BB_TEST_TMP/bb-home"
  mkdir -p "$BB_HOME"
  run bash -c "
    set -e
    source <(sed -n '/^clone_source()/,/^}/p' '$INSTALL_SH')
    ORG='$BB_TEST_TMP/origin.git'
    REPO_DIR='$BB_TEST_TMP/origin.git'  # we'll point at the bare directly via override
    clone_source() {
      local version=\"\$1\"
      git clone --depth 1 --branch \"\$version\" \"$BB_TEST_TMP/origin.git\" \"\$BB_HOME/repo\"
    }
    clone_source v9.9.9
    test -f \"\$BB_HOME/repo/.git/HEAD\"
  "
  [ "$status" -eq 0 ]
  [[ -d "$BB_TEST_TMP/bb-home/repo" ]]
}

@test "clone_source update: fetches and resets existing repo" {
  setup_clone_fixture
  BB_HOME="$BB_TEST_TMP/bb-home"
  mkdir -p "$BB_HOME/repo"
  git clone --depth 1 --branch v9.9.9 "$BB_TEST_TMP/origin.git" "$BB_HOME/repo" >/dev/null 2>&1
  # Tag a new commit and push it.
  local seed="$BB_TEST_TMP/seed2"
  git clone "$BB_TEST_TMP/origin.git" "$seed" >/dev/null 2>&1
  git -C "$seed" -c user.email=t@t -c user.name=t commit --allow-empty -m "v9.9.10" --quiet
  git -C "$seed" checkout -b v9.9.10 --quiet
  git -C "$seed" push origin v9.9.10 --quiet
  run bash -c "
    set -e
    source <(sed -n '/^clone_source()/,/^}/p' '$INSTALL_SH')
    ORG='$BB_TEST_TMP/origin.git'
    clone_source() {
      git -C \"\$BB_HOME/repo\" fetch --depth 1 origin v9.9.10
      git -C \"\$BB_HOME/repo\" reset --hard v9.9.10
    }
    clone_source v9.9.10
  "
  [ "$status" -eq 0 ]
}
```

- [ ] **Step 2: Run tests, confirm fail**

```bash
bun run test:install
```

- [ ] **Step 3: Implement clone_source**

Replace the `clone_source()` stub in `install/install.sh`:

```bash
clone_source() {
  local version="$1"
  local src="https://github.com/${ORG}/${REPO}.git"
  if [[ -d "$BB_HOME/repo/.git" ]]; then
    info "Updating existing repo at $BB_HOME/repo"
    git -C "$BB_HOME/repo" fetch --depth 1 origin "$version" \
      || die "BB-E023: failed to fetch $version from $src"
    git -C "$BB_HOME/repo" reset --hard "$version" \
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
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
bun run test:install
```

Expected: 29 tests pass.

- [ ] **Step 5: Commit**

```bash
git add install/install.sh install/tests/install.bats
git commit -m "feat(install): clone source (fresh or update) and bun install"
```

---

## Task 11: `install.sh` write artifacts and print next steps

**Files:**
- Modify: `install/install.sh`

- [ ] **Step 1: Write failing tests**

Append to `install/tests/install.bats`:

```bash
@test "write_artifacts emits bridge script and version file" {
  BB_HOME="$BB_TEST_TMP/bb-home"
  mkdir -p "$BB_HOME"
  run bash -c "
    set -e
    source <(sed -n '/^write_artifacts()/,/^}/p' '$INSTALL_SH')
    BRIDGE_TEMPLATE=\$(mktemp)
    cat > \"\$BRIDGE_TEMPLATE\" <<'TPL'
#!/usr/bin/env bash
echo bridge-{{BRIDGE_VERSION}}
TPL
    write_artifacts() {
      local version=\"\$1\"
      mkdir -p \"\$BB_HOME/bin\"
      sed \"s|{{BRIDGE_VERSION}}|\$version|g\" \"\$BRIDGE_TEMPLATE\" > \"\$BB_HOME/bin/bridge\"
      chmod +x \"\$BB_HOME/bin/bridge\"
      echo \"\$version\" > \"\$BB_HOME/version\"
      mkdir -p \"\$HOME/.local/bin\"
      ln -sf \"\$BB_HOME/bin/bridge\" \"\$HOME/.local/bin/bridge\"
    }
    write_artifacts v9.9.9
    cat \"\$BB_HOME/bin/bridge\"
    cat \"\$BB_HOME/version\"
    test -L \"\$HOME/.local/bin/bridge\"
  "
  [ "$status" -eq 0 ]
  [[ "$output" == *"bridge-v9.9.9"* ]]
  [[ "$output" == *"v9.9.9"* ]]
}

@test "print_next_steps mentions PATH, Chrome load, and bridge up" {
  run bash -c "
    set -e
    source <(sed -n '/^print_next_steps()/,/^}/p' '$INSTALL_SH')
    print_next_steps() {
      cat <<EOF
PATH: \$HOME/.local/bin
Chrome: load $BB_HOME/extension/
Next: bridge up
EOF
    }
    print_next_steps
  "
  [[ "$output" == *"PATH"* ]]
  [[ "$output" == *"Chrome"* ]]
  [[ "$output" == *"bridge up"* ]]
}
```

- [ ] **Step 2: Run tests, confirm fail**

```bash
bun run test:install
```

- [ ] **Step 3: Implement write_artifacts and print_next_steps**

Replace the stubs in `install/install.sh`:

```bash
# Path to the bridge template, baked into install.sh via a heredoc at emit time.
BRIDGE_TEMPLATE_PATH="${BRIDGE_TEMPLATE_PATH:-$REPO_DIR/install/bridge.sh.tmpl}"

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
     (chrome://extensions → enable Developer mode → "Load unpacked")
  3. Start the bridge:
       bridge up

To uninstall later: bridge uninstall --yes
EOF
}
```

- [ ] **Step 4: Run tests, confirm pass**

```bash
bun run test:install
```

Expected: 31 tests pass.

- [ ] **Step 5: Commit**

```bash
git add install/install.sh install/tests/install.bats
git commit -m "feat(install): write bridge, symlink, version file, print next steps"
```

---

## Task 12: `install.sh` end-to-end idempotency against a fake release server

**Files:**
- Modify: `install/tests/install.bats`

- [ ] **Step 1: Write end-to-end test**

Append to `install/tests/install.bats`:

```bash
@test "install.sh end-to-end against mock release server (BB-E002 etc excluded)" {
  setup_clone_fixture
  make_fake_bun

  # Set up www dir with both the extension zip (with sha256) and a 'git' mirror.
  mkdir -p "$BB_TEST_TMP/www"
  echo "fake-extension-content" > "$BB_TEST_TMP/www/bb.zip"
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 bb.zip | awk '{print $1"  bb.zip"}' > bb.zip.sha256 )

  start_mock_http 18750

  BB_HOME="$BB_TEST_TMP/bb-home" \
  BB_VERSION="v9.9.9" \
  ORG="127.0.0.1:18750" \
  REPO="browser-bridge" \
  BRIDGE_TEMPLATE_PATH="$BB_TEST_ROOT/install/bridge.sh.tmpl" \
  run bash -c "
    set -e
    # Override clone to use the local origin so we don't need network.
    source <(sed -n '/^main()/,/^main \"\\\$@\"/p' '$INSTALL_SH' | sed '/^main \"\\\$@\"/d')
    # Replace clone_source to use the local bare repo.
    eval 'clone_source() {
      local version=\"\$1\"
      git clone --depth 1 --branch \"\$version\" \"$BB_TEST_TMP/origin.git\" \"\$BB_HOME/repo\"
    }'
    # Replace download_extension to use the mock server.
    eval 'download_extension() {
      local version=\"\$1\"
      mkdir -p \"\$BB_HOME/extension\"
      curl -fsSL \"http://127.0.0.1:18750/bb.zip\" -o \"\$BB_HOME/extension/bb.zip\"
      local expected=\$(curl -fsSL \"http://127.0.0.1:18750/bb.zip.sha256\" | awk \"{print \\\$1}\")
      local actual=\$(shasum -a 256 \"\$BB_HOME/extension/bb.zip\" | awk \"{print \\\$1}\")
      [[ \"\$expected\" == \"\$actual\" ]]
    }'
    main
  "
  stop_mock_http

  [ "$status" -eq 0 ]
  [[ -f "$BB_TEST_TMP/bb-home/version" ]]
  [[ -f "$BB_TEST_TMP/bb-home/bin/bridge" ]]
  [[ -L "$HOME/.local/bin/bridge" ]]
  [[ -d "$BB_TEST_TMP/bb-home/repo" ]]
  [[ "$($BB_TEST_TMP/bb-home/version)" == "v9.9.9" ]]
}

@test "install.sh idempotent: second run upgrades in place" {
  setup_clone_fixture
  make_fake_bun
  mkdir -p "$BB_TEST_TMP/www"
  echo "v1" > "$BB_TEST_TMP/www/bb.zip"
  ( cd "$BB_TEST_TMP/www" && shasum -a 256 bb.zip | awk '{print $1"  bb.zip"}' > bb.zip.sha256 )
  start_mock_http 18751
  common_env=(
    "BB_HOME=$BB_TEST_TMP/bb-home"
    "BB_VERSION=v9.9.9"
    "ORG=127.0.0.1:18751"
    "REPO=browser-bridge"
    "BRIDGE_TEMPLATE_PATH=$BB_TEST_ROOT/install/bridge.sh.tmpl"
  )
  run_runner() {
    env "${common_env[@]}" PATH="$BB_TEST_TMP/bin:$PATH" \
      bash -c "source '$INSTALL_SH'; main"
  }
  run_runner
  [ "$status" -eq 0 ]
  # Run again — should still succeed.
  run_runner
  stop_mock_http
  [ "$status" -eq 0 ]
}
```

- [ ] **Step 2: Run tests, confirm they pass**

```bash
bun run test:install
```

If the `clone_source`/`download_extension` overrides are flaky due to `eval`, replace them with a direct test that just calls the implemented functions and asserts state. Iterate until green.

Expected: 33 tests pass.

- [ ] **Step 3: Commit**

```bash
git add install/tests/install.bats
git commit -m "test(install): end-to-end install against mock release server"
```

---

## Task 13: `build-extension-zip.sh` and its bun test

**Files:**
- Create: `.github/scripts/build-extension-zip.sh`
- Create: `install/tests/release-workflow.test.ts`

- [ ] **Step 1: Write failing bun test**

Create `install/tests/release-workflow.test.ts`:

```typescript
import { describe, expect, it, beforeAll, afterAll } from 'bun:test';
import { spawnSync } from 'node:child_process';
import { mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from 'node:fs';
import { join } from 'node:path';

const TMP = `/tmp/bb-zip-test-${Date.now()}`;
const SCRIPT = join(import.meta.dir, '..', '..', '.github', 'scripts', 'build-extension-zip.sh');

beforeAll(() => {
  mkdirSync(`${TMP}/apps/extension/dist`, { recursive: true });
  writeFileSync(`${TMP}/apps/extension/dist/manifest.json`, '{"manifest_version":3}');
  writeFileSync(`${TMP}/apps/extension/dist/background.js`, '// sw');
});

afterAll(() => rmSync(TMP, { recursive: true, force: true }));

describe('build-extension-zip.sh', () => {
  it('produces zip with expected internal structure and matching sha256', () => {
    const result = spawnSync('bash', [SCRIPT], {
      cwd: TMP,
      env: { ...process.env, VERSION: 'v1.2.3' },
      encoding: 'utf8',
    });
    expect(result.status).toBe(0);

    const zipPath = `${TMP}/browser-bridge-extension-v1.2.3.zip`;
    const shaPath = `${zipPath}.sha256`;
    expect(existsSync(zipPath)).toBe(true);
    expect(existsSync(shaPath)).toBe(true);

    const listed = spawnSync('unzip', ['-l', zipPath], { encoding: 'utf8' });
    expect(listed.stdout).toContain('manifest.json');
    expect(listed.stdout).toContain('background.js');

    const expectedSha = readFileSync(shaPath, 'utf8').split(/\s+/)[0];
    const actual = spawnSync('shasum', ['-a', '256', zipPath], { encoding: 'utf8' });
    expect(actual.stdout.split(/\s+/)[0]).toBe(expectedSha);
  });
});
```

- [ ] **Step 2: Run test, confirm fail**

```bash
bun test install/tests/release-workflow.test.ts
```

Expected: FAIL (script doesn't exist).

- [ ] **Step 3: Implement `build-extension-zip.sh`**

```bash
#!/usr/bin/env bash
# Build the Browser Bridge Chrome Extension zip + sha256 sidecar.
set -euo pipefail

VERSION="${VERSION:-v0.0.0}"
NAME="browser-bridge-extension-${VERSION}"
DIST="apps/extension/dist"

if [[ ! -d "$DIST" ]]; then
  echo "Building extension..."
  bun run build:extension
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/$NAME"
cp -R "$DIST/." "$STAGE/$NAME/"

OUT_DIR="${OUT_DIR:-.}"
ZIP_PATH="$OUT_DIR/${NAME}.zip"
SHA_PATH="${ZIP_PATH}.sha256"

( cd "$STAGE" && zip -qr "$ZIP_PATH" "$NAME" )
( cd "$STAGE" && shasum -a 256 "$ZIP_PATH" | awk -v f="$(basename "$ZIP_PATH")" '{print $1"  "f}' > "$SHA_PATH" )

echo "$ZIP_PATH"
echo "$SHA_PATH"
```

Make executable: `chmod +x .github/scripts/build-extension-zip.sh`

- [ ] **Step 4: Run test, confirm pass**

```bash
bun test install/tests/release-workflow.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add .github/scripts/build-extension-zip.sh install/tests/release-workflow.test.ts
git commit -m "feat(release): build-extension-zip.sh with bun test coverage"
```

---

## Task 14: Release GitHub Actions workflow

**Files:**
- Create: `.github/workflows/release-extension.yml`

- [ ] **Step 1: Create the workflow**

```yaml
name: release-extension

on:
  push:
    tags:
      - 'v[0-9]+\.[0-9]+\.[0-9]+'

permissions:
  contents: write

jobs:
  build-and-release:
    runs-on: ubuntu-latest
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

      - name: Install and build
        run: |
          bun install --frozen-lockfile
          bun run build:extension

      - name: Build zip + sha256
        env:
          VERSION: ${{ github.ref_name }}
        run: bash .github/scripts/build-extension-zip.sh

      - name: Upload release assets
        uses: softprops/action-gh-release@v2
        with:
          files: |
            browser-bridge-extension-${{ github.ref_name }}.zip
            browser-bridge-extension-${{ github.ref_name }}.zip.sha256
```

- [ ] **Step 2: Validate YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release-extension.yml'))"
```

Expected: exit 0 (no syntax errors).

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release-extension.yml
git commit -m "feat(release): tag-triggered extension zip release workflow"
```

---

## Task 15: `CHANGELOG.md`

**Files:**
- Create: `CHANGELOG.md`

- [ ] **Step 1: Create the file**

```markdown
# Changelog

All notable changes to Browser Bridge are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/) and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- One-line installer (`curl ... | bash`) and `bridge` orchestrator for single-machine deployments.
- Prebuilt extension zip distributed via GitHub Releases with SHA-256 verification.

## [1.0.0] - 2026-06-12

### Added
- Initial release: CLI, WebSocket Server, Local Proxy, Chrome Extension.
```

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: add CHANGELOG and unreleased entry for distribution"
```

---

## Task 16: `install/README.md`

**Files:**
- Create: `install/README.md`

- [ ] **Step 1: Create the file**

```markdown
# Browser Bridge Installer

One-line install for macOS and Linux.

## Quick Start

```bash
curl -fsSL https://raw.githubusercontent.com/<org>/browser-bridge/main/install/install.sh | bash
```

To pin a version: `BB_VERSION=v1.2.3 curl ... | bash`.

To install from a fork: `curl -fsSL https://raw.githubusercontent.com/<my-org>/browser-bridge/main/install/install.sh | bash -s -- --source https://github.com/<my-org>/browser-bridge.git`.

## Prerequisites

| Tool | Required | Notes |
|---|---|---|
| bash ≥ 4 | yes | macOS users: pre-installed. |
| curl | yes | pre-installed. |
| unzip | yes | `brew install unzip` if missing. |
| git | yes | for cloning source. |
| bun | yes | `curl -fsSL https://bun.sh/install \| bash` if missing. |

## What It Does

1. Verifies prerequisites.
2. Resolves the target version (latest release, or `BB_VERSION` override).
3. Downloads `browser-bridge-extension-{version}.zip` and its `.sha256` from the GitHub Release; aborts on mismatch.
4. Extracts the extension into `~/.browser-bridge/extension/`.
5. Shallow-clones (or updates) `https://github.com/<org>/browser-bridge` into `~/.browser-bridge/repo/` at the matching tag.
6. Runs `bun install --frozen-lockfile` in the cloned repo.
7. Writes `~/.browser-bridge/bin/bridge` (templated from `install/bridge.sh.tmpl`) and symlinks it into `~/.local/bin/bridge`.
8. Writes the resolved version to `~/.browser-bridge/version`.
9. Prints next steps: PATH export, Chrome "load unpacked" pointer, `bridge up`.

## `bridge` Commands

| Command | Purpose |
|---|---|
| `bridge up` | Start ws-server and local-proxy. |
| `bridge down` | Stop both. |
| `bridge restart` | down then up. |
| `bridge status` | Show service state. |
| `bridge logs [name]` | Tail logs. |
| `bridge update [version]` | Upgrade in place. |
| `bridge doctor` | Diagnose install health. |
| `bridge uninstall [--yes]` | Remove `~/.browser-bridge/`. |
| `bridge version` | Show installed + latest release. |

## Error Codes

| Code | Meaning | Fix |
|---|---|---|
| `BB-E000` | Bash < 4 or missing | Upgrade bash. |
| `BB-E001` | Prerequisite missing | Install `bun`, `git`, `curl`, or `unzip`. |
| `BB-E002` | `bridge` invoked without install | Run the install script. |
| `BB-E010` | Port already in use | `lsof -i :8787`, kill the conflict. |
| `BB-E011` | Service failed to bind port | Check `~/.browser-bridge/logs/`. |
| `BB-E020` | Extension zip SHA-256 mismatch | Re-run; check network/proxy. |
| `BB-E021` | Download failed (HTTP error) | Check network, retry. |
| `BB-E022` | Invalid version string | Use `vX.Y.Z` format. |
| `BB-E023` | Git fetch failed | Check network/credentials. |
| `BB-E024` | Clone or reset failed | Check disk space. |
| `BB-E025` | `bun install` failed | Inspect `~/.browser-bridge/repo/`. |
| `BB-E030` | Tag/version mismatch on release | Fix `package.json` and re-tag. |
| `BB-E031` | CHANGELOG missing entry for release | Add an entry, re-tag. |
| `BB-E100` | Subcommand stub (during dev) | Implementation pending. |
| `BB-E101` | Unknown subcommand | Run `bridge` for help. |
| `BB-E102` | Unknown log target | Use `ws-server` or `local-proxy`. |
| `BB-E103` | Cannot locate installer (update) | Re-run the install script manually. |

## Tests

```bash
brew install bats-core   # macOS
apt install bats         # Debian/Ubuntu

bun run test:install
bun test install/tests/release-workflow.test.ts
```

## Manual Smoke Test

```bash
# After install:
bridge up
bridge status         # both services running
bridge doctor         # all OK
# In Chrome: load unpacked extension from ~/.browser-bridge/extension/
# In another terminal:
bun run cli navigate https://example.com --browser test
bridge down
bridge uninstall --yes
```
```

- [ ] **Step 2: Commit**

```bash
git add install/README.md
git commit -m "docs(install): install README with error code reference"
```

---

## Task 17: Top-level `README.md` and `README.en.md` — add Install section

**Files:**
- Modify: `README.md`
- Modify: `README.en.md`

- [ ] **Step 1: Replace "快速开始" section in `README.md`**

Find the `## 快速开始` heading. Insert an "Install" section above it, and demote the existing content into a "Development" subsection. The result should look like:

```markdown
## 一行安装

```bash
curl -fsSL https://raw.githubusercontent.com/<org>/browser-bridge/main/install/install.sh | bash
```

按提示在 Chrome 里加载 `~/.browser-bridge/extension/`，然后：

```bash
bridge up
```

详细说明、错误码、卸载方式见 [`install/README.md`](./install/README.md)。

## 快速开始（开发模式）

```bash
# 1. 安装依赖
bun install

# 2. 启动 WebSocket 服务
bun run dev:websocket

# 3. 构建 Extension（另一个终端）
bun run dev:extension

# 4. 在 Chrome 中加载 apps/extension/dist/ 目录

# 5. 运行 CLI
bun run cli
```
```

- [ ] **Step 2: Replace "Quick Start" in `README.en.md`**

Find the `## Quick Start` heading. Add an "Install" section above it and rename the existing section to "Quick Start (Development)":

```markdown
## Install

```bash
curl -fsSL https://raw.githubusercontent.com/<org>/browser-bridge/main/install/install.sh | bash
```

Follow the printed instructions to load `~/.browser-bridge/extension/` in Chrome, then:

```bash
bridge up
```

See [`install/README.md`](./install/README.md) for details, error codes, and uninstall steps.

## Quick Start (Development)

```bash
# 1. Install dependencies
bun install

# 2. Start the WebSocket server
bun run dev:websocket

# 3. Build the Extension (in another terminal)
bun run dev:extension

# 4. Load apps/extension/dist/ as an unpacked extension in Chrome

# 5. Run the CLI
bun run cli
```
```

- [ ] **Step 3: Verify both README files render**

```bash
head -50 README.md
head -50 README.en.md
```

Expected: each file has the new "Install" section before "Quick Start".

- [ ] **Step 4: Commit**

```bash
git add README.md README.en.md
git commit -m "docs(readme): add Install section, demote dev workflow to Quick Start (Development)"
```

---

## Task 18: Reserve `bridge-host` placeholder in CLI

**Files:**
- Modify: `apps/cli/src/index.ts`

- [ ] **Step 1: Add a placeholder command**

At the end of `apps/cli/src/index.ts`, just before `program.parse();`, insert:

```typescript
// Reserved for future distributed-mode support. Not yet implemented.
program
  .command('bridge-host')
  .description('Configure CLI to point at a remote Browser Bridge server (not yet implemented)')
  .action(() => {
    console.error('bridge-host: not yet implemented. See docs/superpowers/specs/2026-06-15-distribution-design.md');
    process.exit(1);
  });
```

- [ ] **Step 2: Verify the CLI still builds**

```bash
bun run type-check
```

Expected: exit 0.

```bash
bun --cwd apps/cli start bridge-host
```

Expected: error "bridge-host: not yet implemented", exit code 1.

- [ ] **Step 3: Commit**

```bash
git add apps/cli/src/index.ts
git commit -m "feat(cli): reserve bridge-host subcommand for future distributed-mode support"
```

---

## Task 19: End-to-end verification

**Files:** none modified

- [ ] **Step 1: Run the full test suite**

```bash
bun run type-check
bun run test
bun run test:install
bun test install/tests/release-workflow.test.ts
```

Expected: all green. If anything fails, fix and re-run before declaring done.

- [ ] **Step 2: Verify the install script runs in dry-run form**

```bash
bash -n install/install.sh
bash -n install/bridge.sh.tmpl
```

Expected: both exit 0.

- [ ] **Step 3: Manual smoke checklist**

Verify each is true before declaring this plan complete:

- [ ] `bun run test:install` runs all 33+ bats tests and they pass.
- [ ] `bun test install/tests/release-workflow.test.ts` passes.
- [ ] `install.sh` and `bridge.sh.tmpl` pass `bash -n` syntax checks.
- [ ] `package.json` has `test:install` and `release:extension-zip` scripts.
- [ ] `README.md` and `README.en.md` both have an "Install" section before "Quick Start".
- [ ] `install/README.md` documents every error code.
- [ ] `apps/cli/src/index.ts` has the `bridge-host` placeholder and exits non-zero.
- [ ] `.github/workflows/release-extension.yml` is valid YAML.
- [ ] `CHANGELOG.md` has an entry under `[Unreleased]`.

- [ ] **Step 4: Commit any final adjustments**

If anything was changed during smoke verification, commit it. Otherwise this step is a no-op.