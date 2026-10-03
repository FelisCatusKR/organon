# knowledge-nodes Specification

## Purpose
Lets clients keep linked notes (org-roam nodes) next to their tasks: create a note and get a stable ID, read
and find notes again, and follow links and backlinks between notes and tasks, without touching Org files.

## Requirements

### Requirement: Create a node
`POST /api/v1/nodes` SHALL create a knowledge node from a `title` and optional `body`, `tags` and `aliases` as a
new `.org` file under `org/knowledge/` whose file-level `ID` property is a generated UUID, and SHALL return `201`
with the node and a `Location` header. The file name SHALL be derived from the title and SHALL never overwrite an
existing file.

#### Scenario: Node is persisted as an Org file
- **WHEN** a client sends `POST /api/v1/nodes` with title `Emacs 설정 노트`
- **THEN** the response is `201` with a node whose `id` is a UUID and `title` is `Emacs 설정 노트`, and `Location` is `/api/v1/nodes/<id>`
- **AND** a new file under `org/knowledge/` has that `ID` in its top-level property drawer and `#+title: Emacs 설정 노트`

#### Scenario: Tags and aliases
- **WHEN** a node is created with tags `["emacs", "tools"]` and aliases `["init.el", "닷 이맥스"]`
- **THEN** the file has `#+filetags: :emacs:tools:` and a `ROAM_ALIASES` property with both aliases, and the response returns them

#### Scenario: Same title twice
- **WHEN** two nodes with the same title are created within the same second
- **THEN** both exist, in different files, with different IDs

#### Scenario: Retried creation
- **WHEN** the same create request is sent twice with the same `Idempotency-Key`
- **THEN** both responses carry the same `id` and only one file was created

### Requirement: Node text cannot alter Org structure
Node titles and aliases SHALL be a single line of at most 500 characters without control characters or Org
links. Bodies SHALL be stored with the same escaping as task bodies, so they cannot create headings, keywords,
drawers, clock or diary entries. Active timestamps in titles and bodies SHALL be made inactive. Tags SHALL follow
the task tag rules. Org ID links in bodies SHALL be kept as written.

#### Scenario: Title with a newline
- **WHEN** a node is created with a title or an alias that contains a newline
- **THEN** the response is `422` and no file is created

#### Scenario: Link in a title
- **WHEN** a node is created with the title `See [[id:<other node id>][the config]]`
- **THEN** the response is `422` and no file is created

#### Scenario: Body that looks like a heading
- **WHEN** a node body contains the line `* TODO injected` and the line `#+title: other`
- **THEN** the file contains no heading, its title is unchanged, and `GET` returns the body with both lines intact

#### Scenario: Link in a body
- **WHEN** a node body contains `[[id:<other node id>][the other note]]`
- **THEN** the file contains that link unchanged and `GET` returns it unchanged

### Requirement: Read a node by stable ID
`GET /api/v1/nodes/{id}` SHALL return the node with that `ID` (`id`, `title`, `aliases`, `tags`, `body`,
`location_hint`), whether it is a whole file or a heading, and wherever it is under `org/`. The body of a
heading node SHALL exclude its child headings; the body of a file node SHALL be the text before the first
heading, after the file's keywords.

#### Scenario: Read a created node
- **WHEN** a client creates a node and then requests `GET /api/v1/nodes/{id}` with the returned `id`
- **THEN** the response is `200` with the same title, tags, aliases and body

#### Scenario: Hand-written heading node
- **GIVEN** a file in `org/knowledge/` contains a heading `* Reading list` with an `ID` and a body, followed by a child heading
- **WHEN** a client requests that ID
- **THEN** the response has title `Reading list` and the body without the child heading

#### Scenario: Hand-written file node
- **GIVEN** a file starts with a comment line, then its property drawer with an `ID`, `#+title:` and `#+filetags:`, then `#+caption: A picture` above a link
- **WHEN** a client requests that ID
- **THEN** the body starts with the `#+caption:` line and contains neither the comment, the drawer nor the file keywords

