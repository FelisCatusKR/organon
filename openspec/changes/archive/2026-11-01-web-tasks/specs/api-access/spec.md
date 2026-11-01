## MODIFIED Requirements

### Requirement: Bearer token authentication
Every endpoint under `/api/v1` SHALL require `Authorization: Bearer <token>`, and SHALL NOT accept a web session
instead. Apart from `GET /livez`, `GET /readyz` and `GET /healthz`, no other path SHALL answer without a token, unless
the web UI is turned on, in which case the `web-ui` capability governs paths outside `/api/`. Tokens SHALL be
configured as hashes, never stored in plain text, and compared in constant time.

#### Scenario: Missing token
- **WHEN** a request to `GET /api/v1/tasks/today` has no `Authorization` header
- **THEN** the response is `401` and the engine is not contacted

#### Scenario: Health without token
- **WHEN** an unauthenticated client requests `GET /healthz`, `GET /readyz` or `GET /livez`
- **THEN** the response reveals no task or note data; `/healthz` and `/readyz` answer `200` while the engine is healthy and `503` otherwise

#### Scenario: Session cookie is not an API credential
- **GIVEN** the web UI is on and a browser holds a valid session cookie
- **WHEN** it requests `GET /api/v1/tasks/today` with that cookie and no `Authorization` header
- **THEN** the response is `401`
