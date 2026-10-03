# Spec Delta

## MODIFIED Requirements

### Requirement: Bearer token authentication
Every endpoint except `GET /livez`, `GET /readyz` and `GET /healthz` SHALL require `Authorization: Bearer
<token>`. Tokens SHALL be configured as hashes, never stored in plain text, and compared in constant time.

#### Scenario: Missing token
- **WHEN** a request to `GET /api/v1/tasks/today` has no `Authorization` header
- **THEN** the response is `401` and the engine is not contacted

#### Scenario: Health without token
- **WHEN** an unauthenticated client requests `GET /healthz`, `GET /readyz` or `GET /livez`
- **THEN** the response reveals no task or note data; `/healthz` and `/readyz` answer `200` while the engine is healthy and `503` otherwise
