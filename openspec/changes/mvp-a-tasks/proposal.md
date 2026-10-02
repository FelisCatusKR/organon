# Proposal

## Why

Organon's premise is that Emacs + Org-mode already solve task state, recurrence and agenda
semantics, and only lack a safe, stable way to be used from outside Emacs. MVP-A proves that
premise end to end for tasks: a headless Emacs engine, a narrow API in front of it, and a
deployment in which the plain `.org` files remain the only source of truth.

The spike on 2026-10-02 (docs/architecture.md, Appendix A) showed the approach is feasible. It also
found failure modes that must be designed in from the start: dropped LOGBOOK entries, blocking
prompts, a skewed day boundary under UTC, and a repeater that fires on CANCELLED.

## What Changes

- Add a headless Emacs engine container that owns all reads and writes of the data directory and
  exposes a fixed set of operations over a local Unix socket. There is no evaluation path.
- Add a Go HTTP API (`/api/v1`) with token authentication and scopes. It translates Org task data
  into normalized JSON.
- Task operations: create, get, and the state transitions start, wait, complete, skip, cancel, all
  guarded by `expected_state`.
- Recurrence fully delegated to Org repeaters (`+`, `++`, `.+`), with an explicit return state and
  distinct skip-occurrence / cancel-series semantics.
- Agenda queries: today, overdue, waiting, completed-on-date, and a raw one-day agenda, all
  following Org Agenda semantics for a given calendar date.
- An explicit instance time model: a required `calendar_tz` declared in the data directory,
  date-only fields without zone, instants in UTC.
- Deployment: one image, two containers. Docker Compose is the official definition and a Podman
  Quadlet example is provided. Data lives in a host bind mount and survives removal of
  containers and volumes.
- Tooling: CI that runs e2e under both Docker Compose and rootless Podman.

Out of scope for this change: org-roam knowledge nodes and backlinks (MVP-B), journal, capture,
full-text search, Hermes integration, backups.

## Capabilities

### New Capabilities

- `task-lifecycle`: creating tasks, reading them by stable ID, and state transitions with
  optimistic concurrency and idempotency.
- `task-recurrence`: behavior of repeating tasks on completion, skip and cancel, all computed by Org.
- `agenda-queries`: today / overdue / waiting / completed / one-day agenda views and which files
  feed them.
- `time-model`: the instance calendar time zone and how dates and instants appear in the API.
- `api-access`: authentication, scopes, error format, health endpoint and input boundaries of the
  HTTP API.
- `data-integrity`: the data directory as sole source of truth: atomicity, no stray files,
  external-edit handling, bounded response when the engine stalls.
- `deployment`: runtime-neutral image constraints, supported deployment definitions, and data
  survival across container and volume removal.

### Modified Capabilities

(none, first change)

## Impact

- New code: `emacs/` (engine adapter), `api/` (Go module + `openapi.yaml`), `container/`,
  `compose.yaml`, `contrib/quadlet/`, `scripts/`, `tests/fixtures/`, `.github/workflows/`.
- New runtime dependencies: Debian `emacs-nox`, `elpa-org-roam` (installed now, used in MVP-B),
  inside the image only. The Go API uses the standard library only.
- Development tooling: pinned in `mise.toml` (Go, Node, OpenSpec CLI).
