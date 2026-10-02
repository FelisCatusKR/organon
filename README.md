# Organon

**Headless Org-mode as your personal API.**

Organon runs Emacs, Org-mode and org-roam as a headless engine and puts a small, stable HTTP API in front
of it. Your tasks, notes and journal stay in plain `.org` files that you own; every database is a cache that
can be rebuilt from those files. Org already knows how to do TODO states, repeating tasks, deadlines with
warnings and agendas, so Organon does not reimplement any of it. It only exposes it safely.

> **Status:** early development. MVP-A (tasks) works: create, complete, repeat, skip/cancel, today /
> overdue / waiting / completed. Knowledge nodes and backlinks (org-roam) come next. Expect breaking
> changes before 1.0.

- Design and rationale: [`docs/architecture.md`](docs/architecture.md) (Korean)
- Behavioral contract: [`openspec/`](openspec/); HTTP contract: [`api/openapi.yaml`](api/openapi.yaml)

## How it fits together

```
client ──HTTPS──▶ organon api ──Unix socket──▶ organon engine (Emacs + Org, no network) ──▶ your .org files
```

- The **engine** container is the only thing that reads or writes your data. It has no network, and it
  accepts only a fixed list of operations. There is no way to send it Lisp to run.
- The **api** container authenticates clients, validates input and forwards requests. It never mounts your
  data.
- Your data is a plain directory on the host. Removing containers, images or volumes never touches it.

## Quick start (Docker Compose)

You need Docker with the Compose plugin. Rootless Podman works too (see below).

```sh
git clone https://github.com/FelisCatusKR/organon && cd organon
mkdir -p data

# 1. Create the data directory. calendar_tz is the zone your Org files are written in:
#    it decides what "today" is and is stored with your data. It is required.
docker compose run --rm engine organon init --calendar-tz Asia/Seoul

# 2. Create an API token. Scopes: read, tasks:write.
docker compose run --rm --no-deps engine organon token new --name me --scopes read,tasks:write
#    Keep the printed token. Put the printed "tokens file line" into ./tokens:
echo 'me read,tasks:write <sha256…>' > tokens

# 3. Start. The API waits until the engine is healthy.
docker compose up -d
curl -s http://127.0.0.1:8080/healthz
```

Settings (environment variables or a `.env` file next to `compose.yaml`):

| Variable | Default | Meaning |
|---|---|---|
| `ORGANON_DATA` | `./data` | Host directory with your data |
| `ORGANON_TOKENS` | `./tokens` | Host file with token hashes |
| `ORGANON_UID` / `ORGANON_GID` | `1000` | Owner of the data directory. Docker runs as root otherwise and your files would become root-owned |
| `ORGANON_BIND` / `ORGANON_PORT` | `127.0.0.1` / `8080` | Where the API listens on the host |
| `ORGANON_CLIENT_IP_HEADER` | – | Header with the real client address behind a proxy, e.g. `CF-Connecting-IP`. Set it only when every request reaches the API through that proxy: the header is trusted as given (for `X-Forwarded-For`, its last entry) |
| `ORGANON_IMAGE` | `ghcr.io/feliscatuskr/organon:main` | Image to run (see below) |

**Images.** There are no releases yet. Every commit on `main` that passes CI is published for amd64 and arm64
as `ghcr.io/feliscatuskr/organon:sha-<7>` and moves the `:main` tag. `:main` is a **development build**, so to
stay on a version you have tested, pin a `sha-` tag (for example `ORGANON_IMAGE=ghcr.io/feliscatuskr/organon:sha-1a2b3c4`).
`organon version` inside a container prints the commit it was built from.

**Rootless Podman.** Run the same commands with `podman compose`, or with `docker-compose` pointed at the
Podman socket (`DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock`). Also set
`ORGANON_USERNS_MODE=keep-id`, so that `ORGANON_UID` means your own host user. For systemd-managed
containers there are Quadlet units in [`contrib/quadlet/`](contrib/quadlet/) (an example, not the supported
path).

