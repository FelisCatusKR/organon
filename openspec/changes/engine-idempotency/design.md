# Design

## Context

`beginIdempotent` (API) stores responses in an in-memory LRU and calls `Abort` on any engine error, so a
retry reaches the engine again. The engine handles requests one at a time, in arrival order. A request that
arrives while the engine is paused or busy is processed later, even if the API has stopped waiting.

## Decisions

### D1. The engine deduplicates; the API keeps its replay store
The engine is the only component that knows whether a create happened. It keeps an in-memory table instead
of writing the key into the Org file, so that the user's data does not collect opaque request IDs. The API
store stays: it answers ordinary replays without a round trip and keeps `Idempotent-Replayed` and the original
body.

Rejected alternatives:
- **Store the key as a property on the heading.** It survives engine restarts, but adds a machine-only
  property to every created heading and needs a search over all files on each keyed create.
- **API-side "unknown outcome" state.** It answers retries with `409` until a TTL expires. That is simpler,
  but the client never learns the ID of the task that was created.

### D2. What crosses the socket
The API sends:
- `idempotency_key`: hex SHA-256 of `token name NUL path NUL client key`
- `idempotency_fingerprint`: the existing payload hash

Neither reveals the token name or the client's key. The engine validates both as 64 hex characters.

### D3. Lifetime
Entries expire 24 hours after creation, matching the API store. The table is pruned when an entry is added,
and capped at 10,000 entries: the oldest go first.

### D4. Replay result
A replay returns the current state of the created task or project (looked up by ID), not a copy of the first
response. If that entry no longer exists, the engine forgets the key and creates a new one.
