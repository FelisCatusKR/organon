# Design

## Context

The repository is greenfield. The system-wide design (component boundaries, security boundary, time model,
container layout, failure model) is in `docs/architecture.md`, and its Appendix A holds the spike
measurements this design relies on. This document only records decisions specific to implementing MVP-A.
Requirements are in `specs/`.

Fixed inputs: Debian 13 packages (Emacs 30.1, Org 9.7.11, org-roam 2.3.1) inside the image. Go 1.27, stdlib
only, for the production binary. The development host and the maintainer's deployment target are Raspberry
Pi 4 (arm64).

## Goals / Non-Goals

**Goals:**
- Every behavior in `specs/` is backed by an automated test (ERT, Go unit, or e2e).
- The Elisp adapter is the core; the Go layer stays a thin, boring translator.
- Same image and e2e suite under Docker Compose and rootless Podman.

**Non-Goals:**
- org-roam usage (installed but not loaded until MVP-B; task lookup uses `org-id`).
- Persistent idempotency store, journal, capture, backups, Hermes.

## Decisions

### D1. Engine RPC: newline-delimited JSON over a Unix socket, one request per connection
`make-network-process :family 'local :server t` with `:coding 'utf-8`. The filter accumulates bytes until
`\n`, then schedules the handler with `run-at-time 0` so that Org code never runs inside a process filter.
Each method is an entry in a fixed alist from method name to function. Request:
`{"id","method","params"}`. Response: `{"id","ok","result"|"error":{"code","message","data"}}`.
- *Alternative*: `emacsclient --eval` with base64 arguments. Rejected because the server socket is an eval
  interface and cannot be shared with the API container.
- *Alternative*: persistent multiplexed connections. Rejected for MVP; the per-call connect cost is about
  0.6 ms on a Pi 4 (measured).

### D2. One mutation wrapper for every write
All write methods go through a single macro (`organon-with-entry`). It does the following, each step from a
failure mode observed in the spike: block prompts, revert cleanly changed buffers via
`revert-without-query`, locate by ID, verify `expected_state`, run the Org command, flush pending log notes
(`org-add-log-note`), save with `file-precious-flag`, and revert on save failure. No method writes files any
other way.

### D3. Task lookup by `org-id`, not org-roam, in MVP-A
`org-id-locations-file` lives in the cache volume. It is rebuilt at engine start with
`org-id-update-id-locations` over agenda sources plus `org/archive/`. On a miss: one targeted rescan, then
`not_found`.
- *Alternative*: org-roam DB as the ID index. Deferred to MVP-B, because a full sync takes about 25 s per
  1,000 notes and is not needed for tasks.

### D4. Agenda results come from agenda text properties
The engine builds a one-day agenda for the requested date (`org-agenda-list` with span 1 and
`org-agenda-start-day`). It reads `org-hd-marker`, `type`, `ts-date` and `todo-state` from each line, then
reads entry fields at the marker. `overdue` is a filter on those results. `waiting` uses `org-tags-view` with
`TODO="WAITING"`. `completed` uses agenda log mode restricted to state changes into `DONE`, which reads
LOGBOOK entries Org itself wrote. `org-agenda-files` is computed per call by a recursive listing of
`org/tasks/` and `org/projects/`.

### D5. Timestamps are assembled from validated parts, placed by Org
The API validates `date` (calendar-valid), `time` (`HH:MM`), `repeat` (`^(\+|\+\+|\.\+)[1-9][0-9]*[hdwmy]$`)
and `warning_days` (1–365). The engine builds `"<DATE[ TIME][ REPEAT][ -Nd]>"` and calls
`org-deadline`/`org-schedule` with it, so Org adds the weekday and formats the planning line. The engine
returns dates by reading them back with Org's timestamp functions; it never computes them.

### D6. Instants are converted to UTC in the engine
At startup the engine reads `organon.json`, checks that `/usr/share/zoneinfo/<calendar_tz>` exists, and sets
`TZ` for the Emacs process. Instants such as `CLOSED` and log lines are converted with
`format-time-string "%FT%TZ" … t`. Go never needs a time zone database and never reinterprets wall-clock
times.

