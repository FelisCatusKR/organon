# Tasks

## 1. Engine

- [x] 1.1 Implement `task.update` (D1: set/clear, remove-then-set for planning, body replacement, version required) and verify ERT golden tests for task-lifecycle: Postpone a deadline, Clear a schedule and a priority, Rename keeps state, tags and ID, Replace tags and body, Stale version, Invalid edit (engine side)
- [x] 1.2 Add `todo` and `next` to `task.transition` (D2) and verify ERT for task-lifecycle: Waiting task back to NEXT, Reopen a completed task, plus a repeating task requiring `expected_version`
- [x] 1.3 Implement `tasks.list` (D3) and verify ERT for task-listing: Undated backlog is listed, Closed tasks are not listed by default, Next actions of one project, Several states and a tag, Headings with other keywords are not tasks
- [x] 1.4 Implement `project.create` and `projects.list` (D4) and verify ERT for projects: New project file, Same title twice, Tasks can be added to the new project, Counts of open tasks, and a Hangul title producing a Hangul file name

## 2. API

- [ ] 2.1 Extend `api/openapi.yaml` (UpdateTask, ProjectInput, Project, ProjectList, list query parameters, `todo`/`next` actions), regenerate types, and verify `mise run lint:openapi` and `mise run check:generated` pass
- [ ] 2.2 Implement `PATCH /api/v1/tasks/{id}` with presence/null decoding into set/clear (D5) and verify unit tests: absent vs null fields, unknown field and missing `expected_version` → 422, stale version → 409
- [ ] 2.3 Implement `GET /api/v1/tasks` with query validation and verify unit tests for task-listing: Invalid filter, and that valid filters reach the engine unchanged
- [ ] 2.4 Implement `POST/GET /api/v1/projects`, extend scopes, and verify unit tests for api-access: Read-only token tries to edit or create a project; extend the contract test to the new endpoints

## 3. CLI

- [ ] 3.1 Implement `internal/client` (typed HTTP client, problem documents as errors) and verify unit tests against an `httptest` server
- [ ] 3.2 Implement configuration (env over file, permission check) and verify cli: Environment wins over the file, Missing configuration, Config file readable by others
- [ ] 3.3 Implement short-ID resolution for tasks and projects and verify cli: Unique prefix, Ambiguous prefix (and unknown prefix)
- [ ] 3.4 Implement the commands and output (D6) and verify cli: Add a monthly bill (exact request body), Complete a repeating task (read-then-transition), API error (text and `--json`), JSON output
- [ ] 3.5 Document the CLI in README (configure, everyday commands; keep curl as reference) and verify every documented command runs as written against a local stack

## 4. End-to-end

- [ ] 4.1 Add e2e tests for editing, reopening, listing and projects against the real stack and verify they pass under podman and compose
- [ ] 4.2 Add a CLI e2e smoke test (configure via env, `task add` → `today` → `task done` → `completed`, `project add` → `task add --project`) and verify it passes under podman
