# Tasks

## 1. Text round trip

- [ ] 1.1 Add an ERT test for task-lifecycle: Surrounding whitespace is not kept, and verify it passes against the current engine (the behavior already exists; only the spec changes)

## 2. Task states

- [ ] 2.1 Add an engine helper that returns the heading's state only if it is one of the six task states, use it in `task.get`, `task.transition`, `tasks.today` / `tasks.overdue` and `agenda.day`, and verify with ERT for task-lifecycle: Keyword from a file's own TODO line (fails before the change: `state` is `WIP`)

## 3. Problem codes

- [ ] 3.1 Verify that the `rate_limited` code and status in `api/internal/httpapi` match api-access: Throttled request by asserting the problem `code` in `TestFailedAuthenticationIsRateLimited`, which names the scenario

## 4. Integration

- [ ] 4.1 Run `openspec validate --all --strict`, the Go tests and ERT, then archive the change as the last commit of the pull request and verify `mise run spec:check-archived` passes
