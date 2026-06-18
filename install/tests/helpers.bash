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

# Start a tiny Python HTTP server on a free port serving $BB_TEST_TMP/www.
start_mock_http() {
  mkdir -p "$BB_TEST_TMP/www"
  python3 -m http.server -d "$BB_TEST_TMP/www" "$@" >"$BB_TEST_TMP/http.log" 2>&1 &
  MOCK_HTTP_PID=$!
  sleep 0.2
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
}

teardown() { helpers_teardown; }
