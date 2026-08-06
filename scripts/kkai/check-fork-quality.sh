#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BASELINE_FILE="$ROOT/docs/kkai/upstream-baseline.env"
FULL=0

if [[ ${1:-} == "--full" ]]; then
  FULL=1
elif [[ $# -gt 0 ]]; then
  echo "Usage: $0 [--full]" >&2
  exit 2
fi

# shellcheck disable=SC1090
source "$BASELINE_FILE"
BASE=${KKAI_UPSTREAM_BASE:?missing KKAI_UPSTREAM_BASE}

cd "$ROOT"

if ! git cat-file -e "$BASE^{commit}" 2>/dev/null; then
  echo "Pinned upstream commit is unavailable: $BASE" >&2
  exit 1
fi

if ! git merge-base --is-ancestor "$BASE" HEAD; then
  echo "Candidate HEAD is not descended from pinned upstream $BASE" >&2
  exit 1
fi

ACTUAL_BUN_VERSION=$(bun --version)
if [[ $ACTUAL_BUN_VERSION != "$KKAI_BUN_VERSION" ]]; then
  echo "Bun version mismatch: expected $KKAI_BUN_VERSION, got $ACTUAL_BUN_VERSION" >&2
  exit 1
fi

if [[ ! -x "$ROOT/web/node_modules/.bin/oxlint" ]]; then
  echo "Frontend dependencies are missing; run 'cd web && bun install --frozen-lockfile'." >&2
  exit 1
fi

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/kkai-quality.XXXXXX")
BASE_TREE="$TMP_ROOT/upstream"

cleanup() {
  git worktree remove --force "$BASE_TREE" >/dev/null 2>&1 || true
  rm -rf "$TMP_ROOT"
}
trap cleanup EXIT

echo "[1/8] Checking fork ancestry and changed-file hygiene"
"$ROOT/scripts/kkai/check-fork-source-size_test.sh"
"$ROOT/scripts/kkai/check-fork-source-size.sh" "$BASE"

GOFMT_ISSUES="$TMP_ROOT/gofmt-issues.txt"
while IFS= read -r path; do
  [[ -f "$path" ]] || continue
  gofmt -l "$path" >>"$GOFMT_ISSUES"
done < <(git diff --name-only --diff-filter=ACMR "$BASE" -- '*.go')

if [[ -s "$GOFMT_ISSUES" ]]; then
  echo "Changed Go files are not gofmt-clean:" >&2
  sed 's/^/  /' "$GOFMT_ISSUES" >&2
  exit 1
fi

while IFS= read -r path; do
  [[ -f "$path" ]] || continue
  bash -n "$path"
done < <(git diff --name-only --diff-filter=ACMR "$BASE" -- '*.sh')
"$ROOT/scripts/kkai/check-frt-header-patch.sh"

echo "[2/8] Checking production image policy and runtime tools"
"$ROOT/build/kkai-image/test-policy.sh"
"$ROOT/scripts/kkai/build-manual-release_test.sh"
"$ROOT/scripts/kkai/deploy-manual-release_test.sh"
(
  cd "$ROOT/build/kkai-image"
  go test ./...
)

echo "[3/8] Testing and building frontend"
(
  cd "$ROOT/web"
  bun run test
  bun run i18n:test
  bun run i18n:check
  bun run typecheck
  bun run build
)

echo "[4/8] Checking formatting of fork-owned frontend changes"
bun "$ROOT/scripts/kkai/check-changed-format.mjs" "$BASE"

echo "[5/8] Preparing detached upstream baseline"
git worktree add --quiet --detach "$BASE_TREE" "$BASE"
ln -s "$ROOT/web/node_modules" "$BASE_TREE/web/node_modules"
mkdir -p "$BASE_TREE/web/dist"
printf '%s\n' '<!doctype html><title>quality baseline</title>' >"$BASE_TREE/web/dist/index.html"

echo "[6/8] Comparing frontend lint diagnostics with upstream"
OXLINT="$ROOT/web/node_modules/.bin/oxlint"
set +e
(
  cd "$BASE_TREE/web"
  "$OXLINT" -c .oxlintrc.json . --format json
) >"$TMP_ROOT/oxlint-base.json" 2>"$TMP_ROOT/oxlint-base.stderr"
BASE_LINT_STATUS=$?
(
  cd "$ROOT/web"
  "$OXLINT" -c .oxlintrc.json . --format json
) >"$TMP_ROOT/oxlint-current.json" 2>"$TMP_ROOT/oxlint-current.stderr"
CURRENT_LINT_STATUS=$?
set -e

if [[ $BASE_LINT_STATUS -gt 1 || $CURRENT_LINT_STATUS -gt 1 ]]; then
  cat "$TMP_ROOT/oxlint-base.stderr" "$TMP_ROOT/oxlint-current.stderr" >&2
  echo "Oxlint failed to execute." >&2
  exit 1
fi
bun "$ROOT/scripts/kkai/compare-diagnostics.mjs" \
  oxlint "$TMP_ROOT/oxlint-base.json" "$TMP_ROOT/oxlint-current.json"

echo "[7/8] Comparing Go vet diagnostics with upstream"
(cd "$BASE_TREE" && go mod download)
go mod download
set +e
(cd "$BASE_TREE" && go vet ./...) >"$TMP_ROOT/go-vet-base.txt" 2>&1
BASE_VET_STATUS=$?
(cd "$ROOT" && go vet ./...) >"$TMP_ROOT/go-vet-current.txt" 2>&1
CURRENT_VET_STATUS=$?
set -e

if [[ $BASE_VET_STATUS -gt 1 || $CURRENT_VET_STATUS -gt 1 ]]; then
  cat "$TMP_ROOT/go-vet-current.txt" >&2
  echo "Go vet failed to execute." >&2
  exit 1
fi
bun "$ROOT/scripts/kkai/compare-diagnostics.mjs" \
  go-vet "$TMP_ROOT/go-vet-base.txt" "$TMP_ROOT/go-vet-current.txt"

echo "[8/8] Running test suite"
if [[ $FULL -eq 1 ]]; then
  go test ./...
  go test -tags kkai_bridge ./...
else
  echo "Quick mode: skipped feature and kkai_bridge Go test suites; CI runs --full."
fi

echo "KKAI fork quality gate passed against $KKAI_UPSTREAM_LABEL ($BASE)."
