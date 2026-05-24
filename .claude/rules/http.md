## HTTP Handlers

- Use a handler struct implementing `http.Handler` when the endpoint has dependencies
- Handlers are adapters only — three steps: (1) parse request, (2) call domain logic, (3) encode response
- No business logic inside HTTP handlers — delegate to services/stores
- Set up routing in the constructor or `Register` method, not in `ServeHTTP`
- Use middleware for cross-cutting concerns (logging, auth, rate limiting, recovery)
- Never use the default `http.Client` — always configure timeouts
- For handler tests: `httptest.NewRecorder` + `httptest.NewRequest`
- For client tests: `httptest.NewServer` with controlled handlers
