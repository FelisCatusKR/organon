# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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
