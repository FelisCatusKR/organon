# projects Specification

## Purpose
Lets clients create and list projects so that tasks can be grouped, without anyone editing Org files by hand.

## Requirements

### Requirement: Create a project
`POST /api/v1/projects` SHALL create a project from a `title` (and optional `body`) as a new file under
`org/projects/` holding one level-1 heading with a generated `ID`, and return the project. The file name SHALL
be derived from the title and SHALL never overwrite an existing file. Titles follow the same rules as task
titles and SHALL NOT start with a workflow state (`TODO`, `NEXT`, ...), which Org would read as a task. The
endpoint SHALL honor an `Idempotency-Key` like task creation.

#### Scenario: New project file
- **WHEN** a client creates a project titled `Home renovation`
- **THEN** a new `.org` file under `org/projects/` contains a level-1 heading `Home renovation` with an `ID`, and the response returns that `id`

#### Scenario: Same title twice
- **WHEN** two projects with the same title are created
- **THEN** both exist, in different files, with different IDs

#### Scenario: Title starting with a state
- **WHEN** a client creates a project titled `TODO app`
- **THEN** the response is `422` and no file is created

#### Scenario: Retried creation
- **WHEN** the same create request is sent twice with the same `Idempotency-Key`
- **THEN** both responses carry the same `id` and only one project file exists

#### Scenario: Tasks can be added to the new project
- **WHEN** a task is created with the new project's ID as `project_id`
- **THEN** the task belongs to the project

### Requirement: List projects
`GET /api/v1/projects` SHALL return every level-1 heading with an `ID` and no TODO keyword in `org/projects/`
(recursively) with its `id`, `title` and the number of open tasks under it. `project_id` in task creation SHALL
accept exactly these headings.

#### Scenario: A task heading is not a project
- **WHEN** a level-1 heading in `org/projects/` has a TODO keyword and an `ID`
- **THEN** it is not listed as a project, and creating a task with its ID as `project_id` is rejected with `422`

#### Scenario: Counts of open tasks
- **WHEN** a project has two open tasks and one `DONE` task
- **THEN** it is listed with `open_tasks` `2`
