# Tasks

## 1. Publish workflow

- [x] 1.1 Add the `publish` matrix job (D1, D2: native amd64/arm64, push by digest, digest artifact, `packages: write` only there, gated on push to main and successful unit/e2e) and verify with `actionlint` (or a YAML parse) that the workflow is valid and that the job's `if:` excludes pull requests
- [x] 1.2 Add the `publish-manifest` job (D2, D3: metadata-action tags `main` and `sha-<7>`, labels, `VERSION=main-<7>` build argument) and verify the same way

## 2. Documentation

- [x] 2.1 Switch `compose.yaml`, `contrib/quadlet/*` and `README.md` to `:main` with the development-build note and the `sha-` pinning advice, and verify `docker compose config` and the Quadlet dry run still pass
- [x] 2.2 Document the one-time "make the GHCR package public" step in CONTRIBUTING and verify the text matches the GitHub UI

## 3. Integration

- [x] 3.1 Push the branch and verify deployment: Pull request (the PR's CI run skips both publish jobs and pushes nothing)
- [x] 3.2 Archive the change as the last commit of the pull request and verify `mise run spec:check-archived` passes

After merging (outside this task list, because a change is archived before it merges): verify deployment
Commit on main. The manifest has amd64 and arm64 entries, `sha-<7>` and `main` point to it, and the revision
label and `organon version` match the commit.
