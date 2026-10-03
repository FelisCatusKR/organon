# Spec Delta

## MODIFIED Requirements

### Requirement: Scopes
Each token SHALL carry scopes. `read` SHALL be required for every `GET` under `/api/v1`, `tasks:write` for
creating, editing and transitioning tasks and for creating projects, and `nodes:write` for creating knowledge
nodes. A scope SHALL NOT imply another.

#### Scenario: Read-only token tries to write
- **WHEN** a token with only `read` sends `POST /api/v1/tasks`
- **THEN** the response is `403` and no file is modified

#### Scenario: Read-only token tries to edit or create a project
- **WHEN** a token with only `read` sends `PATCH /api/v1/tasks/{id}` or `POST /api/v1/projects`
- **THEN** the response is `403` and no file is modified

#### Scenario: Read-only token tries to create a node
- **WHEN** a token with only `read` sends `POST /api/v1/nodes`
- **THEN** the response is `403` and no file is modified

#### Scenario: Note-taking token cannot change tasks
- **WHEN** a token with `read,nodes:write` sends `POST /api/v1/tasks`, and a token with `read,tasks:write` sends `POST /api/v1/nodes`
- **THEN** both responses are `403` and no file is modified
