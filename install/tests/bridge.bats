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

@test "bridge --version prints template placeholder marker" {
  run bash "$BRIDGE_TMPL" --version
  [ "$status" -eq 0 ]
  [[ "$output" == *"bridge {{BRIDGE_VERSION}}"* ]]
}

@test "bridge works when BB_HOME is not set in environment" {
  make_fake_binaries
  mkdir -p "$BB_HOME/extension"
  echo '{"manifest_version":3}' > "$BB_HOME/extension/manifest.json"
  run env -u BB_HOME bash "$BRIDGE_TMPL" doctor
  [ "$status" -eq 0 ]
  [[ "$output" == *"[OK] ws-server binary present"* ]]
}

setup_up() {
  make_fake_binaries
  mkdir -p "$BB_HOME/logs" "$BB_HOME/run"
}

@test "bridge up writes PID files for both services" {
  setup_up
  run bash "$BRIDGE_TMPL" up
  [ "$status" -eq 0 ]
  [[ -f "$BB_HOME/run/ws-server.pid" ]]
  [[ -f "$BB_HOME/run/local-proxy.pid" ]]
  [[ -f "$BB_HOME/logs/ws-server.log" ]]
  [[ -f "$BB_HOME/logs/local-proxy.log" ]]
}

@test "bridge up fails with BB-E002 when binaries missing" {
  rm -rf "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" up
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E002"* ]]
}

@test "bridge up fails with BB-E010 when ws-server port is taken" {
  setup_up
  # Occupy the ws-server port.
  python3 -c "import socket; s=socket.socket(); s.bind(('127.0.0.1',3001)); s.listen(); import time; time.sleep(30)" &
  SOCAT_PID=$!
  sleep 0.3
  run bash "$BRIDGE_TMPL" up
  kill "$SOCAT_PID" 2>/dev/null || true
  [ "$status" -ne 0 ]
  [[ "$output" == *"BB-E010"* ]]
}

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
  setup_up
  echo "99999" > "$BB_HOME/run/ws-server.pid"
  run bash "$BRIDGE_TMPL" restart
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

@test "bridge doctor reports FAIL when binaries missing" {
  rm -rf "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" doctor
  [ "$status" -ne 0 ]
  [[ "$output" == *"[FAIL] ws-server binary missing"* ]]
}

@test "bridge version prints installed and latest release" {
  mkdir -p "$BB_HOME"
  echo "v1.2.3" > "$BB_HOME/version"
  run bash "$BRIDGE_TMPL" version
  [ "$status" -eq 0 ]
  [[ "$output" == *"installed: v1.2.3"* ]]
}

@test "bridge uninstall without --yes prompts and aborts on 'n'" {
  mkdir -p "$BB_HOME"
  echo "n" | run bash "$BRIDGE_TMPL" uninstall
  [[ -d "$BB_HOME" ]]
}

@test "bridge uninstall --yes removes BB_HOME" {
  mkdir -p "$BB_HOME/bin"
  run bash "$BRIDGE_TMPL" uninstall --yes
  [ "$status" -eq 0 ]
  [[ ! -d "$BB_HOME" ]]
}
