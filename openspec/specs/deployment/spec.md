# deployment Specification

## Purpose
Defines the runtime-neutral constraints the container image must satisfy and the supported ways to deploy it,
so that data survives any container lifecycle and the same image runs under Docker and rootless Podman.

## Requirements

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

### Requirement: Published images
Every commit on `main` whose CI run succeeded, including the unit and e2e jobs, SHALL be published to
`ghcr.io/feliscatuskr/organon` as one multi-arch image (linux/amd64 and linux/arm64). It SHALL be tagged
`sha-<first 7 hex digits of the commit>` and `main`. The image SHALL carry OCI labels for its source repository,
revision and license. Images SHALL NOT be published from pull requests or from commits whose CI failed.

#### Scenario: Commit on main
- **WHEN** a commit is pushed to `main` and all CI jobs succeed
- **THEN** `ghcr.io/feliscatuskr/organon:sha-<7>` and `:main` resolve to the same manifest list, with entries for amd64 and arm64
- **AND** the image label `org.opencontainers.image.revision` equals the full commit SHA and `organon version` prints `main-<7>`

#### Scenario: Pull request
- **WHEN** CI runs for a pull request
- **THEN** no image is pushed

#### Scenario: Failed CI on main
- **WHEN** a unit or e2e job fails for a commit on `main`
- **THEN** no image is pushed for that commit and `main` keeps pointing at the previous image

#### Scenario: Documentation-only commit
- **WHEN** a commit on `main` changes only documentation or specs, so the unit and e2e jobs are skipped
- **THEN** no new image is pushed, because the image content would be unchanged

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
