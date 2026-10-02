# Spec Delta

## Purpose

Lets clients create and list projects so that tasks can be grouped, without anyone editing Org files by hand.

## ADDED Requirements

### Requirement: Create a project
`POST /api/v1/projects` SHALL create a project from a `title` (and optional `body`) as a new file under
`org/projects/` holding one level-1 heading with a generated `ID`, and return the project. The file name SHALL
be derived from the title and SHALL never overwrite an existing file. Titles follow the same rules as task
titles.

#### Scenario: New project file
- **WHEN** a client creates a project titled `Home renovation`
- **THEN** a new `.org` file under `org/projects/` contains a level-1 heading `Home renovation` with an `ID`, and the response returns that `id`

#### Scenario: Same title twice
- **WHEN** two projects with the same title are created
- **THEN** both exist, in different files, with different IDs

#### Scenario: Tasks can be added to the new project
- **WHEN** a task is created with the new project's ID as `project_id`
- **THEN** the task belongs to the project

### Requirement: List projects
`GET /api/v1/projects` SHALL return every level-1 heading with an `ID` in `org/projects/` (recursively) with its
`id`, `title` and the number of open tasks under it.

#### Scenario: Counts of open tasks
- **WHEN** a project has two open tasks and one `DONE` task
- **THEN** it is listed with `open_tasks` `2`
