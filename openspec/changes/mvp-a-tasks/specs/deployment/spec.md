# Spec Delta

## Purpose

Defines the runtime-neutral constraints the container image must satisfy and the supported ways to deploy it,
so that data survives any container lifecycle and the same image runs under Docker and rootless Podman.

## ADDED Requirements

### Requirement: Engine runs without network
The engine container SHALL be fully functional with no network interface other than loopback.

#### Scenario: Network disabled
- **WHEN** the engine runs with networking disabled (`network_mode: none` / `Network=none`)
- **THEN** the e2e suite passes

### Requirement: Arbitrary non-root user
Both containers SHALL run as an arbitrary non-root UID/GID supplied at deploy time, with a read-only root
filesystem, and files they create in the data directory SHALL be owned by that UID.

#### Scenario: Custom UID
- **WHEN** the stack runs as UID 12345 with read-only root filesystems
- **THEN** the e2e suite passes and every file it created in the data directory is owned by UID 12345

### Requirement: API container has no data access
The API container SHALL NOT mount the data directory; it SHALL only share the socket volume with the engine.

#### Scenario: Deployment definitions
- **WHEN** `compose.yaml` and `contrib/quadlet/` are inspected
- **THEN** only the engine service mounts the data directory

### Requirement: Supported deployment definitions
The project SHALL ship `compose.yaml` as the officially supported deployment and Podman Quadlet units under
`contrib/quadlet/` as an example. Both SHALL start the API only after the engine reports healthy.

#### Scenario: Compose from scratch
- **WHEN** a user with an initialized data directory runs `docker compose up -d`
- **THEN** both services become healthy and `GET /healthz` returns `200`

#### Scenario: Quadlet units are valid
- **WHEN** the Quadlet generator runs in dry-run mode on `contrib/quadlet/`
- **THEN** it produces systemd units without errors

### Requirement: Data survives container and volume removal
Removing all containers, images and named volumes SHALL NOT affect the data directory, and a fresh deployment
on the same data directory SHALL serve the same tasks.

#### Scenario: Recreate everything
- **GIVEN** tasks created through the API and a SHA-256 manifest of the data directory
- **WHEN** all containers and named volumes are removed and the stack is started again
- **THEN** the manifest is unchanged and `GET` for each previously created task ID returns the same task

### Requirement: Data directory initialization
The image SHALL provide an `init` command that creates the data directory layout and `organon.json`, proposing
the host time zone and refusing to overwrite an existing declaration.

#### Scenario: Init on an empty directory
- **WHEN** `organon init --calendar-tz Asia/Seoul` runs on an empty directory
- **THEN** it creates `org/tasks/inbox.org`, `org/projects/`, `org/knowledge/`, `org/journal/`, `org/archive/`, `attachments/` and `organon.json` with that zone

#### Scenario: Init on an existing instance
- **WHEN** `organon init` runs where `organon.json` already exists
- **THEN** it exits non-zero and changes nothing