**Exposing it.** The API binds to `127.0.0.1` by default. To reach it from elsewhere, put a TLS-terminating
proxy or a tunnel in front (Caddy, Cloudflare Tunnel, Tailscale, WireGuard). Every endpoint except
`/healthz` requires a token, and failed attempts are rate-limited per client address.

## Using the API

```sh
TOKEN=org_...            # from `organon token new`
API=http://127.0.0.1:8080/api/v1
auth=(-H "Authorization: Bearer $TOKEN")

# Instance info: calendar zone and today's date there
curl -s "${auth[@]}" $API/meta

# A monthly bill: due on the 25th, shown from 3 days before
curl -s "${auth[@]}" -H 'Content-Type: application/json' -H 'Idempotency-Key: spotify-1' \
  -d '{"title":"Spotify 가족 요금제 납부","state":"NEXT","tags":["bills"],
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

Other transitions: `start` (→ DOING), `wait` (→ WAITING), `skip` (cancel this occurrence; a repeating task
moves to the next one), `cancel` (→ CANCELLED; ends a repeating series). Errors are
[RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem documents with a stable `code`.

### Dates and time zones

Org timestamps have no time zone: `<2026-10-25 Sun>` is a calendar date. Each instance declares the zone its
files are written in (`calendar_tz`). It decides what "today" is, when a deadline is overdue, and what
`.+1m` counts from. The API returns planning dates as plain dates (`"2026-10-25"`) and instants such as
`closed_at` in UTC. Clients convert instants to local time, and can ask for "today" in their own sense with
`?date=`.

### Repeating tasks

All recurrence is Org's:

| Repeater | After completing on 2026-10-02 a task due 2026-08-25 |
|---|---|
| `+1m` | 2026-09-25: one step at a time, missed occurrences stay due |
| `++1m` | 2026-10-25: next occurrence in the future |
| `.+1m` | 2026-11-02: one month after you actually did it |

A completed repeating task comes back as `NEXT` (or the task's `repeat_to_state`). Its completions are
recorded in the task's LOGBOOK and show up in `/tasks/completed`.

## Your data

```
data/
├── organon.json        calendar_tz and settings (part of your data: back it up)
├── org/
│   ├── tasks/inbox.org new tasks go here
│   ├── projects/       one file per project; tasks under a level-1 heading belong to it
│   ├── knowledge/      notes (org-roam, from MVP-B)
│   ├── journal/
│   └── archive/
└── attachments/
```

These are ordinary Org files: open them in Emacs or any text editor. While the stack runs, make changes
through the API. To edit by hand, stop the engine first (`docker compose stop engine`), edit, then start it
again.

### Backups

Organon does not back up your data. The data directory is the only thing that matters: the cache volume and
containers are rebuilt automatically. Recommended:

- **[restic](https://restic.net/)** (or borg) of the data directory, daily, to a repository **off the
  host**.
- On Btrfs or ZFS, take snapshots more often and back up from a snapshot.
- Files are written atomically (write to a temporary file, then rename), so a backup never contains a
  half-written file.
- Rehearse a restore: restore into an empty directory, point `ORGANON_DATA` at it, `docker compose up`.

Do not use Git to sync your notes; use it for code and configuration.

## Development

Tools are pinned in [`mise.toml`](mise.toml) (Go, Node, OpenSpec CLI, Redocly). Emacs is pinned by the
container image, so you only need [mise](https://mise.jdx.dev/) and Docker or Podman:

```sh
mise install
mise run test            # Go unit tests + Elisp (ERT) tests inside the test image
mise run lint:openapi
mise run spec:validate
podman build -f container/Containerfile -t organon:dev .
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the checks a pull request must pass.

## License

[MIT](LICENSE). The container image also contains GNU Emacs and other Debian packages under their own
licenses (mostly GPL). The exact package versions are listed in the image at
`/usr/share/doc/organon-debian-packages.tsv`; sources are available from
[snapshot.debian.org](https://snapshot.debian.org/).
