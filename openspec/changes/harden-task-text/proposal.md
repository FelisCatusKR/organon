# Proposal

## Why

A second review tried every way client text could reach Org. It found no path to code execution, but four
ways client input changes what Org reports:

- Body text such as `- State "DONE" from "NEXT" [2026-10-02 Fri 09:00]`, or `CLOSED: [2026-09-01 Tue]` in a
  title or body, makes a task show up as completed on that date. Org's log mode searches the whole file for
  these patterns, and the engine only checked the line prefix and the task's current state.
- A priority cookie in the middle of a title (`pay [#C] rent`) is where `org-priority` puts the requested
  priority, so the response says `priority: null` and Org sorts the task by the cookie in the title.
- A body starting with a `CLOCK:` line is read as metadata: `GET` drops it from the body, and Org's clock
  reports count it.
- The tag `ARCHIVE` hides a task from agenda queries while `GET` and `tasks/waiting` still return it.

## What Changes

- `task-lifecycle`, text requirement: priority cookies are rejected anywhere in a title; `CLOCK:` lines in
  bodies are escaped like other structural lines; the tag `ARCHIVE` is rejected.
- `agenda-queries`, completed tasks: only the `CLOSED:` planning line and state changes in the `LOGBOOK`
  drawer count; text that looks like them does not.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `task-lifecycle`: text input rules.
- `agenda-queries`: completed tasks on a date.

## Impact

- `emacs/organon-task.el` and ERT tests. No API changes: the new rejections are `422` like the existing ones.
