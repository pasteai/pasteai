# Design: Library Refactor (001)

## Goal

Make PasteAI importable as a library so hosted deployments can inject alternative
backends (DynamoDB, S3) without forking. The OSS binary behaviour is unchanged.

---

## Package Structure (target state)

```
github.com/pasteai/pasteai/
├── cmd/
│   └── pasteai/
│       └── main.go          # thin wire-up; no logic
├── server/                  # NEW — replaces internal/api + types from internal/store
│   ├── server.go            # NewServer + internal http.ServeMux + middleware chain
│   ├── interfaces.go        # Store, ContentBackend, AuthProvider, ErrNotFound, ErrContentMissing
│   ├── document.go          # Document, Visibility, ListOptions, ListResult
│   └── options.go           # Options{BaseURL, AuthProvider, Logger}
├── store/                   # NEW public package — replaces internal/store
│   ├── bolt.go              # BoltStore — server.Store via BBolt (metadata only)
│   └── disk.go              # DiskContent — server.ContentBackend via local filesystem
├── mcp/                     # NEW public package — was internal/mcp
│   └── server.go            # New(Options) *Server; Run() error; embedded server support
└── internal/
    ├── renderer/            # unchanged
    └── setup/               # unchanged
```

`internal/api` and `internal/store` are **deleted** at the end of the migration.

---

## Interface Definitions

### server.Store

```go
type Store interface {
    Create(ctx context.Context, doc Document) (*Document, error)
    List(ctx context.Context, opts ListOptions) (*ListResult, error)
    Get(ctx context.Context, id string) (*Document, error)
    // Update overwrites non-empty title only. Content is managed by ContentBackend.
    Update(ctx context.Context, id, title string) (*Document, error)
    Delete(ctx context.Context, id string) error
    Close() error
}
```

**Change from current**: `Update` drops the `content string` parameter. Content updates
are routed through `ContentBackend.Put` by the handler.

### server.ContentBackend

```go
type ContentBackend interface {
    Put(ctx context.Context, id string, content []byte) error
    Get(ctx context.Context, id string) ([]byte, error)
    Delete(ctx context.Context, id string) error
}
```

### server.AuthProvider

Identical to the current `api.AuthProvider` — moved to `server/`.

```go
type AuthProvider interface {
    Authenticate(r *http.Request) (ownerID string, err error)
}
```

### server.Options

```go
type Options struct {
    BaseURL      string       // used in document URL construction; empty = derive from request
    AuthProvider AuthProvider // optional; nil means open writes
    Logger       *log.Logger  // optional; nil = log.Default()
}
```

`Addr` is removed from Options. Callers wrap the returned `http.Handler` in their own
`*http.Server` and set `Addr` there.

---

## Constructor

```go
func NewServer(store Store, content ContentBackend, opts Options) http.Handler
```

Returns a fully configured handler with the middleware chain applied:
`securityHeaders → gzip → authMiddleware (if AuthProvider set) → mux`.

Callers are responsible for the `*http.Server` lifecycle:

```go
handler := server.NewServer(myStore, myContent, server.Options{
    BaseURL:      "https://paste.example.com",
    AuthProvider: myAuth,
})
srv := &http.Server{Addr: ":8080", Handler: handler, ReadTimeout: 30 * time.Second}
srv.ListenAndServe()
```

---

## Data Flow After Split

### Create

```
handler
  → store.Create(ctx, Document{Title, Author, OwnerID, Visibility})
      returns doc with ID set, Content empty
  → content.Put(ctx, doc.ID, []byte(req.Content))
  assemble response: doc.Content = req.Content
```

### Get

```
handler
  → store.Get(ctx, id)          → Document (Content field empty)
  → content.Get(ctx, id)        → []byte
  assemble: doc.Content = string(bytes)
```

### Update

