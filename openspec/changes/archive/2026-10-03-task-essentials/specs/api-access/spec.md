# Spec Delta

## MODIFIED Requirements

### Requirement: Scopes
Each token SHALL carry scopes. `read` SHALL be required for every `GET` under `/api/v1`, and `tasks:write` for
creating, editing and transitioning tasks and for creating projects.

#### Scenario: Read-only token tries to write
- **WHEN** a token with only `read` sends `POST /api/v1/tasks`
- **THEN** the response is `403` and no file is modified

#### Scenario: Read-only token tries to edit or create a project
- **WHEN** a token with only `read` sends `PATCH /api/v1/tasks/{id}` or `POST /api/v1/projects`
- **THEN** the response is `403` and no file is modified
