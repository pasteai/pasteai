# Tasks: Library Refactor (001)

One task per Claude session. Each task must leave `go test ./...` green before the next begins.

---

## Task 1 — Define `server/` package interfaces and types

**Scope**: new files only; nothing existing is deleted or modified.

Create `server/interfaces.go`:
- `Store` interface (with `Update(ctx, id, title string)` — no content param)
- `ContentBackend` interface
- `AuthProvider` interface
- `ErrNotFound`, `ErrContentMissing` sentinel errors
- `StaticKeyAuth` struct + `NewStaticKeyAuth` (moved from `internal/api/middleware.go`)

Create `server/document.go`:
- `Document`, `Visibility`, `ListOptions`, `ListResult` (moved from `internal/store/store.go`)

Create `server/options.go`:
- `Options{BaseURL, AuthProvider, Logger}`

At the end of this task:
- `internal/store` and `internal/api` still compile and are still used by `cmd/` and tests.
- No behaviour change.

---

## Task 2 — Implement `server.NewServer`

**Scope**: port all HTTP handler logic into `server/`.

Create `server/server.go`:
- `NewServer(store Store, content ContentBackend, opts Options) http.Handler`
- Full middleware chain: `securityHeaders → gzip → authMiddleware → mux`
- All route handlers ported from `internal/api/handler.go`
- Handlers call `content.Get/Put/Delete` and `store.*` as described in `design.md` data flow
- `loadTemplates`, `registerRoutes`, helper functions

Create `server/middleware.go`:
- `authMiddleware`, `securityHeaders`, `gzipHandler` (moved from `internal/api/middleware.go`)

`internal/api` is **not** deleted yet — it still compiles.

Verify: add a `server/` package test that constructs `NewServer` with a minimal in-memory
`Store` + `ContentBackend` stub and runs the existing `internal/api` handler test cases
against it.

---

## Task 3 — Create public `store/` package

**Scope**: new `store/` package; strip content I/O from `BoltStore`.

Create `store/bolt.go`:
- `BoltStore` implementing `server.Store` (imports `server/` for the interface + types)
- Ported from `internal/store/bbolt.go`
- `Create` no longer writes content files; `Content` field of returned `Document` is always empty
- `Get` no longer reads content files; `Content` field is always empty
- `Update(ctx, id, title string)` — content param removed, no file writes
- `Delete` no longer removes content files (that is `DiskContent.Delete`'s job)
- `dirFromDBPath` kept as `DirFromDBPath` (exported) for use by callers wiring up `DiskContent`

Create `store/disk.go`:
- `DiskContent` implementing `server.ContentBackend`
- `NewDiskContent(dir string) (*DiskContent, error)` — creates dir if absent
- `Put`, `Get` (returns `server.ErrNotFound` if file absent), `Delete` (no-op if absent)

`internal/store` is **not** deleted yet.

---

## Task 4 — Wire `cmd/pasteai/main.go`

**Scope**: update the entry point to use the new packages; no logic changes.

Update `runServe`:
- Replace `internal/store.NewBolt` with `store.NewBolt`
- Add `store.NewDiskContent(store.DirFromDBPath(*dbPath))`
- Replace `api.NewServer(s, cfg)` with `server.NewServer(boltStore, diskContent, opts)`
- Wrap returned `http.Handler` in `*http.Server` with the existing timeout values
- Replace `api.NewStaticKeyAuth` with `server.NewStaticKeyAuth`

Update `runMCP`:
- Replace `mcp.New()` with `mcp.New(mcp.Options{URL: os.Getenv("PASTEAI_URL"), APIKey: os.Getenv("PASTEAI_API_KEY")})`
  (mcp package is still `internal/mcp` at this point — this is a no-op change to prep for Task 5)

Verify: `pasteai serve` starts and all integration tests pass.

---

## Task 5 — Make `mcp/` public

**Scope**: port `internal/mcp/server.go` to `mcp/server.go`.

Create `mcp/server.go`:
- `Options{URL, APIKey, EmbeddedPort, Logger}` struct
- `New(opts Options) *Server` — no `os.Getenv` calls; caller passes values
- `(*Server).Run() error`
- `startEmbedded` updated to use `store.NewBolt`, `store.NewDiskContent`, `server.NewServer`
  (replaces the current `api.NewServer` + `store.NewBolt` call)

Update `cmd/pasteai/main.go` `runMCP` to import `mcp/` (public) instead of `internal/mcp`.

`internal/mcp` is **not** deleted yet.

---

## Task 6 — Cleanup: delete internal packages and fix all tests

**Scope**: remove `internal/api`, `internal/store`, `internal/mcp`; update every test file.

Delete:
- `internal/api/` (all files)
- `internal/store/` (all files)
- `internal/mcp/` (all files)

Update test files:
- `internal/api/handler_test.go` → move to `server/server_test.go`, update imports
- `internal/store/bbolt_test.go` → move to `store/bolt_test.go`, update imports
- `internal/mcp/embedded_test.go` → move to `mcp/embedded_test.go`, update imports
- `test/integration/server_test.go`, `mcp_test.go` → update import paths
- Any other files importing `internal/api` or `internal/store`

Verify: `go test ./...` passes with zero failures and zero references to `internal/api`,
`internal/store`, or `internal/mcp` remain in the codebase.
