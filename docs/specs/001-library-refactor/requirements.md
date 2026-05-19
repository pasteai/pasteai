# Requirements: Library Refactor (001)

EARS notation: WHEN/WHERE/WHILE/THE SYSTEM SHALL.

---

## 1. Importable server package

WHEN a downstream repo imports `github.com/pasteai/pasteai/server`,
THE SYSTEM SHALL expose `NewServer(store Store, content ContentBackend, opts Options) http.Handler`
as the sole entry point for constructing the HTTP layer.

## 2. Store interface

WHERE the `server` package is imported,
THE SYSTEM SHALL export a `Store` interface with the following methods:

```
Create(ctx, Document) (*Document, error)
List(ctx, ListOptions) (*ListResult, error)
Get(ctx, id string) (*Document, error)
Update(ctx, id, title string) (*Document, error)
Delete(ctx, id string) error
Close() error
```

Note: `Update` operates on metadata only; content is not a `Store` concern.

## 3. ContentBackend interface

WHERE the `server` package is imported,
THE SYSTEM SHALL export a `ContentBackend` interface with the following methods:

```
Put(ctx, id string, content []byte) error
Get(ctx, id string) ([]byte, error)
Delete(ctx, id string) error
```

## 4. AuthProvider interface

WHERE the `server` package is imported,
THE SYSTEM SHALL export an `AuthProvider` interface:

```
Authenticate(r *http.Request) (ownerID string, err error)
```

## 5. Sentinel errors

WHERE the `server` package is imported,
THE SYSTEM SHALL export `ErrNotFound` and `ErrContentMissing` as sentinel errors
that `Store` and `ContentBackend` implementors SHOULD wrap and callers MAY test with `errors.Is`.

## 6. Default BBolt Store implementation

WHEN a caller imports `github.com/pasteai/pasteai/store`,
THE SYSTEM SHALL provide a `BoltStore` type that implements `server.Store`
using BBolt for document metadata storage only (no file I/O).

## 7. Default disk ContentBackend implementation

WHEN a caller imports `github.com/pasteai/pasteai/store`,
THE SYSTEM SHALL provide a `DiskContent` type that implements `server.ContentBackend`
using local filesystem storage, with one file per document at `<dir>/<id>.md`.

## 8. Importable MCP package

WHEN a downstream repo imports `github.com/pasteai/pasteai/mcp`,
THE SYSTEM SHALL expose `New(opts Options) *Server` and `(*Server).Run() error`
so callers can configure the MCP server (remote URL, API key) independently
of the HTTP server process.

## 9. Embedded MCP server

WHEN `mcp.Options.URL` is empty and no existing server is detected at the default port,
THE SYSTEM SHALL start an embedded HTTP server in-process using `store.NewBolt`
and `store.NewDiskContent` with default paths, identical to current behaviour.

## 10. OSS binary behaviour unchanged

WHEN a user runs `pasteai serve [flags]`,
THE SYSTEM SHALL behave identically to the current release:
same flags, same defaults, same REST API contract, same web UI.

WHEN a user runs `pasteai mcp`,
THE SYSTEM SHALL behave identically to the current release.

## 11. No regression

THE SYSTEM SHALL pass `go test ./...` after every task in the implementation plan
before proceeding to the next task.
