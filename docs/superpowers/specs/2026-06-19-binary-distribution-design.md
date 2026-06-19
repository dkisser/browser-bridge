# Browser Bridge Binary Distribution Design

Date: 2026-06-19
Status: Draft

## Goal

Replace the source-based installation path for end users with precompiled macOS binaries for `ws-server`, `local-proxy`, and `bridge-cmd`. After this change, a user installing Browser Bridge no longer needs `bun` or `git` on their machine.

The developer workflow inside the monorepo stays unchanged: contributors still use `bun run dev:*` and `bun test`.

## Scope Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Install method | Precompiled binaries downloaded from GitHub Releases | Removes the `bun` and `git` prerequisites for end users. |
| Platforms | macOS only (arm64 + x64) | Current primary target; Linux/Windows are future follow-ups. |
| Packaging | Per-architecture tarball containing the three runtime binaries | Clean Release page; single download per architecture. |
| Extension delivery | Keep separate zip asset | Extension is architecture-independent and already distributed as a zip for manual loading. |
| Source fallback | Removed | Per project decision; no `--source` git-clone fallback in `install.sh`. |
| Old runtime errors | Removed from docs/script | `BB-E025`/`BB-E026`/`BB-E027` and `bun`/`git` references are deleted. |

## Release Artifacts

Each tag release will contain:

```
browser-bridge-extension-{version}.zip
browser-bridge-extension-{version}.zip.sha256
browser-bridge-macos-arm64-{version}.tar.gz
browser-bridge-macos-arm64-{version}.tar.gz.sha256
browser-bridge-macos-x64-{version}.tar.gz
browser-bridge-macos-x64-{version}.tar.gz.sha256
```

### Tarball Internal Layout

```
browser-bridge-macos-arm64/
└── bin/
    ├── ws-server
    ├── local-proxy
    └── bridge-cmd
```

The binaries are stripped of architecture suffixes inside the tarball; they become the final executable names under `~/.browser-bridge/bin/`.

## Build Pipeline

Add `.github/workflows/release-binaries.yml` triggered on tags matching `v[0-9]+.[0-9]+.[0-9]+`.

Steps:

1. Verify `package.json#version` matches the tag suffix.
2. Verify `CHANGELOG.md` contains an entry for the version.
3. `bun install --frozen-lockfile`.
4. Cross-compile the three binaries for both architectures:
   - `bun build --compile apps/websocket/src/index.ts --outfile dist/ws-server --target=bun-darwin-arm64`
   - `bun build --compile apps/local-proxy/src/index.ts --outfile dist/local-proxy --target=bun-darwin-arm64`
   - `bun build --compile apps/cli/src/index.ts --outfile dist/bridge-cmd --target=bun-darwin-arm64`
   - Repeat with `--target=bun-darwin-x64`.
5. Stage and package:
   - `mkdir -p browser-bridge-macos-arm64/bin`
   - Copy arm64 binaries into `browser-bridge-macos-arm64/bin/`.
   - `tar czf browser-bridge-macos-arm64-{version}.tar.gz browser-bridge-macos-arm64`
   - Repeat for x64.
6. Generate `.sha256` sidecar files.
7. Upload all new artifacts to the same GitHub Release.

> If Bun cross-compilation between Darwin architectures proves unreliable, fall back to a matrix of macOS runners (arm64 runner for arm64 binary, x64 runner for x64 binary).

## Install Flow

### New `~/.browser-bridge/` Layout

```
~/.browser-bridge/
├── bin/
│   ├── bridge              # generated bash orchestrator
│   ├── bridge-cmd          # compiled CLI binary
│   ├── ws-server           # compiled WebSocket server binary
│   └── local-proxy         # compiled local proxy binary
├── extension/              # unpacked Chrome extension
├── run/
│   ├── local-proxy.pid
│   └── ws-server.pid
├── logs/
│   ├── local-proxy.log
│   └── ws-server.log
└── version                 # installed version string
```

`repo/` is removed from the end-user layout.

