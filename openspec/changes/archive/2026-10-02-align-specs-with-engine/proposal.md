# Proposal

## Why

An early review compared the specs with the engine and found three places where they disagree:

- The text requirement says titles and bodies come back "exactly as sent", but Org does not keep surrounding
  whitespace: a title `"  Pay rent  "` comes back as `"Pay rent"`, and a body loses its leading blank lines and
  trailing whitespace.
- A file may declare its own keywords with a `#+TODO:` line. Headings using them are returned as tasks with a
  `state` outside the six states of the HTTP contract (for example `"WIP"`), so responses no longer conform to
  `api/openapi.yaml`.
- The API answers throttled requests with `429` and code `rate_limited`, but the list of problem codes in the
  spec does not mention it.

## What Changes

- `task-lifecycle`: the text requirement states the whitespace that Org drops; everything else is still
  returned as sent. New scenario.
- `task-lifecycle`: new requirement that a heading is a task only if its keyword is one of `TODO`, `NEXT`,
  `DOING`, `WAITING`, `DONE`, `CANCELLED`. Other keywords are not tasks: task queries leave them out, `GET`
  answers `404`, transitions answer `404`, and the agenda lists them with `task: null`, like events.
- `api-access`: `rate_limited` → 429 is added to the problem codes.
- Engine: one helper decides whether the heading at point is a task, used by `task.get`, transitions, the
  task queries and the agenda.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `task-lifecycle`: text round trip, task states.
- `api-access`: problem codes.

## Impact

- `emacs/organon-task.el` and ERT tests. No API or OpenAPI changes: responses now always match the
  existing `State` enum.
