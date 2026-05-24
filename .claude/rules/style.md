## Go Style

- Functions: aim for ~30 lines; hard limit 50 — extract a helper if longer
- Max 3 levels of indentation — extract a function if deeper
- Happy path left-aligned: handle errors first, return early, keep success path unindented
- Omit `else` after `return`/`break`/`continue` — reduces nesting
- Doc comment required for every exported type, function, method, constant, variable
- Doc comments begin with the symbol name: `// Server handles incoming HTTP requests.`
- Acronyms all-caps in identifiers: `URL`, `HTTP`, `ID` — never `Url`, `Http`, `Id`
- Receiver names: 1–2 letters, consistent across all methods of a type; never `this` or `self`
- Package names: short, lowercase, single-word; no `util`, `common`, `helpers`
- After writing any Go code, run `make lint && make coverage` before reporting done
