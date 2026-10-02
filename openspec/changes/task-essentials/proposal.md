# Proposal

## Why

MVP-A covers "add a task, see today, finish it", but a task manager also has to support the daily routine:
fixing a typo, postponing a deadline, putting a waiting task back to NEXT, reopening something closed by
mistake, finding undated backlog items, and grouping tasks by project. Without these, Organon cannot be used
day to day, and real use is what should shape the next features (MVP-B and beyond). A client is needed too:
curl is not something anyone uses every morning.

## What Changes

- **Edit a task** (`PATCH /api/v1/tasks/{id}`): title, body, priority, tags, scheduled, deadline (set,
  change or clear) and repeat_to_state. Guarded by `expected_version`. Dates are placed by Org's own
  scheduling commands, and the same rules against Org structure as for creation apply.
- **Transitions back to `TODO` and `NEXT`** (`todo`, `next` actions): reopen `DONE`/`CANCELLED` tasks and
  move `DOING`/`WAITING` tasks back.
- **List tasks** (`GET /api/v1/tasks?state=…&project=…&tag=…`), including tasks without dates. By default
  the list contains open tasks.
- **Projects**: create (`POST /api/v1/projects`, one file per project under `org/projects/`) and list
  (`GET /api/v1/projects`).
- **CLI**: client subcommands of the `organon` binary that use only the HTTP API, with human-readable output
  and `--json`:
  - views: `today`, `overdue`, `waiting`, `completed`
  - task commands: `task add|list|show|edit|start|wait|done|skip|cancel|todo|next`
  - project commands: `project add|list`
  - short ID prefixes are accepted

Out of scope: publishing images to GHCR, deployment, backups, org-roam (MVP-B), deleting tasks or projects,
closing projects.

## Capabilities

### New Capabilities

- `task-listing`: listing tasks by state, project and tag, independent of the agenda.
- `projects`: creating and listing projects (level-1 headings in `org/projects/`).
- `cli`: the command-line client: configuration, commands, output and ID prefixes.

### Modified Capabilities

- `task-lifecycle`: adds editing a task, and adds `todo` / `next` transitions to the existing state
  transitions requirement.
- `api-access`: the `tasks:write` scope also covers editing tasks and creating projects.

## Impact

- Engine: new RPC methods `task.update`, `tasks.list`, `project.create`, `projects.list`; `task.transition`
  gains two actions.
- API: new endpoints and schemas in `api/openapi.yaml` (generated types follow); new handlers.
- CLI: new client package in the Go module, plus subcommands of `cmd/organon`. The CLI uses only the
  standard library.
- Docs: README usage section switches from curl to the CLI (curl stays as reference).
