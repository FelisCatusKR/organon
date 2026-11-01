# Tasks

## 1. API seam

- [x] 1.1 Add `Server.UI`, `Server.InProcess` and exported `ClientAddr` to `internal/httpapi` (D1, D3), and verify unit tests that a run with `UI` nil behaves as before (web-ui: Off by default) and api-access: Session cookie is not an API credential

## 2. Web package

- [x] 2.1 Add sessions and sign-in, and verify tests for every scenario of web-ui: Sign-in with a token, Sessions
- [x] 2.2 Add templates, CSS, languages and browser protections, and verify tests for web-ui: Browser protections, Languages, plus a catalog test over all templates
- [x] 2.3 Add task pages, actions and quick add, and verify tests for web-ui: Task pages, Task actions, Quick add

## 3. Wiring and docs

- [x] 3.1 Read the two variables in `organon serve`, declare them in `contrib/container-interface.json`, `compose.yaml` and `scripts/e2e.sh`, and verify `mise run check:deploy-interface`
- [x] 3.2 Add HTTP e2e scenarios for signing in, listing, completing and signing out, and verify `mise run e2e -- --runtime compose`
- [x] 3.3 Update `docs/usage.md`, `docs/deployment.md`, `docs/architecture.md` (security boundary and the stale status line) and `README.md`, and verify a grep of the diff finds no private infrastructure names
