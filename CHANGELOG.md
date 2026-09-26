# Changelog

All notable changes to Browser Bridge are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/) and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.4.0] - 2026-09-26

### Changed
- **Breaking for existing installs**: the control plane and CLI have been reimplemented in Go on a single binary (ADR-0012 + ADR-0013). `bridge-core` (the standalone Bun-built daemon) is gone — its functionality lives behind a hidden `bridge serve` subcommand inside `bridge`, so the public CLI surface is unchanged. The runtime binary drops from ~26.2 MB (two Bun builds) to ~17.7 MB (one static Go binary), and the previous failure mode of a partial update leaving a mixed-version CLI/daemon pair behind is no longer possible. The published Chrome extension is wire-compatible with the new binary unchanged. See `docs/adr/0012-control-plane-and-cli-in-go.md` and `docs/adr/0013-single-bridge-binary.md`.
- **Breaking for existing installs**: the bash service lifecycle router (`install/bridge.sh.tmpl`) and its `install/launchagent.plist.tmpl` are gone. `bridge service up|down|restart|status|logs|update|doctor|version|uninstall` are now Go subcommands, so the BB-E### codes, log/pid filenames (`bridge-core.log`, `bridge-core.pid`), and the supervisor design from ADR-0005 (foreground `bridge service up --foreground` LaunchAgent, `KeepAlive`, `ThrottleInterval`, pidfile ownership) are preserved, but `install.sh` no longer templates any bash — it ships the compiled binary plus the `install.sh` driver only. `install/tests/bridge.bats` is retired (replaced by the Go unit/integration suites); `install/tests/install.bats` continues to drive the bash install flow end-to-end against a mock release server.
- Tooling: the package-level scripts now cover both Bun and Go (`bun run check` runs `biome` + `gofmt` + `go vet` + `golangci-lint` + `tsc --noEmit` + `bun test` + `go test -race ./...`; `bun run build:binaries` invokes `go build` with `CGO_ENABLED=0` for darwin/linux × amd64/arm64). Go module path is shortened to `browser-bridge`. CI was bumped to `golangci-lint-action@v7` because v6 rejects golangci-lint v2.

### Fixed
- The MCP server on port 3003 is no longer at risk of disappearing from a built artifact: Go binaries are statically linked with no runtime module loading, which eliminates the `xsschema` dynamic-import failure class (transitive optional peers loaded by `fastmcp` had to be hoisted into `dependencies` to survive `bun build --compile`). After upgrading, port 3003 is present iff the binary itself is.
- `install/tests/install.bats` on the Linux CI runner no longer sees a platform mismatch: when the test mocks `uname -s` to Darwin, the bridge binary now reads `BB_GOOS=$(uname -s)` from the env (gated behind `BB_TESTING=1`, so production installs are unaffected) and walks the corresponding `service up` path. On Linux the post-install auto-start now spawns `bridge service up --foreground` detached via `nohup` so the supervisor writes `supervisor.pid` and `bridge-core.pid` exactly as the launchd path does on macOS, and `wait_for_supervisor` polls for the supervisor pidfile so a port-conflict failure surfaces the same message it does on macOS.

## [0.3.2] - 2026-09-24

### Fixed
- `install.sh print_next_steps` now emits real newlines for the auto-start and skills notes. The strings contained literal `\n`, which a `cat <<EOF` heredoc passes through verbatim, so the post-install message read `Browser Bridge vX.Y.Z installed.\n  Login auto-start is enabled...` on a single line. Switched to `$'...'` so the embedded `\n` becomes an actual newline and the `Browser Bridge … installed.` line is printed via `printf`. Backed by a new BATS assertion that the output contains no literal `\n` and that each note sits on its own line.

## [0.3.1] - 2026-09-24

### Changed
- Bumped the Chrome extension's `manifest.json` version to `0.1.0` so the published extension zip carries a meaningful first-version tag (previously `0.0.4`). Runtime binaries and CLI are unchanged from v0.3.0.

## [0.3.0] - 2026-09-24

