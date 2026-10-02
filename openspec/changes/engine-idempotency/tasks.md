# Tasks

## 1. Engine

- [ ] 1.1 Add the key table and an `organon-idempotent` helper (D1, D3, D4) and use it in `task.create` and `project.create`; verify ERT: same key and fingerprint twice gives one heading and the same id (task and project), a different fingerprint is `invalid`, an invalid key is `invalid`

## 2. API

- [ ] 2.1 Pass `idempotency_key` and `idempotency_fingerprint` (D2) from `beginIdempotent` to both creates; verify Go tests: the engine receives a 64-hex key that contains neither the token name nor the client key, and a retry after an engine timeout sends the same key again

## 3. End to end

- [ ] 3.1 Add an e2e test for task-lifecycle: Retry after an engine timeout (pause the engine, create with a key → 503, unpause, retry → 201 with one heading)

## 4. Integration

- [ ] 4.1 Run `openspec validate --all --strict`, the Go tests and ERT, then archive the change as the last commit of the pull request and verify `mise run spec:check-archived` passes
