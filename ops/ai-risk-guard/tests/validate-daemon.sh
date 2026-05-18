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

CASE_SHOW_CMD_TEMPLATE="${CASE_SHOW_CMD_TEMPLATE:-$RISKCTL show {case_id}}"
TOKEN_STATUS_CMD_TEMPLATE="${TOKEN_STATUS_CMD_TEMPLATE:-podman exec -i $DB_CONTAINER psql -U $DB_USER -d $DB_NAME -At -c \"SELECT status FROM tokens WHERE id={token_id};\"}"
USER_STATUS_CMD_TEMPLATE="${USER_STATUS_CMD_TEMPLATE:-podman exec -i $DB_CONTAINER psql -U $DB_USER -d $DB_NAME -At -c \"SELECT status FROM users WHERE id={user_id};\"}"

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

render_template() {
  local template="$1"
  local rendered="$template"
  rendered="${rendered//\{case_id\}/$CASE_ID}"
  rendered="${rendered//\{token_id\}/$TOKEN_ID}"
  rendered="${rendered//\{user_id\}/$USER_ID}"
  printf '%s\n' "$rendered"
}

run_template() {
  local label="$1"
  local template="$2"
  local output_file="$3"
  local command
  command="$(render_template "$template")"

  bash -c "$command" >"$output_file" 2>&1 || {
    sed -n '1,120p' "$output_file" >&2
    fail "$label command failed: $command"
  }
}

assert_status_2() {
  local label="$1"
  local output_file="$2"

  if grep -Eq "\"status\"[[:space:]]*:[[:space:]]*\"?$EXPECTED_STATUS\"?|status[[:space:]]*=[[:space:]]*\"?$EXPECTED_STATUS\"?|status:[[:space:]]*\"?$EXPECTED_STATUS\"?" "$output_file"; then
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

  sleep "$DAEMON_SETTLE_SECONDS"

  run_template "riskctl case show" "$CASE_SHOW_CMD_TEMPLATE" "$TMP_DIR/case-show.out"
  pass "riskctl case show works for $CASE_ID"

  run_template "token status query" "$TOKEN_STATUS_CMD_TEMPLATE" "$TMP_DIR/token-status.out"
  assert_status_2 "token $TOKEN_ID" "$TMP_DIR/token-status.out"

  run_template "user status query" "$USER_STATUS_CMD_TEMPLATE" "$TMP_DIR/user-status.out"
  assert_status_2 "user $USER_ID" "$TMP_DIR/user-status.out"
}

main "$@"
