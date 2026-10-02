# Spec Delta

## MODIFIED Requirements

### Requirement: Idempotent creation
`POST /api/v1/tasks` SHALL honor an `Idempotency-Key` header for at least 24 hours while the engine runs: a
repeated key with the same payload returns the original task without creating another one. This SHALL hold
even when an earlier request with the key failed because the API stopped waiting for the engine, which may
still have created the task.

#### Scenario: Same key, same payload
- **WHEN** the same create request with the same `Idempotency-Key` is sent twice
- **THEN** both responses carry the same `id` and exactly one heading exists

#### Scenario: Same key, different payload
- **WHEN** a key is reused with a different payload
- **THEN** the response is `422` and no task is created

#### Scenario: Retry after an engine timeout
- **WHEN** a create request with an `Idempotency-Key` gets `503` because the engine did not answer in time, the engine then completes it, and the client retries with the same key and payload
- **THEN** the retry returns `201` with the task the first request created, and exactly one heading with that title exists
