---
name: browser-bridge-release
description: |
  Use this skill when the user wants to cut and publish a new Browser Bridge release — bumping the version, updating the changelog, creating the release tag, pushing it, or verifying that a release built correctly. Trigger phrases include "发布", "发版", "cut a release", "publish a release", "release vX.Y.Z", "tag a release", or any request to ship a new version of Browser Bridge.
whenToUse: When the user asks to publish a new release, cut/tag a version (e.g. "发布 v0.0.9", "发版", "release vX.Y.Z"), or check on a pending/failed release of this repository.
---

# Browser Bridge Release Skill

Releasing Browser Bridge is tag-driven: pushing a tag `vX.Y.Z` triggers three GitHub Actions workflows that build every release asset and attach them to a GitHub Release automatically. The coding agent executes this skill itself — run the commands, edit the files, and create the commit; do not hand the user a checklist and ask them to run it. Flow: pre-flight checks → version bump → tag → push → verify.

## How release works (read first)

- The version source of truth is the root `package.json` `version` field. CI fails with BB-E030 if it does not match the tag.
- `CHANGELOG.md` must contain an entry for the version (BB-E031).
- Three workflows fire on tag push:
  - `release-binaries` (macOS arm64): runtime tarball `browser-bridge-macos-arm64-<tag>.tar.gz` + `.sha256` (contains the single Go binary `bridge` since ADR-0013 — CLI plus the hidden `serve` control-plane subcommand)
  - `release-extension` (Ubuntu): `browser-bridge-extension-<tag>.zip` + `.sha256`
  - `release-installer` (Ubuntu): self-contained `install.sh`
- The GitHub Release itself is auto-created by `softprops/action-gh-release`; do NOT run `gh release create` yourself.

## Step 1 — Pre-flight checks

Run all of these locally, on a clean tree at the tip of `main`:

1. `bun install --frozen-lockfile`
2. `bun run check` — the full gate: lint (biome + gofmt + `go vet` + golangci-lint) + `tsc` + all tests (bun + `go test -race`).
3. `bun run test:install` — BATS installer tests. **If they fail or hang twice in a row, stop using BATS and validate directly with bash**: simulate `bridge up` with fake binaries, confirm services bind to `127.0.0.1`, and verify external IPs cannot connect (see AGENTS.md).
4. `bun run build:binaries`, then smoke-test the compiled binary by actually starting it. ADR-0013 merged everything into one `dist/bridge`; the control plane is the hidden `bridge serve` subcommand. Smoke it isolated — never touch the production ports 3001-3003:
   ```bash
   ./dist/bridge --version   # must print the tag version (injected via -ldflags from package.json)
   BB_HOME=/tmp/bb-smoke BRIDGE_WS_PORT=3311 BRIDGE_LOCAL_PORT=3312 BRIDGE_MCP_PORT=3313 ./dist/bridge serve &
   sleep 2; lsof -nP -iTCP:3311,3312,3313 -sTCP:LISTEN   # all three LISTEN
   kill %1; rm -rf /tmp/bb-smoke
   ```

Do not start Step 2 with any check failing or uncommitted changes.

## Step 2 — Version bump

1. Pick the next version per SemVer; check the current one in root `package.json`.
2. Update `version` in root `package.json` **only**. Do NOT touch the `version` fields in `apps/*/package.json` or `apps/extension/manifest.json` — the release does not use them. The binary version is injected from here at build time (`-ldflags "-X main.version=..."` in build-binaries.sh), so this stays the single sync point.
3. Update `CHANGELOG.md`: move items out of `[Unreleased]` into a new `## [X.Y.Z] - YYYY-MM-DD` section, following the Keep a Changelog format already used in the file (no link-reference definitions needed at the bottom).
4. Commit the two files, e.g. `git commit -am "release: vX.Y.Z"`.

## Step 3 — Tag and push

Pushing the tag is the point of no return: it immediately triggers public release builds and creates the GitHub Release. **Confirm the exact version number with the user before pushing.**

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

## Step 4 — Verify the release

1. Watch the runs and wait for all three to succeed:
   ```bash
   gh run list --workflow=release-binaries.yml
   gh run list --workflow=release-extension.yml
   gh run list --workflow=release-installer.yml
   ```
2. Verify the assets on the release:
   ```bash
   gh release view vX.Y.Z
   ```
   Expect 5 files: macOS tarball + `.sha256`, extension zip + `.sha256`, and `install.sh`.
3. Optionally smoke-test the installer exactly as a user would. Note this installs/updates `~/.browser-bridge` on the machine it runs on:
   ```bash
   curl -fsSL https://github.com/dkisser/browser-bridge/releases/download/vX.Y.Z/install.sh | bash
   ```
4. If a workflow fails after the tag was pushed: fix on a follow-up commit first. Only delete and re-push the tag if no release assets were published yet; otherwise bump the patch version and release that.

## Common failure modes

- **BB-E030**: root `package.json` version ≠ tag. Fix the version, re-tag.
- **BB-E031**: `CHANGELOG.md` missing an entry for the version.
- **Binary starts but a port is missing**: post-ADR-0012 the binary is a static Go build, so this is almost always a port conflict with an already-running instance — check `lsof -nP -iTCP:3001,3002,3003 -sTCP:LISTEN` and `~/.browser-bridge/logs/`.
- **release-binaries only builds arm64**: x86_64 is intentionally disabled because GitHub retired the macos-13 runner. Do not "fix" this.
