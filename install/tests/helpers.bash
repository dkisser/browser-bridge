# Helpers for bats tests. Source this from each .bats file.
# shellcheck shell=bash

# Resolve the project root regardless of where bats is invoked from.
BB_TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Per-test scratch directory. Tests should put everything here.
BB_TEST_TMP="${BATS_TEST_TMPDIR:-/tmp}/bb-test-${BATS_TEST_NUMBER:-0}-$$"
mkdir -p "$BB_TEST_TMP"
export BB_TEST_TMP

# Fake HOME so we never touch the developer's real ~/.browser-bridge.
# The real HOME is kept for setup_file's go build, which needs the shared
# module cache (GOPATH/GOCACHE derive from HOME).
export BB_REAL_HOME="$HOME"
export HOME="$BB_TEST_TMP/home"
mkdir -p "$HOME"

# Set BB_HOME explicitly so bridge/install.sh never reads the real path.
export BB_HOME="$HOME/.browser-bridge"

# Set BB_EXTENSION_DIR explicitly so tests use the fake HOME tree.
export BB_EXTENSION_DIR="$HOME/Browser-Bridge"

# Dedicated test ports. NEVER use the production defaults (3001-3003) here:
# teardown sweeps listeners on these ports, and the developer machine runs a
# real bridge-core on 3001-3003 that must survive the test suite.
export BRIDGE_WS_PORT="${BRIDGE_WS_PORT:-3311}"
export BRIDGE_LOCAL_PORT="${BRIDGE_LOCAL_PORT:-3312}"
export BRIDGE_MCP_PORT="${BRIDGE_MCP_PORT:-3313}"

# Path to the script under test.
INSTALL_SH="$BB_TEST_ROOT/install/install.sh"

