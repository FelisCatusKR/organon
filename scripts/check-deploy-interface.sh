#!/usr/bin/env bash
# Checks that compose.yaml and contrib/quadlet/ use exactly the container
# interface declared in contrib/container-interface.json: commands, health
# checks, mount targets, tmpfs, the engine's missing network, a read-only root
# and the environment variable names.  Needs jq and `docker compose`.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
iface="$root/contrib/container-interface.json"
errors=0

fail() { echo "deploy interface: $*" >&2; errors=$((errors + 1)); }

# expect DEFINITION WHAT EXPECTED ACTUAL
expect() { [[ "$3" == "$4" ]] || fail "$1: $2 is '$4', interface says '$3'"; }

want() { jq -r "$1" "$iface"; }
words() { jq -r '. | join(" ")'; }           # JSON array -> "a b c"
sorted() { tr ' ' '\n' | sed '/^$/d' | sort | tr '\n' ' ' | sed 's/ $//'; }

env_ok() {                                   # DEFINITION SERVICE "NAME..."
  local def=$1 svc=$2 names=$3 n
  for n in $(want ".$svc.environment.required[]"); do
    [[ " $names " == *" $n "* ]] || fail "$def: $svc does not set $n"
  done
  for n in $names; do
    want ".$svc.environment.required + .$svc.environment.optional | .[]" | grep -qx "$n" \
      || fail "$def: $svc sets $n, which the interface does not declare"
  done
}

# --- compose.yaml -------------------------------------------------------------
compose=$(cd "$root" && docker compose -f compose.yaml config --format json)
for svc in engine api; do
  s=$(jq ".services.$svc" <<<"$compose")
  expect compose.yaml "$svc command" "$(want ".$svc.command" | words)" "$(jq '.command' <<<"$s" | words)"
  expect compose.yaml "$svc healthcheck" "$(want ".$svc.healthcheck" | words)" "$(jq '.healthcheck.test[1:]' <<<"$s" | words)"
  expect compose.yaml "$svc mounts" "$(want ".$svc.mounts" | words | sorted)" "$(jq '[.volumes[].target]' <<<"$s" | words | sorted)"
  expect compose.yaml "$svc tmpfs" "$(want ".$svc.tmpfs" | words | sorted)" "$(jq '.tmpfs // []' <<<"$s" | words | sorted)"
  expect compose.yaml "$svc read_only" true "$(jq '.read_only' <<<"$s")"
  env_ok compose.yaml "$svc" "$(jq -r '.environment // {} | keys | join(" ")' <<<"$s")"
done
expect compose.yaml "engine network_mode" "$(want .engine.network)" "$(jq -r '.services.engine.network_mode' <<<"$compose")"

# --- contrib/quadlet/ ---------------------------------------------------------
quadlet_values() { sed -n "s/^$2=//p" "$root/contrib/quadlet/organon-$1.container"; }
for svc in engine api; do
  def="contrib/quadlet/organon-$svc.container"
  expect "$def" Exec "$(want ".$svc.command" | words)" "$(quadlet_values "$svc" Exec)"
  expect "$def" HealthCmd "$(want ".$svc.healthcheck" | words)" "$(quadlet_values "$svc" HealthCmd)"
  expect "$def" "Volume targets" "$(want ".$svc.mounts" | words | sorted)" \
    "$(quadlet_values "$svc" Volume | cut -d: -f2 | tr '\n' ' ' | sorted)"
  expect "$def" Tmpfs "$(want ".$svc.tmpfs" | words | sorted)" "$(quadlet_values "$svc" Tmpfs | tr '\n' ' ' | sorted)"
  expect "$def" ReadOnly true "$(quadlet_values "$svc" ReadOnly)"
  env_ok "$def" "$svc" "$(quadlet_values "$svc" Environment | cut -d= -f1 | tr '\n' ' ')"
done
expect contrib/quadlet/organon-engine.container Network "$(want .engine.network)" "$(quadlet_values engine Network)"

if (( errors )); then
  echo "deploy interface: $errors mismatch(es) with contrib/container-interface.json" >&2
  exit 1
fi
echo "deploy interface: compose.yaml and contrib/quadlet/ match contrib/container-interface.json"
