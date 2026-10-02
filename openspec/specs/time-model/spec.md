# time-model Specification

## Purpose
Makes the instance calendar time zone explicit so that "today", repeaters and logged times are computed in the
owner's calendar while the API exchanges instants unambiguously in UTC.

## Requirements

### Requirement: Calendar time zone is declared with the data
Each instance SHALL declare an IANA `calendar_tz` in `organon.json` at the root of the data directory. The
engine SHALL refuse to become healthy when the declaration is missing or invalid, and SHALL NOT fall back to
UTC or the host zone.

#### Scenario: Missing declaration
- **WHEN** the engine starts with a data directory that has no `organon.json`
- **THEN** the engine health check fails and every API request except `/healthz` returns `503`

#### Scenario: Invalid zone
- **WHEN** `calendar_tz` is `Mars/Olympus`
- **THEN** the engine health check fails with an error naming the invalid zone

### Requirement: Calendar date follows the declared zone
The current calendar date used for agenda defaults, completion stamps and `.+` repeaters SHALL be computed in
`calendar_tz`, independent of the host or container time zone.

#### Scenario: Day boundary in Asia/Seoul
- **GIVEN** `calendar_tz` is `Asia/Seoul`, the container TZ is UTC, and the current instant is `2026-10-02T23:30:00Z`
- **WHEN** a client requests `GET /api/v1/meta`
- **THEN** `today` is `2026-10-03`
- **AND** a task with deadline `2026-10-03` is returned by `GET /api/v1/tasks/today` with kind `deadline`

#### Scenario: Restart repeater uses the calendar date
- **GIVEN** the same instant and zone
- **WHEN** a task with `DEADLINE: <2026-09-20 Sun .+1m>` is completed
- **THEN** its deadline becomes `2026-11-03`

### Requirement: Dates and instants in the API
Date fields (`scheduled`, `deadline`) SHALL be returned as a calendar `date` (`YYYY-MM-DD`) with optional
wall-clock `time` (`HH:MM`) and no zone. Instant fields (`closed_at`, log entries) SHALL be returned as RFC 3339
UTC timestamps, converted from the stored wall-clock time using `calendar_tz`.

#### Scenario: closed_at in UTC
- **GIVEN** `calendar_tz` is `Asia/Seoul`
- **WHEN** a task is completed at 08:30 local time on 2026-10-03
- **THEN** the stored `CLOSED:` stamp shows `2026-10-03 Sat 08:30` and the API returns `closed_at` `2026-10-02T23:30:00Z`

### Requirement: Instance metadata
`GET /api/v1/meta` SHALL return `calendar_tz`, the current calendar date `today`, and the server version.

#### Scenario: Meta response
- **WHEN** an authenticated client requests `GET /api/v1/meta`
- **THEN** the response contains `calendar_tz`, `today` and `version`
