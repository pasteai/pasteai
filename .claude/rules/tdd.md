## TDD

- Write the failing test first — never write production code before a failing test justifies it
- See the test fail for the right reason before making it pass — evergreen tests prove nothing
- Write the minimal code to make the test pass — nothing more
- Refactor only while tests are green; do NOT change public signatures or behaviour during refactor
- Never change tests during the refactor phase
- All bug fixes: write a test that reproduces the bug first, then fix
- More than 3 test doubles in one test means the code is doing too much — redesign the code, not the test
- Prefer hand-written fakes over generated mocks; prefer real implementations when fast and deterministic
- Use `httptest.NewServer` for HTTP dependencies — not a mocked interface
