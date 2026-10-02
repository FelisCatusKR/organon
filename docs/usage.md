# Using Organon

Organon is used through its HTTP API. The `organon` command-line client covers everyday use. The HTTP API
itself is described here briefly and fully in [`api/openapi.yaml`](../api/openapi.yaml). To set up an
instance, see [deployment.md](deployment.md).

## The CLI

The `organon` binary is also a client for the API. It talks only to the HTTP API, so it works the same
against a local or a remote instance. Point it at your instance once:

```sh
mkdir -p ~/.config/organon
cat > ~/.config/organon/client.json <<'JSON'
{"url": "http://127.0.0.1:8080", "token": "org_..."}
JSON
chmod 600 ~/.config/organon/client.json   # the CLI refuses a file others can read
```

`ORGANON_URL` and `ORGANON_TOKEN` override the file. Without Go installed, you can run the CLI from the image:
`docker run --rm -e ORGANON_URL -e ORGANON_TOKEN --network host ghcr.io/feliscatuskr/organon:main organon today`.

```sh
organon today                     # what matters today (overdue, due soon, scheduled)
organon overdue
organon waiting
organon completed --date 2026-10-02

organon task add "Pay the electricity bill" --next --deadline 2026-10-25 --repeat +1m --warn 3 --tag bills
organon task add "Call the plumber" --scheduled "2026-10-05 15:00" --priority A
organon task list                       # open tasks, with or without dates
organon task list --state NEXT --tag bills

organon task done 3f2a                  # IDs can be shortened to a unique prefix (4+ characters)
organon task start 9c00
organon task next 9c00                  # back to NEXT; `todo` / `next` also reopen closed tasks
organon task edit 3f2a --deadline 2026-10-30   # keeps its repeater; add --repeat none to end the series
organon task edit 3f2a --priority none --scheduled none

organon project add "Home renovation"
organon task add "Order tiles" --project 7777
organon project list
```

Dates go to the API exactly as you type them, and Org computes everything else. Every command takes `--json`
to print the API's response unchanged.

If a change fails because the server did not answer, the CLI does not retry. Check with
`organon task show ID` first: re-running a completion that already went through would move a repeating
task twice.

## The HTTP API

Every request needs a token from `organon token new` (see [deployment.md](deployment.md)).

```sh
TOKEN=org_...
API=http://127.0.0.1:8080/api/v1
auth=(-H "Authorization: Bearer $TOKEN")

# Instance info: calendar zone and today's date there
curl -s "${auth[@]}" $API/meta

# A monthly bill: due on the 25th, shown from 3 days before
curl -s "${auth[@]}" -H 'Content-Type: application/json' -H 'Idempotency-Key: electricity-1' \
  -d '{"title":"Pay the electricity bill","state":"NEXT","tags":["bills"],
       "deadline":{"date":"2026-10-25","repeat":"+1m","warning_days":3}}' \
  $API/tasks

# What matters today (or on another day, as if it were today)
curl -s "${auth[@]}" $API/tasks/today
curl -s "${auth[@]}" "$API/tasks/today?date=2026-10-22"
curl -s "${auth[@]}" $API/tasks/overdue
curl -s "${auth[@]}" $API/tasks/waiting
curl -s "${auth[@]}" $API/agenda          # tasks and events

# Complete it. Org moves the deadline to the next month; the task keeps its id.
# Repeating tasks need expected_version (the task's "version"), so that a
# retried request cannot move the dates twice.
curl -s "${auth[@]}" -H 'Content-Type: application/json' \
  -d '{"expected_state":"NEXT","expected_version":"<version>"}' \
  $API/tasks/<id>/complete

curl -s "${auth[@]}" $API/tasks/completed  # completed today, repeating tasks included
```

**Transitions.** Each is `POST /api/v1/tasks/{id}/{action}`:

| Action | Effect |
|---|---|
| `start` | → `DOING` |
| `wait` | → `WAITING` |
| `complete` | → `DONE`; a repeating task moves to its next occurrence |
| `skip` | Cancel this occurrence; a repeating task moves to the next one |
| `cancel` | → `CANCELLED`; ends a repeating series |
| `todo` / `next` | → `TODO` / `NEXT`; also reopens a closed task |

**Other endpoints:**

| Request | Purpose | Notes |
|---|---|---|
| `PATCH /api/v1/tasks/{id}` | Edit a task | Send `expected_version` plus the fields to change; `null` clears a date or the priority |
| `GET /api/v1/tasks?state=NEXT&project=…&tag=…` | List tasks, with or without dates | |
| `GET /api/v1/projects` | List projects | |
| `POST /api/v1/projects` | Create a project | |

Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem documents with a stable `code`.

## Dates and time zones

Org timestamps have no time zone: `<2026-10-25 Sun>` is a calendar date. Each instance declares the zone its
files are written in (`calendar_tz`). That zone decides:
- what "today" is
- when a deadline is overdue
- what `.+1m` counts from

The API returns:
- planning dates as plain dates, such as `"2026-10-25"`
- instants such as `closed_at` in UTC

Clients convert instants to local time. They can ask for "today" in their own sense with `?date=`.

## Repeating tasks

All recurrence is Org's:

| Repeater | After completing on 2026-10-02 a task due 2026-08-25 |
|---|---|
| `+1m` | 2026-09-25: one step at a time, missed occurrences stay due |
| `++1m` | 2026-10-25: next occurrence in the future |
| `.+1m` | 2026-11-02: one month after you actually did it |

A completed repeating task comes back as `NEXT`, or as the task's `repeat_to_state` if it has one. Its
completions are recorded in the task's LOGBOOK and show up in `/tasks/completed`.
