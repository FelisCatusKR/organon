#!/usr/bin/env bash
# End-to-end tests against a real stack (engine + api containers).
#
# Usage:
#   scripts/e2e.sh [--runtime podman|compose] [--image IMAGE] [--uid UID] [-- GO-TEST-ARGS...]
#   scripts/e2e.sh ctl VERB [ARGS...]      (used by the Go tests to drive faults)
#
# --runtime podman   containers started with the same constraints as the Quadlet
#                    units (rootless, keep-id, Network=none, read-only) [default]
# --runtime compose  compose.yaml, through `docker compose`, or `docker-compose` /
#                    `podman compose` against $DOCKER_HOST
# --uid UID          run both containers as UID instead of 1000 (spec: deployment,
#                    Custom UID)
#
# The scenarios live in api/e2e (Go, build tag "e2e"). This script only owns the
# stack: it creates a throw-away data directory, a token, starts the containers,
# runs `go test`, and tears everything down (the data directory included).
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work_base="$root/tests/e2e/.work"
state_file="$work_base/state"

# ---------------------------------------------------------------------------
# Runtime helpers (shared by the runner and `ctl`)

load_state() {
  [[ -f "$state_file" ]] || { echo "no running e2e stack ($state_file missing)" >&2; exit 1; }
  # shellcheck disable=SC1090
  source "$state_file"
}

compose() {
  local cmd
  if docker compose version >/dev/null 2>&1; then cmd=(docker compose)
  elif command -v docker-compose >/dev/null; then cmd=(docker-compose)
  else cmd=(podman compose); fi
  ORGANON_IMAGE="$image" ORGANON_DATA="$work/data" ORGANON_TOKENS="$work/tokens" \
  ORGANON_UID="$uid" ORGANON_GID="$uid" ORGANON_PORT="$port" ORGANON_USERNS_MODE="$userns_mode" \
    "${cmd[@]}" -p organon-e2e -f "$root/compose.yaml" --project-directory "$work" "$@"
}

engine_name() { [[ "$runtime" == compose ]] && echo organon-e2e-engine-1 || echo organon-e2e-engine; }
api_name() { [[ "$runtime" == compose ]] && echo organon-e2e-api-1 || echo organon-e2e-api; }

# Container CLI for exec/pause/kill: podman, or docker when compose runs on Docker.
cli() {
  if [[ "$runtime" == compose && -z "${DOCKER_HOST:-}" ]] && command -v docker >/dev/null; then docker "$@"
  else podman "$@"; fi
}

# Run a command with the privileges needed to touch files owned by the
# container user (they may belong to a subordinate UID under rootless Podman).
as_owner() {
  if [[ "$owner_access" == unshare ]]; then podman unshare "$@"
  elif [[ "$owner_access" == sudo ]]; then sudo "$@"
  else "$@"; fi
}

# Prints exactly one podman option.
podman_userns() {
  if [[ "$uid" == 1000 ]]; then echo "--userns=keep-id:uid=1000,gid=1000"
  else echo "--user=$uid:$uid"; fi
}

start_engine() {
  if [[ "$runtime" == compose ]]; then compose up -d engine; return; fi
  podman run -d --name organon-e2e-engine "$(podman_userns)" \
    --network none --read-only --tmpfs /tmp --cap-drop all --security-opt no-new-privileges \
    -v "$work/data:/data:Z" -v organon-e2e-cache:/cache -v organon-e2e-run:/run/organon \
    --health-cmd "organon rpc-ping" --health-interval 2s --health-start-period 120s \
    "$image" emacs -q --fg-daemon -l /opt/organon/emacs/init.el -f organon-start >/dev/null
}

start_api() {
  if [[ "$runtime" == compose ]]; then compose up -d api; return; fi
  podman run -d --name organon-e2e-api "$(podman_userns)" \
    --read-only --cap-drop all --security-opt no-new-privileges \
    -e ORGANON_LISTEN=0.0.0.0:8080 -e ORGANON_TOKENS_FILE=/tokens -e ORGANON_ENGINE_TIMEOUT=10s \
    -v "$work/tokens:/tokens:ro,Z" -v organon-e2e-run:/run/organon -p "127.0.0.1:$port:8080" \
    "$image" organon serve >/dev/null
}

