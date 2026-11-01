# Agent guide

Read [CONTRIBUTING.md](CONTRIBUTING.md) before changing anything: it holds the project's main rule, the
OpenSpec flow, the checks and the code style. This file adds only what an agent would otherwise miss.

## Workflow

1. Write the OpenSpec change, then implement it.
2. Get a read-only review from a separate agent, and address what it finds.
3. Open the pull request and stop. The maintainer reviews there.
4. After the maintainer approves, archive the change as the last commit, wait for CI, and merge with
   `gh pr merge --merge`.

Check `gh pr view --json mergeable` and whether `main` has moved before pushing to an open pull request.

## Writing

- The maintainer does not read Go, so keep Go plain and comment it fully.
- Specs, code and public docs are in English. `docs/architecture.md` is in Korean.
- This repository is public. Keep real infrastructure out of everything published, including commits, issues
  and pull requests: use generic placeholders for hosts, domains, private repositories, accounts and networks.
  Grep the diff for them before every push.

## Running checks locally

- `go test -race` fails on arm64 kernels with a 39-bit address space. Run `go test` there; CI covers the race
  detector.
- Without a Docker daemon, run the Compose e2e through Podman:
  `DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock`.
- `ORGANON_TEST_HOST=1 scripts/test-elisp.sh [selector]` runs a subset of ERT.
- `mise run check:generated` compares the working tree with the index, so stage your changes first.

## Agent skills

### Issue tracker

GitHub Issues hold the backlog; OpenSpec holds implementation plans. See `docs/agents/issue-tracker.md`.

### Domain docs

Behavior lives in `openspec/specs/`; design, vocabulary and decisions live in `docs/architecture.md`. Read the
parts that touch your area before exploring the code. Use their terms, and call out any conflict with them
explicitly. This repository has no `GLOSSARY.md` or `docs/adr/`.
