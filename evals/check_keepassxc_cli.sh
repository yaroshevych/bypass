#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || ! -x "$1" ]]; then
  echo "Usage: $0 /path/to/keepassxc-cli" >&2
  exit 2
fi

cli="$1"
version_output="$("$cli" --version 2>&1)" || {
  echo "keepassxc-cli --version failed: $cli" >&2
  exit 1
}
help_output="$("$cli" --help 2>&1)" || {
  echo "keepassxc-cli --help failed: $cli" >&2
  exit 1
}

commands=(
# Keep this list aligned with internal/vault/capabilities.go.
  add analyze attachment-export attachment-import attachment-rm clip close
  db-create db-edit db-info diceware edit estimate export generate help import
  ls merge mkdir mv open rm rmdir search show
)

failed=0
for command in "${commands[@]}"; do
  if ! awk -v expected="$command" '$1 == expected { found = 1 } END { exit !found }' <<< "$help_output"; then
    printf 'Missing command: %s\n' "$command" >&2
    failed=1
    continue
  fi
  if ! "$cli" help "$command" >/dev/null 2>&1; then
    printf 'Command help failed: %s\n' "$command" >&2
    failed=1
  fi
done

if [[ "$failed" -ne 0 ]]; then
  printf 'Compatibility check failed for %s (%s).\n' "$cli" "$version_output" >&2
  exit 1
fi

printf 'Compatible: %s (%d required commands)\n' "$version_output" "${#commands[@]}"
