#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BASE=${1:?usage: check-fork-source-size.sh <upstream-base>}

readonly FEATURE_LIMIT=250
readonly SOURCE_LIMIT=500
readonly MODIFIED_SOURCE_ADDITION_LIMIT=100
readonly MODIFIED_FEATURE_ADDITION_LIMIT=50
readonly OVERGROWN_SOURCE_LINES=800
readonly OVERGROWN_ADDITION_LIMIT=25
readonly GIANT_SOURCE_LINES=1200
readonly GIANT_ADDITION_LIMIT=10

failures=()

is_generated() {
  local path=$1
  [[ $path == *.gen.* || $path == *_generated.go ]]
}

is_source() {
  local path=$1
  case "$path" in
    *.go | *.js | *.jsx | *.mjs | *.sh | *.ts | *.tsx)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

while IFS= read -r path; do
  [[ -f "$ROOT/$path" ]] || continue
  is_source "$path" || continue
  is_generated "$path" && continue

  if ! git -C "$ROOT" cat-file -e "$BASE:$path" 2>/dev/null; then
    limit=$SOURCE_LIMIT
    if [[ $path == web/default/src/features/* ]]; then
      limit=$FEATURE_LIMIT
    fi

    lines=$(( $(wc -l <"$ROOT/$path") ))
    if ((lines > limit)); then
      failures+=("$path: $lines lines (limit $limit)")
    fi
    continue
  fi

  baseline_lines=$(( $(git -C "$ROOT" show "$BASE:$path" | wc -l) ))
  addition_limit=$MODIFIED_SOURCE_ADDITION_LIMIT
  if [[ $path == web/default/src/features/* ]]; then
    addition_limit=$MODIFIED_FEATURE_ADDITION_LIMIT
  fi
  if ((baseline_lines >= GIANT_SOURCE_LINES && addition_limit > GIANT_ADDITION_LIMIT)); then
    addition_limit=$GIANT_ADDITION_LIMIT
  elif ((baseline_lines >= OVERGROWN_SOURCE_LINES && addition_limit > OVERGROWN_ADDITION_LIMIT)); then
    addition_limit=$OVERGROWN_ADDITION_LIMIT
  fi

  added=$(git -C "$ROOT" diff --numstat "$BASE" -- "$path" | awk 'NR == 1 { print $1 }')
  if [[ $added =~ ^[0-9]+$ ]] && ((added > addition_limit)); then
    failures+=("$path: $added added lines (limit $addition_limit)")
  fi
done < <(
  {
    git -C "$ROOT" diff --name-only --diff-filter=ACMR "$BASE"
    git -C "$ROOT" ls-files --others --exclude-standard
  } | sort -u
)

if ((${#failures[@]} > 0)); then
  echo "Fork changes exceed the maintainability limits:" >&2
  printf '  %s\n' "${failures[@]}" >&2
  echo "Split each file by domain responsibility before continuing." >&2
  exit 1
fi

echo "Fork source size and growth limits passed."
