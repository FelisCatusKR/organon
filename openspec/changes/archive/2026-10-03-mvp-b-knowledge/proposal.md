# Proposal

## Why

Organon so far manages tasks only. The second half of the goal is a knowledge manager on the same plain-text
data: notes that link to each other, with backlinks, kept by org-roam (docs/architecture.md §15, MVP-B, issue
#19). Clients such as Hermes need to write a note, get a stable ID back, find notes again, and see what links to
them. The index that makes this fast must stay a cache that can be rebuilt from the `.org` files alone.

## What Changes

- **Create a knowledge node** (`POST /api/v1/nodes`): title, optional body, tags and aliases. The engine writes a
  new file-level org-roam node under `org/knowledge/` with a generated UUID `ID`, and returns it. The same text
  rules as for tasks apply (no Org structure from input, inactive timestamps). `Idempotency-Key` is honored.
- **Read a node** (`GET /api/v1/nodes/{id}`): title, aliases, tags, body, location hint.
- **Search nodes** (`GET /api/v1/nodes?q=&tag=`): case-insensitive substring match on title and aliases, and an
  exact tag filter. Tasks and archived notes are left out. (The architecture draft named this
  `/nodes/search?q=`; a filtered collection matches `GET /api/v1/tasks?state=` and is used instead.)
- **Backlinks and forward links** (`GET /api/v1/nodes/{id}/backlinks`, `GET /api/v1/nodes/{id}/links`): the
  nodes and tasks that link to a node by ID, and the nodes and tasks it links to. Backlinks from tasks and from
  archived files are kept.
- **Index is a rebuildable cache**: the org-roam database lives in the cache volume. The engine brings it up to
  date with the files at startup (a full rebuild when it is missing or unreadable), indexes every file the engine
  saves, and re-reads files changed by another process before answering a node query.
- **New scope `nodes:write`** for creating nodes, so a token can write notes without being able to change
  tasks.
- **CLI**: `organon node add|show|search|backlinks|links`, with short ID prefixes like tasks.

Out of scope: editing or deleting nodes, journal (`/journal/{date}`), capture, full-text search of note bodies,
node promotion (`source`), org-roam refs and citations.

## Capabilities

### New Capabilities

- `knowledge-nodes`: creating, reading and searching knowledge nodes, and following links and backlinks
  between nodes and tasks.
- `knowledge-index`: the org-roam index as a cache: rebuilt from the files, kept in step with writes and with
  changes made outside the engine.

### Modified Capabilities

- `api-access`: the Scopes requirement adds `nodes:write`, required for creating nodes.
- `cli`: the Commands requirement adds the `node` commands; Short IDs also apply to node IDs.

## Impact

- Engine: loads org-roam (already in the image, Debian `elpa-org-roam`). New `emacs/organon-node.el` with RPC
  methods `node.create`, `node.get`, `nodes.search`, `node.backlinks`, `node.links`; index sync at startup and
  before node queries. The org-roam database goes to `$ORGANON_CACHE_DIR/org-roam.db`.
- API: new endpoints and schemas in `api/openapi.yaml` (generated types follow), new handlers, new scope in
  `internal/auth` and `organon token new`.
- CLI: `node` commands and client methods.
- Startup: the engine reports healthy only after the index is in sync. A full rebuild takes about 25 s per 1,000
  notes on a Raspberry Pi 4 (Appendix A), within the 120 s health start period of `compose.yaml`.
- Docs: README (scopes, node usage, status), `docs/architecture.md` §5, §8.2, §10.5, §14.
