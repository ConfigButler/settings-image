#!/usr/bin/env bash

set -euo pipefail

log() {
  echo "[post-create] $*"
}

workspace_dir="${1:-${containerWorkspaceFolder:-${WORKSPACE_FOLDER:-$(pwd)}}}"
log "Using workspace directory: ${workspace_dir}"

# Git identity is developer personalization, not part of a usable environment, so its absence is
# reported and not fatal.
git_name="$(git config --get user.name || true)"
git_email="$(git config --get user.email || true)"

if [ -z "${git_name}" ] && [ -n "${GIT_USER_NAME:-}" ]; then
  git_name="${GIT_USER_NAME}"
fi

if [ -z "${git_email}" ] && [ -n "${GIT_USER_EMAIL:-}" ]; then
  git_email="${GIT_USER_EMAIL}"
fi

if [ -n "${git_name}" ] && [ -z "$(git config --global --get user.name || true)" ]; then
  git config --global user.name "${git_name}"
fi

if [ -n "${git_email}" ] && [ -z "$(git config --global --get user.email || true)" ]; then
  git config --global user.email "${git_email}"
fi

if [ -z "${git_name}" ] || [ -z "${git_email}" ]; then
  log "WARNING: no Git identity yet. Set user.name and user.email, or provide GIT_USER_NAME and"
  log "WARNING: GIT_USER_EMAIL to the devcontainer environment."
fi

# Signing is configured in post-start.sh, which runs on every start, when an SSH agent can be
# expected to exist.

log "Ensuring Go cache directories exist"
sudo mkdir -p \
  /home/vscode/.cache/go-build \
  /home/vscode/.cache/golangci-lint

if [ -d "${workspace_dir}" ]; then
  log "Fixing ownership for workspace and home directories"
  sudo chown -R vscode:vscode "${workspace_dir}" /home/vscode || true
else
  log "Workspace directory not found; fixing ownership for home only"
  sudo chown -R vscode:vscode /home/vscode || true
fi

# Persist ~/.claude.json across rebuilds by making it a symlink into the persisted volume.
touch /home/vscode/persisted-home/.claude.json
rm -f /home/vscode/.claude.json && ln -s /home/vscode/persisted-home/.claude.json /home/vscode/.claude.json

log "post-create completed"
