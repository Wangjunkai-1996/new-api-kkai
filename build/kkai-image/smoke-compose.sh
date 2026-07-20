#!/usr/bin/env bash
set -Eeuo pipefail

readonly IMAGE_REF="${IMAGE_REF:-}"
readonly EXPECTED_VERSION="${EXPECTED_VERSION:-}"
readonly EXPECTED_SCHEMA_MANAGEMENT="${EXPECTED_SCHEMA_MANAGEMENT:-}"
readonly EXPECTED_SCHEMA_COMPATIBLE_PREFIXES="${EXPECTED_SCHEMA_COMPATIBLE_PREFIXES:-}"
readonly EXPECTED_SCHEMA_MIGRATION_TARGET="${EXPECTED_SCHEMA_MIGRATION_TARGET:-}"
readonly EXPECTED_SCHEMA_MIGRATION_KIND="${EXPECTED_SCHEMA_MIGRATION_KIND:-}"
readonly EXPECTED_SCHEMA_MIGRATION_SET_DIGEST="${EXPECTED_SCHEMA_MIGRATION_SET_DIGEST:-}"
readonly SCHEMA_BOOTSTRAP_BINARY="${SCHEMA_BOOTSTRAP_BINARY:-}"
readonly PULL_DEPENDENCIES="${PULL_DEPENDENCIES:-false}"
readonly POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:18.4-alpine@sha256:b6a16ed0eb96e2c362811f7eeb951eac8b459e7b40be4149ea5444aa7c65569b}"
readonly REDIS_IMAGE="${REDIS_IMAGE:-redis:8.6.3@sha256:48e78eb9d1e1adcfb10184b2cc3c7fc5ed21e5a3be08875f239257d194bab8c9}"
readonly PROJECT_NAME="kkai-newapi-smoke-${$}"
SCRIPT_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_ROOT
readonly COMPOSE_FILE="${SCRIPT_ROOT}/smoke-compose.yml"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kkai-newapi-compose-smoke.XXXXXX")"
readonly TEST_ROOT
readonly DATABASE_PASSWORD='smoke-database-password-0123456789abcdef'
readonly REDIS_PASSWORD='smoke-redis-password-0123456789abcdef'

for command_name in awk docker grep jq sed sha256sum; do
  command -v "${command_name}" >/dev/null || {
    echo "required command not found: ${command_name}" >&2
    exit 69
  }
done

if [[ -z "${IMAGE_REF}" ]]; then
  echo "IMAGE_REF is required" >&2
  exit 64
fi
if [[ -z "${EXPECTED_VERSION}" ]]; then
  echo "EXPECTED_VERSION is required" >&2
  exit 64
fi
[[ "${EXPECTED_SCHEMA_MANAGEMENT}" == external ]] || {
  echo "formal smoke requires EXPECTED_SCHEMA_MANAGEMENT=external" >&2
  exit 64
}
[[ -x "${SCHEMA_BOOTSTRAP_BINARY}" ]] || {
  echo "SCHEMA_BOOTSTRAP_BINARY must be an executable generic build" >&2
  exit 64
}
[[ "${EXPECTED_SCHEMA_MIGRATION_TARGET}" =~ ^[1-9][0-9]*$ ]] || {
  echo "EXPECTED_SCHEMA_MIGRATION_TARGET is invalid" >&2
  exit 64
}
[[ "${EXPECTED_SCHEMA_MIGRATION_TARGET}" == 3 ]] || {
  echo "PostgreSQL smoke requires the reviewed schema v3 target" >&2
  exit 64
}
[[ "${EXPECTED_SCHEMA_MIGRATION_KIND}" == none ]] || {
  echo "PostgreSQL smoke requires migration kind none" >&2
  exit 64
}
[[ "${EXPECTED_SCHEMA_MIGRATION_SET_DIGEST}" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo "EXPECTED_SCHEMA_MIGRATION_SET_DIGEST is invalid" >&2
  exit 64
}
canonical_compatible_prefixes="$(
  jq --compact-output --sort-keys \
    'if type == "object" and length > 0 and all(.[]; test("^sha256:[0-9a-f]{64}$")) then . else error("invalid compatible prefixes") end' \
    <<<"${EXPECTED_SCHEMA_COMPATIBLE_PREFIXES}"
)"
readonly canonical_compatible_prefixes

