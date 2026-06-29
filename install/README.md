# Browser Bridge Installer

## Shell 环境排查

安装脚本要求 Bash ≥ 4，开发环境通常使用 zsh。macOS 用户在安装和日常使用中可能遇到 Bash 版本、PATH 顺序、默认 shell 切换等问题，详见 [shell-troubleshooting.md](./shell-troubleshooting.md)。

One-line install for macOS and Linux.

## Quick Start

```bash
curl -fsSL https://github.com/dkisser/browser-bridge/releases/latest/download/install.sh | bash
```

To pin a version: `BB_VERSION=v1.2.3 curl ... | bash`.

## Prerequisites

| Tool | Required | Notes |
|---|---|---|
| bash ≥ 4 | yes | macOS users: pre-installed. |
| curl | yes | pre-installed. |
| unzip | yes | `brew install unzip` if missing. |
| shasum | yes | pre-installed on macOS. |
| python3 | yes | pre-installed on macOS. |

## What It Does

1. Verifies prerequisites.
2. Resolves the target version (latest release, or `BB_VERSION` override).
3. Downloads `browser-bridge-extension-{version}.zip` and its `.sha256` from the GitHub Release; aborts on mismatch.
4. Extracts the extension into `~/.browser-bridge/extension/` and exposes it through a symlink at `~/Browser-Bridge/extension/` for easy Chrome loading.
5. Detects the macOS architecture (arm64 or x64).
6. Downloads the matching runtime tarball (`browser-bridge-macos-{arch}-{version}.tar.gz`) and its `.sha256`; aborts on mismatch.
7. Extracts the three binaries (`ws-server`, `local-proxy`, `bridge-cmd`) into `~/.browser-bridge/bin/`.
8. Writes `~/.browser-bridge/bin/bridge` (templated from `install/bridge.sh.tmpl`) and symlinks it into `~/.local/bin/bridge`.
9. Writes the resolved version to `~/.browser-bridge/version`.
10. Stops any already-running bridge services, then starts them again after installation.
11. Prints next steps: PATH export, Chrome "load unpacked" pointer at `~/Browser-Bridge/extension/`.

## `bridge` Commands

### Service orchestration

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

### Browser control (requires `bridge up` and a connected browser)

| Command | Purpose |
|---|---|
| `bridge browser:list` | List connected browsers. |
| `bridge --browser <id> navigate <url>` | Open a URL. |
| `bridge --browser <id> click <selector>` | Click an element. |
| `bridge --browser <id> type <selector> <text>` | Type text. |
| `bridge --browser <id> screenshot` | Take a screenshot. |
| `bridge --browser <id> tab:list` | List tabs. |

Run `bridge --help` for the full command list.

## Error Codes

| Code | Meaning | Fix |
|---|---|---|
| `BB-E000` | Bash < 4 or missing | Upgrade bash. |
| `BB-E001` | Prerequisite missing | Install `curl`, `unzip`, `shasum`, or `python3`. |
| `BB-E002` | `bridge` invoked without install | Run the install script. |
| `BB-E010` | Port already in use | `lsof -i :3001` (ws-server) or `lsof -i :3002` (local-proxy), kill the conflict. |
| `BB-E011` | Service failed to bind port | Check `~/.browser-bridge/logs/`. |
| `BB-E020` | Extension zip SHA-256 mismatch | Re-run; check network/proxy. |
| `BB-E021` | Download failed (HTTP error) | Check network, retry. |
| `BB-E022` | Invalid version string | Use `vX.Y.Z` format. |
| `BB-E028` | Runtime tarball download failed | Check network; verify the release includes a tarball for your architecture. |
| `BB-E029` | Runtime tarball SHA-256 mismatch | Re-run; check network/proxy. |
| `BB-E032` | Runtime tarball extraction failed | Inspect `~/.browser-bridge/bin/`. |
| `BB-E033` | Unsupported architecture | Only macOS arm64 and x64 are supported. |
| `BB-E030` | Tag/version mismatch on release | Fix `package.json` and re-tag. |
| `BB-E031` | CHANGELOG missing entry for release | Add an entry, re-tag. |
| `BB-E100` | Subcommand stub (during dev) | Implementation pending. |
| `BB-E101` | Unknown subcommand | Run `bridge` for help. |
| `BB-E102` | Unknown log target | Use `ws-server` or `local-proxy`. |
| `BB-E103` | Cannot locate installer (update) | Re-run the install script manually. |

### macOS Gatekeeper

If macOS blocks the downloaded binaries with "cannot be opened because the developer cannot be verified", remove the quarantine attribute:

```bash
xattr -d com.apple.quarantine ~/.browser-bridge/bin/*
```

## Tests

```bash
brew install bats-core   # macOS
apt install bats         # Debian/Ubuntu

bun run test:install
bun test install/tests/release-workflow.test.ts
```

## Manual Smoke Test

```bash
# After install, bridge services are already running:
bridge status         # both services running
bridge doctor         # all OK
# In Chrome:
#   1. Open chrome://extensions/
#   2. Enable "Developer mode"
#   3. Click "Load unpacked"
#   4. Select ~/Browser-Bridge/extension/
# In another terminal:
bridge browser:list
bridge --browser <browserId> navigate https://example.com
bridge down
bridge uninstall --yes
```
