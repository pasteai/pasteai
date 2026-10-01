# Contributing to PasteAI

## Getting started

```sh
git clone https://github.com/pasteai/pasteai
cd pasteai
make install   # download deps, install binary to GOPATH/bin, check PATH
make run       # build + start server on :8080
make test      # run tests
make lint      # go vet
```

## Project layout

```
cmd/pasteai/        Entry point — serve and mcp subcommands
internal/store/     Storage interface + bbolt implementation
internal/renderer/  Markdown → HTML (Goldmark + Chroma)
internal/api/       HTTP handlers, REST API, template rendering
internal/mcp/       MCP stdio server (publish_document, list_documents)
web/                Embedded templates and style.css
```

See [CLAUDE.md](CLAUDE.md) for architecture notes, theme/CSS details, and gotchas.

## Running the tests

```sh
make test       # go test ./...
make lint       # go vet
make style      # panics, doc comments, nil maps, context ordering
make coverage   # go test -race + 80% gate
```

### Changes to templates or JavaScript

`web/templates/` and `web/static/*.js` are only reachable from a browser, so Go
tests cannot see a regression in them. Those changes need a Playwright test.

The browser suite lives in the sibling `cloud` repo, because it needs the
authenticated stack (sessions, document owners, per-actor permissions) that this
repo has no server for:

```sh
cd ../cloud
make dev-up        # whole stack in the background (Docker required)
make e2e           # run the browser suite
```

The stack builds against your local `pasteai` working tree, so your changes are
picked up; `make dev-restart` recompiles after an edit.

Specs are in `cloud/e2e/tests/`. See `cloud/dev/README.md` for detail and
`.claude/rules/e2e.md` for what a good browser test looks like here.

If you are contributing from outside and cannot reach that repo, say so in the
PR — a maintainer will add the browser coverage.

## Running against a local MCP server

```sh
make run &   # start the HTTP server

# In a separate terminal, test the MCP tool manually:
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}' \
  | pasteai mcp
```

## Pull requests

- `make test` and `make lint` must pass
- Changes to `web/templates/` or `web/static/*.js` need a Playwright test (see above)
- Keep changes focused — one thing per PR
- Fill in the PR template

## Reporting bugs

Use [GitHub Issues](https://github.com/pasteai/pasteai/issues) and include the output of `pasteai version`.
