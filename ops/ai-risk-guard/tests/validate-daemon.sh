#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -z "${RISKCTL:-}" && -x "$SCRIPT_DIR/../bin/riskctl" ]]; then
  RISKCTL="$SCRIPT_DIR/../bin/riskctl"
else
  RISKCTL="${RISKCTL:-riskctl}"
fi
CASE_ID_FILE="${CASE_ID_FILE:-$SCRIPT_DIR/.last-risk-case-id}"
CASE_ID="${CASE_ID:-}"
TOKEN_ID="${TOKEN_ID:-}"
USER_ID="${USER_ID:-}"
EXPECTED_STATUS="${EXPECTED_STATUS:-2}"
DAEMON_SETTLE_SECONDS="${DAEMON_SETTLE_SECONDS:-3}"
DB_CONTAINER="${DB_CONTAINER:-new-api-postgres}"
DB_NAME="${DB_NAME:-newapi}"
DB_USER="${DB_USER:-newapi}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

pass() {
  echo "PASS: $*"
}

require_value() {
  local name="$1"
  local value="$2"
  [[ -n "$value" ]] || fail "$name is required"
}

require_numeric_id() {
  local name="$1"
  local value="$2"
  [[ "$value" =~ ^[1-9][0-9]*$ ]] || fail "$name must be a positive integer"
}

require_numeric_status() {
  local value="$1"
  [[ "$value" =~ ^[0-9]+$ ]] || fail "EXPECTED_STATUS must be a non-negative integer"
}

require_case_id() {
  local value="$1"
  [[ "$value" =~ ^[A-Za-z0-9_.:-]{1,120}$ ]] || fail "CASE_ID contains unsafe characters"
}

run_command() {
  local label="$1"
  local output_file="$2"
  shift 2

  "$@" >"$output_file" 2>&1 || {
    sed -n '1,120p' "$output_file" >&2
    fail "$label command failed"
  }
}

assert_status_2() {
  local label="$1"
  local output_file="$2"

  if grep -Eq "^[[:space:]]*$EXPECTED_STATUS[[:space:]]*$|\"status\"[[:space:]]*:[[:space:]]*\"?$EXPECTED_STATUS\"?|status[[:space:]]*=[[:space:]]*\"?$EXPECTED_STATUS\"?|status:[[:space:]]*\"?$EXPECTED_STATUS\"?" "$output_file"; then
    pass "$label status=$EXPECTED_STATUS"
    return
  fi

  sed -n '1,120p' "$output_file" >&2
  fail "$label did not show status=$EXPECTED_STATUS"
}

main() {
  command -v "$RISKCTL" >/dev/null 2>&1 || fail "riskctl not found: $RISKCTL"

  if [[ -z "$CASE_ID" && -f "$CASE_ID_FILE" ]]; then
    CASE_ID="$(sed -n '1p' "$CASE_ID_FILE")"
  fi

  require_value "CASE_ID or CASE_ID_FILE" "$CASE_ID"
  require_value "TOKEN_ID" "$TOKEN_ID"
  require_value "USER_ID" "$USER_ID"
  require_case_id "$CASE_ID"
  require_numeric_id "TOKEN_ID" "$TOKEN_ID"
  require_numeric_id "USER_ID" "$USER_ID"
  require_numeric_status "$EXPECTED_STATUS"

  sleep "$DAEMON_SETTLE_SECONDS"

  run_command "riskctl case show" "$TMP_DIR/case-show.out" "$RISKCTL" show "$CASE_ID"
  pass "riskctl case show works for $CASE_ID"

  run_command "token status query" "$TMP_DIR/token-status.out" \
    podman exec -i "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -At \
    -c "SELECT status FROM tokens WHERE id=${TOKEN_ID};"
  assert_status_2 "token $TOKEN_ID" "$TMP_DIR/token-status.out"

  run_command "user status query" "$TMP_DIR/user-status.out" \
    podman exec -i "$DB_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -At \
    -c "SELECT status FROM users WHERE id=${USER_ID};"
  assert_status_2 "user $USER_ID" "$TMP_DIR/user-status.out"
}

main "$@"
