#!/usr/bin/env bash
# Shared helpers for the Meerkat CRM verification hooks.

project_root() {
  if [ -n "${CLAUDE_PROJECT_DIR:-}" ] && [ -d "$CLAUDE_PROJECT_DIR" ]; then
    printf '%s' "$CLAUDE_PROJECT_DIR"
  else
    (cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
  fi
}

state_dir() {
  local d="${TMPDIR:-/tmp}/meerkat-crm-hooks"
  mkdir -p "$d" 2>/dev/null
  printf '%s' "$d"
}

# Content fingerprint of one area of the working tree: tracked modifications
# against HEAD plus the contents of untracked files. Tool-agnostic, so it also
# catches edits made through shell commands rather than the Edit tool.
area_fingerprint() {
  local area="$1"
  {
    git diff HEAD -- "$area" 2>/dev/null
    git ls-files --others --exclude-standard -- "$area" 2>/dev/null | sort | while IFS= read -r f; do
      shasum "$f" 2>/dev/null
    done
  } | shasum | cut -d' ' -f1
}
