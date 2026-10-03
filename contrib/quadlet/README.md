# Podman Quadlet units (example)

These are the units the maintainer runs (rootless Podman 5, systemd user
session). They are an **example**: Docker Compose (`../../compose.yaml`) is the
supported deployment. CI keeps them valid with `quadlet -dryrun` and runs the
e2e suite under rootless Podman with the same constraints.

## Install

```sh
mkdir -p ~/.config/containers/systemd ~/organon/data
cp *.container *.volume ~/.config/containers/systemd/

# One-time setup: data directory and a token.
podman run --rm --userns keep-id:uid=1000,gid=1000 -v ~/organon/data:/data:Z \
  ghcr.io/feliscatuskr/organon:main organon init --calendar-tz Asia/Seoul
podman run --rm ghcr.io/feliscatuskr/organon:main \
  organon token new --name me --scopes read,tasks:write,nodes:write
# put the printed "tokens file line" into a file, then:
podman secret create organon-tokens ./tokens

systemctl --user daemon-reload
systemctl --user start organon-api.service   # starts the engine first
```

The units use `:main`, a development build of the latest commit on `main`; change `Image=` in both files to
a `:sha-<7>` tag to stay on a tested version.

Edit `organon-engine.container` if your data lives elsewhere than
`~/organon/data`. `UserNS=keep-id` makes files in the data directory belong to
your own user on the host.
