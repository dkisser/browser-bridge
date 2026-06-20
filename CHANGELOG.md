# Changelog

All notable changes to Browser Bridge are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/) and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