### Added
- Pairing-based channel authentication: `bridge pair` prints a short-lived code; entering it in the extension popup issues a token (only its SHA-256 hash is stored on disk). The local proxy refuses unpaired WebSocket connections, and its HTTP API rejects non-extension web origins (CORS restricted to `chrome-extension://`).
- Extension-side policy gate (design in `docs/adr/0006`-`0009`): commands are denied immediately with machine-readable reasons (`human_assist_active`, `origin_not_approved`, `origin_denied`, `origin_blocked`, `action_out_of_scope`, `approval_required`) and surfaced as approval cards in the extension popup. Agents act silently only inside their working scope — agent-opened tabs plus human-approved origins; browser system pages and the built-in blocklist (including the Chrome Web Store) are hard-denied with no approval path.
- Human assist (takeover) toggle in the popup; one-time approvals for password/credit-card input and form submission (`type` now honors the previously ignored `submit` param); downloads initiated in agent tabs are paused for review; user-managed origin approvals/denials, custom blocklist entries, and session-scoped approvals that expire on browser restart.

### Changed
- **Breaking for existing installs**: `ws-server` and `local-proxy` are merged into a single `bridge-core` binary (ADR-0011). The install now ships two binaries instead of three (`bridge-core` + `bridge-cmd`), saving one Bun runtime (~60 MB uncompressed). Default ports stay the same: 3001 for the WebSocket control plane (CLI), 3002 for the extension bridge, 3003 for the Streamable HTTP MCP server. The `bridge-cmd --server ws://localhost:3001` URL keeps working unchanged. On upgrade, the install force-reinitializes `~/.browser-bridge/config.json` and removes the old binaries, so run `bridge pair` once to re-pair the extension after upgrading. See `docs/adr/0011-bridge-core-merges-ws-server-and-local-proxy.md`.
- **Breaking for existing installs**: the local proxy now requires a paired extension. After upgrading, run `bridge pair` and enter the code in the extension popup to reconnect (the popup also has a re-pair button if the token goes stale).

### Fixed
- MCP tools now surface the human-readable recovery guidance from error responses (policy denials, relay failures) instead of only the machine-readable reason code. An `origin_not_approved` denial tells the agent that a human must approve the origin in the Browser Bridge extension popup (toolbar icon) and not to bypass the gate with WebFetch, instead of leaving the agent to blindly retry and fall back to other tools.
- `bridge service down`/`restart` no longer fail with `BB-E304` on macOS: launchd loads LaunchAgents into the GUI session domain, and current macOS rejects the per-user domain for both `bootout` ("No such process") and `bootstrap` (I/O error), so service lifecycle calls now target `gui/$UID`; `bootout` still falls back to `user/$UID` for jobs parked there by older releases, and a failed bootout surfaces the launchd error instead of swallowing it.

## [0.2.1] - 2026-09-21

