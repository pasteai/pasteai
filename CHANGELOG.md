# Changelog

## [0.1.0](https://github.com/pasteai/pasteai/compare/v0.0.3...v0.1.0) (2026-05-17)


### Features

* **mcp:** change embedded server default port to 18080 ([490800c](https://github.com/pasteai/pasteai/commit/490800cfba9734305e75a226b83b321c796e8bd4))

## [0.0.3](https://github.com/pasteai/pasteai/compare/v0.0.2...v0.0.3) (2026-05-17)


### Bug Fixes

* remove MCP registry block entirely ([11765ed](https://github.com/pasteai/pasteai/commit/11765ede0c7b34aaf9070cd6a490d545b996e84d))

## [0.0.2](https://github.com/pasteai/pasteai/compare/v0.0.1...v0.0.2) (2026-05-17)


### Bug Fixes

* disable MCP registry until goreleaser fixes OIDC audience bug ([ae4ecbe](https://github.com/pasteai/pasteai/commit/ae4ecbe4597fcd5cacd2789b921c769b5423491c))

## [0.0.1](https://github.com/pasteai/pasteai/releases/tag/v0.0.1) - 2026-05-16

Initial public release.

### Features

* MCP tools: `publish_document`, `list_documents`
* Embedded server mode — zero config, server auto-starts on first MCP call
* `pasteai serve` — standalone HTTP server with web UI and REST API
* 7 themes: Light, Dark, Emerald, Arctic, Catppuccin Mocha, Latte, Frappé
* Auto-generated table of contents from document headings
* `public` and `unlisted` document visibility
* Single binary, embedded bbolt database — no external dependencies
* Docker support (`Dockerfile`, `docker-compose.yml`, GHCR image)
* systemd and launchd service templates
* `install.sh` one-line installer (no Go required)
* `/setup` slash command for Claude Code auto-configuration
* Multi-platform goreleaser release (linux/darwin/windows, amd64/arm64)
