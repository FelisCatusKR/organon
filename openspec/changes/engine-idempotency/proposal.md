# Proposal

## Why

Issue #17. The API remembers `Idempotency-Key`s, but only for requests that finished. When the engine is
slower than `ORGANON_ENGINE_TIMEOUT`, the API gives up, forgets the key and answers `503`. The engine still
completes the create. A client that retries with the same key, which is what the key is for, then gets a
second task. The same applies to `POST /api/v1/projects`.

## What Changes

- `task-lifecycle`: idempotent creation also holds when the API timed out waiting for the engine, and keys
  are kept while the engine runs (not only while the API runs). New scenario *Retry after an engine
  timeout*. Project creation follows, because it is defined as working "like task creation".
- Engine: `task.create` and `project.create` take an optional key and payload fingerprint. The engine keeps
  key → (fingerprint, created ID) in memory for 24 hours. A repeated key with the same fingerprint returns the
  existing task or project instead of creating one. A different fingerprint is `invalid`.
- API: passes a hash of the token-, endpoint- and client-scoped key, plus the payload fingerprint, to the
  engine. The client's key and the token name never reach the engine. The API's own store still answers
  ordinary replays without calling the engine.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `task-lifecycle`: Idempotent creation.

## Impact

- `emacs/organon-task.el` (and a small helper in `emacs/organon.el`), `api/internal/httpapi/server.go`, ERT,
  Go and e2e tests. No change to the HTTP contract.
- Known limit: an engine restart forgets the keys. A create that completed just before a crash, retried
  after the restart, can still be duplicated.
