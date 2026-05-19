# Using PasteAI as a Library

PasteAI exposes three importable packages for building hosted or customised deployments:

| Package | Purpose |
|---|---|
| `github.com/pasteai/pasteai/server` | HTTP handler + interfaces |
| `github.com/pasteai/pasteai/store` | Default BBolt + local-disk implementations |
| `github.com/pasteai/pasteai/mcp` | MCP stdio server |

The OSS binary uses all three with the default implementations. A hosted deployment
imports `server` and `mcp`, then provides its own backend implementations.

---

## Interfaces

All three interfaces live in the `server` package so a downstream repo needs only one import to implement them.

### `server.Store` — document metadata

```go
type Store interface {
    Create(ctx context.Context, doc Document) (*Document, error)
    List(ctx context.Context, opts ListOptions) (*ListResult, error)
    Get(ctx context.Context, id string) (*Document, error)
    // Update overwrites a non-empty title only; content is managed by ContentBackend.
    Update(ctx context.Context, id, title string) (*Document, error)
    Delete(ctx context.Context, id string) error
    Close() error
}
```

`Get` and `Create` return a `*Document` with `Content` always empty — content is
fetched separately through `ContentBackend`. `Delete` should return `server.ErrNotFound`
if the document does not exist.

### `server.ContentBackend` — raw document content

```go
type ContentBackend interface {
    Put(ctx context.Context, id string, content []byte) error
    Get(ctx context.Context, id string) ([]byte, error)
    Delete(ctx context.Context, id string) error
}
```

`Get` should return `fmt.Errorf("%w: %s", server.ErrNotFound, id)` when the content
does not exist. `Delete` should be a no-op (not an error) when the content is already gone.

### `server.AuthProvider` — request authentication

```go
type AuthProvider interface {
    // Authenticate returns the ownerID for the request, or an error if
    // credentials were present but invalid. Empty ownerID means anonymous.
    Authenticate(r *http.Request) (ownerID string, err error)
}
```

Returning `("", nil)` means the caller is unauthenticated. Write operations will
be rejected unless the server was constructed without an `AuthProvider` (open mode).
Returning a non-empty `ownerID` attaches the owner to the request context — `List`
will return all documents owned by that ID regardless of visibility.

---

## Wiring the HTTP server

```go
import (
    "net/http"
    "time"

    "github.com/pasteai/pasteai/server"
)

func NewPasteAIHandler(myStore server.Store, myContent server.ContentBackend, myAuth server.AuthProvider) http.Handler {
    return server.NewServer(myStore, myContent, server.Options{
        BaseURL:      "https://paste.example.com",
        AuthProvider: myAuth,
        Logger:       slog.NewLogLogger(slog.Default().Handler(), slog.LevelInfo),
    })
}

// Caller owns the *http.Server lifecycle:
handler := NewPasteAIHandler(myStore, myContent, myAuth)
srv := &http.Server{
    Addr:              ":8080",
    Handler:           handler,
    ReadTimeout:       30 * time.Second,
    ReadHeaderTimeout: 10 * time.Second,
    WriteTimeout:      60 * time.Second,
    IdleTimeout:       120 * time.Second,
}
srv.ListenAndServeTLS(certFile, keyFile)
```

`Options.Logger` is optional — if nil, `log.Default()` is used.
`Options.AuthProvider` is optional — if nil, all API writes are open (single-user mode).
`Options.BaseURL` is optional — if empty, document URLs are derived from the incoming request host.

---

## Wiring the MCP server

```go
import "github.com/pasteai/pasteai/mcp"

s := mcp.New(mcp.Options{
    URL:    "https://paste.example.com",   // your HTTP server
    APIKey: cfg.MCPAPIKey,                 // passed as Bearer token
})
if err := s.Run(); err != nil {
    log.Fatal(err)
}
```

When `URL` is empty, the MCP server starts an embedded HTTP server locally (using the
default BBolt + disk backends at `~/.pasteai/`). This is the OSS single-user mode.

---

## Minimal complete example

The following is a self-contained hosted deployment that stores metadata in DynamoDB
and content in S3. Error handling is abbreviated for clarity.

```go
package main

import (
    "context"
    "net/http"
    "os"
    "time"

    "github.com/pasteai/pasteai/server"
    "github.com/pasteai/pasteai/mcp"
)

func main() {
    store   := &DynamoStore{table: os.Getenv("DYNAMO_TABLE")}
    content := &S3Content{bucket: os.Getenv("S3_BUCKET")}
    auth    := &JWTAuth{jwksURL: os.Getenv("JWKS_URL")}

    handler := server.NewServer(store, content, server.Options{
        BaseURL:      os.Getenv("BASE_URL"),
        AuthProvider: auth,
    })
    srv := &http.Server{
        Addr:         ":8080",
        Handler:      handler,
        ReadTimeout:  30 * time.Second,
        WriteTimeout: 60 * time.Second,
    }
    srv.ListenAndServeTLS(os.Getenv("TLS_CERT"), os.Getenv("TLS_KEY"))
}

// DynamoStore implements server.Store
type DynamoStore struct{ table string }

func (s *DynamoStore) Create(ctx context.Context, doc server.Document) (*server.Document, error) {
    // assign ID, write to DynamoDB, return doc with Content empty
    panic("implement me")
}
func (s *DynamoStore) List(ctx context.Context, opts server.ListOptions) (*server.ListResult, error) { panic("implement me") }
func (s *DynamoStore) Get(ctx context.Context, id string) (*server.Document, error)                  { panic("implement me") }
func (s *DynamoStore) Update(ctx context.Context, id, title string) (*server.Document, error)        { panic("implement me") }
func (s *DynamoStore) Delete(ctx context.Context, id string) error                                   { panic("implement me") }
func (s *DynamoStore) Close() error                                                                  { return nil }

// S3Content implements server.ContentBackend
type S3Content struct{ bucket string }

func (c *S3Content) Put(ctx context.Context, id string, content []byte) error    { panic("implement me") }
func (c *S3Content) Get(ctx context.Context, id string) ([]byte, error)          { panic("implement me") }
func (c *S3Content) Delete(ctx context.Context, id string) error                 { panic("implement me") }

// JWTAuth implements server.AuthProvider
type JWTAuth struct{ jwksURL string }

func (a *JWTAuth) Authenticate(r *http.Request) (string, error) {
    // validate Bearer JWT, return sub claim as ownerID
    panic("implement me")
}
```

---

## Sentinel errors

Implement `Store` and `ContentBackend` to wrap these so callers can use `errors.Is`:

```go
import "github.com/pasteai/pasteai/server"

// not found — used by Get, Update, Delete
return nil, fmt.Errorf("%w", server.ErrNotFound)

// content missing — used by ContentBackend.Get when the object doesn't exist
return nil, fmt.Errorf("%w: %s", server.ErrNotFound, id)
```

The HTTP layer maps `ErrNotFound` to `404`. Any other error becomes `500`.

---

## Using the default implementations

If you only need to swap one backend, the default implementations are importable:

```go
import "github.com/pasteai/pasteai/store"

// BBolt for metadata, your own ContentBackend for content:
boltStore, _  := store.NewBolt("/var/lib/pasteai/docs.db")
myContent     := &S3Content{bucket: "my-bucket"}

handler := server.NewServer(boltStore, myContent, opts)
```

`store.DirFromDBPath(dbPath)` returns the conventional content directory
(`<db-dir>/documents/`) if you want to use `store.NewDiskContent` alongside a custom store.
