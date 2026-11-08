#!/usr/bin/env bash
# PostToolUse hook: cheap per-file checks right after a file is written.
# Formats Go in place; rejects locale files that are not valid JSON.
# Locale key parity is covered by frontend/src/i18n/localeParity.test.ts.
set -uo pipefail

input=$(cat)
file=$(printf '%s' "$input" | jq -r '.tool_response.filePath // .tool_input.file_path // empty' 2>/dev/null)
[ -n "$file" ] && [ -f "$file" ] || exit 0

case "$file" in
  *.go)
    if command -v gofmt >/dev/null 2>&1; then
      before=$(shasum "$file" 2>/dev/null | cut -d' ' -f1)
      gofmt -w "$file" 2>/dev/null
      after=$(shasum "$file" 2>/dev/null | cut -d' ' -f1)
      if [ "$before" != "$after" ]; then
        msg="format check: gofmt reformatted $(basename "$file") - re-read it before further edits"
        jq -n --arg m "$msg" \
          '{hookSpecificOutput:{hookEventName:"PostToolUse",additionalContext:$m}}'
      fi
    fi
    ;;
  */i18n/locales/*.json)
    if ! jq empty "$file" >/dev/null 2>&1; then
      printf 'Invalid JSON written to %s. Fix it before continuing.\n' "$file" >&2
      exit 2
    fi
    ;;
esac
exit 0