```
handler
  → store.Update(ctx, id, req.Title)    // metadata
  → content.Put(ctx, id, bytes)         // only if req.Content != ""
  → content.Get(ctx, id)                // to populate response if content not changed
```

### Delete

```
handler
  → store.Delete(ctx, id)          // removes metadata; returns ErrNotFound if missing
  → content.Delete(ctx, id)        // non-fatal if already gone
```

### Atomicity

Not guaranteed across the two backends — identical to the current implementation,
which writes the file first and then writes to BBolt. Partial failures leave an orphaned
content file or a metadata entry with no content, both recoverable by re-write or
manual cleanup. This is acceptable for the current scale.

---

## store.BoltStore (after split)

`BoltStore` stores document metadata only. The `Content` field on `Document` is always
empty when returned by `BoltStore.Get` or `BoltStore.Create`. Callers (the handler)
populate it from `ContentBackend`.

The `filesDir` field and all `os.ReadFile` / `os.WriteFile` calls are removed from
`bbolt.go`.

## store.DiskContent

```go
type DiskContent struct{ dir string }

func NewDiskContent(dir string) (*DiskContent, error)   // creates dir if absent
func (d *DiskContent) Put(ctx, id string, content []byte) error
func (d *DiskContent) Get(ctx, id string) ([]byte, error)   // ErrNotFound if absent
func (d *DiskContent) Delete(ctx, id string) error          // no-op if absent
```

`dir` defaults to `~/.pasteai/documents/` in the OSS wire-up (derived from the DB path
as it is today via `dirFromDBPath`).

---

## mcp.Options

```go
type Options struct {
    URL          string       // remote server URL; empty = use embedded server
    APIKey       string       // sent as Bearer token; empty = unauthenticated
    EmbeddedPort string       // port for the embedded server; default "18080"
    Logger       *log.Logger
}
```

`mcp.New` no longer reads `os.Getenv` directly. The OSS `main.go` passes env values
when constructing `mcp.Options`.

The embedded server startup inside `mcp` still imports `store/` and `server/` to spin
up a `BoltStore` + `DiskContent` + `server.NewServer`.

---

## Migration Path from Current main.go

| Current | After |
|---|---|
| `internal/api.NewServer(s, cfg)` → `*http.Server` | `server.NewServer(s, c, opts)` → `http.Handler` |
| `internal/store.NewBolt(path)` | `store.NewBolt(path)` |
| `store.BoltStore` handles content files | `store.DiskContent` handles content files |
| `internal/mcp.New()` reads env directly | `mcp.New(mcp.Options{URL: os.Getenv(...)})` |
| All types in `internal/store` | Types/interfaces in `server/`, BBolt impl in `store/` |

**cmd/pasteai/main.go** after the refactor:

```go
func runServe(args []string) {
    // parse flags ...
    boltStore, _ := store.NewBolt(*dbPath)
    defer boltStore.Close()
    diskContent, _ := store.NewDiskContent(store.DirFromDBPath(*dbPath))

    opts := server.Options{BaseURL: *baseURL, Logger: logger}
    if *apiKey != "" {
        opts.AuthProvider = server.NewStaticKeyAuth(map[string]string{*apiKey: "owner"})
    }
    handler := server.NewServer(boltStore, diskContent, opts)
    srv := &http.Server{
        Addr: *addr, Handler: handler,
        ReadTimeout: 30 * time.Second, ReadHeaderTimeout: 10 * time.Second,
        WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
    }
    srv.ListenAndServe()
}

func runMCP() {
    s := mcp.New(mcp.Options{
        URL:    os.Getenv("PASTEAI_URL"),
        APIKey: os.Getenv("PASTEAI_API_KEY"),
    })
    s.Run()
}
```

---

## Import Graph (target, no cycles)

```
cmd/pasteai  →  server, store, mcp
mcp          →  server, store
store        →  server          (for Store/ContentBackend interfaces + Document types)
server       →  internal/renderer, web
internal/*   →  (no changes to deps)
```
