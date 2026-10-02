# Running Organon

How to install, configure, expose and back up an Organon instance. For using it, see [usage.md](usage.md).

## How it runs

```
client ──HTTPS──▶ organon api ──Unix socket──▶ organon engine (Emacs + Org, no network) ──▶ your .org files
```

- The **engine** container is the only thing that reads or writes your data. It has no network, and it
  accepts only a fixed list of operations. There is no way to send it Lisp to run.
- The **api** container authenticates clients, validates input and forwards requests. It never mounts your
  data.
- Your data is a plain directory on the host. Removing containers, images or volumes never touches it.

Both containers run from the same image.

## Install with Docker Compose

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

Only token hashes are stored. To add a client, run step 2 again and append its line to `tokens`, then
`docker compose restart api`.

## Settings

Environment variables, or a `.env` file next to `compose.yaml`:

| Variable | Default | Meaning |
|---|---|---|
| `ORGANON_DATA` | `./data` | Host directory with your data |
| `ORGANON_TOKENS` | `./tokens` | Host file with token hashes |
| `ORGANON_UID` / `ORGANON_GID` | `1000` | Owner of the data directory. Docker runs as root otherwise and your files would become root-owned |
| `ORGANON_BIND` / `ORGANON_PORT` | `127.0.0.1` / `8080` | Where the API listens on the host |
| `ORGANON_CLIENT_IP_HEADER` | – | Header with the real client address behind a proxy (see [Exposing it](#exposing-it)) |
| `ORGANON_IMAGE` | `ghcr.io/feliscatuskr/organon:main` | Image to run (see [Images](#images)) |

## Images

There are no releases yet. Every commit on `main` that passes CI is published for amd64 and arm64 as
`ghcr.io/feliscatuskr/organon:sha-<7>`, and moves the `:main` tag.

`:main` is a **development build**. To stay on a version you have tested, pin a `sha-` tag, for example
`ORGANON_IMAGE=ghcr.io/feliscatuskr/organon:sha-1a2b3c4`. Inside a container, `organon version` prints the
commit the image was built from.

To run your own build: `docker build -f container/Containerfile -t organon:dev .` and
`ORGANON_IMAGE=organon:dev`.

## Rootless Podman

You can run the same commands with `podman compose`. You can also use `docker-compose` pointed at the Podman
socket (`DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock`). Either way, set
`ORGANON_USERNS_MODE=keep-id`, so that `ORGANON_UID` means your own host user.

For containers managed by systemd, there are Quadlet units in [`contrib/quadlet/`](../contrib/quadlet/). They
are an example, not the supported path.

## Custom deployments

`compose.yaml` and `contrib/quadlet/` implement a small container interface. It is declared in
[`contrib/container-interface.json`](../contrib/container-interface.json), and CI checks both definitions
against it. Your own unit files can rely on the same interface:

| | Engine | API |
|---|---|---|
| Command | `emacs -q --fg-daemon -l /opt/organon/emacs/init.el -f organon-start` | `organon serve` |
| Health check | `organon rpc-ping` | `organon healthcheck` (`GET /healthz`) |
| Mounts | `/data` (your data), `/cache` (rebuildable), `/run/organon` (the socket, shared) | `/run/organon` only, never `/data` |
| Other writable paths | a tmpfs at `/tmp`; the root filesystem may be read-only | none |
| Network | none (loopback only) | wherever clients reach it |
| Environment | none needed | `ORGANON_LISTEN` (e.g. `0.0.0.0:8080`; the default `127.0.0.1:8080` is unreachable from outside the container), `ORGANON_TOKENS_FILE`; optional `ORGANON_CLIENT_IP_HEADER`, `ORGANON_ENGINE_TIMEOUT` (default `10s`) |

- **User.** Both containers run as any non-root UID, and the image default is `1000:1000`. Use the same UID for
  both containers, because they share the socket. That UID should also own the data directory: rootless Podman
  maps your user onto the default with `--userns keep-id:uid=1000,gid=1000`.
- **Setup.**
  - `organon init --calendar-tz ZONE` creates a data directory. It refuses to overwrite an existing instance.
  - `organon token new` prints a token and its tokens-file line.
  - The tokens file has one `name scopes sha256` line per client; lines starting with `#` are comments.
- **No implicit initialization.** Without `organon.json`, the engine stays unhealthy and creates nothing.
  Keep `init` a separate, deliberate step. Running it automatically in the start command would turn a wrong
  or empty data mount into a new, empty instance: your data would seem gone, and new writes would land in
  the wrong place.
- **Changes.** A change to this interface is a breaking change. Its pull request title carries `!` (for
  example `feat(deploy)!: …`) and the `breaking` label, and CI enforces the title marker. To list them, run
  `git log --first-parent --grep '!:' origin/main`.
- **Tracking `:main`.** A stated interface does not stop images from changing underneath you. For a custom
  deployment, pin a `sha-` tag and update it through a reviewed change, for example with Renovate.

## Exposing it

The API binds to `127.0.0.1` by default. To reach it from elsewhere, put a TLS-terminating reverse proxy or a
tunnel in front, for example Caddy, Cloudflare Tunnel, Tailscale or WireGuard. Every endpoint except
`/healthz` requires a token.

Failed authentication attempts are rate-limited per client address, or per /64 network for IPv6. Behind a
proxy, every request appears to come from the proxy. Set `ORGANON_CLIENT_IP_HEADER` to the header that
carries the real client address, for example `CF-Connecting-IP` or `X-Forwarded-For`:

- Set it only when every request reaches the API through that proxy, because the header is trusted as given.
- For `X-Forwarded-For`, the last entry counts: it is the one your proxy appended.
- A value that is not an address falls back to the proxy's own address, which all clients then share.

## Your data

```
data/
├── organon.json        calendar_tz and settings (part of your data: back it up)
├── org/
│   ├── tasks/inbox.org new tasks go here
│   ├── projects/       one file per project; tasks under a level-1 heading belong to it
│   ├── knowledge/      notes (not used yet)
│   ├── journal/
│   └── archive/
└── attachments/
```

These are ordinary Org files: open them in Emacs or any text editor. While the stack runs, make changes
through the API. To edit by hand:
1. Stop the engine first (`docker compose stop engine`).
2. Edit the files.
3. Start the engine again.

## Backups

Organon does not back up your data. The data directory is the only thing that matters: the cache volume and
containers are rebuilt automatically. Recommended:

- **[restic](https://restic.net/)** (or borg) of the data directory, daily, to a repository **off the
  host**.
- On Btrfs or ZFS, take snapshots more often and back up from a snapshot.
- Files are written atomically (write to a temporary file, then rename), so a backup never contains a
  half-written file.
- Rehearse a restore:
  1. Restore into an empty directory.
  2. Point `ORGANON_DATA` at it.
  3. Run `docker compose up`.

Do not use Git to sync your notes; use it for code and configuration.

## Licenses in the image

Organon is [MIT](../LICENSE). The image also contains GNU Emacs and other Debian packages under their own
licenses (mostly GPL). The exact package versions are listed in the image at
`/usr/share/doc/organon-debian-packages.tsv`; sources are available from
[snapshot.debian.org](https://snapshot.debian.org/).
