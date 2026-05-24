# PasteAI — Developer Guide

## Build & Run

```sh
make build          # compile binary
make run            # build + run server on :8080
make test           # go test ./...
make lint           # go vet ./...
make style          # style checks (panics, doc comments, nil maps, context)
make coverage       # go test -race + 80% gate
make docker-restart # docker compose build --no-cache && up -d
```

## Architecture

Two processes, both from the same binary:

- `pasteai serve` — HTTP server (web UI + REST API). Stores documents in bbolt.
- `pasteai mcp` — MCP stdio server. Thin HTTP client that calls the serve process.

The MCP process has no storage — it forwards all calls to `PASTEAI_URL`.

## Key Packages

| Package | Role |
|---|---|
| `internal/store` | `Store` interface + bbolt implementation. All persistence here. |
| `internal/renderer` | Goldmark markdown → HTML. Chroma syntax highlighting (class-based). |
| `internal/api` | HTTP handlers, template rendering, REST API. |
| `internal/mcp` | MCP tool definitions (`publish_document`, `list_documents`). |
| `web/` | Embedded templates and `style.css` (6 themes via CSS custom properties). |

## Auth

Auth is opt-in. Start the server with `-api-key <key>` to require Bearer tokens on API writes. The MCP client reads `PASTEAI_API_KEY` and sends it as `Authorization: Bearer <key>`.

Without `-api-key`, the server accepts all requests (self-hosted single-user mode).

## Themes

Themes are CSS custom properties under `[data-theme="<name>"]` selectors in `web/static/style.css`. To add a theme:

1. Add a `[data-theme="my-theme"]` block with all the required variables (copy an existing block).
2. Add a swatch button in `web/templates/base.html` (search for `class="theme-option"`).
3. Add entries to `themeLabels` and `themeSwatches` in the same template's JS.

Chroma (syntax highlighting) uses two style groups: `github` for light themes, `github-dark` for dark. Map your new theme to one of these groups in `renderer.go:ThemeCSS()`.

## Chroma CSS

Code block colours are injected per-page as a `<style>` block. `renderer.ThemeCSS()` generates scoped CSS — each rule is prefixed with `:is([data-theme="..."])`. The `background-color` is intentionally stripped from Chroma's PreWrapper rule so the CSS variable `--color-surface-card-strong` controls the background per-theme.

Run `go test ./internal/renderer/...` to verify the CSS invariants if you change this.

## Adding a New API Field

`documentResponse` is built by `(*Server).toResponse()` in `handler.go`. Add the field there — all three API endpoints pick it up automatically.

## Notes

- `GET /{$}` is the home route (exact slash match). Without `{$}`, Go's mux matches all unmatched paths to `/`.
- The bbolt time index uses `UnixNano + UUID` as the key for newest-first ordering. Two creates in the same nanosecond have undefined relative order (UUID-lexicographic).
- `handleHome` passes `ownerFromCtx` to `List`. In self-hosted mode this is always `""`, which returns all public documents. When auth is enabled and an owner is authenticated, they'll see their own documents regardless of visibility — this is intentional for the owner's dashboard view.

## Go Standards

Full reference: https://pasteai.io/d/c7e7c355-e01a-4230-8430-0fbf16a8478a

@.claude/rules/errors.md
@.claude/rules/interfaces.md
@.claude/rules/context.md
@.claude/rules/concurrency.md
@.claude/rules/di.md
@.claude/rules/tdd.md
@.claude/rules/http.md
@.claude/rules/style.md
