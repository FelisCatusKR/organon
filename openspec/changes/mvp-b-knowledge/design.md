# Design

## Context

MVP-A and task-essentials are archived (`openspec/changes/archive/`). The engine (`emacs/organon.el`) already
has the single write wrapper (`organon-with-file` / `organon-with-entry`), text escaping for task bodies
(`emacs/organon-task.el`), and an org-id index in the cache directory. The image already installs Debian's
`elpa-org-roam` 2.3.1, but nothing loads it yet. See proposal.md for why; specs/ for the requirements.

Settled by the 2026-10-02 spike (docs/architecture.md, Appendix A):
- Task headings stay org-roam nodes. Excluding them with an include-function drops the backlinks they carry,
  so tasks are filtered out at query time instead.
- A full `org-roam-db-sync` takes about 25 s per 1,000 notes and blocks the daemon. An incremental sync takes
  about 0.5 s; indexing one saved file about 0.03 s.
- Writes must go through `find-file` and `save-buffer`. A file written with `write-region` is not indexed
  until the next sync.

Checked on Emacs 30.1 / Org 9.7.11 / org-roam 2.3.1 before writing this design:
- `org-id-get-create` at `point-min` of a file without headings writes a file-level property drawer above
  `#+title:`. org-roam reads it as a level-0 node.
- `org-roam-property-add "ROAM_ALIASES"` and `org-roam-tag-add` (which writes `#+filetags:` at level 0) work
  without prompting when given their arguments. Aliases with quotes round-trip.
- `org-roam-node-slug` keeps Hangul and strips Latin diacritics (`"한글 Note: Été"` → `한글_note_ete`).
- Emacs 30's built-in SQLite is used by emacsql; no external `sqlite3` binary is needed.
- `org-roam-db-update-file` reads a file from disk for its hash but parses a buffer that already visits the
  file, so a stale buffer would be indexed with the new hash and never corrected.

## Goals / Non-Goals

**Goals:**
- Every node feature is org-roam's: file format, IDs, slugs, link extraction and backlinks. New code only
  validates input, writes the file through the existing wrapper, and serializes what org-roam stored.
- The index can be deleted at any time; the files are the only state.
- Node queries reflect the files on disk, also after changes made outside the engine.

**Non-Goals:**
- Editing, deleting, renaming or moving nodes; journal and capture; full-text search of bodies.
- Pagination of search results (personal scale, like task lists).
- Rebuilding the index on demand through the API. A full rebuild blocks the engine far longer than the RPC
  timeout; deleting the cache and restarting the engine does the same job (D3).

## Decisions

### D1. A node is a file under `org/knowledge/`, written like org-roam would
`node.create` validates everything first, then picks a file name, then writes the file through
`organon-with-file`:
- **File name:** `<YYYYMMDDHHMMSS>-<slug>.org`, the shape of org-roam's default capture template. The time is
  local time in `calendar_tz`. The slug comes from `org-roam-node-slug`, cut to 60 characters (so the name
  stays far below 255 bytes for Hangul), and is `node` if nothing is left. On a clash, `-2`, `-3`, … are added.
- **Content:** `#+title:` with the cleaned title, then the escaped body. Then `org-id-get-create` at
  `point-min` writes the ID drawer, `org-roam-property-add` adds each alias, and `org-roam-tag-add` writes
  `#+filetags:`.
- **Not used:** `org-roam-capture-`. Capture templates expand `%(...)`, and §8.5 forbids building templates
  from user input.

Text rules reuse the task helpers from `organon-task.el`:
- The body uses `organon-escape-body` (headings, keywords, drawers, clock and diary lines escaped; active
  timestamps made inactive). Links are not touched, so `[[id:…][…]]` in a body becomes a real link.
- Tags use `organon--param-tags`.
- Titles and aliases are not headings, so priority cookies, a leading `COMMENT` or a trailing `:tag:` mean
  nothing in them. They must be one line without control characters, at most 500 characters, with
  timestamps made inactive, and without Org links: org-roam stores a title through `org-link-display-format`
  (so `[[id:…][x]]` would come back as `x`) and counts the link as one of the node's links.
