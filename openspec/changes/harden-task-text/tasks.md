# Tasks

## 1. Text input

- [x] 1.1 Reject priority cookies anywhere in a title and verify ERT for task-lifecycle: Title that Org would parse as structure (with `pay [#C] rent`)
- [x] 1.2 Escape `CLOCK:` lines in bodies and verify ERT for task-lifecycle: Body that looks like metadata
- [x] 1.3 Reject the `ARCHIVE` tag and verify ERT for task-lifecycle: Archive tag

## 2. Completed tasks

- [x] 2.1 Count a log entry only if it is a `CLOSED:` planning line or a state change inside `LOGBOOK`, and verify ERT for agenda-queries: Text that looks like a completion (fails before the change)

## 3. Integration

- [x] 3.1 Run `openspec validate --all --strict` and ERT, then archive the change as the last commit of the pull request and verify `mise run spec:check-archived` passes
