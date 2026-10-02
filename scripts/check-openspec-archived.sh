#!/usr/bin/env bash
# Fail if an OpenSpec change has all its tasks done but has not been archived.
#
# A finished change is archived (`openspec archive <name>`, or /opsx:archive)
# as the last commit of its pull request, so that the merged spec deltas in
# openspec/specs/ are part of what gets reviewed. Changes still in progress
# pass.
set -euo pipefail

list=$(openspec list --json)
finished=$(jq -r '.changes[] | select(.totalTasks > 0 and .completedTasks == .totalTasks) | .name' <<<"$list")

if [[ -n "$finished" ]]; then
  echo "These changes have all tasks done but are not archived:" >&2
  sed 's/^/  - /' <<<"$finished" >&2
  echo "Archive them in this pull request: openspec archive <name> (or /opsx:archive)." >&2
  exit 1
fi
echo "openspec: no finished change is waiting to be archived"
