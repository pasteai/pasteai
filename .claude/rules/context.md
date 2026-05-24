## Context Usage

- First argument of every function doing I/O: `ctx context.Context` — no exceptions
- Never store context in a struct — pass it through the call chain
- `context.Value` is for cross-cutting metadata only (trace IDs, request IDs) — never for functional inputs; using it for dependencies is a design failure
- Use `context.WithTimeout` for operations that should not run forever
- Never pass `nil` as context — use `context.Background()` in tests and at startup
- Use `http.NewRequestWithContext(ctx, ...)` for outbound HTTP calls
