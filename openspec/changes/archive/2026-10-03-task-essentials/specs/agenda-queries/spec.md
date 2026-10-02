# Spec Delta

## MODIFIED Requirements

### Requirement: Today's tasks
`GET /api/v1/tasks/today` SHALL return the task entries of the agenda for the date (entries that are tasks: one
of the six workflow states and an `ID`), excluding tasks in a done state, and SHALL NOT include plain events.

#### Scenario: Done and event entries excluded
- **WHEN** the agenda for the date contains a `DONE` task scheduled that day and an event
- **THEN** neither appears in `GET /api/v1/tasks/today`

#### Scenario: Hand-written heading on the agenda
- **WHEN** a heading `* WAITING Hand-written` without an `ID` is scheduled for the date
- **THEN** it does not appear in `GET /api/v1/tasks/today`, and `GET /api/v1/agenda` lists it with `task` set to `null`
