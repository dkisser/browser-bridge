# Changelog

All notable changes to Browser Bridge are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/) and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
