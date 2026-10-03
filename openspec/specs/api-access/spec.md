# api-access Specification

## Purpose
Defines who may call the HTTP API and with which rights, how errors are reported, and which inputs the API will
never accept, so that it is safe to expose through a public tunnel.

## Requirements

### Requirement: Bearer token authentication
Every endpoint except `GET /healthz` SHALL require `Authorization: Bearer <token>`. Tokens SHALL be configured
as hashes, never stored in plain text, and compared in constant time.

#### Scenario: Missing token
- **WHEN** a request to `GET /api/v1/tasks/today` has no `Authorization` header
- **THEN** the response is `401` and the engine is not contacted

#### Scenario: Health without token
- **WHEN** an unauthenticated client requests `GET /healthz`
- **THEN** the response is `200` while the engine is healthy and `503` otherwise, and reveals no task data

### Requirement: Scopes
Each token SHALL carry scopes. `read` SHALL be required for every `GET` under `/api/v1`, `tasks:write` for
creating, editing and transitioning tasks and for creating projects, and `nodes:write` for creating knowledge
nodes. A scope SHALL NOT imply another.

#### Scenario: Read-only token tries to write
- **WHEN** a token with only `read` sends `POST /api/v1/tasks`
- **THEN** the response is `403` and no file is modified

#### Scenario: Read-only token tries to edit or create a project
- **WHEN** a token with only `read` sends `PATCH /api/v1/tasks/{id}` or `POST /api/v1/projects`
- **THEN** the response is `403` and no file is modified

#### Scenario: Read-only token tries to create a node
- **WHEN** a token with only `read` sends `POST /api/v1/nodes`
- **THEN** the response is `403` and no file is modified

#### Scenario: Note-taking token cannot change tasks
- **WHEN** a token with `read,nodes:write` sends `POST /api/v1/tasks`, and a token with `read,tasks:write` sends `POST /api/v1/nodes`
- **THEN** both responses are `403` and no file is modified

### Requirement: Failed authentication is rate limited
Repeated failed authentication attempts from one client address SHALL be throttled.

#### Scenario: Brute force
- **WHEN** a client sends more than 10 requests with invalid tokens within one minute
- **THEN** subsequent requests from that address receive `429` for the rest of the window

### Requirement: Problem details errors
Error responses SHALL use `application/problem+json` (RFC 9457) with a stable machine-readable `code`:
`invalid` → 422, `not_found` → 404, `conflict` → 409, `unauthorized` → 401, `forbidden` → 403,
`rate_limited` → 429, `engine_unavailable` → 503, `internal` → 500.

#### Scenario: Conflict error body
- **WHEN** a transition fails because `expected_state` does not match
- **THEN** the response is `409` with content type `application/problem+json`, `code` `conflict`, and the actual state in the body

#### Scenario: Throttled request
- **WHEN** a client address is throttled after failed authentication attempts
- **THEN** the response is `429` with content type `application/problem+json` and `code` `rate_limited`

### Requirement: No code execution or file addressing through the API
The API SHALL NOT accept Lisp code, Org capture templates, file paths or file names as input. Identifiers in paths
SHALL be UUIDs.

#### Scenario: Non-UUID identifier
- **WHEN** a client requests `GET /api/v1/tasks/..%2F..%2Fetc%2Fpasswd`
- **THEN** the response is `422` and the engine is not contacted

#### Scenario: Lisp in text fields stays text
- **WHEN** a task is created with title `(shell-command "touch /tmp/pwned")` and body `%(shell-command "id")`
- **THEN** the title and body are stored verbatim as text and no command runs

### Requirement: HTTP contract is published
The API SHALL ship an OpenAPI 3.1 document describing every `/api/v1` endpoint. The document SHALL be the source
of the API's JSON types (they are generated from it), and responses SHALL conform to it.

#### Scenario: Responses match the schema
- **WHEN** the e2e suite runs
- **THEN** every recorded response validates against `api/openapi.yaml`

#### Scenario: Generated types match the contract
- **WHEN** CI regenerates the Go types from `api/openapi.yaml`
- **THEN** the result is identical to the committed code