### Fixed
- `bridge [service] update` with no argument works again: the installer now treats `BB_VERSION=latest` (the update command's default target) as "resolve the latest release" instead of failing with `BB-E022: invalid version 'latest'`. Older installed bridges pick up the fix automatically because the update command always downloads the newest release installer.
- Upgrading from a pre-supervision install no longer strands launchd: `bridge service up` verifies a live supervisor before declaring "already running", replaces a stale loaded LaunchAgent job (old `bridge up` semantics) via `launchctl bootout` + re-bootstrap, and adopts already-running pidfile-owned services instead of refusing with `BB-E010`.

## [0.2.0] - 2026-09-21

### Added
- macOS login auto-start is now real supervision: a single per-user LaunchAgent runs `bridge service up --foreground`, a supervisor process that holds ws-server and local-proxy and restarts whichever child dies (`KeepAlive=true`; crash loops are bounded by `ThrottleInterval`). Design recorded in `docs/adr/0005-service-namespace-launchd-supervision.md`.
- `bridge service enable|disable` manage login auto-start explicitly (state is shown by `bridge service status`); on macOS `service up` bootstraps the supervisor from a staging plist when auto-start is disabled, so "start now" and "start at login" are independent.
- Browser commands print an actionable hint (`Start it with: bridge service up`) when the bridge server is not reachable.

### Changed
- **Breaking**: all service lifecycle commands moved under the `bridge service` namespace (`up`, `down`, `restart`, `status`, `logs`, `update`, `doctor`, `version`, `uninstall`). Top-level `bridge` is browser control only; the old top-level spellings fail with `BB-E305` pointing at the new form, and unknown verbs no longer fall through to the browser-command binary when it is missing. `bridge autostart on|off|status` remains for one release as a deprecated alias.
- macOS: `bridge service down` now unloads the LaunchAgent job (`launchctl bootout`) instead of leaving orphaned processes; `service disable` no longer stops running services — it only governs login behavior.
- The installer calls the new `bridge service …` spellings (`--no-autostart` is unchanged and maps to `bridge service disable`).

### Fixed
- Review fixes on the supervision mechanics:
  - the supervisor watch loop pauses a real second between polls (`sleep 1`); a `read -t` on `/dev/null` returns immediately on EOF and would have busy-looped the launchd-resident process at 100% CPU;
  - login-time idempotency requires alive pidfiles plus answering ports, so foreign port listeners fail non-zero and launchd backs off instead of silently restarting every `ThrottleInterval` on a clean exit;
  - `service down` on macOS only sweeps pidfile-backed strays after `bootout`, suppressing "already stopped" noise, while still catching orphans of a SIGKILLed supervisor;
  - LaunchAgent plist rendering escapes `& < >` (awk `gsub` replacement expansion and XML) so paths containing them no longer produce a malformed plist;
  - `port_in_use` probes the configured hostname instead of a hardcoded `localhost`;
  - `service disable` also removes the staging plist, not just the `~/Library/LaunchAgents` copy.
  - a supervisor killed with SIGKILL no longer strands its children: on relaunch it verifies pidfile pids via `ps`, adopts the orphaned bridge children (including half-orphaned pairs), starts what's missing, and keeps watching them; a `supervisor.pid` lock (BB-E307) keeps a second foreground supervisor from racing the first.

## [0.1.1] - 2026-09-15

### Changed
- `snapshot` defaults to the `interactive` filter — interactive elements (links, buttons, text boxes, checkboxes, combos) and headings with stable `@eN` refs — landing ~1.5K tokens on typical app pages; pass `filter='full'` for the complete pseudo-tree including text runs and structural containers. The default budget is 8000 chars for interactive snapshots (3000 remains the `full` default). Reading page content stays with `gettext`/`get_text`. Design recorded in `docs/adr/0002-interactive-default-filter.md`.
- `full`-filter snapshots are tightened: img lines no longer carry `src` URLs (alt remains the img name), text runs that duplicate their nearest named ancestor are omitted (including runs wrapped in elements like spans), and chains of nameless single-child `generic` wrappers collapse into one line. Ref-bearing lines stay addressable; interactive output is unchanged.

### Fixed
- Tier-2 snapshot truncation no longer drops text runs silently: `full`-filter output marks each suppressed run with `text [text suppressed]`, and the stats line reports the active tier (e.g. `[nodes: 40/484 | tier: 2 | truncated: true]`), so an empty-looking cell is distinguishable from suppressed text. (TODO.md #1)
- `wait:navigation` no longer times out when the navigation already completed before the command arrived (e.g. a remedial call after `navigate` timed out on a redirect chain): it checks the tab's current status before listening for `onUpdated`. (TODO.md #2)
- Selector resolution failures now report that both CSS and exact-text matching were tried, with a hint for virtualized lists like Gmail; `gettext`/`get_text` on an element with empty or whitespace-only text now explains the match-but-empty case instead of returning an empty string. (TODO.md #3)

## [0.1.0] - 2026-09-15

### Added
- `snapshot` command that captures the page as a DOM pseudo-tree with stable element refs, exposed via both the CLI and the MCP server. Agents can read page structure and target elements precisely without fetching full HTML. Design recorded in `docs/adr/0001-snapshot-pseudo-tree.md`.
- Comprehensive tests for the CLI and local-proxy components.
- Draft launch post under `docs/hn-launch-post.md`.

### Changed
- Test suites reorganized into per-app `tests/` directories (previously colocated `src/__tests__`).
- README gained CLI and MCP usage sections; the glossary in CONTEXT.md now defines the Inbound adapter.

### Fixed
- Regenerated `bun.lockb` for bun 1.3+ resolution. The committed lockfile failed `bun install --frozen-lockfile` on current bun, which would have broken CI and release workflows that pin `bun-version: latest`.
- `ManagedClient` tests now await socket close after `dispose()`; bun 1.4 completes the WebSocket closing handshake asynchronously, so `readyState` is no longer `CLOSED` synchronously.

## [0.0.8] - 2026-07-07

### Added
- Explicit tab handles for all browser tool operations. Every page-level command now requires `--tab <id>` (CLI) or `tab_id` (MCP), enabling concurrent multi-tab workflows and protecting the user's active tab.
- `tab:new` defaults to opening tabs in the background (`active: false`) and returns the new tab id for follow-up commands.
- Schema-level validation that rejects page-level MCP tool calls missing `tab_id`.
- CLI validation that rejects page-level commands missing `--tab`.
- Extension service worker validation that rejects commands missing `tabId`.
- CI compile check in `.github/workflows/base-check.yml` to verify `bun build --compile` succeeds for `ws-server`, `local-proxy`, and `bridge-cmd` on every PR.

### Changed
- Moved `@valibot/to-json-schema`, `arktype`, `effect`, and `sury` from `devDependencies` to `dependencies` in `apps/websocket/package.json` because they are dynamically imported at runtime inside the compiled `ws-server` binary.
- Updated `skills/browser-bridge-user/SKILL.md`, `README.md`, and `README_CN.md` to document the new `--tab` workflow.

## [0.0.7] - 2026-06-29

### Fixed
- Added missing optional peer dependencies required by `xsschema` (`@valibot/to-json-schema`, `arktype`, `effect`, `sury`) to `apps/websocket/package.json`. This fixes the v0.0.6 release `ws-server` binary that only started the WebSocket server and left the MCP Streamable HTTP endpoint on port 3003 unbound.

## [0.0.6] - 2026-06-29

### Added
- Installer now exposes the Chrome extension through a visible symlink at `~/Browser-Bridge/extension/` pointing to `~/.browser-bridge/extension/`, so Chrome "Load unpacked" no longer requires showing hidden folders.
- Installer automatically starts `bridge up` after installation; users only need to load the Chrome extension manually.
- Installer skips re-installation when the installed version already matches the target version; use `--force` to reinstall anyway.

### Changed
- Installer stops any running bridge services before downloading an update, then restarts them after installation.
- `bridge doctor` now recognizes the extension manifest at `~/Browser-Bridge/extension/manifest.json`.
- `bridge uninstall --yes` now removes both `~/.browser-bridge/` and `~/Browser-Bridge/` and cleans up the stale `~/.local/bin/bridge` symlink.
- README and install README updated to document the new one-step install flow.

### Fixed
- CLI commands no longer hang for ~5 seconds after printing output; the WebSocket connection timeout timer is now cleared as soon as the socket opens.

### Removed
- Stray `console.log('Connected to server')` from the WebSocket client.

## [0.0.4] - 2026-06-23

### Added
- User-facing `browser-bridge-user` skill under `skills/browser-bridge-user/` with installation and usage guide.
- `install.sh` auto-discovers and installs the bundled `./skills` directory by default, with `--skills-dir` and `--no-skills` overrides.
- Timestamped `bridge logs` output via a lightweight Python helper for cross-platform compatibility.
- `install/tests/install.bats` coverage for skills installation, including default detection, custom directory, and opt-out.

### Changed
- README and extension manifest now lead with the cross-agent positioning: "Browser as a tool for any agent."
- README layout refreshed to highlight cross-agent / multi-agent compatibility.
- Project logo centralized in `docs/assets/logo.png`; extension build copies it automatically to `dist/icon.png`.
- `bridge.sh.tmpl` updated to support the new installer flags and logging behavior.

### Fixed
- `bridge up` unbound `BB_HOME` variable guard strengthened for environments that do not export it.
- `install.sh` option parsing and help text cleaned up after flag renames.

### Security
- Local WebSocket and proxy servers now bind to `127.0.0.1` only, preventing external network connections.

## [0.0.3] - 2026-06-20

### Added
- Self-contained `install.sh` release asset. The installer now embeds the `bridge` orchestrator template so installations no longer fetch scripts from the `main` branch.
- `bridge update [version]` fetches the installer from the matching release asset URL.

### Fixed
- `bridge up` failing with an unbound `BB_HOME` variable when the environment did not export it.

## [0.0.2] - 2026-06-19

### Added
- One-line installer (`curl ... | bash`) and `bridge` orchestrator for single-machine deployments.
- Prebuilt extension zip distributed via GitHub Releases with SHA-256 verification.

## [0.0.1] - 2026-06-12

### Added
- Initial release: CLI, WebSocket Server, Local Proxy, Chrome Extension.
