# Tasks

## 1. Engine

- [ ] 1.1 Load org-roam and configure the index (D3: directory, database in the cache directory, no external listing commands, encrypted files excluded, `organon-configure-hook`, save hook), and verify ERT for knowledge-index: Index stays out of the data directory, Task that links to a note
- [ ] 1.2 Sync the index at startup with the delete-and-retry fallback, and verify ERT for knowledge-index: Rebuild after the index is deleted, Corrupt index
- [ ] 1.3 Implement `organon-index-ensure-current` (D3) and verify ERT for knowledge-index: Note written by another process, Link removed by another process, File deleted while the engine was stopped
- [ ] 1.4 Implement `node.create` (D1) and verify ERT golden tests for knowledge-nodes: Node is persisted as an Org file, Tags and aliases, Same title twice, Title with a newline, Body that looks like a heading, Link in a body, and knowledge-index: New node is searchable
- [ ] 1.5 Implement `node.get` (D4) and verify ERT for knowledge-nodes: Read a created node, Hand-written heading node, Unknown node, Task ID under /nodes
- [ ] 1.6 Implement `nodes.search` (D2, D4) and verify ERT for knowledge-nodes: Match on an alias ignoring case, Filter by tag, Archived notes are not searched, Heading with another keyword
- [ ] 1.7 Implement `node.backlinks` and `node.links` (D4) and verify ERT for knowledge-nodes: Backlinks from a note and a task, Backlink from an archived file, New link appears at once, Links of a note, Dangling link

## 2. API

- [ ] 2.1 Extend `api/openapi.yaml` (CreateNode, Node, NodeSummary, NodeList, NodeRef, NodeRefList, the five operations, `nodes:write` in the security description), regenerate types, and verify `mise run lint:openapi` and `mise run check:generated` pass
- [ ] 2.2 Add the `nodes:write` scope to `internal/auth` and `organon token new`, and verify unit tests for api-access: Read-only token tries to create a node, Note-taking token cannot change tasks
- [ ] 2.3 Implement the node handlers with query and path validation and idempotent creation (D5), and verify unit tests for knowledge-nodes: Invalid search, Retried creation (API side), valid filters reach the engine unchanged, and contract validation of every new response

## 3. CLI

- [ ] 3.1 Add node methods to `internal/client` and node prefix resolution, and verify cli: Node prefix
- [ ] 3.2 Implement `node add|show|search|backlinks|links` (D6) and verify cli: Add a note (exact request body), Backlinks of a note, and `--json` output
- [ ] 3.3 Document nodes, the `nodes:write` scope and the rebuild behavior in README and `docs/architecture.md` (§5, §8.2, §10.5, §14), and verify every documented command runs as written against a local stack

## 4. End-to-end

- [ ] 4.1 Add the `drop-index` and `corrupt-index` ctl verbs to `scripts/e2e.sh`, and e2e tests for S5 (create, read, search), S6 (backlinks from a node and a task), S7 (drop the index and compare; corrupt index), external edits picked up, and node scopes; extend the recreate test with nodes; verify they pass under podman
- [ ] 4.2 Add a CLI e2e smoke test for nodes (`node add` → `node search` → `node add` with a link → `node backlinks`) and verify it passes under podman
