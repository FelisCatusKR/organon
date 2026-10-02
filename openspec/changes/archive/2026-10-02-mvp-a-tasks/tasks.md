# Tasks

## 1. Repository scaffolding

- [x] 1.1 Add `LICENSE` (MIT, Hansaem Woo), `.gitignore`, `.editorconfig`, `README.md` stub (project summary, license, link to `docs/architecture.md`) and verify `git status` shows no generated or cache files
- [x] 1.2 Add `mise.toml` tasks `test`, `test:elisp`, `test:go`, `e2e`, `spec:validate` (wired to placeholder commands) and verify `mise tasks` lists them
- [x] 1.3 Create the Go module `api/` with `cmd/organon` printing its version and verify `mise exec -- go build ./...` succeeds

## 2. Container image and test harness

- [x] 2.1 Write `container/Containerfile` with `build`, `runtime` and `test` stages (D10) and verify both `runtime` and `test` targets build on arm64 with podman
- [x] 2.2 Add `emacs/init.el` with the global settings from architecture §6.3 and §7.1 (keywords, logging, no backups/lockfiles, `enable-local-variables nil`, repeat-to-state `NEXT`, warning days 7) and verify `emacs -q --batch -l init.el` loads cleanly in the `test` image
- [x] 2.3 Add native-comp AOT for `emacs/` in the `runtime` stage and verify no `.eln` files are written to the cache dir on engine start
- [x] 2.4 Add an ERT runner (`scripts/test-elisp.sh`) that copies `tests/fixtures/` to a temp dir, runs ERT under `faketime` in the `test` image, and compares resulting `.org` files with golden files; verify with one trivial passing and one deliberately failing golden test

## 3. Engine core: RPC boundary and time model

- [x] 3.1 Implement the socket server and fixed dispatch table with `ping` and `meta` (D1); verify with an ERT test that an unknown method returns `{"ok":false,"error":{"code":"invalid"}}` and that a payload like `(kill-emacs)` as method name is rejected
- [x] 3.2 Implement prompt blocking and the `organon-with-entry` wrapper (D2); verify ERT: an external modification of a clean buffer is reverted silently, a modified buffer plus external change yields `conflict`, and no prompt function is ever reached (data-integrity: External edits are picked up)
- [x] 3.3 Implement `organon.json` loading, zone validation and TZ setup (D6); verify ERT for missing file, invalid zone and the Asia/Seoul day-boundary instant (time-model: Missing declaration, Invalid zone, Day boundary in Asia/Seoul)
- [x] 3.4 Implement startup sequence (load config → id locations → warm agenda → open socket) and the `rpc-ping` health method; verify the engine container becomes healthy only after the socket answers

## 4. Engine task operations

- [x] 4.1 Implement `task.create` (ID assignment, inbox default, project parent, planning via D5, tags, priority, `repeat_to_state`) and verify golden tests for task-lifecycle: Task is persisted as an Org heading, Task with deadline, warning and tags, Task created under a project, Unknown project
- [x] 4.2 Implement body/title handling (D7) and verify task-lifecycle: Body that looks like a heading, Active timestamps in title and body, Title that Org would parse as structure, and api-access: Lisp in text fields stays text (no command side effects)
- [x] 4.3 Implement `task.get` and the task JSON serializer (dates as date/time, instants UTC per D6) and verify task-lifecycle: Read after the heading moved files, Unknown ID, and time-model: closed_at in UTC
- [x] 4.4 Implement transitions `start`, `wait`, `complete` with `expected_state` and LOGBOOK flush, plus the `doing_limit` warning; verify task-lifecycle: Complete a non-repeating task, Start and wait are logged, Retried completion of a non-repeating task, Retried completion of a repeating task, Repeating task without expected_version, Fourth DOING task
- [x] 4.5 Verify repeater behavior under `faketime` for task-recurrence: Cumulative, Catch-up, Restart, Warning period is preserved, Default return state, Per-task return state, Restart repeater uses the calendar date
- [x] 4.6 Implement `skip` and `cancel` (series end removes the repeater) and verify task-recurrence: Skip a monthly payment, Cancel a subscription