- Tags use Org's alphabet, `[[:alnum:]_@#%]`, in the engine, in Go and in `openapi.yaml` alike, so a Hangul
  tag can be set and also filtered on. (Go and the contract used to accept ASCII only, which made Hangul task
  tags unfilterable too.)

The node module requires `organon-task` instead of moving these helpers. Moving them would churn the task code
for no behavior change.

### D2. Nodes and tasks partition the index
org-roam stores `todo` for every heading node. A node, for this API, is any org-roam node that is not a task in
the sense of the existing `organon-task-state`: a TODO keyword among the six states and an ID. Every org-roam
node has an ID, so in SQL terms this is `todo IS NULL OR todo NOT IN (six states)`. This is the spike's
`todo IS NULL` filter, adjusted to the existing spec's rule that only the six states make tasks. A heading
with `IDEA` from a file's own `#+TODO:` would otherwise be neither a task nor a node.

Link endpoints return both kinds as `{id, title, kind}`, so a client knows whether to follow
`/api/v1/nodes/{id}` or `/api/v1/tasks/{id}`.

Search leaves out files under `org/archive/` (architecture §5). `GET /nodes/{id}`, backlinks and links still
include them: archived notes keep their IDs and the links they carry.

### D3. Index lifecycle
- **Location:** `org-roam-directory` is `org/`, and `org-roam-db-location` is `$ORGANON_CACHE_DIR/org-roam.db`.
  Both are set in `organon-configure` through a new `organon-configure-hook`. The hook first closes
  connections to a previous instance's database (tests configure many instances in one process).
- **Startup:** `organon-start` runs `org-roam-db-sync` after the org-id scan and before the socket listens.
  The health check therefore fails until the index is in sync. With an existing database the sync is
  incremental: org-roam compares file hashes. Without one it is a full rebuild. If the sync or a first query
  signals an error (an unreadable or foreign database file), the engine deletes the database and syncs once
  more. If that also
  fails, it logs and keeps serving tasks; node queries then fail with `internal`.
- **On save:** a global `after-save-hook` function calls `org-roam-db-update-file` for files under `org/`.
  We don't enable `org-roam-db-autosync-mode`, for two reasons. The mode also advises the primitives
  `rename-file` and `delete-file`, and the engine disables runtime trampolines. And an error inside its hook
  would propagate out of `save-buffer`, so a save that succeeded would look failed to the write wrapper. Our
  hook catches the error and logs it. The file then stays unrecorded (next point), so the next node query
  indexes it again.
- **Before every node query** (`organon-index-ensure-current`):
  - The engine keeps a table of file → (size, modification time) as of the last indexing.
  - It compares that table with `org-roam-list-files` and `file-attributes`. A stat per file is cheap, unlike
    hashing every file.
  - On any difference it reverts clean buffers whose files changed on disk (`organon-fresh-buffer`; see the
    stale-buffer note in Context) and runs `org-roam-db-sync`, which only re-parses files whose hash changed.
  - It then rebuilds the table from the stats taken before the sync. A file that changes during the sync is
    caught by the next query.
  - The save hook updates the table entry of each file it indexed, so the engine's own writes don't cause a sync.
- `org-roam-list-files-commands` is `nil` (pure Elisp listing, no `find`/`rg` subprocess). Encrypted
  `.org.gpg`/`.org.age` files are excluded: decrypting would prompt.

### D4. Queries go through org-roam's API, filtered in Lisp
- `node.create` answers from the index after the save. If org-roam could not index the new file, that is
  `internal` (the file is saved), never `not_found`.
- `node.get`: `org-roam-node-from-id`, then `org-roam-node-file` / `org-roam-node-point` to read the body
  from a fresh buffer. A heading node uses the task body reader, which stops at the next heading. A file node
  takes the text after its property drawer (which only comment and blank lines may precede) and the file's
  keywords and blank lines, up to the first heading. Affiliated keywords such as `#+caption:` belong to the
  element below them and stay in the body (`org-element-affiliated-keywords`, `#+attr_*`). The body is
  unescaped like a task body.
- `node.backlinks`: `org-roam-backlinks-get` with `:unique t`.
- `node.links`: a query on org-roam's `links` table (`source = id`, `type = "id"`), joined with `nodes` so
  that dangling targets drop out.
