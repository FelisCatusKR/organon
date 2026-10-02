#!/usr/bin/env bash
# Run the Elisp (ERT) suite inside the organon:test image.  This is part of L1.
#
# Usage: scripts/test-elisp.sh [SELECTOR-REGEXP]
#
# Environment:
#   ORGANON_TEST_HOST=1      use the host's emacs + libfaketime instead of a container
#                            (faster while developing; versions must match the image)
#   ORGANON_UPDATE_GOLDEN=1  rewrite tests/golden/ instead of comparing (host mode only,
#                            because the container mounts the repository read-only)
#   CONTAINER_RUNTIME        podman or docker (default: whichever is installed)
#   ORGANON_TEST_IMAGE       image to use (default: organon:test, built if missing)
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
selector=${1:-}

# The engine never uses D-Bus, but Org's clock code probes logind over the
# system bus when it loads; under a frozen clock that probe never times out.
no_dbus=(DBUS_SYSTEM_BUS_ADDRESS=unix:path=/nonexistent DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent)

if [[ "${ORGANON_TEST_HOST:-}" == 1 ]]; then
  lib=$(ls /usr/lib/*/faketime/libfaketime.so.1 2>/dev/null | head -1)
  [[ -n "$lib" ]] || { echo "libfaketime not found; install the faketime package" >&2; exit 1; }
  clock=$(mktemp)
  trap 'rm -f "$clock"' EXIT
  exec env TZ=UTC "${no_dbus[@]}" LD_PRELOAD="$lib" FAKETIME_TIMESTAMP_FILE="$clock" FAKETIME_NO_CACHE=1 NO_FAKE_STAT=1 \
    ORGANON_TEST_SELECTOR="$selector" \
    emacs -q --batch -l "$root/emacs/test/run.el"
fi

if [[ -n "${ORGANON_UPDATE_GOLDEN:-}" ]]; then
  echo "ORGANON_UPDATE_GOLDEN requires ORGANON_TEST_HOST=1" >&2
  exit 2
fi

runtime=${CONTAINER_RUNTIME:-$(command -v podman || command -v docker || true)}
[[ -n "$runtime" ]] || { echo "need podman or docker" >&2; exit 1; }
image=${ORGANON_TEST_IMAGE:-organon:test}

if ! "$runtime" image inspect "$image" >/dev/null 2>&1; then
  echo "building $image ..." >&2
  "$runtime" build -q -f "$root/container/Containerfile" --target test -t "$image" "$root" >/dev/null
fi

exec "$runtime" run --rm \
  -v "$root:/src:ro" \
  -e TZ=UTC \
  -e "${no_dbus[0]}" -e "${no_dbus[1]}" \
  -e LD_PRELOAD=/usr/local/lib/libfaketime.so.1 \
  -e FAKETIME_TIMESTAMP_FILE=/tmp/organon-clock \
  -e FAKETIME_NO_CACHE=1 \
  -e NO_FAKE_STAT=1 \
  -e ORGANON_TEST_SELECTOR="$selector" \
  "$image" emacs -q --batch -l /src/emacs/test/run.el
