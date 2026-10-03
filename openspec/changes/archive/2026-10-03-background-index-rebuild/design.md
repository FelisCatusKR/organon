# Design

## Context

`emacs/organon-node.el` (from mvp-b-knowledge) keeps the org-roam database in `$ORGANON_CACHE_DIR/org-roam.db`:
- It syncs the database at startup, before the socket listens, and rebuilds it when missing or unreadable.
- It indexes every file the engine saves.
- It re-syncs files changed by other processes before each node query (stat comparison).

The engine is one single-threaded Emacs daemon, so a full rebuild inside it blocks every request: about 25 s
per 1,000 notes on a Pi 4. Issue #27 has the problem statement and the rejected alternatives: lazy indexing in a
request, and a custom index.

## Goals / Non-Goals

**Goals:**
- A missing or unreadable database never delays task requests or the health check.
- Clients can tell "rebuilding, retry later" from a failure, and see progress.
- Probes follow the Kubernetes convention (`/livez`, `/readyz`). Kubelet probes look only at the status code;
  the body is informational and shaped like `application/health+json` (draft-inadarei-api-health-check:
  `status` `pass` / `warn` / `fail`).

**Non-Goals:**
- Making the rebuild faster (ahead-of-time compilation, profiling).
- Rebuilding on demand through the API, or running incremental syncs outside the daemon. An incremental sync is
  fast (about 0.5 s per 1,000 notes).
- Per-check probe endpoints (`/readyz/index`) or `?verbose`.

## Decisions

### D1. The daemon runs the rebuild as a child process
`organon-index-startup` decides:
- No database file: rebuild in the background.
- A database file: sync incrementally in the daemon, as today. If that signals an error, delete the database and
  rebuild in the background.

The background rebuild uses `make-process`. The child runs the same Emacs binary (`invocation-directory` /
`invocation-name`), `-q --batch`, the same `init.el` (path recorded by `init.el` as `organon-init-file`), and
the function `organon-index-build`, with three arguments:
- the data directory
- the cache directory
- the output file, a new name `org-roam.db.rebuild-XXXXXX` in the cache directory per rebuild, so that a
  stray child of an earlier engine can never share it; leftovers are deleted before each rebuild

The child configures the instance like the daemon (with the cache directory as its run directory: it never
opens the socket, and so it writes nothing outside the mounts), then points `org-roam-db-location` at the output file and runs
`org-roam-db-sync`. It must not write the daemon's caches. Org-id global tracking is turned off in the child, so
its exit hook never rewrites `org-id-locations`.

The rebuild needs no network and no files outside the data and cache directories, so it runs under the same
container constraints. While it runs it costs a second Emacs process (about 60 MB).

### D2. Progress is a fixed line format on the child's stderr
Before each file it indexes, the child prints `organon-index-progress DONE TOTAL`, from a `:before` advice on
`org-roam-db-update-file` installed only in the child. It writes to stderr: stdout into a pipe is block-buffered,
so the lines would only arrive when the child exits. `make-process` merges both streams into one pipe.

The daemon's process filter keeps a partial line buffer and accepts only lines that match
`^organon-index-progress \([0-9]+\) \([0-9]+\)$`. Every other line is written to the engine log; nothing from
the child is ever read as Lisp.

### D3. Swapping in the new database
The process sentinel handles the child's exit:
- **Exit 0 with the output file present:**
  1. Close the daemon's org-roam connections.
  2. `rename-file` the output over `org-roam.db` (same directory, so atomic).
  3. Forget the recorded stats.
  4. Set the state to `ready`.
  5. Run `organon-index-ensure-current`. That indexes every file the engine wrote during the rebuild; their stats
     were never recorded (D4).
  Only the rename decides success. If the catch-up sync fails, that is logged and the state stays `ready`:
  every node query runs it again.
- **Anything else:** set the state to `failed`, log the exit status, delete the output file.
- **The child cannot be started** (`make-process` fails, the cache is not writable): `failed`, logged, and the
  engine still starts and serves tasks.
  - Node methods then fail with `internal` until the engine restarts. The next start finds no database and tries
    again.

The sentinel runs in the main loop between requests. It binds `organon--in-request` so that prompts fail
instead of hanging.

### D4. Behavior while rebuilding
`organon--index-state` is `ready` (the default, also in tests that never start the engine), `rebuilding` or
`failed`.
- **Node methods:** every one checks the state first.
  - `rebuilding` signals the new engine code `index_rebuilding`; `failed` signals `internal`.
  - `node.create` checks before writing, so nothing is created.
- **Save hook:** skips indexing unless the state is `ready`, and records no stats. The database file it would
  write does not exist yet.
- **Tasks, projects and agenda:** unaffected. They use the org-id index and Org itself.

### D5. HTTP surface
- **RPC code `index_rebuilding`**: maps to `503` with problem code `index_rebuilding` and `Retry-After: 10`.
  Ten seconds is a polling hint, not an estimate; clients that want more read `/meta`.
- **`ping`**: returns `{status: "ok", index: <state>}`.
  - `GET /readyz` and `GET /healthz` use it, behind the existing one-second cache.
  - Engine reachable: `200` with `{"status":"pass"}`, or `{"status":"warn","output":"note index rebuilding"}` /
    `"note index failed"`. Engine unreachable: `503` with `{"status":"fail","output":"engine unavailable"}`.
  - Content type `application/health+json`.
- **`GET /livez`**: `200` with `{"status":"pass"}`. No engine call, no cache.
- **`Meta.index`**: `{state, files_done, files_total}`. The counts are `null` unless the state is `rebuilding`.
- **Health check commands:** `organon healthcheck` keeps calling `/healthz`, and `organon rpc-ping` keeps calling
  `ping`, so `contrib/container-interface.json` does not change. The engine reports healthy as soon as it
  listens.

### D6. Tests
- **ERT:**
  - background rebuild of the knowledge fixture: state goes `rebuilding` → `ready`, results equal the snapshot
  - node methods during the rebuild: `index_rebuilding`, no file created
  - task created during the rebuild, then backlinks after it
  - a child that fails: state `failed`, node methods `internal`
  - corrupt database and empty `org/` take the background path
  - A helper waits for the child with `accept-process-output`.
- **Go unit:** probes (pass / warn / fail, `/livez` without engine), `index_rebuilding` → 503 + `Retry-After`,
  `meta.index` contract.
- **e2e:**
  - New ctl verb `seed-notes N` writes N small linked notes into the data directory.
  - New ctl verb `drop-index` (existing) restarts without a database.
  - The test then checks:
    - right after restart, `/readyz` answers `200` (`warn`) and task requests work
    - node requests answer `503` with `Retry-After`
    - `/meta` shows progress
    - after the rebuild, the snapshot equals the one from before
  - `/livez` answers while the engine is paused.

## Risks / Trade-offs

- [The engine dies during a rebuild.] → The child dies with the container. The output file is a temporary name and
  is deleted at the next start, which rebuilds again.
- [A rebuild competes for CPU with requests.] → Only on first start or after cache loss. A Pi 4 has four cores and
  the daemon is single-threaded.
- [Writes during a rebuild are indexed only after the swap.] → They become visible to node queries when the
  rebuild finishes, which is also when node queries become possible.
- [`Retry-After` is a fixed 10 s.] → Good enough for polling. An estimate from the progress rate can come later.
