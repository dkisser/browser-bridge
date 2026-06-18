# AGENTS.md

This file provides guidance to the AI agent when working with code in this repository.

For project structure, commands, architecture, and conventions, see [README.md](./README.md).

## Non-obvious constraints

- **Package scope mismatch is intentional**: apps use `@browser-bridge/*`, the shared package uses `@browser-bridge/shared`. Do not "fix" this.
- **No build step for workspace packages**: `@browser-bridge/shared` and `@browser-bridge/websocket` ship raw `.ts` source. Do not add a build step.
- **Extension tsconfig**: `apps/extension/tsconfig.json` sets `types: ["chrome"]` only — node-style globals are unavailable.
- **Extension Vite plugin**: `apps/extension/vite.config.ts` flattens popup.html and copies manifest.json in `closeBundle`. See README for details.
- **CLI bin entry**: `apps/cli/package.json` points `bin.mycli` at `./src/index.ts` (raw TS, works via Bun).
