#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURE_DIR="${FIXTURE_DIR:-$SCRIPT_DIR/fixtures}"

BASE_URL="${BASE_URL:-http://127.0.0.1}"
NORMAL_API_KEY="${NORMAL_API_KEY:-local-normal-risk-guard-test-key}"
RISK_API_KEY="${RISK_API_KEY:-local-high-risk-guard-test-key}"
RISK_GAME_API_KEY="${RISK_GAME_API_KEY:-local-high-risk-game-guard-test-key}"
RISK_PWN_API_KEY="${RISK_PWN_API_KEY:-local-high-risk-pwn-guard-test-key}"
RISK_CHAT_API_KEY="${RISK_CHAT_API_KEY:-local-high-risk-chat-guard-test-key}"
RISK_COMPLETIONS_API_KEY="${RISK_COMPLETIONS_API_KEY:-local-high-risk-completions-guard-test-key}"
CLIENT_IP="${CLIENT_IP:-203.0.113.44}"
NORMAL_CLIENT_IP="${NORMAL_CLIENT_IP:-203.0.113.45}"
RISK_GAME_CLIENT_IP="${RISK_GAME_CLIENT_IP:-203.0.113.46}"
RISK_PWN_CLIENT_IP="${RISK_PWN_CLIENT_IP:-203.0.113.47}"
RISK_CHAT_CLIENT_IP="${RISK_CHAT_CLIENT_IP:-203.0.113.48}"
RISK_COMPLETIONS_CLIENT_IP="${RISK_COMPLETIONS_CLIENT_IP:-203.0.113.49}"
CONNECT_TIMEOUT="${CONNECT_TIMEOUT:-3}"
MAX_TIME="${MAX_TIME:-15}"
LOCAL_403_MARKER="${LOCAL_403_MARKER:-}"
CASE_ID_FILE="${CASE_ID_FILE:-$SCRIPT_DIR/.last-risk-case-id}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

pass() {
  echo "PASS: $*"
}

curl_request() {
  local name="$1"
  local method="$2"
  local path="$3"
  local api_key="$4"
  local client_ip="$5"
  local body_file="${6:-}"
  local header_file="$TMP_DIR/$name.headers"
  local body_out="$TMP_DIR/$name.body"
  local status_file="$TMP_DIR/$name.status"

  local args=(
    --silent
    --show-error
    --connect-timeout "$CONNECT_TIMEOUT"
    --max-time "$MAX_TIME"
    --request "$method"
    --dump-header "$header_file"
    --output "$body_out"
    --write-out "%{http_code}"
    --header "Authorization: Bearer $api_key"
    --header "X-Forwarded-For: $client_ip"
    --header "Content-Type: application/json"
    "$BASE_URL$path"
  )

  if [[ -n "$body_file" ]]; then
    args+=(--data-binary "@$body_file")
  fi

  curl "${args[@]}" >"$status_file"
  printf '%s\n' "$header_file" "$body_out" "$status_file"
}

curl_multipart_request() {
  local name="$1"
  local method="$2"
  local path="$3"
  local api_key="$4"
  local client_ip="$5"
  local file_fixture="$6"
  local header_file="$TMP_DIR/$name.headers"
  local body_out="$TMP_DIR/$name.body"
  local status_file="$TMP_DIR/$name.status"

  curl \
    --silent \
    --show-error \
    --connect-timeout "$CONNECT_TIMEOUT" \
    --max-time "$MAX_TIME" \
    --request "$method" \
    --dump-header "$header_file" \
    --output "$body_out" \
    --write-out "%{http_code}" \
    --header "Authorization: Bearer $api_key" \
    --header "X-Forwarded-For: $client_ip" \
    --form "purpose=risk-guard-test" \
    --form "prompt=Please summarize this harmless uploaded fixture." \
    --form "file=@$file_fixture;type=application/octet-stream" \
    "$BASE_URL$path" >"$status_file"

  printf '%s\n' "$header_file" "$body_out" "$status_file"
}

http_status() {
  tr -d '\r\n' <"$1"
}

assert_not_blocked() {
  local label="$1"
  local status="$2"
  local body_file="$3"

  if [[ "$status" == "403" ]]; then
    echo "Response body:" >&2
    sed -n '1,80p' "$body_file" >&2
    fail "$label was blocked with HTTP 403"
  fi

  pass "$label not locally blocked (HTTP $status)"
}

