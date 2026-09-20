# Service lifecycle lives in `bridge service`, supervised by one LaunchAgent

The `bridge` CLI mixed two roles — service lifecycle (up/down/…) and browser command dispatch (delegated to `bridge-cmd`) — behind a silent fallback that forwarded unknown verbs and inverted `--help`, and the macOS LaunchAgent only launched services at login without supervising them: a crashed service stayed down until the next login. We split the surfaces — `bridge <verb>` is browser control only, every lifecycle operation lives under `bridge service <verb>` — and turned login auto-start into real supervision: a single per-user LaunchAgent runs `bridge service up --foreground`, a supervisor process that holds ws-server and local-proxy in the foreground and restarts whichever child dies. `KeepAlive=true` plus `bootout`-based `service down` keep "stop now" orthogonal to "start at login" (enable/disable = whether the plist sits in `~/Library/LaunchAgents`). The word "daemon" was deliberately rejected: login auto-start is per-user and login-scoped, never a boot-time system daemon.

## Considered Options

- **launchd manages ws-server and local-proxy directly (two plists, `KeepAlive=true`)**: most native, zero new process code — but `up/down/status` would become launchctl wrappers on macOS while Linux keeps the pidfile path, forking the orchestration logic per platform forever, and "start now without login auto-start" needs a staging-directory trick regardless.
- **Single LaunchAgent supervising a foreground supervisor (chosen)**: CLI semantics (up/down/status/logs) stay identical, one launchd job, smaller supervision surface, and the same `--foreground` supervisor can later run under systemd on Linux. Cost: a child-reaping loop in the bridge script.
- **`bridge daemon` vs `bridge service enable|disable`**: "daemon" implies a system-level always-on process, which this is not. `service` matches the systemd / brew-services vocabulary; `bridge autostart` survives one release as a deprecated alias.

## Consequences

- `service down` must be `launchctl bootout`, never a signal: with `KeepAlive=true`, killing the supervisor process makes launchd relaunch it. Stopping = unloading the job; login behavior is governed separately by plist presence in `~/Library/LaunchAgents`.
- The supervisor restarts only the child that died. Both local-proxy (exponential backoff, 30s cap) and the extension reconnect to a restarted ws-server on their own, so a group restart would only widen the disconnect window.
- At startup the supervisor exits 0 when both ports are already served by a healthy instance (idempotent up — e.g. a manually started pair); a conflict against an unhealthy process exits non-zero and relies on launchd's ThrottleInterval backoff.
- Auto-start remains macOS-only; Linux keeps pidfile-based orchestration and can later wrap the same supervisor in a systemd user unit.
