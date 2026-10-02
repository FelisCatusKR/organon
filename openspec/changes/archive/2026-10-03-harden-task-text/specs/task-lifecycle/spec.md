# Spec Delta

## MODIFIED Requirements

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