### `install.sh` Steps

1. **Prerequisite checks**
   - bash >= 4
   - `curl`
   - `unzip`
   - `shasum -a 256` (preinstalled on macOS)
   - `python3` (preinstalled on macOS; used for release JSON parsing and manifest validation)
   - No `bun` or `git` check.

2. **Resolve version**
   - Default to latest GitHub Release, overridable via `BB_VERSION`.

3. **Download extension zip**
   - Keep existing logic: download `browser-bridge-extension-{version}.zip` + `.sha256`, verify, extract to `extension/`.

4. **Download runtime tarball**
   - Detect architecture via `uname -m`:
     - `arm64` -> `browser-bridge-macos-arm64-{version}.tar.gz`
     - `x86_64` -> `browser-bridge-macos-x64-{version}.tar.gz`
     - Anything else -> `BB-E033`.
   - Download tarball + `.sha256`, verify checksum.
   - Extract tarball under `~/.browser-bridge/`.
   - Move the contents of the tarball's `bin/` directory into `~/.browser-bridge/bin/`.

5. **Write `bridge` orchestrator**
   - Generate `~/.browser-bridge/bin/bridge` from `install/bridge.sh.tmpl`, substituting version/org markers.
   - Symlink it to `~/.local/bin/bridge`.

6. **Write version file**
   - `echo "{version}" > ~/.browser-bridge/version`.

7. **Print next steps**
   - PATH reminder, Chrome "Load unpacked" pointer, `bridge up`.

## `bridge` Orchestrator Changes

### `cmd_up`

Start services by executing the compiled binaries directly instead of `cd repo && bun run start`:

```bash
BRIDGE_WS_PORT="$WS_PORT" start_service ws-server "$BB_HOME/bin/ws-server" "$WS_PORT"
BRIDGE_LOCAL_PORT="$LOCAL_PROXY_PORT" BRIDGE_LOCAL_PROXY_PORT="$LOCAL_PROXY_PORT" BRIDGE_WS_URL="ws://127.0.0.1:$WS_PORT" start_service local-proxy "$BB_HOME/bin/local-proxy" "$LOCAL_PROXY_PORT"
```

`start_service` runs the binary with stdout/stderr redirected to the log file and writes the PID file.

### `cmd_doctor`

Remove checks for `bun` and `repo/`. Add checks:

- `ws-server`, `local-proxy`, and `bridge-cmd` exist under `$BB_HOME/bin/` and are executable.
- `extension/manifest.json` is valid JSON.
- Services are running if PID files point to live processes.

### `cmd_update`

Keep the same user-facing behavior but internally re-run `install.sh` logic (download new extension zip + tarball, extract, symlink) instead of `git fetch` + `bun install`. After install, run `bridge restart` if the install succeeded.

## Error Codes

| Code | Meaning | Notes |
|---|---|---|
| `BB-E000` | Bash < 4 or missing | Unchanged. |
| `BB-E001` | Prerequisite missing | Now refers to bash/curl/unzip/shasum/python3. No `bun`/`git` mention. |
| `BB-E002` | Install not run or binaries missing | Unchanged message, updated check. |
| `BB-E010` | Port already in use | Unchanged. |
| `BB-E011` | Service failed to bind port within timeout | Unchanged. |
| `BB-E020` | Extension zip SHA-256 mismatch | Unchanged. |
| `BB-E021` | Download failed (HTTP error) | Unchanged. |
| `BB-E022` | Invalid version string | Unchanged. |
| `BB-E023` | Git fetch failed | **Removed**. |
| `BB-E024` | Clone or reset failed | **Removed**. |
| `BB-E025` | `bun install` failed | **Removed**. |
| `BB-E026` | CLI build failed | **Removed**. |
| `BB-E027` | CLI binary copy failed | **Removed**. |
| `BB-E028` | Runtime tarball download failed | New. |
| `BB-E029` | Runtime tarball SHA-256 mismatch | New. |
| `BB-E032` | Runtime tarball extraction failed or missing expected binaries | New. |
| `BB-E033` | Unsupported architecture | New; only arm64/x86_64 macOS supported in this design. |
| `BB-E101` | Unknown subcommand | Unchanged. |
| `BB-E102` | Unknown log target | Unchanged. |
| `BB-E103` | Cannot locate installer (update) | Unchanged. |