# Create a fake launchctl that emulates the parts of launchd the tests rely
# on: it records every call, reports the supervisor as loaded while its
# pidfile process is alive, `bootstrap` actually starts the plist's
# ProgramArguments (so the Go binary's macOS code path runs end-to-end), and
# `bootout` stops the supervisor again.
make_fake_launchctl() {
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/launchctl" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$BB_TEST_TMP/launchctl_calls.txt"
BB_HOME="\${BB_HOME:-\$HOME/.browser-bridge}"
label="com.browser-bridge.bridge"
case "\$1" in
  list)
    spid=\$(cat "\$BB_HOME/run/supervisor.pid" 2>/dev/null || true)
    if [[ -n "\$spid" ]] && kill -0 "\$spid" 2>/dev/null; then
      printf -- '-\t0\t%s\n' "\$label"
    fi
    exit 0
    ;;
  bootstrap)
    plist="\$3"
    args=()
    while IFS= read -r line; do
      args+=("\$line")
    done < <(awk '/<key>ProgramArguments<\\/key>/{f=1;next} f&&/<\\/array>/{exit} f&&/<string>/{gsub(/.*<string>/,""); gsub(/<\\/string>.*/,""); print}' "\$plist")
    [[ \${#args[@]} -gt 0 ]] || exit 1
    mkdir -p "\$BB_HOME/logs"
    env BB_HOME="\$BB_HOME" nohup "\${args[@]}" >>"\$BB_HOME/logs/launchagent.log" 2>&1 &
    exit 0
    ;;
  bootout)
    spid=\$(cat "\$BB_HOME/run/supervisor.pid" 2>/dev/null || true)
    if [[ -n "\$spid" ]]; then
      kill "\$spid" 2>/dev/null || true
    fi
    exit 0
    ;;
esac
exit 0
EOF
  chmod +x "$BB_TEST_TMP/bin/launchctl"
  export PATH="$BB_TEST_TMP/bin:$PATH"
}

# Create a fake uname that returns a fixed value. Only influences install.sh
# itself — the Go bridge binary uses its compiled-in runtime.GOOS.
make_fake_uname() {
  local sysname="${1:-Darwin}"
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/uname" <<EOF
#!/usr/bin/env bash
if [[ "\$*" == *-s* ]]; then
  echo '$sysname'
else
  command uname "\$@"
fi
exit 0
EOF
  chmod +x "$BB_TEST_TMP/bin/uname"
  export PATH="$BB_TEST_TMP/bin:$PATH"
}

# Create a fake id that returns a fixed uid.
make_fake_id() {
  local uid="${1:-501}"
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/id" <<EOF
#!/usr/bin/env bash
if [[ "\$1" == "-u" ]]; then
  echo '$uid'
else
  echo 'uid=$uid(test) gid=20(staff) groups=20(staff)'
fi
exit 0
EOF
  chmod +x "$BB_TEST_TMP/bin/id"
  export PATH="$BB_TEST_TMP/bin:$PATH"
}

# Create a fake runtime tarball for install.bats tests. Layout matches
# build-tarball.sh: browser-bridge-macos-<arch>-<version>/bin/bridge (the
# single binary since ADR-0013) plus a per-tarball .sha256 sidecar.
# bin/bridge is the REAL Go binary (built once per test file by setup_file
# into BB_TEST_BRIDGE_BIN): the supervisor spawns `<bin>/bridge serve`,
# which binds the three test ports so the service manager's liveness probe
# succeeds. Returns the path to the tarball.
make_fake_runtime_tarball() {
  local version="${1:-v9.9.9}" arch="${2:-arm64}"
  local name="browser-bridge-macos-${arch}-${version}"
  local stage="$BB_TEST_TMP/${name}"
  mkdir -p "$stage/bin"

  if [[ -z "${BB_TEST_BRIDGE_BIN:-}" || ! -x "$BB_TEST_BRIDGE_BIN" ]]; then
    echo "BB_TEST_BRIDGE_BIN not built (setup_file must go build ./cmd/bridge)" >&2
    return 1
  fi
  cp "$BB_TEST_BRIDGE_BIN" "$stage/bin/bridge"
  chmod +x "$stage/bin/"*

  ( cd "$BB_TEST_TMP" && tar czf "${name}.tar.gz" "$name" )
  ( cd "$BB_TEST_TMP" && shasum -a 256 "${name}.tar.gz" > "${name}.tar.gz.sha256" )

  echo "$BB_TEST_TMP/${name}.tar.gz"
}

# Extract a single bash function's source from a script via `declare -f`,
# so callers can `source <(extract_function_source name script)` for
# in-process function testing. Robust to nested `}` inside the function
# body (heredocs, case branches, ${var/pat/replace} substitutions) — the
# previous `sed -n '/^name()/,/^}/p'` pattern matched the first top-level
# `}` and silently truncated or mis-extracted when the function gained
# any nested braces.
#
# Usage: extract_function_source <function_name> <script_path>
extract_function_source() {
  local func_name="$1"
  local script_path="$2"
  bash -c 'source "$1"; declare -f "$2"' _ "$script_path" "$func_name"
}

# Create a fake skills tarball for install.bats tests. Layout matches
# .github/scripts/build-skills-tarball.sh (ADR-0015): a single top-level
# browser-bridge/ directory containing SKILL.md, plus a .sha256 sidecar —
# that is what install.sh's download_skills extracts and installs. Returns
# the path to the tarball.
make_fake_skills_tarball() {
  local version="${1:-v9.9.9}"
  local name="browser-bridge-skills-${version}"
  local stage="$BB_TEST_TMP/skills-stage-${name}"
  mkdir -p "$stage/browser-bridge"
  cat > "$stage/browser-bridge/SKILL.md" <<'EOF'
---
name: browser-bridge
description: test
---
EOF

  ( cd "$stage" && tar czf "$BB_TEST_TMP/${name}.tar.gz" browser-bridge )
  ( cd "$BB_TEST_TMP" && shasum -a 256 "${name}.tar.gz" > "${name}.tar.gz.sha256" )

  echo "$BB_TEST_TMP/${name}.tar.gz"
}

# Start a tiny Python HTTP server on a free port serving $BB_TEST_TMP/www.
start_mock_http() {
  mkdir -p "$BB_TEST_TMP/www"
  python3 -m http.server -d "$BB_TEST_TMP/www" "$@" >"$BB_TEST_TMP/http.log" 2>&1 &
  MOCK_HTTP_PID=$!
  local port="${1:-8000}"
  local waited=0
  while [[ $waited -lt 50 ]]; do
    if python3 -c "import socket; s=socket.socket(); s.settimeout(0.1); s.connect(('127.0.0.1', $port)); s.close()" 2>/dev/null; then
      return 0
    fi
    sleep 0.1
    waited=$((waited + 1))
  done
}

stop_mock_http() {
  [[ -n "${MOCK_HTTP_PID:-}" ]] && kill "$MOCK_HTTP_PID" 2>/dev/null || true
}

# Wait up to ~5s for a file to appear (services start asynchronously when the
# fake launchctl emulates launchd).
wait_for_file() {
  local path="$1" waited=0
  while [[ $waited -lt 50 ]]; do
    [[ -e "$path" ]] && return 0
    sleep 0.1
    waited=$((waited + 1))
  done
  return 1
}

# Default teardown. Test files that need their own teardown should call
# `helpers_teardown` from within their override rather than redefining this.
helpers_teardown() {
  stop_mock_http
  # Kill anything still running from the test.
  pkill -P $$ 2>/dev/null || true
  # Supervisors started via the fake launchctl are nohup-detached (reparented)
  # and a fast test can finish before the supervisor wrote its pidfile. Their
  # command line always contains this test's scratch path, so match on that
  # and wait for them to exit — a supervisor's TERM shutdown stops its
  # `bridge serve` child, which keeps the test ports free for the next test.
  pkill -f "$BB_TEST_TMP/" 2>/dev/null || true
  local waited=0
  while pgrep -f "$BB_TEST_TMP/" >/dev/null 2>&1 && [[ $waited -lt 30 ]]; do
    sleep 0.1
    waited=$((waited + 1))
  done
  # Stop test services recorded in pidfiles under the scratch dir (the
  # supervisor stops its daemon child on TERM).
  local pidfile pid
  while IFS= read -r pidfile; do
    pid=$(cat "$pidfile" 2>/dev/null || true)
    [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  done < <(find "$BB_TEST_TMP" -path '*/run/*.pid' -type f 2>/dev/null)
  # Sweep ONLY the dedicated test ports — never the production 3001-3003,
  # which a developer's real bridge-core is listening on.
  for port in "$BRIDGE_WS_PORT" "$BRIDGE_LOCAL_PORT" "$BRIDGE_MCP_PORT"; do
    for pid in $(lsof -t -i ":$port" 2>/dev/null); do
      kill "$pid" 2>/dev/null || true
    done
  done
  # Remove the visible extension directory created by tests.
  rm -rf "$BB_EXTENSION_DIR" 2>/dev/null || true
}

teardown() { helpers_teardown; }
