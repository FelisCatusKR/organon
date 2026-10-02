# Spec Delta

## ADDED Requirements

### Requirement: Only the six workflow states are tasks
A heading SHALL be treated as a task only if its TODO keyword is one of `TODO`, `NEXT`, `DOING`, `WAITING`,
`DONE` or `CANCELLED`. Headings with other keywords, for example from a file's own `#+TODO:` line, SHALL NOT be
returned as tasks: task queries SHALL leave them out, reading or transitioning them SHALL answer `404`, and the
agenda SHALL list their dated entries with `task` set to `null`.

#### Scenario: Keyword from a file's own TODO line
- **GIVEN** a file in `org/tasks/` starts with `#+TODO: WIP | FIN` and contains `* WIP Draft` with an `ID` and `SCHEDULED: <2026-10-02 Fri>`
- **WHEN** a client requests `GET /api/v1/tasks/today?date=2026-10-02` and `GET /api/v1/tasks/{id}` for that heading
- **THEN** the heading is not in the task list, `GET` answers `404`, and `GET /api/v1/agenda?date=2026-10-02` lists it as `scheduled` with `task` `null`

## MODIFIED Requirements

### Requirement: Text input cannot alter Org structure
Titles SHALL be a single line of at most 500 characters and SHALL be rejected if Org would read part of them as
structure (a leading priority cookie or `COMMENT`, a trailing tag list). Bodies SHALL be stored so they cannot
create headings, keywords, drawers or diary entries. In both, active timestamps and diary sexps SHALL be made
inactive. Org does not keep surrounding whitespace, so titles SHALL be returned without leading and trailing
whitespace, and bodies without leading blank lines and trailing whitespace; otherwise text SHALL be returned
exactly as sent.

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

#### Scenario: Surrounding whitespace is not kept
- **WHEN** a task is created with title `"  Pay rent  "` and body `"\n\n  indented\n\nlast  \n\n"`
- **THEN** the response and a later `GET` return title `"Pay rent"` and body `"  indented\n\nlast"`
