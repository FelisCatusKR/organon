# Organon

**Headless Org-mode as your personal API.**

Organon runs Emacs and Org-mode as a headless engine and puts a small, stable HTTP API in front of it. Your
tasks stay in plain `.org` files that you own, and any database is a cache that can be rebuilt from them.

Org already handles TODO states, repeating tasks, deadlines with warnings and agendas. Organon does not
reimplement any of it: it exposes Org safely, so that scripts, apps and assistants can use your tasks without
touching the files.

> **Status:** early development. Tasks work today: create, edit, list and complete them, including repeating
> tasks and projects, view today / overdue / waiting / completed, and use the command-line client. Notes and
> backlinks (org-roam) come next. Expect breaking changes before 1.0.

## Quick start

You need Docker with the Compose plugin (or rootless Podman, see [docs/deployment.md](docs/deployment.md)).

```sh
git clone https://github.com/FelisCatusKR/organon && cd organon
mkdir -p data
docker compose run --rm engine organon init --calendar-tz Asia/Seoul   # the zone your files are written in
docker compose run --rm --no-deps engine organon token new --name me --scopes read,tasks:write
# Keep the printed token, and save the printed "tokens file line" as ./tokens
docker compose up -d
```

The API now listens on `http://127.0.0.1:8080`. Try it with the client that ships in the image:

```sh
alias organon='docker compose exec -e ORGANON_URL=http://127.0.0.1:8080 -e ORGANON_TOKEN=org_... api organon'
organon task add "Pay the electricity bill" --deadline 2026-10-25 --repeat +1m --tag bills
organon task list
```

## Documentation

- [Using Organon](docs/usage.md): the CLI, the HTTP API, dates and repeating tasks
- [Running Organon](docs/deployment.md): settings, images, Podman, exposing it, your data and backups
- [Architecture](docs/architecture.md) (Korean): design, security boundaries, time model, decisions
- Contracts: behavior in [`openspec/specs/`](openspec/specs/), HTTP in [`api/openapi.yaml`](api/openapi.yaml)
- [Contributing](CONTRIBUTING.md) and [security reports](SECURITY.md)

## License

[MIT](LICENSE). The container image also contains GNU Emacs and other packages under their own licenses; see
[docs/deployment.md](docs/deployment.md#licenses-in-the-image).
