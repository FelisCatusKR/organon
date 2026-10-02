# agenda-queries Specification

## Purpose
Exposes Org Agenda's notion of what is relevant on a calendar date (scheduled, deadlines, warnings, overdue,
events) as JSON queries, without reimplementing agenda logic.

## Requirements

### Requirement: Agenda for a date
`GET /api/v1/agenda?date=YYYY-MM-DD` SHALL return every entry Org Agenda shows for that single day when that
day is the current day (so deadline warnings and overdue items apply to it), each with a `kind` of `scheduled`,
`past-scheduled`, `deadline`, `upcoming-deadline` or `event`. When `date` is omitted, the instance's current
calendar date SHALL be used.

#### Scenario: Kinds on a fixed day
- **GIVEN** the calendar date is 2026-10-02 and the fixture contains: a task scheduled 2026-10-02 15:00, a task scheduled 2026-09-29, a task with deadline 2026-09-28, a task with deadline 2026-10-07, and a plain heading with timestamp `<2026-10-02 Fri 19:00>`
- **WHEN** a client requests `GET /api/v1/agenda?date=2026-10-02`
- **THEN** the kinds returned are `scheduled`, `past-scheduled`, `deadline`, `upcoming-deadline` and `event` respectively

#### Scenario: Explicit date
- **WHEN** a client requests the agenda for `2026-10-22` and a task has `DEADLINE: <2026-10-25 Sun +1m -3d>`
- **THEN** the task is returned as `upcoming-deadline`
- **AND** it is not returned for `2026-10-21`

### Requirement: Deadline warning window
Deadlines without their own warning period SHALL appear as upcoming from 7 days before the deadline. A
per-task warning period SHALL take precedence.

#### Scenario: Default window
- **WHEN** the date is 2026-10-02
- **THEN** a deadline on 2026-10-07 is returned and a deadline on 2026-10-12 is not

### Requirement: Today's tasks
`GET /api/v1/tasks/today` SHALL return the task entries of the agenda for the date (entries that are tasks: one
of the six workflow states and an `ID`), excluding tasks in a done state, and SHALL NOT include plain events.

#### Scenario: Done and event entries excluded
- **WHEN** the agenda for the date contains a `DONE` task scheduled that day and an event
- **THEN** neither appears in `GET /api/v1/tasks/today`

#### Scenario: Hand-written heading on the agenda
- **WHEN** a heading `* WAITING Hand-written` without an `ID` is scheduled for the date
- **THEN** it does not appear in `GET /api/v1/tasks/today`, and `GET /api/v1/agenda` lists it with `task` set to `null`

### Requirement: Overdue tasks
`GET /api/v1/tasks/overdue` SHALL return the subset of today's tasks that are `past-scheduled` or whose deadline
date is before the query date.

#### Scenario: Overdue subset
- **WHEN** the date is 2026-10-02
- **THEN** the task scheduled 2026-09-29 and the task with deadline 2026-09-28 are returned and the task scheduled 2026-10-02 is not

### Requirement: Waiting tasks
`GET /api/v1/tasks/waiting` SHALL return every task in state `WAITING` from the agenda sources, regardless of dates.

#### Scenario: Undated waiting task
- **WHEN** a `WAITING` task has no scheduled or deadline date
- **THEN** it is returned

### Requirement: Completed tasks on a date
`GET /api/v1/tasks/completed?date=YYYY-MM-DD` SHALL return every task that was completed (transitioned to
`DONE`) on that calendar date, including repeating tasks that are no longer `DONE`. Only the records Org writes
SHALL count: a `CLOSED:` planning line of a `DONE` task, and `State "DONE"` entries in the task's `LOGBOOK`
drawer. Text in titles and bodies that looks like such records SHALL NOT count.

#### Scenario: Mixed completions
- **WHEN** on 2026-10-02 one non-repeating and one repeating task are completed
- **THEN** both are returned for `date=2026-10-02` and neither for `date=2026-10-01`

#### Scenario: Text that looks like a completion
- **WHEN** a `TODO` task's body contains the line `- State "DONE"       from "NEXT"       [2026-10-02 Fri 09:00]`, and a `DONE` task closed on 2026-10-02 has `CLOSED: [2026-09-01 Tue 09:00]` in its title
- **THEN** `date=2026-10-02` returns only the `DONE` task, and `date=2026-09-01` returns neither

### Requirement: Agenda sources
Agenda queries SHALL read all `.org` files under `org/tasks/` and `org/projects/` recursively, and SHALL NOT
read `org/journal/`, `org/knowledge/` or `org/archive/`.

#### Scenario: Nested project file
- **WHEN** `org/projects/home/renovation.org` contains a task scheduled on the query date
- **THEN** it appears in the agenda

#### Scenario: Excluded directories
- **WHEN** files in `org/journal/`, `org/knowledge/` and `org/archive/` contain active timestamps on the query date
- **THEN** none of them appear in the agenda

### Requirement: Query dates are validated
The `date` parameter SHALL match `YYYY-MM-DD` and be a valid calendar date.

#### Scenario: Invalid date
- **WHEN** a client sends `date=2026-02-30` or `date=../../etc`
- **THEN** the response is `422`