- `nodes.search`: one query for all nodes with their aliases and tags. The filter runs in Lisp: substring on
  `downcase`d title and aliases, tag membership, archive and task exclusion. Request values never become SQL
  or Org match syntax. Sorting is by downcased title, then ID, so the order is deterministic.
- Tags and aliases are returned in the order they appear in the file.

### D5. HTTP surface
- `POST /api/v1/nodes` (`nodes:write`):
  - body `CreateNode {title, body?, tags?, aliases?}`
  - `201` with `Location: /api/v1/nodes/{id}`
  - `Idempotency-Key` like tasks and projects: `beginIdempotent` in the API, `organon-idempotent` in the
    engine (so a retry after an engine timeout creates the node once)
- `GET /api/v1/nodes?q=&tag=` (`read`): `q` at most 200 characters without control characters; `tag` with the
  task tag pattern. Both are validated in Go before the engine is called. Returns `NodeList` of `NodeSummary`.
- `GET /api/v1/nodes/{id}` (`read`): `Node`. `{id}` must be a lowercase UUID.
- `GET /api/v1/nodes/{id}/backlinks`, `GET /api/v1/nodes/{id}/links` (`read`): `NodeRefList` of
  `NodeRef {id, title, kind}`.
- New scope `nodes:write` in `internal/auth` and `organon token new`. No scope implies another.
- RPC methods: `node.create`, `node.get`, `nodes.search`, `node.backlinks`, `node.links`. As in the
  architecture §6.1 table, `not_found` maps to 404 and `invalid` to 422.

### D6. CLI
- `node add TITLE [--body B] [--tag T]... [--alias A]...` creates a node.
- `node show ID` prints title, ID, aliases and tags, then the body.
- `node search [QUERY] [--tag T]` prints one line per node: short ID, title, `:tags:`.
- `node backlinks ID`, `node links ID` print one line per entry: kind, short ID, title.
- Node ID prefixes are resolved against `GET /api/v1/nodes`, which leaves out archived nodes. Those need the
  full ID.

### D7. Tests
- **ERT**, with a new fixture `tests/fixtures/knowledge/`: notes with links, a task linking to a note, an
  archived file with a link, a heading node with a child, an `IDEA` heading, and a dangling link.
  - Golden files for `node.create`: plain, with tags and aliases, an escaped body, a Hangul title.
  - get, search, backlinks and links per scenario.
  - Rebuild: run the queries, delete the database, sync again, compare.
  - Corrupt database at startup.
  - External add, change and delete via `write-region` / `delete-file` behind the engine's back.
- **Go unit tests:** handlers and validation, scopes (`nodes:write` vs `tasks:write`), contract validation of
  the new responses, CLI against `httptest`.
- **e2e:**
  - S5: create, get, search. S6: backlinks from a node and from a task.
  - S7: delete only the index (new `ctl` verb `drop-index`: stop the engine, remove `org-roam.db` from the cache
    volume, start the engine) and compare.
  - External edit via `host-append`; corrupt index.
  - The existing recreate test also compares the node list and backlinks.

## Risks / Trade-offs

- [A full rebuild of a large collection exceeds the health start period: about 5,000 notes on a Pi 4.] → It
  happens only without a usable database (first start, deleted cache). The engine keeps syncing and becomes
  healthy afterwards; Compose and Quadlet keep waiting with `depends_on`/`Requires=`. The README states the
  figure. A batch rebuild outside the daemon can be added later if this becomes real.
- [Each node query stats every file.] → About 1,300 stats in the spike's collection: milliseconds. Hashing
  happens only after a stat changed.
- [Node bodies cannot hold real Org structure (headings, drawers), because bodies are escaped like task
  bodies.] → It's the same safety rule as for tasks, and plain paragraphs with links are what clients send.
  Hand-written notes keep whatever structure they have. Structured notes can come with node editing later.
- [The search ordering and case folding use Emacs `downcase`, not locale collation.] → Deterministic and good
  enough for Hangul and Latin titles.
- [org-roam warns through `display-warning` when one file fails to parse during a sync.] → It skips that file
  and indexes the rest. The warning goes to the engine log.
