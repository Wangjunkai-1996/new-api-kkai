#!/usr/bin/env bash
set -Eeuo pipefail

readonly IMAGE_REF="${IMAGE_REF:-}"
readonly EXPECTED_VERSION="${EXPECTED_VERSION:-}"
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

if [[ -z "${IMAGE_REF}" ]]; then
  echo "IMAGE_REF is required" >&2
  exit 64
fi
if [[ -z "${EXPECTED_VERSION}" ]]; then
  echo "EXPECTED_VERSION is required" >&2
  exit 64
fi

compose() {
  docker compose --project-name "${PROJECT_NAME}" --file "${COMPOSE_FILE}" "$@"
}

newapi_environment() {
  local container_id
  container_id="$(compose ps --quiet newapi)"
  [[ -n "${container_id}" ]]
  docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "${container_id}"
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

for command_name in docker grep; do
  command -v "${command_name}" >/dev/null || {
    echo "required command not found: ${command_name}" >&2
    exit 69
  }
done

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

umask 077
printf '%s\n' "${DATABASE_PASSWORD}" >"${TEST_ROOT}/database-password"
printf '%s\n' "${REDIS_PASSWORD}" >"${TEST_ROOT}/redis-password"
printf '%s\n' 'smoke-session-secret-0123456789abcdef' >"${TEST_ROOT}/session-secret"
printf '%s\n' 'smoke-crypto-secret-0123456789abcdef0' >"${TEST_ROOT}/crypto-secret"
printf '%s\n' 'smoke-invitations-secret-0123456789abc' >"${TEST_ROOT}/invitations-secret"
printf '%s\n' 'smoke-risk-signing-secret-0123456789ab' >"${TEST_ROOT}/risk-signing-secret"

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
compose up --detach --wait --wait-timeout 300 postgres redis

printf '%s\n' \
  "postgres://newapi_stage:${DATABASE_PASSWORD}@postgres:5432/newapi_stage?sslmode=disable" |
  docker run --rm --interactive --pull=never --platform linux/amd64 \
    --read-only --cap-drop=ALL --security-opt=no-new-privileges:true \
    --network "${PROJECT_NAME}_default" \
    --entrypoint /kkai-migrate "${IMAGE_REF}" \
    --dsn-stdin --timeout 2m

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

echo "Compose smoke passed for ${IMAGE_REF}"
echo "status: ${status_response}"