### D7. Body escaping
On write, the body passes through `org-escape-code-in-string` (comma-prefixes lines that would parse as
headings or `#+` keywords), drawer-looking lines are comma-prefixed, and active timestamps are rewritten
inactive. On read, `org-unescape-code-in-string` is applied. Titles reject control characters and newlines.

### D8. Go API layout
- `cmd/organon` holds the subcommands: `serve`, `healthcheck`, `rpc-ping`, `init`, `token hash`.
- `internal/rpc` is the socket client with a per-call deadline (default 10 s, env `ORGANON_ENGINE_TIMEOUT`).
- `internal/httpapi` uses `net/http` `ServeMux` method patterns.
- `internal/auth` handles token hashing, scopes and rate limiting.
- `internal/model` holds the JSON types.

Configuration comes only from environment variables. The token file (`ORGANON_TOKENS_FILE`, a mounted
secret) has one entry per line: `<name> <scope,scope> <sha256-hex>`. The rate-limit client address uses
`ORGANON_CLIENT_IP_HEADER` when set (e.g. `CF-Connecting-IP` behind cloudflared) and the TCP peer otherwise.
Idempotency keys are held in an in-memory LRU (10,000 entries, 24 h).

### D9. Production binary is stdlib-only; tests may use libraries
The `organon` binary imports only the standard library. Test code may depend on an OpenAPI validator
(`kin-openapi`) to check e2e responses against `api/openapi.yaml`. `openapi.yaml` is hand-written and is
the source; no code generation.

### D10. Image targets
One Containerfile with stages `build` (Go), `runtime` (trixie-slim + `emacs-nox`, `elpa-org-roam`,
`tzdata`, AOT-native-compiled `emacs/`, `organon` binary) and `test` (`runtime` + `faketime` + ERT files).
ERT always runs inside the `test` image, so contributors do not need Emacs installed. The engine starts with
`emacs -q --fg-daemon -l /opt/organon/init.el` (not `-Q`, which skips Debian's package activation).
Writable paths: `ORGANON_DATA_DIR`, `ORGANON_CACHE_DIR`, `ORGANON_RUN_DIR`, and `HOME=$ORGANON_CACHE_DIR`.

### D11. e2e suite in Go, runtime selected by script
`tests/e2e` is a Go test package behind the `e2e` build tag. It talks HTTP to a running stack and drives
faults (pause, kill, recreate) through a small runtime shim invoked by `scripts/e2e.sh --runtime
compose|podman`. Clock-dependent e2e scenarios use a stack started with `FAKETIME` set on the engine (test
image only).
- *Alternative*: bash + curl + jq. Rejected because of the scenario count and the need for schema validation.

### D12. Local development uses Podman; Compose is verified in CI
The Pi has rootless Podman. `scripts/e2e.sh --runtime compose` works locally with `docker compose` or
`podman compose` when available, but the merge gate is CI. L1/L2/L3 are defined in
`docs/architecture.md` §16.2.

## Risks / Trade-offs

- [Agenda log mode may not report repeating-task completions the way D4 assumes] → Verify first in task group
  4. Fallback: walk agenda-source entries and read LOGBOOK state lines with `org-element` (still Org's parser,
  not regex).
- [`org-deadline` string parsing may reinterpret partial timestamps] → ERT golden tests for every repeater and
  warning form. Fallback: `org-add-planning-info` with an explicit time value.
- [Emacs is single-threaded; a slow agenda blocks writes] → Measured 0.33 s for 300 tasks. The API timeout
  returns 503 instead of hanging. Warm agenda at startup removes the 2.9 s first-call cost.
- [In-memory idempotency is lost on API restart] → Transitions are additionally protected by
  `expected_state`. Documented limitation.
- [Rate limiting behind a proxy sees one address] → `ORGANON_CLIENT_IP_HEADER`, documented for the
  cloudflared setup.
- [Faketime in e2e differs from real clocks] → Only clock-dependent scenarios use it; the rest run on real time.

## Migration Plan

Greenfield, nothing to migrate. Rollback is removing the containers; the data directory is untouched by
design.

## Open Questions

- License (MIT / Apache-2.0 / AGPL-3.0). Needed before the repository is made public; does not affect this
  change.