compose() {
  docker compose --project-name "${PROJECT_NAME}" --file "${COMPOSE_FILE}" "$@"
}

newapi_environment() {
  local container_id
  container_id="$(compose ps --quiet newapi)"
  [[ -n "${container_id}" ]]
  docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "${container_id}"
}

delivery_disabled_stage_version() {
  docker run --rm --pull=never --platform linux/amd64 \
    --read-only --cap-drop=ALL --security-opt=no-new-privileges:true \
    --env NEWAPI_DATABASE_HOST=postgres \
    --env NEWAPI_DATABASE_USER=newapi_stage \
    --env NEWAPI_DATABASE_NAME=newapi_stage \
    --env NEWAPI_REDIS_HOST=redis \
    --env NEWAPI_REDIS_USER=newapi_stage \
    --env NEWAPI_REDIS_DATABASE=0 \
    --env NEWAPI_DATABASE_PASSWORD_FILE=/run/secrets/database_password \
    --env NEWAPI_REDIS_PASSWORD_FILE=/run/secrets/redis_password \
    --env NEWAPI_SESSION_SECRET_FILE=/run/secrets/session_secret \
    --env NEWAPI_CRYPTO_SECRET_FILE=/run/secrets/crypto_secret \
    --env NEWAPI_INVITATIONS_INTERNAL_SECRET_FILE=/run/secrets/invitations_internal_secret \
    --env NEWAPI_RISK_STREAM_SECRET_FILE=/run/secrets/risk_signing_secret \
    --mount type=bind,src="${TEST_ROOT}/database-password",dst=/run/secrets/database_password,readonly \
    --mount type=bind,src="${TEST_ROOT}/redis-password",dst=/run/secrets/redis_password,readonly \
    --mount type=bind,src="${TEST_ROOT}/session-secret",dst=/run/secrets/session_secret,readonly \
    --mount type=bind,src="${TEST_ROOT}/crypto-secret",dst=/run/secrets/crypto_secret,readonly \
    --mount type=bind,src="${TEST_ROOT}/invitations-secret",dst=/run/secrets/invitations_internal_secret,readonly \
    --mount type=bind,src="${TEST_ROOT}/risk-signing-secret",dst=/run/secrets/risk_signing_secret,readonly \
    "${IMAGE_REF}" --version
}

run_migrator() {
  printf '%s\n' "${migration_dsn}" |
    docker run --rm --interactive --pull=never --platform linux/amd64 \
      --read-only --cap-drop=ALL --security-opt=no-new-privileges:true \
      --network "${PROJECT_NAME}_default" \
      --entrypoint /kkai-migrate "${IMAGE_REF}" \
      --dsn-stdin --timeout 2m
}

observe_schema() {
  printf '%s\n' "${migration_dsn}" |
    docker run --rm --interactive --pull=never --platform linux/amd64 \
      --read-only --cap-drop=ALL --security-opt=no-new-privileges:true \
      --network "${PROJECT_NAME}_default" \
      --entrypoint /kkai-migrate "${IMAGE_REF}" \
      --observe --current --json --dsn-stdin --timeout 1m
}

outbox_event_key_length() {
  compose exec --no-TTY postgres \
    psql --username=newapi_stage --dbname=newapi_stage --tuples-only --no-align \
    --command "SELECT character_maximum_length FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'kkai_outbox' AND column_name = 'event_key'"
}

schema_fingerprint() {
  compose exec --no-TTY postgres \
    pg_dump --username=newapi_stage --dbname=newapi_stage \
    --schema=public --schema-only --no-owner --no-privileges --no-comments |
    sed '/^\\restrict /d; /^\\unrestrict /d' |
    sha256sum | awk '{print $1}'
}

ledger_fingerprint() {
  compose exec --no-TTY postgres \
    psql --username=newapi_stage --dbname=newapi_stage --tuples-only --no-align \
    --command "COPY (SELECT version, name, checksum, applied_at, execution_ms FROM kkai_schema_migrations ORDER BY version) TO STDOUT WITH (FORMAT csv)" |
    sha256sum | awk '{print $1}'
}

bootstrap_application_schema() {
  local database_port
  database_port="$(compose port postgres 5432 | awk -F: 'NR == 1 {print $NF}')"
  printf '%s\n' \
    "postgres://newapi_stage:${DATABASE_PASSWORD}@127.0.0.1:${database_port}/newapi_stage?sslmode=disable" |
    "${SCHEMA_BOOTSTRAP_BINARY}" --dsn-stdin
}

