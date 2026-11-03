#!/usr/bin/env bash
# GitHub refuses to load an action whose input or output description holds a ${{ }} expression
# (the `github` context is not available there), which breaks the Action for every user, and
# actionlint does not read action.yml. Before `runs:`, an expression may only be a default or a
# value.
set -euo pipefail
file="${1:-action.yml}"
awk '
  /^runs:/ { exit }
  /^[[:space:]]*#/ { next }
  /\$\{\{/ && $1 != "default:" && $1 != "value:" {
    printf "%s:%d: an expression outside default: or value: (GitHub will not load the action)\n", FILENAME, NR
    bad = 1
  }
  END { exit bad }
' "$file"
