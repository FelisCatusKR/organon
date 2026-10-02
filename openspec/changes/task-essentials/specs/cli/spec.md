# Spec Delta

## Purpose

Gives people a fast way to use Organon from a terminal. The command-line client talks only to the HTTP API,
so it works the same against a local or a remote instance.

## ADDED Requirements

### Requirement: Configuration
The client SHALL read the API URL and token from `ORGANON_URL` and `ORGANON_TOKEN`, falling back to
`~/.config/organon/client.json` (`{"url": ..., "token": ...}`). It SHALL refuse to read a config file that is
readable by other users, and SHALL never print the token.

#### Scenario: Environment wins over the file
- **WHEN** both the environment and the config file set a URL
- **THEN** the client uses the URL from the environment

#### Scenario: Missing configuration
- **WHEN** neither the environment nor the file provides a URL and token
- **THEN** the client exits non-zero with a message saying how to configure it, without contacting any server

#### Scenario: Config file readable by others
- **WHEN** `client.json` has mode `0644`
- **THEN** the client exits non-zero and asks for mode `0600`

### Requirement: Commands
The client SHALL provide `today`, `overdue`, `waiting`, `completed` and `task add|list|show|edit|start|wait|done|skip|cancel|todo|next`,
`project add|list`, each mapping to one API call (plus a read of the task right before a transition or edit).
Date options SHALL be passed to the API unchanged; the client SHALL NOT compute dates.

#### Scenario: Add a monthly bill
- **WHEN** a user runs `organon task add "Spotify 가족 요금제 납부" --next --deadline 2026-10-25 --repeat +1m --warn 3 --tag bills`
- **THEN** the API receives a create request with exactly those values and the client prints the new task with its short ID

#### Scenario: Complete a repeating task
- **WHEN** a user runs `organon task done <id>` on a repeating task
- **THEN** the client reads the task, sends `complete` with its current state and version, and prints the next due date returned by the API

#### Scenario: API error
- **WHEN** the API returns a problem document
- **THEN** the client prints its `detail`, exits non-zero, and with `--json` prints the problem document itself

### Requirement: Short IDs
Wherever a task or project ID is expected, the client SHALL accept a unique prefix of at least 4 characters,
resolved against the API's task or project list, and SHALL fail on an ambiguous or unknown prefix.

#### Scenario: Unique prefix
- **WHEN** exactly one open task's ID starts with `3f2a`
- **THEN** `organon task done 3f2a` acts on that task

#### Scenario: Ambiguous prefix
- **WHEN** two task IDs start with `3f2a`
- **THEN** the client lists both and exits non-zero without changing anything

### Requirement: Output
The client SHALL print human-readable output by default (state, short ID, title, dates; dates as given by the
API) and the API's JSON unchanged with `--json`. Instants such as `closed_at` SHALL be shown in the local time
zone of the machine running the client.

#### Scenario: JSON output
- **WHEN** a user runs `organon today --json`
- **THEN** stdout is the JSON body returned by `GET /api/v1/tasks/today`