assert_local_403() {
  local label="$1"
  local status="$2"
  local body_file="$3"

  [[ "$status" == "403" ]] || {
    echo "Response body:" >&2
    sed -n '1,80p' "$body_file" >&2
    fail "$label expected HTTP 403, got HTTP $status"
  }

  if [[ -n "$LOCAL_403_MARKER" ]] && ! grep -Fq "$LOCAL_403_MARKER" "$body_file"; then
    echo "Response body:" >&2
    sed -n '1,80p' "$body_file" >&2
    fail "$label returned 403 but did not include LOCAL_403_MARKER=$LOCAL_403_MARKER"
  fi

  pass "$label returned local HTTP 403"
}

extract_case_id() {
  local header_file="$1"
  local body_file="$2"
  local found=""

  found="$(
    awk 'BEGIN{IGNORECASE=1} /^x-risk-case-id:/ {gsub("\r", "", $0); sub(/^[^:]+:[[:space:]]*/, "", $0); print; exit}' "$header_file"
  )"

  if [[ -z "$found" ]]; then
    found="$(
      sed -nE 's/.*"(case_id|caseId|risk_case_id)"[[:space:]]*:[[:space:]]*"([^"]+)".*/\2/p' "$body_file" | sed -n '1p'
    )"
  fi

  if [[ -n "$found" ]]; then
    printf '%s\n' "$found" >"$CASE_ID_FILE"
    pass "captured risk case id $found"
  else
    echo "WARN: no risk case id found in response headers/body; daemon validation may need CASE_ID set manually" >&2
  fi
}

