# Browser Bridge Installer

## Shell 环境排查

安装脚本要求 Bash ≥ 4，开发环境通常使用 zsh。macOS 用户在安装和日常使用中可能遇到 Bash 版本、PATH 顺序、默认 shell 切换等问题，详见 [shell-troubleshooting.md](./shell-troubleshooting.md)。

One-line install for macOS and Linux.

## Quick Start

```bash
curl -fsSL https://raw.githubusercontent.com/dkisser/browser-bridge/main/install/install.sh | bash
```

To pin a version: `BB_VERSION=v1.2.3 curl ... | bash`.

To install from a fork: `curl -fsSL https://raw.githubusercontent.com/dkisser/browser-bridge/main/install/install.sh | bash -s -- --source https://github.com/dkisser/browser-bridge.git`.

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
5. Shallow-clones (or updates) `https://github.com/dkisser/browser-bridge` into `~/.browser-bridge/repo/` at the matching tag.
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
