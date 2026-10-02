# Spec Delta

## Purpose

Lets clients create tasks and move them through the TODO / NEXT / DOING / WAITING / DONE / CANCELLED
workflow by stable ID, without ever touching Org files directly.

## ADDED Requirements

### Requirement: Create a task
The API SHALL create a task from a title and optional body, state (`TODO` or `NEXT`, default `TODO`),
priority (`A`–`C`), tags, scheduled and deadline dates, and project. The task SHALL be written as an Org
heading with a generated UUID `ID` property, and the response SHALL return that ID.

#### Scenario: Task is persisted as an Org heading
- **WHEN** a client sends `POST /api/v1/tasks` with title `Spotify 가족 요금제 납부` and no project
- **THEN** the response is `201` with a task whose `id` is a UUID and `state` is `TODO`
- **AND** `org/tasks/inbox.org` contains a heading `* TODO Spotify 가족 요금제 납부` whose `ID` property equals that `id`

#### Scenario: Task with deadline, warning and tags
- **WHEN** a client creates a task with deadline `{"date":"2026-10-25","repeat":"+1m","warning_days":3}`, state `NEXT` and tags `["bills"]`
- **THEN** the stored heading has state `NEXT`, tag `bills`, and the planning line `DEADLINE: <2026-10-25 Sun +1m -3d>`

#### Scenario: Task created under a project
- **WHEN** a client creates a task with `project_id` set to the ID of a level-1 heading in `org/projects/`
- **THEN** the task is stored as a direct child of that heading and the response `project` is `{"id": <project_id>, "title": <heading title>}`

#### Scenario: Unknown project
- **WHEN** `project_id` does not resolve to a project heading
- **THEN** the response is `422` and no file is modified

### Requirement: Read a task by stable ID
The API SHALL return a task by its Org `ID`, independent of the file that currently contains it.

#### Scenario: Read after the heading moved files
- **WHEN** a task heading with ID `X` is moved from `org/tasks/inbox.org` to `org/tasks/personal.org` and the engine is restarted
- **THEN** `GET /api/v1/tasks/X` returns the task with the same `id` and title

#### Scenario: Unknown ID
- **WHEN** a client requests `GET /api/v1/tasks/{id}` for a well-formed UUID that does not exist
- **THEN** the response is `404`

### Requirement: State transitions
The API SHALL provide `start` (→ `DOING`), `wait` (→ `WAITING`), `complete` (→ `DONE`), `skip` and
`cancel` (→ `CANCELLED`) actions, each delegated to Org's own state change so that Org hooks, logging and
repeaters apply.

#### Scenario: Complete a non-repeating task
- **WHEN** a client sends `POST /api/v1/tasks/{id}/complete` with `{"expected_state":"NEXT"}` for a non-repeating `NEXT` task
- **THEN** the response task has `state` `DONE` and a non-null `closed_at`
- **AND** the stored heading has a `CLOSED:` timestamp and a LOGBOOK line `State "DONE" from "NEXT"`

#### Scenario: Start and wait are logged
- **WHEN** a task is started and later put into waiting
- **THEN** its LOGBOOK contains one timestamped state-change line for each transition

### Requirement: Optimistic concurrency on transitions
Every task SHALL carry an opaque `version` that changes whenever its entry changes. Every transition request
SHALL carry `expected_state`, and SHALL also carry `expected_version` when the task repeats (a repeating task
returns to an open state, so the state alone cannot detect a retry). On any mismatch the API SHALL reject the
request with `409` and leave the file unchanged.

#### Scenario: Retried completion of a non-repeating task
- **WHEN** a client repeats an identical `complete` request after the first one succeeded
- **THEN** the second response is `409` with the actual state `DONE` and the file is unchanged by it

#### Scenario: Retried completion of a repeating task
- **WHEN** a client completes a repeating task with `expected_state` and `expected_version` and then repeats the identical request
- **THEN** the second response is `409` and the dates moved exactly once

#### Scenario: Repeating task without expected_version
- **WHEN** a transition request for a repeating task has no `expected_version`
- **THEN** the response is `422` and the file is unchanged

#### Scenario: Missing expected_state
- **WHEN** a transition request has no `expected_state`
- **THEN** the response is `422`

### Requirement: Idempotent creation
`POST /api/v1/tasks` SHALL honor an `Idempotency-Key` header for at least 24 hours while the API process
runs: a repeated key with the same payload returns the original response without creating another task.

#### Scenario: Same key, same payload
- **WHEN** the same create request with the same `Idempotency-Key` is sent twice
- **THEN** both responses carry the same `id` and exactly one heading exists

#### Scenario: Same key, different payload
- **WHEN** a key is reused with a different payload
- **THEN** the response is `422` and no task is created

### Requirement: WIP limit warning
When `start` results in more `DOING` tasks than the instance `doing_limit` (default 3), the API SHALL still
perform the transition and SHALL include the warning `doing_limit_exceeded` in the response.

#### Scenario: Fourth DOING task
- **WHEN** three tasks are `DOING` and a fourth is started
- **THEN** the response is `200`, the task is `DOING`, and `warnings` contains `doing_limit_exceeded`

### Requirement: Text input cannot alter Org structure
Titles SHALL be a single line of at most 500 characters and SHALL be rejected if Org would read part of them as
structure (a leading priority cookie or `COMMENT`, a trailing tag list). Bodies SHALL be stored so they cannot
create headings, keywords, drawers or diary entries. In both, active timestamps and diary sexps SHALL be made
inactive; otherwise text SHALL be returned exactly as sent.

#### Scenario: Title with a newline
- **WHEN** a title contains a newline
- **THEN** the response is `422`

#### Scenario: Title that Org would parse as structure
- **WHEN** a title is `[#A] pay rent`, `COMMENT pay rent` or `pay rent :bills:`
- **THEN** the response is `422` and no file is modified

#### Scenario: Body that looks like a heading
- **WHEN** a task body contains the line `* NEXT injected`
- **THEN** the file contains exactly one new heading (the task itself) and `GET` returns the body with the line `* NEXT injected` intact

#### Scenario: Active timestamps in title and body
- **WHEN** a task title contains `<2026-10-02 Fri>` and its body contains `<2026-10-02 Fri>` and `<%%(diary-float t 4 2)>`
- **THEN** they are stored and returned as `[2026-10-02 Fri]` and `[%%(diary-float t 4 2)>`, no agenda entry comes from them, and no diary expression is evaluated
