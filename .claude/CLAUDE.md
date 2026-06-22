# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Browser Bridge is a personal/demo Bun + TypeScript monorepo. See `@README.md` for project structure, setup, and workflow details.

- Runtime/package manager: Bun (`bun.lockb` present)
- Workspaces: `apps/*`, `packages/*`
- Formatter/linter: Biome (`biome.json`)
- Test runner: Bun's built-in test runner (`bun:test`)

## Common commands

Run these from the repository root:

- `bun run dev:websocket` — start the WebSocket server in watch mode
- `bun run dev:extension` — build the Chrome extension in watch mode
- `bun run build:extension` — production build of the Chrome extension
- `bun run cli` — run the CLI tool
- `bun run type-check` — run `tsc --noEmit` across the workspace
- `bun run test` — run all tests
- `bun run test:watch` — run tests in watch mode
- `bunx @biomejs/biome check --write .` — format and lint the workspace

Per-package scripts also exist under `apps/<name>/` and `packages/shared/`.

## Testing notes

- `bun run test` runs the Bun unit/integration tests.
- `bun run test:install` runs the BATS installer tests. These tests spawn real subprocesses and can hang in some environments if background services are not detached cleanly.
- **If the BATS installer tests fail or hang twice in a row, fall back to direct bash validation**: simulate `bridge up` with fake binaries, verify ports bind to `127.0.0.1`, and check that external IPs cannot connect. Do not keep retrying BATS indefinitely.

## Extension build quirks

`apps/extension/vite.config.ts` uses a custom `closeBundle` plugin:

- It flattens `dist/src/popup.html` to `dist/popup.html` and rewrites `../` asset paths to `./`.
- It copies `manifest.json` into `dist/` manually.
- It copies the centralized logo (`docs/assets/logo.png`) to `dist/icon.png` so the Chrome extension package has a single source of truth for the project logo.

When adding new entry points or HTML assets, verify the output paths in `dist/` and update the plugin if the flat layout changes.

## TypeScript

- `tsconfig.base.json` enables `strict`, `moduleResolution: "bundler"`, and path alias `@browser-bridge/shared` → `./packages/shared/src/index.ts`.
- Each app/package extends `tsconfig.base.json` in its own `tsconfig.json`.
