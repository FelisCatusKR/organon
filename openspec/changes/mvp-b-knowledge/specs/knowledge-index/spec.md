# Spec Delta

## Purpose

Keeps the org-roam index (nodes, links, backlinks) a disposable cache of the `.org` files: it can be deleted
at any time and is rebuilt from the files, and node queries always reflect the files on disk.

## ADDED Requirements

### Requirement: Index is a rebuildable cache
The node index SHALL be stored only in the cache directory, never in the data directory. When the index is
missing, the engine SHALL rebuild it from the `.org` files before it reports healthy, and node queries SHALL then
return the same nodes, links and backlinks as before.

#### Scenario: Rebuild after the index is deleted
- **GIVEN** nodes and tasks that link to each other, and the results of listing all nodes and of the backlinks and links of each node
- **WHEN** the index is deleted and the engine is restarted
- **THEN** the same requests return the same results

#### Scenario: Recreate the whole stack
- **WHEN** all containers and named volumes are removed and the stack is started again on the same data directory
- **THEN** the node list and the backlinks of each node are unchanged

#### Scenario: Index stays out of the data directory
- **WHEN** nodes have been created and queried
- **THEN** the data directory contains no database file

### Requirement: Unreadable index is replaced
If the index cannot be opened or read at startup, the engine SHALL discard it and rebuild it from the files
instead of failing.

#### Scenario: Corrupt index
- **WHEN** the index file is replaced with bytes that are not a database and the engine is restarted
- **THEN** the engine becomes healthy and node queries return the same results as before

### Requirement: Writes are indexed when saved
Every file the engine saves SHALL be indexed before the response is sent, so that the next request sees the
change.

#### Scenario: Task that links to a note
- **WHEN** a task is created whose body links to node A
- **THEN** the next request for A's backlinks contains the task

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
