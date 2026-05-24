## Error Handling

- Return `error` as the last return value; check it immediately after the call
- Wrap with `fmt.Errorf("operation: %w", err)`; bare `return err` only when there is nothing useful to add
- Sentinel errors: `var ErrFoo = errors.New("...")` at package level
- Check wrapped errors with `errors.Is`/`errors.As` — never compare `.Error()` strings
- Error strings: lowercase, no trailing punctuation, prefix with package/operation name
- Never discard errors with `_` in production code
- Do not use `log.Fatal` in library code — it calls `os.Exit` and prevents deferred cleanup
- Panic only for truly unrecoverable programmer errors; recover at API boundaries (HTTP handlers) to prevent server crashes
