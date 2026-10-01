## End-to-End Tests

- Every new user-facing feature ships with a Playwright test — a feature is not done until a browser exercises it
- The suite lives in the sibling cloud repo at `../cloud/e2e/tests/`, because browser tests need the authenticated stack (sessions, owners, per-actor permissions) that this repo has no server for
- Run it with `cd ../cloud && make dev-run` in one terminal and `make e2e` in another; see `../cloud/dev/README.md`
- This is in addition to Go tests, not a replacement: Go covers handlers, rendering and storage; Playwright covers what the user sees and clicks
- Anything in `web/templates/` or `web/static/*.js` is only reachable by a browser — Go tests cannot see a JavaScript regression, so changes there need Playwright coverage
- A change with no UI surface (store method, renderer internals, MCP tool) needs no Playwright test
- Cover the permission matrix explicitly — signed out, author, document owner — whenever behaviour differs per actor
- Assert persistence for anything that writes: reload and re-assert, so a client-only mutation cannot pass
