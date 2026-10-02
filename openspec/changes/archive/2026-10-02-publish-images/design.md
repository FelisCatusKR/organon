# Design

## Context

CI (`.github/workflows/ci.yml`) already builds the image on both architectures for the e2e jobs, and decides
in its `changes` job whether code changed. The image is built from `container/Containerfile`, whose default
target is `runtime`. `organon version` prints the `VERSION` build argument. GHCR is owned by the repository
owner, so the image path is `ghcr.io/feliscatuskr/organon`, lowercase.

## Goals / Non-Goals

**Goals:**
- Publish exactly what CI tested, never anything untested.
- Native builds on both architectures. Emacs native compilation under QEMU takes tens of minutes.

**Non-Goals:**
- Release tags, `latest`, signing, SBOM or provenance attestations, and package visibility. Package
  visibility is a manual UI step.

## Decisions

### D1. A job in the existing workflow, gated by `needs`
`publish` is a matrix job (`amd64` on `ubuntu-24.04`, `arm64` on `ubuntu-24.04-arm`). It declares
`needs: [contracts, unit, e2e]` and `if: github.event_name == 'push' && github.ref == 'refs/heads/main' &&
needs.unit.result == 'success' && needs.e2e.result == 'success'`. A second job, `publish-manifest`, merges the
two digests.

Why not a separate workflow triggered by `workflow_run`: it adds a second place to reason about which commit
is being built, and `workflow_run` runs with the default branch's workflow file. Keeping it in `ci.yml` makes the
gate visible next to the tests.

Docs-only commits skip unit and e2e (see the `changes` job), so `publish` is skipped too. The image content
would be identical anyway, because the Containerfile copies only `emacs/` and `api/`.

### D2. Push by digest, then merge
Each architecture job uses `docker/build-push-action` with `push-by-digest=true` and uploads the digest as an
artifact. `publish-manifest` uses `docker buildx imagetools create` with both digests and the tags from
`docker/metadata-action`. This is Docker's documented multi-platform pattern for native runners, and it
avoids QEMU. Only these two jobs get `packages: write`; the workflow default stays `contents: read`.

### D3. Tags and labels
`docker/metadata-action`:
- tags: `type=sha,prefix=sha-,format=short` and `type=raw,value=main`
- labels: the action's defaults (source, revision, created, licenses from the repository)

The `VERSION` build argument is `main-<7>`, so a running container can say what it is.

### D4. Documentation
Use `:main` and call it a development build in:
- `compose.yaml` (default image)
- `contrib/quadlet/*.container`
- `README.md`

The README suggests pinning `sha-<7>` for stability. CONTRIBUTING gets the one-time maintainer step: make the
GHCR package public and link it to the repository.

## Risks / Trade-offs

- [The first push creates the package as private.] → Documented manual step. Until then, anonymous pulls fail
  and the quick start does not work.
- [`main` moves under deployments.] → Pin `sha-` tags. Release tags will add stable `latest` and semver later.
- [Third-party actions (docker/*) run with `packages: write`.] → Only in the publish jobs, pinned to major
  versions from the official `docker` organization. Pull requests never reach these jobs.
