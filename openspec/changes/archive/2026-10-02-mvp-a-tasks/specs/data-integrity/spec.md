# Spec Delta

## Purpose

Guarantees that the plain Org files remain a complete, consistent and sole source of truth, and that the engine
fails safely instead of hanging or losing writes.

## ADDED Requirements

### Requirement: Writes are durable and atomic
Every successful write request SHALL be saved to disk before the response is sent. Each file SHALL be replaced
atomically, so that a reader never observes a partially written file.

#### Scenario: Durable after engine kill
- **WHEN** a task is created successfully and the engine process is killed immediately afterwards
- **THEN** after restart the task is present in the file and readable through the API

### Requirement: No stray files in the data directory
Engine operation SHALL NOT leave backup, auto-save or lock files (`*~`, `#*#`, `.#*`) in the data directory.

#### Scenario: Directory after a test run
- **WHEN** the full e2e suite has run
- **THEN** the data directory contains only `.org` files, `organon.json` and `attachments/` content

### Requirement: External edits are picked up
When a file is changed on disk by another process, the next request SHALL operate on the on-disk content
without prompting and without overwriting the external change.

#### Scenario: Edited while engine runs
- **WHEN** a heading is appended to `org/tasks/inbox.org` by another process and a client then creates a task in the same file
- **THEN** the file contains both the externally appended heading and the new task

### Requirement: Bounded response when the engine stalls
The API SHALL answer every request within its configured engine timeout (default 10 s) plus one second. If the
engine does not answer in time, the response SHALL be `503`, and the API SHALL recover without restart once
the engine responds again.

#### Scenario: Paused engine
- **WHEN** the engine container is paused and a client requests `GET /api/v1/tasks/today`
- **THEN** the response is `503` within 11 seconds
- **AND** after the container is unpaused the same request returns `200`

### Requirement: Unicode round trip
Text SHALL be stored as UTF-8 and returned byte-for-byte as sent (subject to the structural escaping rules of
task bodies).

#### Scenario: Korean, emoji and quotes
- **WHEN** a task is created with title `한글 ✓ "quote" 🎉`
- **THEN** the file contains that title encoded as UTF-8 and `GET` returns it unchanged
