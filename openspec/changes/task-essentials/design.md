# Design

## Context

This builds on the archived MVP-A (`openspec/changes/archive/2026-10-02-mvp-a-tasks/`) and the system design
in `docs/architecture.md`. Every write already goes through `organon-with-entry` / `organon-with-file`
(`emacs/organon.el`). Tasks carry a `version`. The Go types are generated from `api/openapi.yaml`, and the
production binary uses the standard library only. See proposal.md for why; specs/ for the requirements.

Org behavior checked before writing this design (Emacs 30.1, Org 9.7.11):
- `org-todo "TODO"` on a `DONE` task removes its `CLOSED:` stamp and keeps the LOGBOOK.
- `org-schedule '(4)` / `org-deadline '(4)` remove the planning entry.
- Calling `org-deadline` with a timestamp that has no repeater **keeps the old repeater** (seen in MVP-A).
- `org-priority ?\s` signals an error when the heading has no cookie.

## Goals / Non-Goals

**Goals:**
- Every new operation is a thin delegation to an Org command, behind the existing write wrapper.
- The CLI is a plain HTTP client: anything it can do, any other client can do the same way.

**Non-Goals:**
- Deleting tasks or projects, closing or archiving projects, refile between projects.
- Bulk operations, sorting options, pagination (personal scale; revisit if lists get long).
- Shell completion, interactive TUI.

## Decisions

### D1. Edits are partial, and "clear" is explicit at the engine boundary
JSON `null` and an absent field mean different things in a PATCH, but the engine parses `null` as absent
(MVP-A's `json-parse-string` settings). The API therefore decodes the body as raw fields, rejects unknown
ones, and sends the engine `{id, expected_version, set: {...}, clear: [...]}`. The engine applies, in order:
- `title`: `org-edit-headline`, which keeps keyword, priority cookie and tags.
- `priority`: `org-priority` with the letter. Clearing removes the cookie only if there is one.
- `tags`: `org-set-tags`. An empty list removes all tags.
- `scheduled`, `deadline`: always remove first (`'(4)`), then set from the validated string. Otherwise
  `org-deadline` would keep an old repeater the client meant to drop. Clearing is the removal alone.
- `repeat_to_state`: `org-entry-put` / `org-entry-delete`.
- `body`: replace the region between the end of the metadata and the next heading with the escaped body.

`expected_version` is required for every edit, unlike transitions, where it is required only for repeating
tasks. An edit overwrites fields, so it must not race with any other change.

### D2. `todo` and `next` are ordinary `org-todo` calls, not logged
Reopening is `org-todo "TODO"` / `org-todo "NEXT"`; Org removes `CLOSED`. The keywords keep no `!`, so these
moves are not written to the LOGBOOK. LOGBOOK records progress and completion. Logging every demotion
would also log the automatic return to `NEXT` after each repeat. On a repeating task, `todo`/`next` need
`expected_version` like every other transition.

### D3. Listing walks the agenda sources and filters in Lisp
`tasks.list` uses `org-map-entries` over `organon-agenda-files` and keeps entries whose state is in the
requested set, whose project (MVP-A's `organon--project-json`) matches, and whose local tags contain the tag.
Filtering happens in Lisp rather than by building an Org match string, so request values never become Org
query syntax. Tag filtering uses local tags, the same tags the task JSON shows.

### D4. Projects are files named after their title
`project.create` derives a file name from the title:
- lowercase it, keep letters (Hangul included) and digits, and turn every other run of characters into `-`
- trim the result to 50 characters; if nothing remains, use `project`
- add `-2`, `-3`, … until the name is unused

The engine then writes `#+title:` and a level-1 heading through `organon-with-file`, and `org-id-get-create`
assigns the ID. The file name is a location hint only. The title passes the same `organon-clean-title` rules as
task titles.

`projects.list` returns level-1 headings that have an ID and no TODO keyword, found in `org/projects/`. For
each it counts open tasks below it.

### D5. HTTP surface
- `PATCH /api/v1/tasks/{id}` (`tasks:write`): body `UpdateTask` with nullable fields plus
  `expected_version`. Decoded as `map[string]json.RawMessage` so that the API can tell absent from null.
  The generated type still documents the schema.
- `GET /api/v1/tasks?state=&project=&tag=` (`read`):
  - `state`: comma-separated, whitelist of the six states, default the four open ones
  - `project`: UUID
  - `tag`: `[A-Za-z0-9_@#%]+`
  - all validated in Go before the engine is called
- `POST /api/v1/projects` (`tasks:write`) and `GET /api/v1/projects` (`read`).
- `TransitionTask.action` enum gains `todo`, `next`.

### D6. CLI inside the `organon` binary
- `internal/client`: a typed HTTP client over the generated model types.
- `internal/cli`: commands, flags (stdlib `flag`), configuration and output.
- `cmd/organon` dispatches the client commands (`today`, `overdue`, `waiting`, `completed`, `task`,
  `project`) next to the server commands.

Configuration:
- `ORGANON_URL` / `ORGANON_TOKEN` take precedence over `~/.config/organon/client.json`.
- The file is refused if it has any group or other permission bits. The token is never printed.

Dates:
- `--deadline "2026-10-25"` or `--deadline "2026-10-25 15:00"`; the client only splits date and time.
- `--repeat` applies to the deadline if one is given, otherwise to the schedule.
- `--warn N` applies to the deadline.
- In `edit`, `none` clears a field.

The client never computes dates.

Short IDs:
- A full UUID is used as is.
- A prefix of 4 or more characters is resolved against `GET /api/v1/tasks?state=<all six>` (so closed tasks
  can be reopened) or `GET /api/v1/projects`.
- If the prefix matches more than one entry, the client lists the matches and exits non-zero.

Transitions and edits:
- The client reads the task (`GET`) right before acting and sends that state and version.
- The client never retries on its own. After a timeout or a 503 it says the outcome is unknown and suggests
  `organon task show` before trying again (see Risks).

Output:
- Default is a compact table: state, 8-character ID, planning dates as returned, title.
- `--json` prints the API body unchanged.
- `closed_at` is shown in the machine's local zone.

### D7. Tests
- **ERT (golden files):** `task.update` per field, including clearing, repeater replacement and the stale
  version; `todo`/`next` including reopening; `tasks.list` filters; project creation, including name collisions
  and Hangul titles.
- **Go unit tests:** PATCH presence/null handling, list query validation, scope checks, contract validation of
  the new responses.
- **Client and CLI:** tested against `httptest` servers: config precedence and permissions, prefix resolution,
  error mapping, and the exact create body produced from flags.
- **e2e:** the new scenarios, plus a CLI smoke test against the real stack.

## Risks / Trade-offs

- [A person re-runs `organon task done` after a timeout. The first call succeeded, so a repeating task moves
  twice, because the CLI reads a fresh version each time.] → The CLI never retries automatically. After an
  unknown outcome it tells the user to check with `task show` first. The API-level protection
  (`expected_version`) still covers programmatic clients that keep the version they read.
- [Prefix resolution lists every task.] → Fine at personal scale. If it becomes slow, add an ID-prefix query
  to the API later.
- [File names from Hangul titles] → ext4 and Btrfs handle UTF-8 names. Backups (restic) do too. The name is
  never an identifier.
- [Tag filter ignores inherited tags.] → Documented. It matches what the task JSON shows.
