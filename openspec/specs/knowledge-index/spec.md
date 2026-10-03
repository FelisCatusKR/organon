# knowledge-index Specification

## Purpose
Keeps the org-roam index (nodes, links, backlinks) a disposable cache of the `.org` files: it can be deleted
at any time and is rebuilt from the files, and node queries always reflect the files on disk.

## Requirements

### Requirement: Index is a rebuildable cache
The node index SHALL be stored only in the cache directory, never in the data directory. When the index is
missing, the engine SHALL rebuild it from the `.org` files in the background while it serves task requests, and
node queries SHALL then return the same nodes, links and backlinks as before.

#### Scenario: Rebuild after the index is deleted
- **GIVEN** nodes and tasks that link to each other, and the results of listing all nodes and of the backlinks and links of each node
- **WHEN** the index is deleted, the engine is restarted, and the rebuild has finished
- **THEN** the same requests return the same results

#### Scenario: Recreate the whole stack
- **WHEN** all containers and named volumes are removed and the stack is started again on the same data directory
- **THEN** once the index is ready, the node list and the backlinks of each node are unchanged

#### Scenario: Index stays out of the data directory
- **WHEN** nodes have been created and queried
- **THEN** the data directory contains no database file

### Requirement: Unreadable index is replaced
If the index cannot be opened or read at startup, the engine SHALL discard it and rebuild it from the files in
the background instead of failing.

#### Scenario: Corrupt index
- **WHEN** the index file is replaced with bytes that are not a database and the engine is restarted
- **THEN** the engine becomes healthy, and once the rebuild has finished node queries return the same results as before

#### Scenario: Corrupt index without any file
- **WHEN** `org/` holds no `.org` file, the index file is not a database, and the engine is restarted
- **THEN** once the rebuild has finished a search answers with an empty list

### Requirement: Writes are indexed when saved
Every file the engine saves SHALL be indexed before the response is sent, so that the next request sees the
change. If a new node's file is saved but cannot be indexed, creating it SHALL fail with `500`, never `404`.

#### Scenario: Task that links to a note
- **WHEN** a task is created whose body links to node A
- **THEN** the next request for A's backlinks contains the task

#### Scenario: New node that cannot be indexed
- **WHEN** a node is created and org-roam fails to index its saved file
- **THEN** the response is `500`, and the file stays on disk

#### Scenario: New node is searchable
- **WHEN** a node is created
- **THEN** the next search for its title returns it

### Requirement: Changes outside the engine are picked up
Before answering a node query, the engine SHALL re-read every file under `org/` that was added, changed or
removed by another process since it last indexed it, whether the engine was running or stopped at the time.

#### Scenario: Note written by another process
- **WHEN** another process writes a new file under `org/knowledge/` with an `ID`, a title and a link to node A while the engine runs
- **THEN** the next search returns the new node and A's backlinks contain it, without a restart

#### Scenario: Link removed by another process
- **WHEN** another process removes the only link from node B to node A
- **THEN** the next request for A's backlinks no longer contains B

#### Scenario: File deleted while the engine was stopped
- **WHEN** the engine is stopped, a node's file is deleted, and the engine is started again
- **THEN** the node is no longer returned and `GET /api/v1/nodes/{id}` answers `404`

### Requirement: Tasks are served during a rebuild
While the index is rebuilding, the engine SHALL report healthy and SHALL serve every task, project and agenda
request as usual.

#### Scenario: Large collection without an index
- **GIVEN** a data directory with a few thousand notes and no index
- **WHEN** the engine starts
- **THEN** it reports healthy within seconds, and `GET /api/v1/tasks` and `POST /api/v1/tasks` succeed before the rebuild has finished

### Requirement: Node requests wait for the rebuild
While the index is rebuilding, every node endpoint SHALL answer `503` with a `Retry-After` header and a problem
document with code `index_rebuilding`, and SHALL NOT create anything. Once the rebuild has finished, node
requests SHALL also reflect files the engine wrote during it.

#### Scenario: Node request during a rebuild
- **WHEN** the index is rebuilding and a client sends `GET /api/v1/nodes` or `POST /api/v1/nodes`
- **THEN** the response is `503` with `Retry-After` and code `index_rebuilding`, and no file is created

#### Scenario: Task written during a rebuild
- **WHEN** a task linking to node A is created while the index is rebuilding
- **THEN** once the rebuild has finished, A's backlinks contain the task

### Requirement: Index state is reported
`GET /api/v1/meta` SHALL report the index state as `ready`, `rebuilding` or `failed`, and while rebuilding the
number of files indexed so far and in total. A failed rebuild SHALL leave node endpoints answering `500` until
the engine is restarted, which tries again.

#### Scenario: Progress
- **WHEN** a client requests `GET /api/v1/meta` during a rebuild
- **THEN** `index.state` is `rebuilding` and `index.files_done` is at most `index.files_total`

#### Scenario: Ready after the rebuild
- **WHEN** the rebuild has finished
- **THEN** `index.state` is `ready`
