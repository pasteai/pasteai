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

## Running against a local MCP server

```sh
make run &   # start the HTTP server

# In a separate terminal, test the MCP tool manually:
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}' \
  | pasteai mcp
```

## Pull requests

- `make test` must pass
- Keep changes focused — one thing per PR
- Fill in the PR template

## Reporting bugs

Use [GitHub Issues](https://github.com/pasteai/pasteai/issues) and include the output of `pasteai version`.