wait_engine() {
  local deadline=$((SECONDS + 180))
  until cli exec "$(engine_name)" organon rpc-ping >/dev/null 2>&1; do
    (( SECONDS < deadline )) || { echo "engine did not become healthy" >&2; cli logs "$(engine_name)" | tail -20 >&2; return 1; }
    sleep 1
  done
}

wait_api() {
  local deadline=$((SECONDS + 60))
  until curl -sf "http://127.0.0.1:$port/healthz" >/dev/null; do
    (( SECONDS < deadline )) || { echo "api did not become healthy" >&2; cli logs "$(api_name)" | tail -20 >&2; return 1; }
    sleep 1
  done
}

stack_up() { start_engine; wait_engine; start_api; wait_api; }

kill_engine() {
  # `kill` returns before the container has stopped; a start issued in that
  # window finds it still running and does nothing.
  cli kill "$(engine_name)" >/dev/null
  cli wait "$(engine_name)" >/dev/null
}

restart_engine() {
  if [[ "$runtime" == compose ]]; then compose up -d engine >/dev/null
  else cli start "$(engine_name)" >/dev/null; fi
  wait_engine
}

stack_down() {
  # Removes containers and named volumes. Never touches $work/data.
  if [[ "$runtime" == compose ]]; then
    compose down -v --remove-orphans >/dev/null 2>&1 || true
  else
    podman rm -f organon-e2e-api organon-e2e-engine >/dev/null 2>&1 || true
    podman volume rm -f organon-e2e-cache organon-e2e-run >/dev/null 2>&1 || true
  fi
}

# ---------------------------------------------------------------------------
# ctl: actions the Go tests request

if [[ "${1:-}" == ctl ]]; then
  shift
  load_state
  verb=${1:?verb}; shift
  case "$verb" in
    pause-engine)   cli pause "$(engine_name)" >/dev/null ;;
    unpause-engine) cli unpause "$(engine_name)" >/dev/null ;;
    kill-engine)    kill_engine ;;
    start-engine)   restart_engine ;;
    drop-index)     # delete the org-roam database, then restart the engine
      cli exec "$(engine_name)" rm -f /cache/org-roam.db
      kill_engine; restart_engine ;;
    corrupt-index)  # replace the database with bytes that are not one, then restart
      # rm first: the running engine keeps its open (now unlinked) file.
      cli exec "$(engine_name)" sh -c 'rm -f /cache/org-roam.db && echo "not a database" > /cache/org-roam.db'
      kill_engine; restart_engine ;;
    recreate)       stack_down; stack_up ;;
    engine-interfaces)
      cli exec "$(engine_name)" cat /proc/net/dev | tail -n +3 | awk -F: '{gsub(/ /, "", $1); print $1}' ;;
    owner-violations)
      # Files in the data directory not owned by the container user.
      # keep-id maps the container user to the host user; otherwise (Docker,
      # or Podman with --user) the files carry the container UID itself.
      if [[ "$owner_access" == unshare ]]; then
        podman unshare find "$work/data" ! -uid "$uid" -print
      elif [[ "$userns_mode" == keep-id || ( "$runtime" == podman && "$uid" == 1000 ) ]]; then
        find "$work/data" ! -uid "$host_uid" -print
      else
        as_owner find "$work/data" ! -uid "$uid" -print
      fi ;;
    host-append)    # RELPATH TEXT: append to a data file as another process would
      printf '%s' "$2" | as_owner tee -a "$work/data/$1" >/dev/null ;;
    *) echo "unknown ctl verb: $verb" >&2; exit 2 ;;
  esac
  exit 0
fi

# ---------------------------------------------------------------------------
# Runner

runtime=podman
image=${ORGANON_E2E_IMAGE:-organon:dev}
uid=1000
port=${ORGANON_E2E_PORT:-18080}
go_args=()
while (( $# )); do
  case "$1" in
    --runtime) runtime=$2; shift 2 ;;
    --image) image=$2; shift 2 ;;
    --uid) uid=$2; shift 2 ;;
    --) shift; go_args=("$@"); break ;;
    *) echo "unknown argument $1" >&2; exit 2 ;;
  esac
done
[[ "$runtime" == podman || "$runtime" == compose ]] || { echo "--runtime must be podman or compose" >&2; exit 2; }