application_business_row_count() {
  compose exec --no-TTY postgres \
    psql --username=newapi_stage --dbname=newapi_stage --tuples-only --no-align \
    --command 'SELECT (SELECT count(*) FROM users) + (SELECT count(*) FROM setups) + (SELECT count(*) FROM options)'
}

cleanup() {
  local exit_status="$?"
  trap - EXIT

  if [[ "${exit_status}" -ne 0 ]]; then
    compose ps >&2 || true
    compose logs --no-color >&2 || true
  fi

  compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf -- "${TEST_ROOT}"
  exit "${exit_status}"
}
trap cleanup EXIT

case "${PULL_DEPENDENCIES}" in
  true)
    docker pull --platform linux/amd64 "${POSTGRES_IMAGE}" >/dev/null
    docker pull --platform linux/amd64 "${REDIS_IMAGE}" >/dev/null
    ;;
  false) ;;
  *)
    echo "PULL_DEPENDENCIES must be true or false" >&2
    exit 64
    ;;
esac

docker image inspect "${IMAGE_REF}" "${POSTGRES_IMAGE}" "${REDIS_IMAGE}" >/dev/null
image_contract_json="$(
  docker run --rm --pull=never --platform linux/amd64 --entrypoint /kkai-migrate "${IMAGE_REF}" \
    --describe-contract --dialect postgres --json
)"
readonly image_contract_json
image_schema_management="$(docker image inspect --format '{{index .Config.Labels "com.kkai.runtime.schema-management"}}' "${IMAGE_REF}")"
readonly image_schema_management
image_compatible_prefixes="$(docker image inspect --format '{{index .Config.Labels "com.kkai.schema.compatible-prefixes"}}' "${IMAGE_REF}")"
readonly image_compatible_prefixes
canonical_image_compatible_prefixes="$(jq --compact-output --sort-keys . <<<"${image_compatible_prefixes}")"
readonly canonical_image_compatible_prefixes
[[ "${image_compatible_prefixes}" == "${canonical_image_compatible_prefixes}" ]]
[[ "${image_schema_management}" == "${EXPECTED_SCHEMA_MANAGEMENT}" ]]
[[ "${image_schema_management}" == "$(jq --raw-output '.schema_management' <<<"${image_contract_json}")" ]]
[[ "${canonical_image_compatible_prefixes}" == "${canonical_compatible_prefixes}" ]]
[[ "${canonical_image_compatible_prefixes}" == "$(jq --compact-output --sort-keys '.compatible_prefixes' <<<"${image_contract_json}")" ]]
jq --exit-status \
  --argjson target "${EXPECTED_SCHEMA_MIGRATION_TARGET}" \
  --arg kind "${EXPECTED_SCHEMA_MIGRATION_KIND}" \
  --arg digest "${EXPECTED_SCHEMA_MIGRATION_SET_DIGEST}" \
  '.migration_target_version == $target and .migration_kind == $kind and
   .migration_set_digest == $digest' <<<"${image_contract_json}" >/dev/null

umask 077
printf '%s\n' "${DATABASE_PASSWORD}" >"${TEST_ROOT}/database-password"
printf '%s\n' "${REDIS_PASSWORD}" >"${TEST_ROOT}/redis-password"
printf '%s\n' 'smoke-session-secret-0123456789abcdef' >"${TEST_ROOT}/session-secret"
printf '%s\n' 'smoke-crypto-secret-0123456789abcdef0' >"${TEST_ROOT}/crypto-secret"
printf '%s\n' 'smoke-invitations-secret-0123456789abc' >"${TEST_ROOT}/invitations-secret"
printf '%s\n' 'smoke-rebate-ingest-secret-0123456789' >"${TEST_ROOT}/rebate-event-ingest-secret"
printf '%s\n' 'smoke-risk-signing-secret-0123456789ab' >"${TEST_ROOT}/risk-signing-secret"
chmod 0444 \
  "${TEST_ROOT}/database-password" \
  "${TEST_ROOT}/redis-password" \
  "${TEST_ROOT}/session-secret" \
  "${TEST_ROOT}/crypto-secret" \
  "${TEST_ROOT}/invitations-secret" \
  "${TEST_ROOT}/rebate-event-ingest-secret" \
  "${TEST_ROOT}/risk-signing-secret"

