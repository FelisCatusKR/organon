# Proposal

## Why

The engine rebuilds a missing org-roam database inside the daemon before its socket listens (first start, or
a deleted cache). On a Raspberry Pi 4 this takes about 25 s per 1,000 notes. Beyond roughly 5,000 notes the
engine misses the 120 s health start period, Compose marks it unhealthy and the API never starts, although
tasks do not need the note index at all (issue #27). Liveness and readiness are also one endpoint today
(`/healthz`), so a deployment cannot tell "the process is alive" from "it can serve requests".

## What Changes

- **The rebuild runs outside the daemon.** When the database is missing or unreadable at startup, the engine
  starts a separate batch Emacs process. That process builds the database in a temporary file in the cache
  directory and renames it into place. The engine listens at once, and a database that is still usable keeps
  being synced incrementally in the daemon, as today.
- **Tasks keep working during a rebuild.** Node endpoints answer `503` with `Retry-After` and the problem code
  `index_rebuilding` until the new database is in place. The engine then catches up with files written in the
  meantime.
- **Index state in `GET /api/v1/meta`**: `ready`, `rebuilding` or `failed`, with progress in files.
- **Probes in the Kubernetes style.** All three answer without a token and carry no data beyond a status.
  - `GET /livez`: the API process is up. It never contacts the engine.
  - `GET /readyz`: the engine answers. `200` with status `pass`, or `warn` while the index is rebuilding or has
    failed; `503` with `fail` when the engine is unreachable.
  - `GET /healthz`: stays, as an alias of `/readyz`.

Out of scope: speeding up the rebuild itself (ahead-of-time compilation of org-roam, profiling), and rebuilding
on demand through the API.

## Capabilities

### New Capabilities

- `service-health`: the liveness and readiness endpoints and what they promise.

### Modified Capabilities

- `knowledge-index`: a missing or unreadable index is rebuilt in the background while the engine serves tasks;
  node requests during a rebuild are refused with `503`; the index state is reported.
- `api-access`: the endpoints that need no token are now `/livez`, `/readyz` and `/healthz`.

## Impact

- Engine (`emacs/organon-node.el`): a batch entry point for the rebuild; a child process with a progress filter
  and an exit sentinel; index state in `ping` and `meta`; node methods refuse while rebuilding; the save hook
  skips indexing while rebuilding.
- API: `/livez`, `/readyz`, `/healthz` as an alias, `Retry-After` on `index_rebuilding`, `index` in `Meta`, a new
  problem code. `api/openapi.yaml` and the generated types follow.
- Deployment: no change to commands, mounts or health check commands (`contrib/container-interface.json`). The
  engine now becomes healthy within seconds even on a first start. A rebuild costs a second Emacs process
  (about 60 MB) while it runs.
- Docs: `docs/deployment.md` (probes, rebuild), `docs/usage.md` (`index_rebuilding`), architecture §10.5.
