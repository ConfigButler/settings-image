#!/usr/bin/env bash

set -euo pipefail

workspace_dir="${1:-${containerWorkspaceFolder:-${WORKSPACE_FOLDER:-$(pwd)}}}"

# Refresh signing on every start: this is the first hook that can expect a forwarded SSH agent,
# and a reconnect can change SSH_AUTH_SOCK or the loaded key. Git itself refuses an unsigned
# commit, so startup never depends on this succeeding.
bash "${workspace_dir}/.devcontainer/sync-signing-key.sh" ||
  echo "[post-start] WARNING: sync-signing-key.sh failed; run it by hand to see why." >&2
