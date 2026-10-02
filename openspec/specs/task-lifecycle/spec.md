# task-lifecycle Specification

## Purpose
Lets clients create tasks and move them through the TODO / NEXT / DOING / WAITING / DONE / CANCELLED
workflow by stable ID, without ever touching Org files directly.

## Requirements

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
structure (a priority cookie anywhere, a leading `COMMENT`, a trailing tag list). Bodies SHALL be stored so they
cannot create headings, keywords, drawers, clock entries or diary entries. In both, active timestamps and diary
sexps SHALL be made inactive. Tags SHALL NOT include `ARCHIVE`, which makes Org hide a task from the agenda.
Org does not keep surrounding whitespace, so titles SHALL be returned without leading and trailing whitespace,
and bodies without leading blank lines and trailing whitespace; otherwise text SHALL be returned exactly as
sent.

#### Scenario: Title with a newline
- **WHEN** a title contains a newline
- **THEN** the response is `422`

#### Scenario: Title that Org would parse as structure
- **WHEN** a title is `[#A] pay rent`, `pay [#C] rent`, `COMMENT pay rent` or `pay rent :bills:`
- **THEN** the response is `422` and no file is modified

#### Scenario: Body that looks like a heading
- **WHEN** a task body contains the line `* NEXT injected`
- **THEN** the file contains exactly one new heading (the task itself) and `GET` returns the body with the line `* NEXT injected` intact

#### Scenario: Body that looks like metadata
- **WHEN** a task body starts with the line `CLOCK: [2026-10-02 Fri 09:00]--[2026-10-02 Fri 10:00] =>  1:00`
- **THEN** `GET` returns the body with that line intact, and Org does not read it as a clock entry

#### Scenario: Archive tag
- **WHEN** a task is created with tags `["ARCHIVE"]`
- **THEN** the response is `422` and no file is modified

#### Scenario: Active timestamps in title and body
- **WHEN** a task title contains `<2026-10-02 Fri>` and its body contains `<2026-10-02 Fri>` and `<%%(diary-float t 4 2)>`
- **THEN** they are stored and returned as `[2026-10-02 Fri]` and `[%%(diary-float t 4 2)>`, no agenda entry comes from them, and no diary expression is evaluated

#### Scenario: Surrounding whitespace is not kept
- **WHEN** a task is created with title `"  Pay rent  "` and body `"\n\n  indented\n\nlast  \n\n"`
- **THEN** the response and a later `GET` return title `"Pay rent"` and body `"  indented\n\nlast"`

### Requirement: Only the six workflow states are tasks
A heading SHALL be treated as a task only if its TODO keyword is one of `TODO`, `NEXT`, `DOING`, `WAITING`,
`DONE` or `CANCELLED`. Headings with other keywords, for example from a file's own `#+TODO:` line, SHALL NOT be
returned as tasks: task queries SHALL leave them out, reading or transitioning them SHALL answer `404`, and the
agenda SHALL list their dated entries with `task` set to `null`.

#### Scenario: Keyword from a file's own TODO line
- **GIVEN** a file in `org/tasks/` starts with `#+TODO: WIP | FIN` and contains `* WIP Draft` with an `ID` and `SCHEDULED: <2026-10-02 Fri>`
- **WHEN** a client requests `GET /api/v1/tasks/today?date=2026-10-02` and `GET /api/v1/tasks/{id}` for that heading
- **THEN** the heading is not in the task list, `GET` answers `404`, and `GET /api/v1/agenda?date=2026-10-02` lists it as `scheduled` with `task` `null`

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

### Requirement: Headings without an ID are not tasks
A heading SHALL be treated as a task only if it also has an `ID` property, since clients address tasks by ID.
Task lists and views SHALL leave out headings without an ID, and the agenda SHALL list their dated entries
with `task` set to `null`.

#### Scenario: Hand-written heading
- **WHEN** a heading `* WAITING Hand-written` without an `ID` is added to `org/tasks/inbox.org` outside the API
- **THEN** it is not returned by `GET /api/v1/tasks` or `GET /api/v1/tasks/waiting`, and no list returns a task whose `id` is `null`

### Requirement: Responses carry the saved version
The task in a successful edit or transition response SHALL carry the `version` of the entry as saved, including
LOGBOOK lines written by the change, so that it can be used as the next `expected_version`.

#### Scenario: Reopen with the version from the completion
- **WHEN** a client completes a task and then sends `todo` with the `version` returned by the completion
- **THEN** the task is reopened
