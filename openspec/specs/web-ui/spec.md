# web-ui Specification

## Purpose
TBD - created by archiving change web-tasks. Update Purpose after archive.

## Requirements

### Requirement: Opt-in
The web UI SHALL be served only when `ORGANON_WEB_ENABLED` is exactly `true`. Otherwise every path outside the API and
the probes SHALL answer as it did without the UI: a `404` problem document.

#### Scenario: Off by default
- **GIVEN** `ORGANON_WEB_ENABLED` is unset
- **WHEN** a client requests `GET /login`
- **THEN** the response is a `404` problem document

### Requirement: Sign-in with a token
A client SHALL sign in by posting an API token that has the `read` scope to `POST /login`. A success SHALL create a new
session and redirect to `/`. The token SHALL be accepted only in the POST body. An unknown token SHALL count against
the same per-address limit as the API. The answer SHALL NOT say why an unknown token failed. A valid token without
`read` SHALL be refused with a message saying so, because only its holder can present it.

#### Scenario: Valid token
- **WHEN** a client posts a valid token to `/login`
- **THEN** the response redirects to `/` and sets a session cookie that does not contain the token

#### Scenario: Invalid token
- **WHEN** a client posts an unknown token to `/login`
- **THEN** the response is `401`, no session cookie is set, and the failure is counted

#### Scenario: Too many failures
- **GIVEN** an address has used up its failed authentication attempts
- **WHEN** it posts a valid token to `/login`
- **THEN** the response is `429` and no session is created

#### Scenario: Token in the query string
- **WHEN** a client posts to `/login?token=<valid token>` with an empty body
- **THEN** the sign-in fails

#### Scenario: Token without read
- **WHEN** a client posts a valid token that lacks `read`
- **THEN** the response is `403` and no session is created

### Requirement: Sessions
A session SHALL last at most 30 days and end at sign-out or when the process restarts. The session cookie SHALL be
`HttpOnly` and `SameSite=Strict`, and `Secure` unless `ORGANON_WEB_INSECURE_COOKIE` is exactly `true`. Every sign-in SHALL
create a new random session ID, and only a hash of it SHALL be kept in the server. Every page except sign-in SHALL
redirect to `/login` without a valid session.

#### Scenario: Cookie attributes
- **WHEN** a client signs in
- **THEN** the cookie is `HttpOnly`, `SameSite=Strict`, `Secure`, and expires within 30 days

#### Scenario: Insecure cookie on request
- **GIVEN** `ORGANON_WEB_INSECURE_COOKIE` is `true`
- **WHEN** a client signs in
- **THEN** the cookie is not `Secure`

#### Scenario: No session
- **WHEN** a client without a session requests `GET /`
- **THEN** the response redirects to `/login`

#### Scenario: Expired session
- **GIVEN** a session older than 30 days
- **WHEN** its cookie is used
- **THEN** the response redirects to `/login`

#### Scenario: Sign-out
- **WHEN** a signed-in client posts to `/logout`
- **THEN** the old cookie no longer gives access, even if presented again

### Requirement: Browser protections
Every UI response SHALL carry a Content-Security-Policy that allows no script and no framing, `Referrer-Policy:
no-referrer` and `Cache-Control: no-store`. State-changing requests from another origin SHALL be refused. Task text
SHALL be escaped.

#### Scenario: Security headers
- **WHEN** a client requests any UI page
- **THEN** the response has those three headers

#### Scenario: Cross-origin form
- **GIVEN** a signed-in browser
- **WHEN** another site submits a form to `POST /tasks`
- **THEN** the response is `403` and nothing is created

#### Scenario: Markup in a title
- **GIVEN** a task titled `<script>alert(1)</script>`
- **WHEN** it is listed
- **THEN** the page shows the text and contains no such element

### Requirement: Task pages
`GET /` SHALL list today's tasks, overdue ones included. `GET /tasks` SHALL list all open tasks, with or without dates.
`GET /waiting` SHALL list waiting tasks. Each row SHALL show the title, state, priority, tags, project and planning
dates. The pages SHALL use the API's results without computing dates. When the engine cannot answer, the page SHALL say
so with the matching status.

#### Scenario: Tabs
- **GIVEN** an undated task and a task due today
- **WHEN** a client opens `/` and `/tasks`
- **THEN** `/` lists the task due today and not the undated one, and `/tasks` lists both

#### Scenario: Engine unavailable
- **GIVEN** the engine does not answer
- **WHEN** a signed-in client opens `/`
- **THEN** the response is `503` and the page says that the service is unavailable

### Requirement: Task actions
A signed-in client SHALL be able to start, complete and skip a task from the lists. Controls SHALL be hidden when the
session's token lacks `tasks:write`, and the server SHALL refuse such requests anyway. An action SHALL be applied only
if the task still has the state and version the page showed. Completing or skipping a repeating task SHALL first ask for
confirmation.

#### Scenario: Complete a task
- **GIVEN** a task that does not repeat
- **WHEN** a client posts `complete` with the state and version it was shown
- **THEN** the task is completed and the response redirects to the list it came from

#### Scenario: Repeating task asks first
- **GIVEN** a repeating task
- **WHEN** a client posts `complete` without confirmation
- **THEN** the task does not change and the client is sent to a confirmation page naming it

#### Scenario: Confirmed
- **WHEN** a client submits the confirmation page
- **THEN** the task moves to its next occurrence

#### Scenario: Changed in the meantime
- **GIVEN** the task changed after the page was rendered
- **WHEN** a client posts an action with the old version
- **THEN** the task does not change and the list shows a conflict notice

#### Scenario: Read-only session
- **GIVEN** a session from a token with only `read`
- **WHEN** it opens a list and posts an action
- **THEN** the list shows no action controls and no add form, and the post changes nothing

#### Scenario: Open redirect refused
- **WHEN** a client posts an action with `back=https://example.org/`
- **THEN** the response redirects to `/`

### Requirement: Quick add
A signed-in client with `tasks:write` SHALL be able to add a task with a title and an optional deadline date. The task's
state SHALL be `TODO`. Submitting the same rendered form twice SHALL create one task. An input the API rejects SHALL
produce a notice.

#### Scenario: Add
- **WHEN** a client posts a title and a deadline to `POST /tasks`
- **THEN** the task exists with state `TODO` and that deadline

#### Scenario: Double submit
- **WHEN** the same form is posted twice
- **THEN** one task is created

#### Scenario: Rejected input
- **WHEN** a client posts an empty title
- **THEN** nothing is created and the list shows a notice

### Requirement: Languages
The UI SHALL be available in English and Korean. A language chosen with the language switch SHALL take precedence, then
`Accept-Language`, then English. The switch SHALL be available on every page, including sign-in. Dates SHALL always be
shown as in the API. Task content SHALL be shown as written.

#### Scenario: Initial language
- **WHEN** a client with `Accept-Language: ko-KR,ko;q=0.9,en;q=0.8` opens `/login`
- **THEN** the page is in Korean with `lang="ko"`

#### Scenario: Unsupported language
- **WHEN** a client with `Accept-Language: fr` opens `/login`
- **THEN** the page is in English

#### Scenario: Switch wins
- **GIVEN** a browser with `Accept-Language: ko`
- **WHEN** it posts `lang=en` to `/language`
- **THEN** later pages are in English

#### Scenario: Invalid language
- **WHEN** a client posts `lang=xx` to `/language`
- **THEN** no language cookie is set
