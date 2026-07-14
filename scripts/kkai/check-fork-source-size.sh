#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BASE=${1:?usage: check-fork-source-size.sh <upstream-base>}

readonly FEATURE_LIMIT=250
readonly SOURCE_LIMIT=800

failures=()

while IFS= read -r path; do
  [[ -f "$ROOT/$path" ]] || continue

  case "$path" in
    *.gen.* | *_generated.go)
      continue
      ;;
    *.go | *.js | *.jsx | *.mjs | *.sh | *.ts | *.tsx)
      ;;
    *)
      continue
      ;;
  esac

  limit=$SOURCE_LIMIT
  if [[ $path == web/default/src/features/* ]]; then
    limit=$FEATURE_LIMIT
  fi

  lines=$(wc -l <"$ROOT/$path")
  if ((lines > limit)); then
    failures+=("$path: $lines lines (limit $limit)")
  fi
done < <(git -C "$ROOT" diff --name-only --diff-filter=A "$BASE")

if ((${#failures[@]} > 0)); then
  echo "Fork-owned source files exceed the maintainability limits:" >&2
  printf '  %s\n' "${failures[@]}" >&2
  echo "Split each file by domain responsibility before continuing." >&2
  exit 1
fi

echo "Fork-owned source size limits passed."
