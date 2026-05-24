## Interface Design

- Keep interfaces small: prefer 1–2 methods; 3+ requires justification
- Name single-method interfaces with the `-er` suffix: `Reader`, `Writer`, `Storer`, `Fetcher`
- Define interfaces at the consumer (import) site, not alongside the implementation — consumer owns the contract
- Accept interfaces, return concrete types
- Verify interface satisfaction at compile time: `var _ MyInterface = (*MyStruct)(nil)`
- Creating an interface for testability is valid even with one production implementation — the test double counts as the second
- Do NOT mirror an entire struct's public API as an interface — interfaces should be subsets
- Do NOT use `interface{}`/`any` when a concrete type or constrained generic suffices
