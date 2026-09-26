# AGENTS.md

This file provides guidance to the AI agent when working with code in this repository.

For project structure, commands, architecture, and conventions, see [README.md](./README.md).

## Non-obvious constraints

- **Package scope mismatch is intentional**: apps use `@browser-bridge/*`, the shared package uses `@browser-bridge/shared`. Do not "fix" this.
- **No build step for workspace packages**: `@browser-bridge/shared` ships raw `.ts` source. Do not add a build step.
- **Branch `go-rewrite`**: `apps/bridge-core/` is a Go module (`go.mod`, `cmd/bridge`, `internal/`) replacing the deleted Bun/TS control plane and CLI — see `docs/adr/0012-control-plane-and-cli-in-go.md`. The TS `apps/cli/` was deleted in Phase 2b; `cmd/bridge` (cobra) replaces it, backed by `internal/ws` (WS client) and `internal/core` (envelope). The TS bridge-core remains available in a `main` worktree for fixture extraction.
- **Single binary (ADR-0013)**: `cmd/bridge-core` is deleted; the control plane runs as the hidden `bridge serve` subcommand of the one `bridge` binary, spawned by the supervisor (`bridge service up --foreground`). The *service* keeps the bridge-core name (log/pidfile stems, status text).
- **Bridge-core merge (ADR-0011)**: the pre-merge `apps/websocket/` and `apps/local-proxy/` are gone; both halves lived in `apps/bridge-core/` (TS, deleted on this branch). The `@browser-bridge/websocket` package is gone — its `protocol` and `client` modules were in-tree under `apps/bridge-core/src/`, then vendored in `apps/cli/src/`, and are now deleted with it; the Go counterparts are `internal/core` (envelope) and `internal/ws` (client).
- **Extension tsconfig**: `apps/extension/tsconfig.json` sets `types: ["chrome"]` only — node-style globals are unavailable.
- **Extension Vite plugin**: `apps/extension/vite.config.ts` flattens `sidepanel.html` / `settings.html` / `offscreen.html` and copies manifest.json in `closeBundle`. `src/content.ts` is a separate build (`vite.content.config.ts`) that must stay IIFE — Chrome's `content_scripts` has no `type: "module"`, so `dev` runs both watchers concurrently. See README for details.
- **CLI entry**: `apps/bridge-core/cmd/bridge` (cobra) is the CLI. Root scripts drive it: `bun run cli` = `go -C apps/bridge-core run ./cmd/bridge`, `bun run build:cli` = `go build` to `dist/bridge`.
- **Command result contracts are typed**: per-command `data` shapes live in `packages/shared/src/types.ts` (`CommandResultMap`). Extension handlers (`handleCommand`, `executeCommand`) are annotated against them, MCP tools read `data` through them, and test mocks must be constructed from them. `ResponsePayload.data` stays `unknown` at the wire level on purpose — do not bypass the contract types.

## Testing

- Use `bun run test` for the Bun unit/integration test suite.
- Use `bun run test:install` for the BATS installer tests (`install/tests/install.bats` — install.sh only; the service lifecycle moved into the Go binary and its BATS suite is gone).
- One umbrella for both toolchains: `bun run check` = lint (biome + gofmt check + `go vet` + golangci-lint) + `type-check` + `test` (bun test + `go test -race ./...`). `bun run fmt` fixes both (biome --write + gofmt -w). The Go contract suite (golden JSON fixtures, `tools/list` diff) runs as part of `go test` under `apps/bridge-core`.
- BATS tests spawn real subprocesses and may hang if background services are not detached cleanly.
- **BATS never touches real launchd or the production ports**: the suite exports test ports 3311-3313 (never 3001-3003), and `install.bats` installs a fake `launchctl` in `setup()` because the Go binary always walks the launchd path on macOS regardless of any fake `uname`.
- **If the BATS installer tests fail or hang twice in a row, stop using BATS and validate directly with bash.** Simulate `bridge service up` with fake binaries, confirm services bind to `127.0.0.1`, and verify external IPs cannot connect.

## Compiled binaries

On this branch the only runtime binary is Go: `bridge` (stateless CLI + `service` lifecycle + the hidden `serve` control-plane subcommand, ADR-0013). It is built with plain `go build` (see `bun run build:binaries` / `.github/scripts/build-binaries.sh`, which injects the package.json version via `-ldflags "-X main.version=..."`); the old `bun build --compile` fragility class (dynamic `import()` of optional peers like `xsschema` inside compiled binaries) is gone by construction — ADR-0012.

