# Spec Delta

## MODIFIED Requirements

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