## 5. Engine agenda queries

- [x] 5.1 Implement recursive agenda-source computation and `agenda.day` from text properties (D4); verify agenda-queries: Kinds on a fixed day, Explicit date, Default window, Nested project file, Excluded directories
- [x] 5.2 Implement `tasks.today`, `tasks.overdue`, `tasks.waiting` and verify agenda-queries: Done and event entries excluded, Overdue subset, Undated waiting task
- [x] 5.3 Spike agenda log mode for completed queries (Risk 1), choose it or the `org-element` fallback, record the choice in design.md, implement `tasks.completed`, and verify agenda-queries: Mixed completions and task-recurrence: Repeating completion appears in history

## 6. Go API

- [x] 6.1 Implement `internal/rpc` client with per-call deadline and error-code mapping; verify unit tests against a fake socket server including timeout → `engine_unavailable`
- [x] 6.2 Implement `internal/auth` (token file parsing, SHA-256 + constant-time compare, scopes, rate limit with optional client-IP header) and `organon token hash`; verify unit tests for api-access: Missing token, Read-only token tries to write, Brute force
- [x] 6.3 Implement handlers for `/healthz`, `/api/v1/meta`, `/agenda`, `/tasks/*` with input validation (UUID, date, timestamp parts) and problem+json errors; verify unit tests for api-access: Non-UUID identifier, Conflict error body, agenda-queries: Invalid date, task-lifecycle: Title with a newline, Missing expected_state
- [x] 6.4 Implement the idempotency LRU for `POST /tasks`; verify unit tests for task-lifecycle: Same key, same payload and Same key, different payload
- [x] 6.5 Write `api/openapi.yaml` for all endpoints and verify it passes an OpenAPI 3.1 linter and that unit-test responses validate against it
- [x] 6.6 Implement `organon init` and verify deployment: Init on an empty directory, Init on an existing instance
- [x] 6.7 Generate the Go JSON types from `api/openapi.yaml` (oapi-codegen, models only) and add a drift check (`mise run check:generated`); verify api-access: Generated types match the contract, and that the `organon` binary still has no third-party dependencies

## 7. Deployment definitions

- [x] 7.1 Write `compose.yaml` (engine `network_mode: none`, `user: ${UID}:${GID}`, read-only, healthchecks, `depends_on: service_healthy`, secrets) and verify `docker compose config` (or `podman compose config`) succeeds
- [x] 7.2 Write `contrib/quadlet/` units (`.container` ×2, `.volume` ×2) with `UserNS=keep-id`, `Network=none`, `Notify=healthy` and verify `/usr/libexec/podman/quadlet -dryrun -user` generates units without errors (deployment: Quadlet units are valid)
- [x] 7.3 Document install, `organon init`, token creation and example curl commands in `README.md`, and verify every documented command runs as written against a local stack

## 8. End-to-end and fault tests

- [x] 8.1 Implement `scripts/e2e.sh --runtime compose|podman` and the Go `e2e` package skeleton with schema validation; verify a smoke test (healthz + meta) passes under podman
- [x] 8.2 Port acceptance S1–S4 as e2e tests (create, recurring completion, today, complete) and verify they pass under podman
- [x] 8.3 Add e2e for data-integrity: Durable after engine kill, Paused engine, Edited while engine runs, Korean emoji and quotes, Directory after a test run; verify they pass
- [x] 8.4 Add e2e for deployment: Network disabled, Custom UID, Recreate everything (S8), and verify they pass under podman

## 9. CI and contributor docs

- [x] 9.1 Add `.github/workflows/ci.yml` (mise setup, `openspec validate --strict`, Go unit, ERT in image, quadlet dry-run, e2e compose + e2e podman, amd64 and arm64 runners) and verify the workflow passes on a pushed branch
- [x] 9.2 Write `CONTRIBUTING.md` (L1/L2/L3 duties, spec-scenario-test rule, mise usage) and verify the L1 command sequence in it runs as written on a fresh clone

## 10. Integration check

- [x] 10.1 Run the full e2e suite under both runtimes on the Pi and on CI, confirm all MVP-A scenarios pass, and run `openspec validate --strict` before archiving the change
