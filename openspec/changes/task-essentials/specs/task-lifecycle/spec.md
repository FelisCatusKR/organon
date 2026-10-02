# Spec Delta

## ADDED Requirements

### Requirement: Edit a task
`PATCH /api/v1/tasks/{id}` SHALL change only the fields present in the request: `title`, `body`, `priority`,
`tags`, `scheduled`, `deadline` and `repeat_to_state`. `null` SHALL clear `priority`, `scheduled`, `deadline`
and `repeat_to_state`. The request SHALL carry `expected_version`, and the same input rules as for creation
SHALL apply.

#### Scenario: Postpone a deadline
- **WHEN** a client sends `{"expected_version": <v>, "deadline": {"date": "2026-10-30"}}` for a task due 2026-10-25
- **THEN** the response task has deadline date `2026-10-30`, every other field is unchanged, and the file contains `DEADLINE: <2026-10-30 Fri>`

#### Scenario: Clear a schedule and a priority
- **WHEN** a client sends `{"expected_version": <v>, "scheduled": null, "priority": null}` for a scheduled task with priority `A`
- **THEN** the task has no `SCHEDULED` line and no priority cookie, and `scheduled` and `priority` are `null`

#### Scenario: Rename keeps state, tags and ID
- **WHEN** a client changes only the `title` of a `NEXT` task tagged `bills`
- **THEN** the heading keeps state `NEXT`, tag `bills` and its `ID`, and shows the new title

#### Scenario: Replace tags and body
- **WHEN** a client sends `tags: ["home"]` and a new `body` for a task tagged `bills` with an old body
- **THEN** the task's only tag is `home` and its body is exactly the new body, with structural lines escaped as on creation

#### Scenario: Stale version
- **WHEN** the `expected_version` differs from the task's current version
- **THEN** the response is `409` with the actual version, and the file is unchanged

#### Scenario: Invalid edit
- **WHEN** a client sends a title that Org would parse as structure, an unknown field, or no `expected_version`
- **THEN** the response is `422` and the file is unchanged

## MODIFIED Requirements

### Requirement: State transitions
The API SHALL provide `start` (→ `DOING`), `wait` (→ `WAITING`), `complete` (→ `DONE`), `skip` and
`cancel` (→ `CANCELLED`), `todo` (→ `TODO`) and `next` (→ `NEXT`) actions, each delegated to Org's own state
change so that Org hooks, logging and repeaters apply. Moving a closed task back to an open state reopens it.

#### Scenario: Complete a non-repeating task
- **WHEN** a client sends `POST /api/v1/tasks/{id}/complete` with `{"expected_state":"NEXT"}` for a non-repeating `NEXT` task
- **THEN** the response task has `state` `DONE` and a non-null `closed_at`
- **AND** the stored heading has a `CLOSED:` timestamp and a LOGBOOK line `State "DONE" from "NEXT"`

#### Scenario: Start and wait are logged
- **WHEN** a task is started and later put into waiting
- **THEN** its LOGBOOK contains one timestamped state-change line for each transition

#### Scenario: Waiting task back to NEXT
- **WHEN** a client sends `next` for a `WAITING` task
- **THEN** the task is `NEXT`

#### Scenario: Reopen a completed task
- **WHEN** a client sends `todo` for a non-repeating `DONE` task
- **THEN** the task is `TODO`, its `CLOSED:` stamp is gone, `closed_at` is `null`, and its earlier LOGBOOK lines are kept
