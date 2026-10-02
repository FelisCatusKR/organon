# task-recurrence Specification

## Purpose
Defines how repeating tasks behave so that recurrence is computed entirely by Org repeaters and the API
never performs date arithmetic.

## Requirements

### Requirement: Completion advances dates by Org repeater rules
Completing a task whose scheduled or deadline timestamp has a repeater SHALL move that timestamp according
to Org's rules for `+`, `++` and `.+`, keep the same task ID, and leave the task in an open state.

#### Scenario: Cumulative repeater (+)
- **WHEN** a task with `DEADLINE: <2026-08-25 Tue +1m>` is completed on 2026-10-02
- **THEN** its deadline becomes `2026-09-25` and its `id` is unchanged

#### Scenario: Catch-up repeater (++)
- **WHEN** a task with `DEADLINE: <2026-08-25 Tue ++1m>` is completed on 2026-10-02
- **THEN** its deadline becomes `2026-10-25`

#### Scenario: Restart repeater (.+)
- **WHEN** a task with `DEADLINE: <2026-08-25 Tue .+1m>` is completed on 2026-10-02
- **THEN** its deadline becomes `2026-11-02`

#### Scenario: Warning period is preserved
- **WHEN** a task with `DEADLINE: <2026-10-25 Sun +1m -3d>` is completed on 2026-10-02
- **THEN** its deadline is `<2026-11-25 Wed +1m -3d>`

### Requirement: Return state after repetition
After a repeat, the task SHALL return to `NEXT` by default, or to its per-task `repeat_to_state` when one is
set. Only `TODO` and `NEXT` SHALL be accepted as `repeat_to_state`.

#### Scenario: Default return state
- **WHEN** a repeating `DOING` task is completed
- **THEN** its state afterwards is `NEXT`

#### Scenario: Per-task return state
- **WHEN** a repeating task created with `repeat_to_state: "TODO"` is completed
- **THEN** its state afterwards is `TODO`

#### Scenario: Invalid return state
- **WHEN** a client creates a task with `repeat_to_state: "DOING"`
- **THEN** the response is `422`

### Requirement: Completion history is recorded
Every completion of a repeating task SHALL be recorded with its timestamp in the task's LOGBOOK, so that it is
visible to completed-task queries even though the task is no longer `DONE`.

#### Scenario: Repeating completion appears in history
- **WHEN** a repeating task is completed on 2026-10-02
- **THEN** its LOGBOOK contains `State "DONE" from "<previous state>"` with a 2026-10-02 timestamp
- **AND** the task appears in `GET /api/v1/tasks/completed?date=2026-10-02`

### Requirement: Skip one occurrence
`skip` on a repeating task SHALL record the occurrence as `CANCELLED` and advance it to the next occurrence by
the repeater rule. On a non-repeating task `skip` SHALL behave exactly like `cancel`.

#### Scenario: Skip a monthly payment
- **WHEN** `skip` is called on a task with `DEADLINE: <2026-10-25 Sun +1m>`
- **THEN** the deadline becomes `2026-11-25`, the state is the return state, and the LOGBOOK records `State "CANCELLED"`

### Requirement: Cancel a series
`cancel` on a repeating task SHALL remove the repeater and set the task to `CANCELLED`, ending the series.

#### Scenario: Cancel a subscription
- **WHEN** `cancel` is called on a task with `DEADLINE: <2026-10-25 Sun +1m>`
- **THEN** the task is `CANCELLED`, its deadline is `<2026-10-25 Sun>` without a repeater, and it no longer appears in any agenda query

### Requirement: No date arithmetic outside Org
The API layer SHALL NOT compute next occurrences, warning windows or overdue status itself; it SHALL only
validate and assemble the timestamp components it receives and report what Org stored.

#### Scenario: API returns Org's result verbatim
- **WHEN** a repeating task is completed
- **THEN** the dates in the response equal the dates parsed from the stored heading after the engine saved it
