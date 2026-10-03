# Spec Delta

## MODIFIED Requirements

### Requirement: Commands
The client SHALL provide `today`, `overdue`, `waiting`, `completed` and `task add|list|show|edit|start|wait|done|skip|cancel|todo|next`,
`project add|list` and `node add|show|search|backlinks|links`, each mapping to one API call (plus a read of the
task right before a transition or edit). Date options SHALL be passed to the API unchanged; the client SHALL NOT
compute dates.

#### Scenario: Add a monthly bill
- **WHEN** a user runs `organon task add "Spotify 가족 요금제 납부" --next --deadline 2026-10-25 --repeat +1m --warn 3 --tag bills`
- **THEN** the API receives a create request with exactly those values and the client prints the new task with its short ID

#### Scenario: Complete a repeating task
- **WHEN** a user runs `organon task done <id>` on a repeating task
- **THEN** the client reads the task, sends `complete` with its current state and version, and prints the next due date returned by the API

#### Scenario: Postponing keeps the repeater
- **WHEN** a user runs `organon task edit <id> --deadline 2026-10-30` on a task due `<2026-10-25 +1m -3d>`
- **THEN** the client sends the deadline `{"date": "2026-10-30", "repeat": "+1m", "warning_days": 3}` copied from the task it just read, and `--repeat none` would drop the repeater

#### Scenario: API error
- **WHEN** the API returns a problem document
- **THEN** the client prints its `detail`, exits non-zero, and with `--json` prints the problem document itself

#### Scenario: Add a note
- **WHEN** a user runs `organon node add "Emacs 설정 노트" --tag emacs --alias init.el --body "See [[id:<uuid>][keys]]"`
- **THEN** the API receives a node create request with exactly those values and the client prints the new node with its short ID

#### Scenario: Backlinks of a note
- **WHEN** a user runs `organon node backlinks <id>`
- **THEN** the client prints one line per source with its kind, short ID and title

### Requirement: Short IDs
Wherever a task, project or node ID is expected, the client SHALL accept a unique prefix of at least 4
characters, resolved against the API's task, project or node list, and SHALL fail on an ambiguous or unknown
prefix.

#### Scenario: Unique prefix
- **WHEN** exactly one open task's ID starts with `3f2a`
- **THEN** `organon task done 3f2a` acts on that task

#### Scenario: Ambiguous prefix
- **WHEN** two task IDs start with `3f2a`
- **THEN** the client lists both and exits non-zero without changing anything

#### Scenario: Node prefix
- **WHEN** exactly one node's ID starts with `a1b2`
- **THEN** `organon node show a1b2` shows that node
