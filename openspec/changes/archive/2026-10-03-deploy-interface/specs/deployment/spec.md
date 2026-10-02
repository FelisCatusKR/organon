# Spec Delta

## ADDED Requirements

### Requirement: Container interface for custom deployments
The interface that deployment definitions rely on SHALL be declared in `contrib/container-interface.json`. It
covers the engine and API commands and health checks, their mount targets and tmpfs paths, the engine's
missing network, and the API's required and optional environment variables. `compose.yaml` and
`contrib/quadlet/` SHALL use exactly that interface, and the rootless Podman e2e SHALL start the containers
with its commands. A pull request that changes an existing declaration SHALL mark itself as breaking with `!`
in its title.

#### Scenario: Reference definitions follow the interface
- **WHEN** `scripts/check-deploy-interface.sh` runs
- **THEN** it passes for `compose.yaml` and `contrib/quadlet/`
- **AND** it fails if either uses a command, health check, mount target, tmpfs path or environment variable that differs from the declaration, for example a `/data` mount in the API

#### Scenario: Unmarked interface change
- **WHEN** a pull request changes `contrib/container-interface.json` and its title has no `!` before the colon
- **THEN** CI fails and names the required marker

## MODIFIED Requirements

### Requirement: Data directory initialization
The image SHALL provide an `init` command that creates the data directory layout and `organon.json` for a
calendar zone given with `--calendar-tz`. Without that flag it SHALL write nothing and print the command to
run, suggesting the host zone. It SHALL refuse to overwrite an existing declaration. The engine SHALL NOT
initialize a data directory itself.

#### Scenario: Init on an empty directory
- **WHEN** `organon init --calendar-tz Asia/Seoul` runs on an empty directory
- **THEN** it creates `org/tasks/inbox.org`, `org/projects/`, `org/knowledge/`, `org/journal/`, `org/archive/`, `attachments/` and `organon.json` with that zone

#### Scenario: Init on an existing instance
- **WHEN** `organon init` runs where `organon.json` already exists
- **THEN** it exits non-zero and changes nothing

#### Scenario: No implicit initialization
- **WHEN** the engine runs on a data directory without `organon.json`
- **THEN** every method, including the health check, fails with `unavailable`, and the data directory is left unchanged