## Environment Variables

All existing environment variables remain supported because the compiled binaries read them the same way the source entry points do:

| Variable | Consumer | Purpose |
|---|---|---|
| `BRIDGE_WS_PORT` | ws-server | Port the cloud WebSocket server binds. |
| `BRIDGE_LOCAL_PORT` | local-proxy | Port the local proxy binds. |
| `BRIDGE_LOCAL_PROXY_PORT` | local-proxy | Alias for the local proxy port. |
| `BRIDGE_WS_URL` / `BRIDGE_SERVER_URL` | local-proxy | Upstream ws-server URL. |
| `BRIDGE_API_KEYS` | ws-server | Comma-separated API keys enabling handshake auth. |
| `BRIDGE_API_TOKEN` | local-proxy | Bearer token sent during upstream handshake. |

## Testing Strategy

| Suite | Changes |
|---|---|
| `install/tests/install.bats` | Replace git-clone/bun-install mocks with a fake GitHub release: a fake tarball containing three shell-script binaries. Verify `install.sh` downloads, checksums, extracts, writes `bridge`, and symlinks to `~/.local/bin/bridge`. |
| `install/tests/bridge.bats` | Remove dependency on `$REPO_DIR`. Use fake `ws-server`/`local-proxy` binaries that bind ports and sleep, then verify `up`/`down`/`restart`/`status` and env var propagation. |
| `apps/cli`, `apps/websocket`, `apps/local-proxy` unit tests | No changes; they continue to validate source behavior. |
| Manual smoke test | On a clean macOS machine, run the curl-pipe install, load the extension, run `bridge up`, `bridge browser:list`, `bridge down`. |

## CI / Release Workflow

- New `.github/workflows/release-binaries.yml` handles binary/tarball build and upload.
- It can reuse the version/tag verification logic from the existing extension release workflow.
- Optional local helper scripts for manual testing:
  - `bun run build:binaries` -> produce `dist/ws-server`, `dist/local-proxy`, `dist/bridge-cmd` for the host architecture.
  - `bun run build:tarball` -> produce a tarball for the host architecture.

## Documentation Updates

- `install/README.md`
  - Update prerequisites table: remove `bun` and `git`.
  - Update "What It Does" steps: remove source clone and `bun install`.
  - Remove error codes `BB-E023` through `BB-E027`.
  - Add `BB-E028`, `BB-E029`, `BB-E032`.
  - Add a macOS Gatekeeper note.
- `shell-troubleshooting.md`
  - Remove or condense sections about bun/PATH/source-clone issues.
- `README.md` / `README.en.md`
  - Move the curl-pipe install + `bridge up` flow to the top as the user-facing Quick Start.
  - Keep the `bun run dev:*` workflow in a "Development" section, clearly labeled as contributor-only.

## Risks and Follow-ups

1. **macOS Gatekeeper**
   - Binaries are unsigned and will not be notarized.
   - Files downloaded via `curl` generally do not receive the `com.apple.quarantine` attribute, but browser-downloaded tarballs may.
   - Mitigation: document `xattr -d com.apple.quarantine ~/.browser-bridge/bin/*` in `install/README.md`.

2. **Linux / Windows expansion**
   - Out of scope for this design.
   - Future work: add `browser-bridge-linux-arm64.tar.gz` and `browser-bridge-linux-x64.tar.gz` using the same pipeline.

3. **Auto-update background checks**
   - Out of scope. `bridge version` continues to show installed + latest; `bridge update` remains manual.

4. **Source-install fallback**
   - Per project decision, not retained. If needed later, it can be reintroduced as an opt-in `--source` flag.
