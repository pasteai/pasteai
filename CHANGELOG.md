# Changelog

## [0.0.7](https://github.com/pasteai/pasteai/compare/v0.0.6...v0.0.7) (2026-05-19)


### Features

* Refactor into library to allow for extention ([fbf9f8e](https://github.com/pasteai/pasteai/commit/fbf9f8e8a76cacc304d624574edb6f563236b6e8))


### Bug Fixes

* Fix colours when using 3p dark theme plugins ([79f1bed](https://github.com/pasteai/pasteai/commit/79f1bedfd51350166a89a73cae22c2491fe004c4))

## [0.0.6](https://github.com/pasteai/pasteai/compare/v0.0.5...v0.0.6) (2026-05-18)


### Features

* Add raw mode, pagination and update ([e1d57e8](https://github.com/pasteai/pasteai/commit/e1d57e847ab016245d9e4c6fd035de0ad5107539))
* **mcp:** change embedded server default port to 18080 ([490800c](https://github.com/pasteai/pasteai/commit/490800cfba9734305e75a226b83b321c796e8bd4))
* Update docker container to avoid UID and GID injection ([c7f2185](https://github.com/pasteai/pasteai/commit/c7f218591a6d98bbfaec57361900049f4897d0f3))
* UX polish: accessibility, modal focus trap, TOC active state, copy-link, heading anchors, auth-gated delete, OG image, compact home ([ba1eb37](https://github.com/pasteai/pasteai/commit/ba1eb3747833c92367dac02c4748cd2ff5b87e5e))
* v0.0.5 ([699118b](https://github.com/pasteai/pasteai/commit/699118bb6b7195aa2c735ee63abc3a4cd1c78197))


### Bug Fixes

* disable MCP registry until goreleaser fixes OIDC audience bug ([ae4ecbe](https://github.com/pasteai/pasteai/commit/ae4ecbe4597fcd5cacd2789b921c769b5423491c))
* remove MCP registry block entirely ([11765ed](https://github.com/pasteai/pasteai/commit/11765ede0c7b34aaf9070cd6a490d545b996e84d))
* run tests on release-please branches ([7ee415e](https://github.com/pasteai/pasteai/commit/7ee415ea5964b4d25b0851e60e1419c108b4cf40))
* use workflow_dispatch to trigger tests ([9fae7ce](https://github.com/pasteai/pasteai/commit/9fae7ce9e5bb3a0bbd102c1bd853a05903aef27b))

## [0.0.4](https://github.com/pasteai/pasteai/compare/v0.0.3...v0.0.4) (2026-05-17)


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
