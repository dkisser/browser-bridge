# Helpers for bats tests. Source this from each .bats file.
# shellcheck shell=bash

# Resolve the project root regardless of where bats is invoked from.
BB_TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Per-test scratch directory. Tests should put everything here.
BB_TEST_TMP="${BATS_TEST_TMPDIR:-/tmp}/bb-test-${BATS_TEST_NUMBER:-0}-$$"
mkdir -p "$BB_TEST_TMP"
export BB_TEST_TMP

# Fake HOME so we never touch the developer's real ~/.browser-bridge.
export HOME="$BB_TEST_TMP/home"
mkdir -p "$HOME"

# Set BB_HOME explicitly so bridge/install.sh never reads the real path.
export BB_HOME="$HOME/.browser-bridge"

# Set BB_EXTENSION_DIR explicitly so tests use the fake HOME tree.
export BB_EXTENSION_DIR="$HOME/Browser-Bridge"

# Path to the script under test.
INSTALL_SH="$BB_TEST_ROOT/install/install.sh"
BRIDGE_TMPL="$BB_TEST_ROOT/install/bridge.sh.tmpl"
LAUNCHAGENT_TMPL="$BB_TEST_ROOT/install/launchagent.plist.tmpl"

# Allow direct install.sh tests to find templates without fetching from the network.
export BRIDGE_TEMPLATE_PATH="$BRIDGE_TMPL"
export LAUNCHAGENT_TEMPLATE_PATH="$LAUNCHAGENT_TMPL"

# Stub PATH so "bun" can be a controlled fake when needed.
make_fake_bun() {
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/bun" <<'EOF'
#!/usr/bin/env bash
# Fake bun for tests. Honors BB_FAKE_BUN_BEHAVIOR env var.
if [[ "$1" == "run" && "$2" == "build:cli" ]]; then
  mkdir -p dist
  printf '#!/usr/bin/env bash\necho "fake-bridge-cmd"\n' > dist/bridge
  chmod +x dist/bridge
  exit 0
fi
case "${BB_FAKE_BUN_BEHAVIOR:-ok}" in
  ok)
    port="${BRIDGE_WS_PORT:-${BRIDGE_LOCAL_PROXY_PORT:-${BRIDGE_LOCAL_PORT:-}}}"
    if [[ -n "$port" ]]; then
      # Simulate a real service by binding the expected port.
      exec python3 -c "import socket, time; s=socket.socket(); s.bind(('', int('$port'))); s.listen(); time.sleep(9999)"
    fi
    echo "fake-bun: $*"
    exit 0
    ;;
  fail)      echo "fake-bun: $*"; exit 1 ;;
  hang)      sleep 999 ;;
esac
EOF
  chmod +x "$BB_TEST_TMP/bin/bun"
  export PATH="$BB_TEST_TMP/bin:$PATH"
}

# Create a fake launchctl for cross-platform auto-start tests.
make_fake_launchctl() {
  mkdir -p "$BB_TEST_TMP/bin"
  cat > "$BB_TEST_TMP/bin/launchctl" <<EOF
#!/usr/bin/env bash
# Fake launchctl for tests. Records calls and exits 0.
printf '%s\n' "\$*" >> "$BB_TEST_TMP/launchctl_calls.txt"
exit 0
EOF
  chmod +x "$BB_TEST_TMP/bin/launchctl"
  export PATH="$BB_TEST_TMP/bin:$PATH"
}

# Create a fake uname that returns a fixed value.
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

# Create fake runtime binaries under $BB_HOME/bin for bridge.bats tests.
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

# Create a fake runtime tarball for install.bats tests.
# Returns the path to the tarball.
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

# Default teardown. Test files that need their own teardown should call
# `helpers_teardown` from within their override rather than redefining this.
helpers_teardown() {
  stop_mock_http
  # Kill anything still running from the test.
  pkill -P $$ 2>/dev/null || true
  # Also clean up any listeners the orchestrator may have left on default ports.
  for port in 3001 3002; do
    for pid in $(lsof -t -i ":$port" 2>/dev/null); do
      kill "$pid" 2>/dev/null || true
    done
  done
  # Remove the visible extension directory created by tests.
  rm -rf "$BB_EXTENSION_DIR" 2>/dev/null || true
}

teardown() { helpers_teardown; }
