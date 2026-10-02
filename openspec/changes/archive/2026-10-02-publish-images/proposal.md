# Proposal

## Why

`compose.yaml`, the Quadlet example and the README point to `ghcr.io/feliscatuskr/organon:latest`, but no
image has ever been published, so the documented quick start does not work without building locally. Before
1.0 there are no releases, so the natural thing to publish is every commit on `main` that passed CI: the same
artifact CI has just tested, for both architectures. The maintainer's own deployment needs a pullable image
too.

## What Changes

- A `publish` job in the CI workflow. It runs only for pushes to `main`, and only after the unit and e2e jobs
  succeeded. It builds the image natively on amd64 and arm64 runners, pushes each by digest, and merges them
  into one multi-arch manifest on GHCR.
- Tags: `main` (moves with the branch) and `sha-<7 hex>` (fixed). No `latest` and no semver until release tags
  exist.
- OCI labels: source repository, revision, MIT license, and the version baked into the binary (`organon
  version` reports `main-<sha>`).
- `compose.yaml`, the Quadlet example and the README use `:main` and say that it is a development build, and
  suggest pinning a `sha-` tag.

Out of scope: release tags and `latest`, image signing and SBOM attestations, making the GHCR package public
(a one-time manual step in the GitHub UI, documented in CONTRIBUTING).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `deployment`: adds a requirement for published images (when they are published, tags, architectures,
  labels).

## Impact

- `.github/workflows/ci.yml`: new `publish` job (needs `packages: write` for that job only).
- `compose.yaml`, `contrib/quadlet/*`, `README.md`, `CONTRIBUTING.md`: image references and notes.
- No code changes.
