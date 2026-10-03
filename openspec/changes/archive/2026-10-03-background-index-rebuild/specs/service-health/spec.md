# Spec Delta

## Purpose

Lets deployments (Compose, Quadlet, Kubernetes probes) tell whether the API process is alive and whether it can
serve requests, without a token and without revealing any data.

## ADDED Requirements

### Requirement: Liveness
`GET /livez` SHALL answer `200` with `{"status": "pass"}` whenever the API process can answer HTTP, without
contacting the engine, so that an engine outage never makes a deployment restart the API.

#### Scenario: Engine paused
- **WHEN** the engine is paused and a client requests `GET /livez`
- **THEN** the response is `200` within one second

### Requirement: Readiness
`GET /readyz` SHALL answer `200` when the engine answers, with status `pass`, or `warn` while the node index is
rebuilding or has failed (tasks still work). It SHALL answer `503` with status `fail` when the engine does not
answer. The body SHALL hold the status and, for `warn` and `fail`, a short reason, and nothing else.

#### Scenario: Ready
- **WHEN** the engine answers and the index is ready
- **THEN** `GET /readyz` answers `200` with status `pass`

#### Scenario: Index rebuilding
- **WHEN** the engine answers and the index is rebuilding
- **THEN** `GET /readyz` answers `200` with status `warn`

#### Scenario: Engine unavailable
- **WHEN** the engine is paused
- **THEN** `GET /readyz` answers `503` with status `fail`

### Requirement: Legacy health endpoint
`GET /healthz` SHALL answer exactly like `GET /readyz`.

#### Scenario: Same answer
- **WHEN** a client requests `/healthz` and `/readyz` in the same state
- **THEN** both have the same status code and body
