# Spec Delta

## MODIFIED Requirements

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