host_uid=$(id -u)
userns_mode=""
owner_access=none
if [[ "$runtime" == podman ]]; then
  [[ "$uid" != 1000 ]] && owner_access=unshare
else
  # compose on rootless Podman: map the host user like keep-id; on Docker,
  # a non-default UID needs root to prepare the data directory.
  if [[ -n "${DOCKER_HOST:-}" && "$DOCKER_HOST" == *podman* ]] || ! command -v docker >/dev/null; then
    userns_mode=keep-id
    [[ "$uid" != 1000 ]] && { echo "--uid with compose needs Docker (rootful)" >&2; exit 2; }
  elif [[ "$uid" != "$host_uid" ]]; then
    owner_access=sudo
  fi
fi

if ! cli image inspect "$image" >/dev/null 2>&1 && ! podman image inspect "$image" >/dev/null 2>&1; then
  echo "building $image ..." >&2
  podman build -q -f "$root/container/Containerfile" -t "$image" "$root" >/dev/null
fi

work="$work_base/run-$$"
mkdir -p "$work/data"
cat > "$state_file" <<EOF
runtime=$runtime
image=$image
uid=$uid
host_uid=$host_uid
port=$port
work=$work
userns_mode=$userns_mode
owner_access=$owner_access
EOF

cleanup() {
  stack_down
  as_owner rm -rf "$work" 2>/dev/null || true
  rm -f "$state_file"
}
trap cleanup EXIT
stack_down

# Data directory owned by the container user, initialized by the image itself.
if [[ "$owner_access" == unshare ]]; then podman unshare chown "$uid:$uid" "$work/data"
elif [[ "$owner_access" == sudo ]]; then sudo chown "$uid:$uid" "$work/data"; fi
if [[ "$runtime" == compose ]]; then
  compose run --rm --no-deps engine organon init --calendar-tz Asia/Seoul >/dev/null
  token_out=$(compose run --rm --no-deps engine organon token new --name e2e --scopes read,tasks:write,nodes:write)
else
  podman run --rm "$(podman_userns)" -v "$work/data:/data:Z" "$image" organon init --calendar-tz Asia/Seoul >/dev/null
  token_out=$(podman run --rm "$image" organon token new --name e2e --scopes read,tasks:write,nodes:write)
fi
token=$(sed -n 2p <<<"$token_out" | tr -d ' \r')
sed -n 4p <<<"$token_out" | sed 's/^ *//' | tr -d '\r' > "$work/tokens"
# A read-only token for scope tests.
if [[ "$runtime" == compose ]]; then
  ro_out=$(compose run --rm --no-deps engine organon token new --name e2e-read --scopes read)
else
  ro_out=$(podman run --rm "$image" organon token new --name e2e-read --scopes read)
fi
ro_token=$(sed -n 2p <<<"$ro_out" | tr -d ' \r')
sed -n 4p <<<"$ro_out" | sed 's/^ *//' | tr -d '\r' >> "$work/tokens"
# A note-taking token (no tasks:write) for scope tests.
if [[ "$runtime" == compose ]]; then
  notes_out=$(compose run --rm --no-deps engine organon token new --name e2e-notes --scopes read,nodes:write)
else
  notes_out=$(podman run --rm "$image" organon token new --name e2e-notes --scopes read,nodes:write)
fi
notes_token=$(sed -n 2p <<<"$notes_out" | tr -d ' \r')
sed -n 4p <<<"$notes_out" | sed 's/^ *//' | tr -d '\r' >> "$work/tokens"
chmod 644 "$work/tokens"

echo "e2e: runtime=$runtime image=$image uid=$uid" >&2
stack_up

cd "$root/api"
ORGANON_E2E_URL="http://127.0.0.1:$port" \
ORGANON_E2E_TOKEN="$token" \
ORGANON_E2E_READ_TOKEN="$ro_token" \
ORGANON_E2E_NOTES_TOKEN="$notes_token" \
ORGANON_E2E_DATA="$work/data" \
ORGANON_E2E_UID="$uid" \
ORGANON_E2E_CTL="$root/scripts/e2e.sh ctl" \
  go test -tags e2e -count=1 ./e2e/ "${go_args[@]}"