main() {
  local required_fixtures=(
    normal-responses.json
    high-risk-ananta-cracker.json
    high-risk-game-frida-zygisk.json
    high-risk-pwn-tcache-free-hook.json
    high-risk-pwn-rop-flag-chat.json
    high-risk-completions-rop-flag.json
    false-positive-rpc-random.json
    false-positive-responses-system-tool-output.json
    false-positive-chat-non-user-history.json
    false-positive-generic-single-term.json
    false-positive-multipart-file-pwn.txt
  )
  for fixture in "${required_fixtures[@]}"; do
    [[ -f "$FIXTURE_DIR/$fixture" ]] || fail "missing fixture: $FIXTURE_DIR/$fixture"
  done

  local oversize_fixture="$TMP_DIR/false-positive-oversize.json"
  python3 - "$oversize_fixture" <<'PY'
import json
import sys

body = "Harmless Codex large-context regression sample. " + ("normal text " * 240000)
payload = {
    "model": "gpt-5.4",
    "input": [{"role": "user", "content": body}],
    "max_output_tokens": 16,
}
with open(sys.argv[1], "w", encoding="utf-8") as fh:
    json.dump(payload, fh)
PY

  mapfile -t models_result < <(curl_request "normal_models" "GET" "/v1/models" "$NORMAL_API_KEY" "$NORMAL_CLIENT_IP")
  assert_not_blocked "normal /v1/models" "$(http_status "${models_result[2]}")" "${models_result[1]}"

  mapfile -t responses_result < <(curl_request "normal_responses" "POST" "/v1/responses" "$NORMAL_API_KEY" "$NORMAL_CLIENT_IP" "$FIXTURE_DIR/normal-responses.json")
  assert_not_blocked "normal /v1/responses" "$(http_status "${responses_result[2]}")" "${responses_result[1]}"

  mapfile -t rpc_random_result < <(curl_request "false_positive_rpc_random" "POST" "/v1/responses" "$NORMAL_API_KEY" "$NORMAL_CLIENT_IP" "$FIXTURE_DIR/false-positive-rpc-random.json")
  assert_not_blocked "false-positive _RPC5 random string" "$(http_status "${rpc_random_result[2]}")" "${rpc_random_result[1]}"

  mapfile -t responses_history_result < <(curl_request "false_positive_responses_history" "POST" "/v1/responses" "$NORMAL_API_KEY" "$NORMAL_CLIENT_IP" "$FIXTURE_DIR/false-positive-responses-system-tool-output.json")
  assert_not_blocked "false-positive /v1/responses system/developer/assistant/tool/patch output" "$(http_status "${responses_history_result[2]}")" "${responses_history_result[1]}"

  mapfile -t chat_history_result < <(curl_request "false_positive_chat_history" "POST" "/v1/chat/completions" "$NORMAL_API_KEY" "$NORMAL_CLIENT_IP" "$FIXTURE_DIR/false-positive-chat-non-user-history.json")
  assert_not_blocked "false-positive /v1/chat/completions non-user history" "$(http_status "${chat_history_result[2]}")" "${chat_history_result[1]}"

  mapfile -t generic_single_result < <(curl_request "false_positive_generic_single_term" "POST" "/v1/responses" "$NORMAL_API_KEY" "$NORMAL_CLIENT_IP" "$FIXTURE_DIR/false-positive-generic-single-term.json")
  assert_not_blocked "false-positive single generic technical term" "$(http_status "${generic_single_result[2]}")" "${generic_single_result[1]}"

  mapfile -t multipart_result < <(curl_multipart_request "false_positive_multipart_file_pwn" "POST" "/v1/files" "$NORMAL_API_KEY" "$NORMAL_CLIENT_IP" "$FIXTURE_DIR/false-positive-multipart-file-pwn.txt")
  assert_not_blocked "false-positive multipart file content ignored" "$(http_status "${multipart_result[2]}")" "${multipart_result[1]}"

  mapfile -t oversize_result < <(curl_request "false_positive_oversize" "POST" "/v1/responses" "$NORMAL_API_KEY" "$NORMAL_CLIENT_IP" "$oversize_fixture")
  assert_not_blocked "false-positive oversize harmless context" "$(http_status "${oversize_result[2]}")" "${oversize_result[1]}"

  mapfile -t high_risk_result < <(curl_request "high_risk" "POST" "/v1/responses" "$RISK_API_KEY" "$CLIENT_IP" "$FIXTURE_DIR/high-risk-ananta-cracker.json")
  assert_local_403 "high-risk AnantaCracker/DumpedLua/tolua sample" "$(http_status "${high_risk_result[2]}")" "${high_risk_result[1]}"
  extract_case_id "${high_risk_result[0]}" "${high_risk_result[1]}"

  mapfile -t repeat_result < <(curl_request "repeat_block" "GET" "/v1/models" "$RISK_API_KEY" "$CLIENT_IP")
  assert_local_403 "repeat same IP/key" "$(http_status "${repeat_result[2]}")" "${repeat_result[1]}"

  mapfile -t game_runtime_result < <(curl_request "high_risk_game_runtime" "POST" "/v1/responses" "$RISK_GAME_API_KEY" "$RISK_GAME_CLIENT_IP" "$FIXTURE_DIR/high-risk-game-frida-zygisk.json")
  assert_local_403 "high-risk frida/xposed/zygisk hook dump login/role sample" "$(http_status "${game_runtime_result[2]}")" "${game_runtime_result[1]}"

  mapfile -t pwn_tcache_result < <(curl_request "high_risk_pwn_tcache" "POST" "/v1/responses" "$RISK_PWN_API_KEY" "$RISK_PWN_CLIENT_IP" "$FIXTURE_DIR/high-risk-pwn-tcache-free-hook.json")
  assert_local_403 "high-risk tcache poisoning __free_hook sample" "$(http_status "${pwn_tcache_result[2]}")" "${pwn_tcache_result[1]}"

  mapfile -t pwn_chat_result < <(curl_request "high_risk_pwn_chat_rop" "POST" "/v1/chat/completions" "$RISK_CHAT_API_KEY" "$RISK_CHAT_CLIENT_IP" "$FIXTURE_DIR/high-risk-pwn-rop-flag-chat.json")
  assert_local_403 "high-risk chat ROP open read write sample" "$(http_status "${pwn_chat_result[2]}")" "${pwn_chat_result[1]}"

  mapfile -t pwn_completions_result < <(curl_request "high_risk_completions_rop" "POST" "/v1/completions" "$RISK_COMPLETIONS_API_KEY" "$RISK_COMPLETIONS_CLIENT_IP" "$FIXTURE_DIR/high-risk-completions-rop-flag.json")
  assert_local_403 "high-risk completions prompt ROP open read write sample" "$(http_status "${pwn_completions_result[2]}")" "${pwn_completions_result[1]}"
}

main "$@"
