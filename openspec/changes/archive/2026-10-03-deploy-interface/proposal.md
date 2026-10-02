# Proposal

## Why

Issue #23. The `deployment` spec says what a deployment guarantees, but not the interface a deployment
definition is written against. `compose.yaml`, `contrib/quadlet/` and the Podman e2e in `scripts/e2e.sh` each
spell it out separately, and only Compose is exercised against the image. Custom unit files that track
`:main` break silently when a command, health check, mount or environment variable changes. Nothing marks
such a change.

## What Changes

- A machine-readable declaration, `contrib/container-interface.json`. It covers the engine and API commands,
  health checks, mount targets, tmpfs, the engine's missing network, and the API's environment variables.
- `scripts/check-deploy-interface.sh` (`mise run check:deploy-interface`, run in CI). It checks that
  `compose.yaml` and `contrib/quadlet/` use exactly that interface, which also automates the existing "only
  the engine mounts the data" inspection. The Podman e2e takes its commands and health check from the
  declaration, so the image is tested against it.
- CI fails when a pull request changes the declaration without `!` in its title. A `breaking` label is
  added. `git log --first-parent --grep '!:'` lists the changes.
- `docs/deployment.md` gets a "Custom deployments" section. It covers the interface, users, setup commands,
  the tokens file, why auto-init is unsafe, and pinning `sha-` tags.
- `deployment` spec: a new requirement for the interface. *Data directory initialization* is corrected
  (`--calendar-tz` is required) and gains a scenario: the engine never initializes a directory itself.

Not adopted from the issue: blessing "init if `organon.json` is missing, then start" as a supported
pattern. Refusing to serve without `organon.json` is what catches a wrong or empty data mount.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `deployment`: Container interface for custom deployments (new); Data directory initialization.

## Impact

- New `contrib/container-interface.json` and `scripts/check-deploy-interface.sh`; `scripts/e2e.sh`,
  `.github/workflows/ci.yml` (`pull_request` also on `edited`, so that a corrected title re-runs the check),
  `mise.toml`, `docs/deployment.md`, `CONTRIBUTING.md`, one ERT test. No change to the image or the API.
