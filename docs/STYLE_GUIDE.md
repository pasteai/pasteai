# Go Style Guide

Derived from [Learn Go with Tests](https://quii.gitbook.io/learn-go-with-tests) by quii.
Apply these rules to all Go code in the `pasteai` and `cloud` repositories.

---

## 1. Test-Driven Development

1. **Write the failing test first.** Never write production code before a failing test justifies it.
2. **Follow the red-green-refactor cycle strictly.** Red: write a failing test. Green: write the minimal code to pass. Refactor: clean up with tests green.
3. **Write the smallest reasonable change** to make a test pass. Do not anticipate future requirements.
4. **See the failure message before making it pass.** A test you haven't seen fail may be an evergreen test — it proves nothing.
5. **Refactor tests too.** Test code is production code; keep it clean.

## 2. Table-Driven Tests

6. **Use table-driven tests for multiple related scenarios.** A slice of structs with `name`, `input`, and `want` fields is the standard pattern.
7. **Use `t.Run(tc.name, ...)` for each table case** so failures name the failing scenario precisely and `go test -run TestFoo/case_name` works.
8. **Don't force unrelated scenarios into one table.** If a case needs a different setup or assertion shape, write a separate test function.
9. **Aim for one logical assertion per test case.** Multiple assertions per case obscure which one failed.
10. **Prefer `want` and `got` as variable names** for expected and actual values in assertions.
11. **Use `%q` in error messages** to wrap string values in double quotes for clarity.
12. **Use `%#v`** to print full struct values in assertion failures.

## 3. Test Helpers

13. **Extract repeated assertion logic into helper functions.** Don't copy-paste `if got != want` blocks.
14. **Accept `testing.TB`**, not `*testing.T`, in helper functions — this supports both tests and benchmarks.
15. **Call `t.Helper()`** at the top of every test helper so failure line numbers point to the call site, not the helper.
16. **Mark helpers with `t.Helper()` even when they seem trivial.** The cost is zero; the debugging benefit is real.

## 4. Naming Conventions

17. **Exported symbols start with a capital letter; unexported with lowercase.** This is Go's visibility model — never work around it.
18. **Receiver names are the first letter (or two) of the type.** `r Rectangle`, `c Circle`, `s *Store`. Be consistent across all methods of a type.
19. **Keep receiver names the same across all methods of a type** even if only some methods need pointer receivers.
20. **Name package-level sentinel errors with the `Err` prefix:** `ErrNotFound`, `ErrInsufficientFunds`.
21. **Name test cases descriptively.** `"valid input"`, `"empty string"`, `"negative balance"` — not `"test1"`.
22. **Named return values** are appropriate when the return's purpose is not obvious from context. They appear in `go doc` output.
23. **Abbreviate only well-known terms** (`w http.ResponseWriter`, `r *http.Request`, `ctx context.Context`). Don't invent abbreviations.

## 5. Error Handling

24. **Return errors; don't panic.** Panics are for programmer errors (e.g. nil dereference), not for recoverable failures.
25. **Check every error return.** Unchecked errors are bugs waiting to happen (`errcheck` will catch these).
26. **Define sentinel errors as package-level variables or typed constants:**
    ```go
    var ErrNotFound = errors.New("not found")
    // or, for immutability:
    type storeErr string
    func (e storeErr) Error() string { return string(e) }
    const ErrNotFound storeErr = "not found"
    ```
27. **Define specific error values for distinct failure modes.** `ErrNotFound` and `ErrAlreadyExists` allow callers to handle each case; a single `ErrGeneric` does not.
28. **Wrap errors with context using `fmt.Errorf("doing X: %w", err)`.** Callers can unwrap with `errors.Is` / `errors.As`.
29. **Don't add test-mode flags to production code** to make error paths reachable. Use interfaces and dependency injection instead.
30. **Check for nil before dereferencing a pointer** returned from a function that may return nil.

## 6. Interfaces

31. **Keep interfaces small.** A one- or two-method interface is almost always the right size. "The bigger the interface, the weaker the abstraction."
32. **Define interfaces at the consumer, not the producer.** The package that _uses_ the interface should define it; the package that _implements_ it should not know about it.
33. **Accept interfaces, return concrete types.** Functions should depend on the minimum interface they need; they should return the specific type they create.
34. **Depend on standard library interfaces wherever possible:** `io.Reader`, `io.Writer`, `fs.FS`. This gives you testability and reusability for free.
35. **Don't create an interface until you have two implementations** (one real, one test double). Premature interfaces add complexity without value.
36. **Never expose a large interface to package consumers.** It forces them to implement every method in their test doubles. Prefer wrapping with a smaller facade.

## 7. Dependency Injection

37. **Pass dependencies as parameters; never access them via globals.** Global state makes tests order-dependent and unreproducible.
38. **Inject dependencies through constructors or function parameters**, not through environment variables read inside a function.
39. **Use interfaces to define the dependency contract.** The concrete type is chosen by the caller (main or test), not the function that uses it.
40. **A function that is hard to test is a function that is hard to use.** Refactor the design, not the test.
41. **No framework needed.** Go's interfaces and standard library are sufficient for DI.

## 8. Mocking and Test Doubles

42. **More than three mocks in one test is a red flag.** The code under test is doing too much.
43. **Test behaviour, not implementation.** A refactoring that doesn't change observable behaviour should never require test changes.
44. **Don't expose private functions just to test them.** If you feel the urge, extract the logic into a testable unit with a public interface.
45. **Use spies only when interaction order or call count genuinely matters** — not as a default strategy.
46. **Prefer real implementations in tests when they are fast and deterministic** (e.g. `bytes.Buffer` instead of a mock writer).
47. **Use `testing/fstest.MapFS` for filesystem tests.** Never hit the real filesystem in a unit test.

## 9. Structs and Methods

48. **Use structs to bundle related data.** Don't pass long parameter lists when a struct expresses the concept more clearly.
49. **Methods belong on the type that owns the state.** A function that takes a struct as its primary argument is usually a method.
50. **Don't embed `sync.Mutex`.** Embedding exposes `Lock()` and `Unlock()` as public API. Keep the mutex as a named unexported field.
51. **Never copy a struct that contains a mutex.** Always pass by pointer. Run `go vet` — it catches this.

## 10. Concurrency

52. **Use channels to communicate; use mutexes to protect shared state.** Don't substitute one for the other.
53. **Always run the race detector: `go test -race`.** Fix data races before merging.
54. **Detect races in CI, not in production.** Add `-race` to test targets.
55. **Never write to a map concurrently without synchronisation.** Go maps are not goroutine-safe.
56. **"Make it work, make it right, make it fast."** Optimise only after correctness is established.
57. **Goroutine leaks are bugs.** Every goroutine you start must have a clear termination condition.

## 11. Context

58. **The first argument of every function on the request path is `ctx context.Context`.** No exceptions.
59. **Pass context; don't store it in a struct.** Storing context breaks the cancellation contract.
60. **Use `ctx.Done()` in long-running loops** to respect cancellation.
61. **Return `ctx.Err()` when context cancellation causes early exit.**
62. **Never use `context.Value` for required inputs.** Use typed function parameters. `context.Value` is for optional, cross-cutting metadata (trace IDs, request IDs).
63. **Incoming server requests must create a context; outgoing calls must accept one.**

## 12. Maps

64. **Never use a nil map for writing.** Always initialise: `m := map[K]V{}` or `make(map[K]V)`.
65. **Use the two-value map lookup** (`v, ok := m[k]`) to distinguish missing keys from zero values.
66. **Maps are reference types.** Passing a map to a function allows mutation; you rarely need `*map[K]V`.

## 13. Packages and Code Organisation

67. **Separate domain logic from infrastructure.** A function that fetches data from a database should not also format an HTTP response.
68. **Keep package names short, lowercase, single-word.** Avoid `util`, `common`, `helpers` — these are not packages, they are junk drawers.
69. **An `internal/` package is a hard boundary.** Code outside the module cannot import it; use this to protect implementation details.
70. **No circular dependencies.** Design the import graph to be a DAG.

## 14. Documentation

71. **Every exported symbol needs a doc comment.** Start it with the symbol name: `// Store provides...`
72. **Document the why, not the what.** The code says what it does; the comment should explain a non-obvious constraint, invariant, or design decision.
73. **Don't comment out dead code.** Delete it; git history is the record.

## 15. Anti-Patterns to Avoid

| Anti-pattern | Why it's wrong |
|---|---|
| Evergreen tests | A test that can't fail proves nothing |
| Asserting on whole objects when only one field matters | Unrelated changes cause spurious failures |
| Interface with many methods | Weak abstraction; forces large test doubles |
| Public function exists only for tests | Pollutes the API; signals a design smell |
| Test mode flags in production code | Couples test concerns to production |
| Global mutable state | Makes tests order-dependent |
| Ignoring returned errors | Silent failures become production incidents |
| Copying a mutex | `go vet` will catch this; the result is a deadlock |
| `context.Value` for functional inputs | Bypasses the type system; hidden coupling |
| Overcomplicated table tests | Split into focused tests instead |
