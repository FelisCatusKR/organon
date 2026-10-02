# Spec Delta

## Purpose

Lets clients find tasks independently of dates and the agenda: the backlog, everything that is NEXT, or the
tasks of one project.

## ADDED Requirements

### Requirement: List tasks
`GET /api/v1/tasks` SHALL return the tasks in the agenda sources (`org/tasks/` and `org/projects/`,
recursively), in file order. Without a `state` filter it SHALL return tasks in open states (`TODO`, `NEXT`,
`DOING`, `WAITING`), with or without dates.

#### Scenario: Undated backlog is listed
- **WHEN** a `TODO` task has no scheduled or deadline date
- **THEN** it is returned by `GET /api/v1/tasks`

#### Scenario: Closed tasks are not listed by default
- **WHEN** a task is `DONE` or `CANCELLED`
- **THEN** it is not returned by `GET /api/v1/tasks` without a `state` filter

### Requirement: Filter by state, project and tag
The `state` parameter SHALL accept one or more comma-separated states, `project` a project ID, and `tag` a
single tag. Filters SHALL combine with AND. An unknown state, a malformed project ID or an invalid tag SHALL be
rejected.

#### Scenario: Next actions of one project
- **WHEN** a client requests `GET /api/v1/tasks?state=NEXT&project=<id>`
- **THEN** only `NEXT` tasks that belong to that project are returned

#### Scenario: Several states and a tag
- **WHEN** a client requests `GET /api/v1/tasks?state=DONE,CANCELLED&tag=bills`
- **THEN** only closed tasks tagged `bills` are returned

#### Scenario: Invalid filter
- **WHEN** a client requests `state=SOMEDAY`, `project=../x` or `tag=a b`
- **THEN** the response is `422`
