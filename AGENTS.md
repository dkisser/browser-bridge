# AGENTS.md

This file provides guidance to the AI agent when working with code in this repository.

For project structure, commands, architecture, and conventions, see [README.md](./README.md).

## Non-obvious constraints

- **Package scope mismatch is intentional**: apps use `@browser-bridge/*`, the shared package uses `@browser-bridge/shared`. Do not "fix" this.
- **No build step for workspace packages**: `@browser-bridge/shared` and `@browser-bridge/websocket` ship raw `.ts` source. Do not add a build step.
- **Extension tsconfig**: `apps/extension/tsconfig.json` sets `types: ["chrome"]` only — node-style globals are unavailable.
- **Extension Vite plugin**: `apps/extension/vite.config.ts` flattens popup.html and copies manifest.json in `closeBundle`. See README for details.
- **CLI bin entry**: `apps/cli/package.json` points `bin.mycli` at `./src/index.ts` (raw TS, works via Bun).

## Testing

- Use `bun run test` for the Bun unit/integration test suite.
- Use `bun run test:install` for the BATS installer tests.
- BATS tests spawn real subprocesses and may hang if background services are not detached cleanly.
- **If the BATS installer tests fail or hang twice in a row, stop using BATS and validate directly with bash.** Simulate `bridge up` with fake binaries, confirm services bind to `127.0.0.1`, and verify external IPs cannot connect.

## Compiled binaries and dynamic imports

`bun build --compile` only bundles packages it can statically resolve. Some transitive dependencies—notably `xsschema`, which is pulled in via `fastmcp`—load optional peer dependencies (`@valibot/to-json-schema`, `arktype`, `effect`, `sury`, `zod-to-json-schema`) with dynamic `import()` calls at runtime inside the compiled binary.

- Any package that is dynamically imported inside a compiled binary must be declared in `dependencies` of the app that is compiled, **not** `devDependencies`.
- If a compiled binary starts but a runtime feature (for example, the MCP Streamable HTTP server on port 3003) is missing with no clear error, suspect a missing transitive optional peer dependency.
- When adding or upgrading a dependency that uses dynamic imports, verify the compiled binary still works by running `bun run build:binaries` locally or confirming the CI compile step passes.

