# Contributing to Organon

Thanks for helping. Organon has one rule above all: **don't reimplement what Org already does.** Task
states, repeaters, agenda membership, deadline warnings, IDs and backlinks belong to Org and org-roam. New
code is an adapter that exposes them safely. If you find yourself writing a parser or computing a date,
stop and look for the Org function that already does it.

Security problems are reported privately, not as issues: see [SECURITY.md](SECURITY.md).

## What you need

- [mise](https://mise.jdx.dev/) (installs the pinned Go, Node, OpenSpec CLI and Redocly from `mise.toml`)
- Docker **or** Podman (Emacs runs inside the test image; you don't need it installed)

```sh
mise install
```

## How changes are made

Behavior is specified with [OpenSpec](https://github.com/Fission-AI/OpenSpec) in `openspec/`:

- `openspec/specs/<capability>/spec.md`: how the system behaves today
- `openspec/changes/<change>/`: a proposed change (proposal, design, spec deltas, tasks)

A change that alters behavior comes with a spec delta. Review happens on the unarchived change, so that spec
deltas can still be revised. Once the pull request is approved, **archive the change as its last commit**
(`openspec archive <name>` or `/opsx:archive`), so that what merges includes the updated
`openspec/specs/`. The `openspec: changes archived` check stays red while a finished change is left
unarchived, and blocks the merge. An archive-only commit does not re-run the tests: CI skips unit and e2e for
a code tree that already passed. Working alone, you can push the archive together with the last code commit.

Tasks list only work you can verify yourself (a test, a command, a file). Do not add "CI is green" or
"the PR run shows ...": CI is already required to merge, and a change is archived before it merges.
To change the HTTP API, edit `api/openapi.yaml` and run
`mise run generate`: the Go types in `api/internal/model/model.gen.go` are generated from it (never edit them
by hand; CI fails if they are out of date). **Every scenario in a spec has at least one test**
that names it, either in a docstring (Elisp) or a comment (Go). Design rationale lives in
`docs/architecture.md`; the HTTP contract in `api/openapi.yaml`.

## Checks

| Level | Who | What |
|---|---|---|
| **L1** | you, before opening a PR | the commands below |
| **L2** | CI, required to merge | specs + finished changes archived + OpenAPI lint + generated-type drift check + Quadlet dry run, unit tests (with the race detector) and ERT on amd64 and arm64, e2e under Docker Compose and rootless Podman (also as UID 12345) |
| **L3** | maintainer, before a release | real deployment with Quadlet + systemd |

You don't need Podman, systemd or an arm64 machine: if L1 passes and CI is green, you're done.

### L1

```sh
mise run spec:validate   # OpenSpec specs and changes
mise run lint:openapi    # HTTP contract
mise run test            # Go unit tests + ERT inside the test image
```

Optional, if you have the time (about a minute):

```sh
podman build -f container/Containerfile -t organon:dev .   # or: docker build ...
mise run e2e -- --runtime podman                            # or: --runtime compose
```

`ORGANON_TEST_HOST=1 scripts/test-elisp.sh` runs ERT with your local Emacs (it must match the image:
Emacs 30.1, Org 9.7) for a faster loop. Golden files under `tests/golden/` are rewritten with
`ORGANON_TEST_HOST=1 ORGANON_UPDATE_GOLDEN=1 scripts/test-elisp.sh`. **Read the diff** before committing
them: they are the expected Org output.

## Code style

- **Go:** standard library only in the binary (test code may use libraries), `gofmt`, small packages, and a
  comment at the top of each file saying what it is for.
- **Elisp:** `lexical-binding: t`. Every write goes through `organon-with-entry` / `organon-with-file`.
  Never evaluate or `read` anything that came from a request.
- **Commits:** imperative mood, explain why.

## Releasing images (maintainers)

Every commit on `main` that passes CI is published to GHCR by the `publish` jobs in
`.github/workflows/ci.yml` (`:sha-<7>` and `:main`, amd64 and arm64). Pull requests never publish.

The package inherits the repository's visibility through the image's source label, so a public repository
gives a public package. If pulls ever fail anonymously, check the repository owner's profile → **Packages** →
`organon` → **Package settings**: visibility, and **Manage Actions access** (this repository needs write
access).

## License

By contributing you agree that your contributions are licensed under the [MIT License](LICENSE). Source files
don't need a license header; `LICENSE` covers the repository. The Elisp libraries (`emacs/organon*.el`)
keep the usual Emacs package header with an SPDX line.
