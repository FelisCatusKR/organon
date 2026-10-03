# Tasks

## 1. Engine

- [x] 1.1 Add the batch entry point `organon-index-build` and the progress lines (D1, D2), and verify an ERT test that runs it on the knowledge fixture and gets a database equal to an in-daemon sync
- [x] 1.2 Run the rebuild as a child process from `organon-index-startup`, with the filter, the sentinel and the swap (D1–D3), and verify ERT for knowledge-index: Rebuild after the index is deleted, Corrupt index, Corrupt index without any file, Ready after the rebuild
- [x] 1.3 Refuse node methods and skip the save hook while rebuilding, report the state in `ping` and `meta` (D4), and verify ERT for knowledge-index: Node request during a rebuild, Task written during a rebuild, Progress, and a failed child leaving node methods `internal`

## 2. API

- [x] 2.1 Extend `api/openapi.yaml` (`/livez`, `/readyz`, `/healthz` body, `Meta.index`, problem code `index_rebuilding`), regenerate types, and verify `mise run lint:openapi` and `mise run check:generated`
- [x] 2.2 Implement `/livez`, `/readyz` and `/healthz` as its alias (D5), and verify unit tests for service-health: Engine paused (liveness), Ready, Index rebuilding, Engine unavailable, Same answer; and api-access: Health without token
- [x] 2.3 Map `index_rebuilding` to `503` with `Retry-After` and pass `meta.index` through, and verify unit tests including contract validation

## 3. End-to-end and docs

- [ ] 3.1 Add the `seed-notes` ctl verb and e2e tests for knowledge-index: Large collection without an index, Node request during a rebuild, Progress, Rebuild after the index is deleted (after waiting for `ready`); and service-health: liveness while the engine is paused; verify under podman
- [ ] 3.2 Document the probes, the background rebuild and `index_rebuilding` in `docs/deployment.md`, `docs/usage.md` and architecture §10.5, and verify the documented requests against a local stack
