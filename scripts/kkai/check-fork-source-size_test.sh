#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
CHECKER="$ROOT/scripts/kkai/check-fork-source-size.sh"
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/kkai-source-size-test.XXXXXX")
REPO="$TMP_ROOT/repo"

cleanup() {
  rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

write_lines() {
  local path=$1
  local count=$2
  mkdir -p "$(dirname "$path")"
  : >"$path"
  for ((line = 1; line <= count; line++)); do
    printf '// line %d\n' "$line" >>"$path"
  done
}

append_lines() {
  local path=$1
  local count=$2
  local start
  start=$(($(wc -l <"$path") + 1))
  for ((line = start; line < start + count; line++)); do
    printf '// line %d\n' "$line" >>"$path"
  done
}

reset_candidate() {
  git -C "$REPO" reset --hard --quiet "$BASE"
  git -C "$REPO" clean -fdq
}

expect_pass() {
  local label=$1
  git -C "$REPO" add -A
  if ! output=$("$REPO/scripts/kkai/check-fork-source-size.sh" "$BASE" 2>&1); then
    printf 'expected pass: %s\n%s\n' "$label" "$output" >&2
    exit 1
  fi
}

expect_fail() {
  local label=$1
  local expected=$2
  git -C "$REPO" add -A
  if output=$("$REPO/scripts/kkai/check-fork-source-size.sh" "$BASE" 2>&1); then
    printf 'expected failure: %s\n%s\n' "$label" "$output" >&2
    exit 1
  fi
  if [[ $output != *"$expected"* ]]; then
    printf 'wrong failure for %s; expected %q\n%s\n' "$label" "$expected" "$output" >&2
    exit 1
  fi
}

expect_untracked_fail() {
  local label=$1
  local expected=$2
  if output=$("$REPO/scripts/kkai/check-fork-source-size.sh" "$BASE" 2>&1); then
    printf 'expected untracked failure: %s\n%s\n' "$label" "$output" >&2
    exit 1
  fi
  if [[ $output != *"$expected"* ]]; then
    printf 'wrong failure for %s; expected %q\n%s\n' "$label" "$expected" "$output" >&2
    exit 1
  fi
}

mkdir -p "$REPO/scripts/kkai"
cp "$CHECKER" "$REPO/scripts/kkai/check-fork-source-size.sh"
git -C "$REPO" init --quiet
git -C "$REPO" config user.name 'KKAI Quality Test'
git -C "$REPO" config user.email 'quality-test@invalid.example'

write_lines "$REPO/legacy.ts" 20
write_lines "$REPO/overgrown.ts" 801
write_lines "$REPO/giant.ts" 1201
write_lines "$REPO/web/default/src/features/existing/index.ts" 100
git -C "$REPO" add .
git -C "$REPO" commit --quiet -m baseline
BASE=$(git -C "$REPO" rev-parse HEAD)

write_lines "$REPO/small.ts" 500
expect_pass '500-line added source boundary'
reset_candidate

write_lines "$REPO/too-large.ts" 501
expect_fail 'oversized added source' 'too-large.ts: 501 lines (limit 500)'
reset_candidate

write_lines "$REPO/untracked-too-large.ts" 501
expect_untracked_fail 'oversized untracked source' 'untracked-too-large.ts: 501 lines (limit 500)'
reset_candidate

write_lines "$REPO/web/default/src/features/new-feature/index.ts" 251
expect_fail 'oversized added frontend feature' 'index.ts: 251 lines (limit 250)'
reset_candidate

append_lines "$REPO/legacy.ts" 100
expect_pass '100-line existing-source addition boundary'
reset_candidate

append_lines "$REPO/legacy.ts" 101
expect_fail 'large addition to existing source' 'legacy.ts: 101 added lines (limit 100)'
reset_candidate

append_lines "$REPO/overgrown.ts" 26
expect_fail 'growth in overgrown upstream source' 'overgrown.ts: 26 added lines (limit 25)'
reset_candidate

append_lines "$REPO/giant.ts" 11
expect_fail 'growth in giant upstream source' 'giant.ts: 11 added lines (limit 10)'
reset_candidate

write_lines "$REPO/web/default/src/routeTree.gen.ts" 2000
expect_pass 'generated source exemption'

echo 'Fork source size gate tests passed.'