#### Scenario: Unknown node
- **WHEN** a client requests `GET /api/v1/nodes/{id}` for a well-formed UUID that no entry has
- **THEN** the response is `404`

### Requirement: Tasks are not nodes
An entry that is a task (one of the six workflow states and an `ID`) SHALL NOT be returned as a node: reading it
as a node SHALL answer `404` and searches SHALL leave it out. Every other entry with an `ID` under `org/` SHALL be
a node, including headings with keywords outside the six states.

#### Scenario: Task ID under /nodes
- **WHEN** a client requests `GET /api/v1/nodes/{id}` with the ID of a task
- **THEN** the response is `404`, and `GET /api/v1/nodes?q=<task title>` does not return it

#### Scenario: Heading with another keyword
- **GIVEN** a knowledge file declares `#+TODO: IDEA | DROPPED` and contains `* IDEA Garden pond` with an `ID`
- **WHEN** a client searches for `Garden`
- **THEN** that heading is returned as a node

### Requirement: Search nodes
`GET /api/v1/nodes` SHALL return the nodes outside `org/archive/`, ordered by title. `q` SHALL keep nodes whose
title or one of whose aliases contains it, ignoring case; `tag` SHALL keep nodes that carry that tag. Filters
SHALL combine with AND. Without filters every such node SHALL be returned. Each item SHALL carry `id`, `title`,
`aliases`, `tags` and `location_hint`.

#### Scenario: Match on an alias, ignoring case
- **GIVEN** a node titled `Emacs 설정 노트` with alias `init.el`
- **WHEN** a client requests `GET /api/v1/nodes?q=INIT.EL` and `GET /api/v1/nodes?q=설정`
- **THEN** both responses contain that node once

#### Scenario: Filter by tag
- **WHEN** a client requests `GET /api/v1/nodes?tag=emacs`
- **THEN** only nodes tagged `emacs` are returned

#### Scenario: Hangul tag
- **WHEN** a node is created with the tag `이맥스` and a client requests `GET /api/v1/nodes?tag=이맥스`
- **THEN** the node is returned

#### Scenario: Archived notes are not searched
- **WHEN** a node in `org/archive/` matches the query
- **THEN** it is not returned, and `GET /api/v1/nodes/{id}` still returns it

#### Scenario: Invalid search
- **WHEN** a client sends `q` longer than 200 characters or containing a control character, or `tag=a b`
- **THEN** the response is `422`

### Requirement: Backlinks
`GET /api/v1/nodes/{id}/backlinks` SHALL return every node and task that contains an Org `id:` link to the node,
once per source, ordered by title, each as `id`, `title` and `kind` (`node` or `task`). Sources in
`org/archive/` and tasks SHALL be included. An unknown node SHALL answer `404`.

#### Scenario: Backlinks from a note and a task
- **GIVEN** node A, a node B whose body links to A twice, and a task whose body links to A
- **WHEN** a client requests `GET /api/v1/nodes/<A>/backlinks`
- **THEN** the response contains B once with `kind` `node` and the task with `kind` `task`, and nothing else

#### Scenario: Backlink from an archived file
- **WHEN** a heading in `org/archive/` links to node A
- **THEN** it is among A's backlinks

#### Scenario: New link appears at once
- **WHEN** a node linking to A is created through the API
- **THEN** the next request for A's backlinks contains it

### Requirement: Forward links
`GET /api/v1/nodes/{id}/links` SHALL return the nodes and tasks the node links to with Org `id:` links, once per
target, ordered by title, in the same form as backlinks. Links to IDs that no entry has SHALL be left out. An
unknown node SHALL answer `404`.

#### Scenario: Links of a note
- **WHEN** node B links to node A and to a task
- **THEN** `GET /api/v1/nodes/<B>/links` returns A with `kind` `node` and the task with `kind` `task`

#### Scenario: Dangling link
- **WHEN** node B also links to a UUID that no entry has
- **THEN** that link is not returned and the request still succeeds
