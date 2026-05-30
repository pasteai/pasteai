# Changelog

## [0.0.14](https://github.com/pasteai/pasteai/compare/v0.0.13...v0.0.14) (2026-05-30)


### Features

* add NavExtrasFunc/Footer server options, fix diff spacing, add robots.txt ([0446e88](https://github.com/pasteai/pasteai/commit/0446e88016ee824e79ef61e28b2843c5a743f836))
* AJAX load-more, doc-tabs block, install step fix, suggest-feature template URL ([33f36f4](https://github.com/pasteai/pasteai/commit/33f36f436118ccbeef9ed809d6b0816ac1d91e33))
* limit modal, nav capacity dot CSS, favicon, logo ([d7df3f6](https://github.com/pasteai/pasteai/commit/d7df3f6263f854ee75efc30b65779b666117cf90))


### Bug Fixes

* add HSTS header for HTTPS requests ([4fbebd8](https://github.com/pasteai/pasteai/commit/4fbebd8b23838d39f62835e30facfd18c2cb3e2d))
* enforce ownership on document UPDATE and DELETE ([8e7a67b](https://github.com/pasteai/pasteai/commit/8e7a67b854eb03a1fc1c1a60641fd30d421dc063))

## [0.0.13](https://github.com/pasteai/pasteai/compare/v0.0.12...v0.0.13) (2026-05-24)


### Features

* add internal/diff package with Unified and CountLines ([9bd83c0](https://github.com/pasteai/pasteai/commit/9bd83c02785422888a97c2e1f9f37556ce80ae6a))
* add revision list, view, and diff handlers with split-panel UI ([fefc113](https://github.com/pasteai/pasteai/commit/fefc113ce90d0b627ae33c96c59c28fe55f5cdc0))
* add Revision type and RevisionStore/RevisionContentBackend interfaces ([7b1bf64](https://github.com/pasteai/pasteai/commit/7b1bf6469a0db95fc8457dc33f5133aa3aa84019))
* implement RevisionContentBackend on DiskContent ([e414e3c](https://github.com/pasteai/pasteai/commit/e414e3c712f39b8327e093fec04e76f180f0dfff))
* implement RevisionStore on BoltStore with sequence tracking and 50-revision cap ([899a1da](https://github.com/pasteai/pasteai/commit/899a1da93f1bc8d487d6ee81ee6e0f7fa2505116))
* wire revision saving into handleUpdateDocument and cleanup into handleDeleteDocument ([892e241](https://github.com/pasteai/pasteai/commit/892e241c8b720856930abec9bf54dc26249bb7fe))


### Bug Fixes

* collapse secondary nav links on mobile and fix search styling on iOS ([88b7e60](https://github.com/pasteai/pasteai/commit/88b7e60f1d912bc7f9cf679ea6deac3155ca8c08))
* correct line counts for title-only updates, 404 for missing doc revisions API, CSS cache busting, mobile sheet display, layout wrapping ([72def44](https://github.com/pasteai/pasteai/commit/72def44a0dabdcf9e9a8a8786f2106af50ed73f0))
* replace undefined --color-accent with --color-primary, use --color-danger on delete modal ([66ceada](https://github.com/pasteai/pasteai/commit/66ceadab84cbb69177bb38dc303500d23f03735f))
* show error message in search results on fetch failure ([4c8bb0a](https://github.com/pasteai/pasteai/commit/4c8bb0adaa322c99afe27dfb150fa5584680142e))
* UX improvements to revision UI — ARIA cleanup, error path buttons, Escape to close sheet, colour-blind diff accents, breadcrumb back-link, remove duplicate date ([2cd00d7](https://github.com/pasteai/pasteai/commit/2cd00d7c4cd71570bf123ee9688f9458fc0b8bc9))

## [0.0.12](https://github.com/pasteai/pasteai/compare/v0.0.11...v0.0.12) (2026-05-24)


### Features

* solid visibility badge with SVG icons in doc header ([abfeccf](https://github.com/pasteai/pasteai/commit/abfeccf1daada5bfa57e252568847ed6480cadd7))
* visibility badge, TTL countdown, debounced live search, private doc enforcement ([0867876](https://github.com/pasteai/pasteai/commit/0867876dfe132659bf1830d1905a8dfe06d5789e))


### Bug Fixes

* github icon far left, avatar/sign-out far right in nav ([6e9e345](https://github.com/pasteai/pasteai/commit/6e9e345336f572dbd63f96d685903a902441cd3f))
* reduce search debounce to 150ms ([eff7d15](https://github.com/pasteai/pasteai/commit/eff7d151d77ce75b91c388dfdceb046da97c0034))
* resolve staticcheck and style violations ([78d8306](https://github.com/pasteai/pasteai/commit/78d8306684f6719b3f1e9c180bf923b67f7b7e1d))

## [0.0.11](https://github.com/pasteai/pasteai/compare/v0.0.10...v0.0.11) (2026-05-21)


### Features

* add delete_document MCP tool closes [#12](https://github.com/pasteai/pasteai/issues/12) ([dcbbc07](https://github.com/pasteai/pasteai/commit/dcbbc07cc399791e0114142c965fc94a93e3746d))
* add EventListener hook for document lifecycle events ([0e17e08](https://github.com/pasteai/pasteai/commit/0e17e08f3bb35987036ac0d45e8b2b0ca5c54fe9))
* add streamable-HTTP MCP transport for claude.ai Custom Connectors closes [#10](https://github.com/pasteai/pasteai/issues/10) ([9e3b584](https://github.com/pasteai/pasteai/commit/9e3b58493fa0383308eca8cedb1a77ed7e841c7d))
* add title search (API, home page, MCP tool) with themed search form closes [#11](https://github.com/pasteai/pasteai/issues/11) ([ee75b72](https://github.com/pasteai/pasteai/commit/ee75b7234b262b88b06ef1f446e4cea9a1afaa68))


### Bug Fixes

* preserve user config when re-running setup on upgrade ([cd93ae0](https://github.com/pasteai/pasteai/commit/cd93ae0d4c6c307e4b722fe3c6f73db952478161))

## [0.0.10](https://github.com/pasteai/pasteai/compare/v0.0.9...v0.0.10) (2026-05-21)


### Features

* add nav-sign-in pill button style for hosted sign-in link ([af9db20](https://github.com/pasteai/pasteai/commit/af9db20aefbb6295cf3f0acc0e6b3428bd3673c3))
* configure Kiro and opencode MCP on setup alongside Claude Code ([9128476](https://github.com/pasteai/pasteai/commit/912847650593bf41379b9c75c26e9b29cbaf262a))
* GitHub/suggest-feature links on splash, Kiro/opencode in readme and steps ([72600dd](https://github.com/pasteai/pasteai/commit/72600dd5897083207a89afb31ece2dd8f9af1fcb))

## [0.0.9](https://github.com/pasteai/pasteai/compare/v0.0.8...v0.0.9) (2026-05-20)


### Features

* add HTTPClient option to MCP server Options ([4e9e8aa](https://github.com/pasteai/pasteai/commit/4e9e8aa5d2b432281d36211367e4c6852241d8a0))
* arctic default theme, nav button reset, avatar styles ([61f0e88](https://github.com/pasteai/pasteai/commit/61f0e8827e8f4e58ce90e52e9ace9aac714d6b5b))
* owner-only delete, visibility toggle styles ([31fd274](https://github.com/pasteai/pasteai/commit/31fd2748e2d305e7f8f51763aab4bf24af8167c5))
* visibility controls — toggle per-doc, configurable default ([c48cc32](https://github.com/pasteai/pasteai/commit/c48cc329de3d091e4c677e6862721a22668068e2))

## [0.0.8](https://github.com/pasteai/pasteai/compare/v0.0.7...v0.0.8) (2026-05-20)


### Features

* add AllowAnonymousWrites, HomeHandler option, and NavExtras template slot ([4c0074d](https://github.com/pasteai/pasteai/commit/4c0074d23e1bfdd128713a9454f73485ea7e023a))


### Bug Fixes

* log rollback failures, shut down embedded MCP server on exit, surface content errors in update, and add real-store integration test ([c465189](https://github.com/pasteai/pasteai/commit/c4651899494d2a57b9bf3f625d7c7e3502f77baf))

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
