## Concurrency

- `go test -race` always; the race detector must pass in CI
- Channels for ownership transfer and coordination; mutexes for protecting shared state — do not substitute one for the other
- Never embed `sync.Mutex` or `sync.RWMutex` — keep as a named unexported field (`mu sync.Mutex`)
- Never copy a struct that contains a mutex — always pass by pointer; `go vet` catches this
- Never write to a map concurrently without synchronisation
- Every goroutine must have a clear termination condition — goroutine leaks are bugs
- Use `sync.WaitGroup` to wait for goroutine completion — never `time.Sleep`
