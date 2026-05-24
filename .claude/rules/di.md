## Dependency Injection

- Wire all dependencies in `main()` or `cmd/` — business packages never import infrastructure directly
- Business logic never instantiates its own dependencies — receive them from the outside
- No global variables or package-level singletons for dependencies — they prevent parallel testing
- No `init()` functions for dependency setup — they hide coupling and prevent testing
- Accept interfaces at dependency boundaries; wire concrete implementations in `main`
- Infrastructure adapters import domain packages, not vice versa