{
  printf '%s\n' \
    'bind 0.0.0.0' \
    'protected-mode yes' \
    'port 6379' \
    'aclfile /run/secrets/redis-users.acl' \
    'dir /data' \
    'save ""' \
    'appendonly no' \
    'logfile ""' \
    'daemonize no'
} >"${TEST_ROOT}/redis.conf"
{
  printf '%s\n' \
    'user default off' \
    'user health on nopass ~* +ping' \
    "user newapi_stage on >${REDIS_PASSWORD} ~* +@all"
} >"${TEST_ROOT}/redis-users.acl"
chmod 0444 "${TEST_ROOT}/redis.conf" "${TEST_ROOT}/redis-users.acl"

export IMAGE_REF POSTGRES_IMAGE REDIS_IMAGE TEST_ROOT
compose config --quiet
stage_version="$(delivery_disabled_stage_version)"
readonly stage_version
grep -Fx "${EXPECTED_VERSION}" <<<"${stage_version}" >/dev/null
compose up --detach --wait --wait-timeout 300 postgres

migration_dsn="postgres://newapi_stage:${DATABASE_PASSWORD}@postgres:5432/newapi_stage?sslmode=disable"
readonly migration_dsn
run_migrator
bootstrap_application_schema
[[ "$(application_business_row_count)" == 0 ]]

compose up --detach --wait --wait-timeout 300 redis

schema_observation="$(observe_schema)"
readonly schema_observation
jq --exit-status \
  --argjson prefixes "${EXPECTED_SCHEMA_COMPATIBLE_PREFIXES}" \
  --argjson target "${EXPECTED_SCHEMA_MIGRATION_TARGET}" \
  --arg digest "${EXPECTED_SCHEMA_MIGRATION_SET_DIGEST}" \
  'keys == ["current_version", "migration_set_digest", "schema"] and
   .schema == 1 and .current_version == $target and
   .migration_set_digest == $digest and
   .migration_set_digest == $prefixes[($target | tostring)]' \
  <<<"${schema_observation}" >/dev/null

event_key_length="$(outbox_event_key_length)"
readonly event_key_length
[[ "${event_key_length}" == 192 ]]

schema_fingerprint_before="$(schema_fingerprint)"
readonly schema_fingerprint_before
ledger_fingerprint_before="$(ledger_fingerprint)"
readonly ledger_fingerprint_before

run_migrator

NEWAPI_NODE_TYPE=master NEWAPI_NODE_ROLE=leader \
  compose up --detach --no-deps --force-recreate --wait --wait-timeout 300 newapi
leader_environment="$(newapi_environment)"
readonly leader_environment
grep -Fx 'KKAI_NODE_ROLE=leader' <<<"${leader_environment}" >/dev/null
grep -Fx 'DISABLE_BACKGROUND_TASKS=true' <<<"${leader_environment}" >/dev/null

NEWAPI_NODE_TYPE=slave NEWAPI_NODE_ROLE=serving \
  compose up --detach --no-deps --force-recreate --wait --wait-timeout 300 newapi
serving_environment="$(newapi_environment)"
readonly serving_environment
grep -Fx 'KKAI_NODE_ROLE=serving' <<<"${serving_environment}" >/dev/null
grep -Fx 'DISABLE_BACKGROUND_TASKS=true' <<<"${serving_environment}" >/dev/null

status_response="$(
  compose exec --no-TTY newapi \
    /usr/bin/wget --quiet --output-document=- http://127.0.0.1:3000/api/status
)"
readonly status_response

grep -F '"success":true' <<<"${status_response}" >/dev/null
grep -F "\"version\":\"${EXPECTED_VERSION}\"" <<<"${status_response}" >/dev/null

final_schema_observation="$(observe_schema)"
readonly final_schema_observation
[[ "${final_schema_observation}" == "${schema_observation}" ]]
schema_fingerprint_after="$(schema_fingerprint)"
readonly schema_fingerprint_after
ledger_fingerprint_after="$(ledger_fingerprint)"
readonly ledger_fingerprint_after
[[ "${schema_fingerprint_after}" == "${schema_fingerprint_before}" ]]
[[ "${ledger_fingerprint_after}" == "${ledger_fingerprint_before}" ]]
[[ "$(outbox_event_key_length)" == 192 ]]

echo "Compose smoke passed for ${IMAGE_REF}"
echo "status: ${status_response}"
