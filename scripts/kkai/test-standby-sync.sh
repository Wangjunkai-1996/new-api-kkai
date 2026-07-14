#!/usr/bin/env bash
set -Eeuo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly root
readonly postgres_image="postgres:18.4-bookworm"
readonly postgres_container="kkai-newapi-standby-sync-pg-$$"
readonly writer_role="newapi_sync_writer"
readonly standby_role="newapi_sync_standby"
readonly database="newapi_sync"
readonly writer_password="test-only-writer-password"
readonly standby_password="test-only-standby-password"
readonly admin_password="test-only-admin-password"
readonly model_name="zz-standby-sync-model"
readonly channel_id="990001"
readonly sync_frequency_seconds="1"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/kkai-newapi-standby-sync.XXXXXX")"
readonly test_root
readonly newapi_binary="${test_root}/new-api"
readonly migrate_binary="${test_root}/kkai-migrate"
leader_pid=''
standby_pid=''

cleanup() {
  for pid in "${standby_pid}" "${leader_pid}"; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
      kill -TERM "${pid}" >/dev/null 2>&1 || true
      wait "${pid}" >/dev/null 2>&1 || true
    fi
  done
  docker rm --force "${postgres_container}" >/dev/null 2>&1 || true
  rm -rf -- "${test_root}"
}
trap cleanup EXIT

free_port() {
  ruby -rsocket -e 'server = TCPServer.new("127.0.0.1", 0); puts server.addr[1]; server.close'
}

admin_sql() {
  local target_database="$1"
  local sql="$2"
  docker exec "${postgres_container}" psql \
    --username=postgres \
    --dbname="${target_database}" \
    --no-psqlrc \
    --set=ON_ERROR_STOP=1 \
    --tuples-only \
    --no-align \
    --command="${sql}"
}

writer_sql() {
  local sql="$1"
  printf '%s\n' "${writer_password}" | docker exec --interactive \
    "${postgres_container}" sh -ec '
      IFS= read -r PGPASSWORD
      export PGPASSWORD
      exec psql --host=127.0.0.1 --username="$1" --dbname="$2" \
        --no-psqlrc --set=ON_ERROR_STOP=1 --tuples-only --no-align \
        --command="$3"
    ' sh "${writer_role}" "${database}" "${sql}"
}

wait_for_postgres() {
  for _ in $(seq 1 60); do
    if docker exec "${postgres_container}" pg_isready --username=postgres >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done
  echo "PostgreSQL did not become ready" >&2
  docker logs "${postgres_container}" >&2 || true
  return 1
}

start_newapi() {
  local role="$1"
  local port="$2"
  local dsn="$3"
  local disable_writers="$4"
  local output="$5"
  (
    cd "${test_root}"
    env \
      PORT="${port}" \
      SQL_DSN="${dsn}" \
      KKAI_NODE_ROLE="${role}" \
      NODE_NAME="standby-sync-${role}" \
      DISABLE_BACKGROUND_TASKS="${disable_writers}" \
      BATCH_UPDATE_ENABLED=false \
      MEMORY_CACHE_ENABLED=true \
      SYNC_FREQUENCY="${sync_frequency_seconds}" \
      SQL_MAX_OPEN_CONNS=8 \
      SQL_MAX_IDLE_CONNS=2 \
      SESSION_SECRET=test-only-session-secret-01234567890123456789 \
      CRYPTO_SECRET=test-only-crypto-secret-012345678901234567890 \
      GIN_MODE=release \
      "${newapi_binary}" --log-dir ''
  ) >"${output}" 2>&1 &
  started_pid=$!
}

wait_for_http() {
  local pid="$1"
  local url="$2"
  local output="$3"
  for _ in $(seq 1 120); do
    if curl --fail --silent --show-error "${url}/api/status" >/dev/null 2>&1; then
      return
    fi
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      echo "NewAPI exited before becoming ready: ${url}" >&2
      sed -n '1,240p' "${output}" >&2
      return 1
    fi
    sleep 0.25
  done
  echo "NewAPI did not become ready: ${url}" >&2
  sed -n '1,240p' "${output}" >&2
  return 1
}

pricing_matches() {
  local response="$1"
  local expected_ratio="$2"
  local expected_endpoint="$3"
  ruby -rjson -e '
    payload = JSON.parse(File.read(ARGV.fetch(0)))
    abort "unsuccessful pricing response" unless payload["success"] == true
    item = payload.fetch("data").find { |entry| entry["model_name"] == ARGV.fetch(1) }
    abort "model missing" unless item
    abort "ratio mismatch" unless item["model_ratio"] == Float(ARGV.fetch(2))
    endpoints = item.fetch("supported_endpoint_types")
    abort "endpoint mismatch" unless endpoints == [ARGV.fetch(3)]
  ' "${response}" "${model_name}" "${expected_ratio}" "${expected_endpoint}"
}

wait_for_matching_pricing() {
  local active_endpoint="$1"
  local readonly_endpoint="$2"
  local active_response="$3"
  local readonly_response="$4"
  local expected_ratio="$5"
  local expected_endpoint="$6"
  local attempts="$7"
  for _ in $(seq 1 "${attempts}"); do
    if curl --fail --silent --show-error "${active_endpoint}/api/pricing" >"${active_response}.next" 2>/dev/null &&
      curl --fail --silent --show-error "${readonly_endpoint}/api/pricing" >"${readonly_response}.next" 2>/dev/null &&
      pricing_matches "${active_response}.next" "${expected_ratio}" "${expected_endpoint}" >/dev/null 2>&1 &&
      pricing_matches "${readonly_response}.next" "${expected_ratio}" "${expected_endpoint}" >/dev/null 2>&1; then
      mv -- "${active_response}.next" "${active_response}"
      mv -- "${readonly_response}.next" "${readonly_response}"
      return
    fi
    sleep 0.1
  done
  echo "leader and standby pricing did not converge together" >&2
  [[ ! -f "${active_response}.next" ]] || sed -n '1,80p' "${active_response}.next" >&2
  [[ ! -f "${readonly_response}.next" ]] || sed -n '1,80p' "${readonly_response}.next" >&2
  return 1
}

response_hash() {
  ruby -rdigest -e 'puts Digest::SHA256.file(ARGV.fetch(0)).hexdigest' "$1"
}

require_matching_response_hashes() {
  local leader_response="$1"
  local standby_response="$2"
  local leader_hash standby_hash
  leader_hash="$(response_hash "${leader_response}")"
  standby_hash="$(response_hash "${standby_response}")"
  if [[ "${leader_hash}" != "${standby_hash}" ]]; then
    echo "leader and standby pricing hashes differ: ${leader_hash} != ${standby_hash}" >&2
    return 1
  fi
}

echo "Building NewAPI integration binaries"
(
  cd "${root}"
  go build -o "${newapi_binary}" .
  go build -o "${migrate_binary}" ./cmd/kkai-migrate
)

docker run --detach --rm \
  --name "${postgres_container}" \
  --env POSTGRES_PASSWORD="${admin_password}" \
  --publish 127.0.0.1::5432 \
  "${postgres_image}" \
  -c shared_preload_libraries=pg_stat_statements \
  -c pg_stat_statements.track=all \
  -c log_statement=mod \
  -c "log_line_prefix=%m [%p] user=%u,db=%d " >/dev/null
wait_for_postgres

postgres_port="$(docker port "${postgres_container}" 5432/tcp | awk -F: 'NR == 1 {print $NF}')"
readonly postgres_port
readonly writer_dsn="postgres://${writer_role}:${writer_password}@127.0.0.1:${postgres_port}/${database}?sslmode=disable"
readonly standby_dsn="postgres://${standby_role}:${standby_password}@127.0.0.1:${postgres_port}/${database}?sslmode=disable"

admin_sql postgres "
  CREATE ROLE ${writer_role} LOGIN PASSWORD '${writer_password}'
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
  CREATE ROLE ${standby_role} LOGIN PASSWORD '${standby_password}'
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
  ALTER ROLE ${standby_role} SET default_transaction_read_only = on;
" >/dev/null
docker exec "${postgres_container}" createdb \
  --username=postgres --owner="${writer_role}" "${database}"
admin_sql "${database}" 'CREATE EXTENSION pg_stat_statements;' >/dev/null

"${migrate_binary}" --dsn "${writer_dsn}" >/dev/null

leader_port="$(free_port)"
readonly leader_port
readonly leader_url="http://127.0.0.1:${leader_port}"
readonly leader_output="${test_root}/leader.log"
start_newapi leader "${leader_port}" "${writer_dsn}" false "${leader_output}"
leader_pid="${started_pid}"
wait_for_http "${leader_pid}" "${leader_url}" "${leader_output}"

writer_sql "
  INSERT INTO options (\"key\", value) VALUES
    ('ModelRatio', '{\"${model_name}\":1.25}')
  ON CONFLICT (\"key\") DO UPDATE SET value = EXCLUDED.value;
  INSERT INTO channels (
    id, type, key, status, name, models, \"group\", created_time, settings, channel_info
  ) VALUES (
    ${channel_id}, 58, 'test-key', 1, 'standby-sync', '${model_name}', 'default', 1,
    '{\"advanced_custom\":{\"advanced_routes\":[{\"incoming_path\":\"/v1/chat/completions\",\"upstream_path\":\"/v1/chat/completions\"}]}}',
    '{}'
  );
  INSERT INTO abilities (\"group\", model, channel_id, enabled, weight)
    VALUES ('default', '${model_name}', ${channel_id}, true, 0);
" >/dev/null

admin_sql "${database}" "
  REVOKE CONNECT, TEMPORARY ON DATABASE ${database} FROM PUBLIC;
  GRANT CONNECT, TEMPORARY ON DATABASE ${database} TO ${writer_role};
  GRANT CONNECT ON DATABASE ${database} TO ${standby_role};
  REVOKE ALL PRIVILEGES ON SCHEMA public FROM ${standby_role};
  GRANT USAGE ON SCHEMA public TO ${standby_role};
  REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM ${standby_role};
  GRANT SELECT ON ALL TABLES IN SCHEMA public TO ${standby_role};
  REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM ${standby_role};
  GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO ${standby_role};
  ALTER DEFAULT PRIVILEGES FOR ROLE ${writer_role} IN SCHEMA public
    GRANT SELECT ON TABLES TO ${standby_role};
  ALTER DEFAULT PRIVILEGES FOR ROLE ${writer_role} IN SCHEMA public
    GRANT SELECT ON SEQUENCES TO ${standby_role};
" >/dev/null
admin_sql "${database}" 'SELECT pg_stat_statements_reset();' >/dev/null

standby_port="$(free_port)"
readonly standby_port
readonly standby_url="http://127.0.0.1:${standby_port}"
readonly standby_output="${test_root}/standby.log"
start_newapi standby-readonly "${standby_port}" "${standby_dsn}" true "${standby_output}"
standby_pid="${started_pid}"
wait_for_http "${standby_pid}" "${standby_url}" "${standby_output}"

readonly leader_initial="${test_root}/leader-initial.json"
readonly standby_initial="${test_root}/standby-initial.json"
wait_for_matching_pricing \
  "${leader_url}" "${standby_url}" "${leader_initial}" "${standby_initial}" \
  1.25 openai 60
require_matching_response_hashes "${leader_initial}" "${standby_initial}"

writer_sql "
  UPDATE options SET value = '{\"${model_name}\":2.75}' WHERE \"key\" = 'ModelRatio';
  UPDATE channels SET settings =
    '{\"advanced_custom\":{\"advanced_routes\":[{\"incoming_path\":\"/v1/responses\",\"upstream_path\":\"/v1/responses\"}]}}'
  WHERE id = ${channel_id};
" >/dev/null

readonly leader_updated="${test_root}/leader-updated.json"
readonly standby_updated="${test_root}/standby-updated.json"
wait_for_matching_pricing \
  "${leader_url}" "${standby_url}" "${leader_updated}" "${standby_updated}" \
  2.75 openai-response 30
require_matching_response_hashes "${leader_updated}" "${standby_updated}"

standby_dml_count="$(admin_sql "${database}" "
  SELECT count(*)
  FROM pg_stat_statements AS statement
  JOIN pg_roles AS role ON role.oid = statement.userid
  WHERE role.rolname = '${standby_role}'
    AND statement.query ~* '^[[:space:]]*(insert|update|delete|merge|truncate|create|alter|drop|grant|revoke|comment|vacuum|analyze)';
")"
[[ "${standby_dml_count}" == '0' ]]

if grep -Eiq 'standby-readonly node rejected a database write|permission denied|cannot execute (insert|update|delete|create|alter|drop)' "${standby_output}"; then
  echo "standby attempted a database write" >&2
  grep -Ein 'standby-readonly node rejected a database write|permission denied|cannot execute' "${standby_output}" >&2
  exit 1
fi
if docker logs "${postgres_container}" 2>&1 |
  grep -Ei "user=${standby_role}.*statement:.*(insert|update|delete|merge|truncate|create|alter|drop|grant|revoke)" >/dev/null; then
  echo "PostgreSQL logged a standby DML or DDL attempt" >&2
  exit 1
fi

echo "NewAPI leader/standby PostgreSQL pricing sync test passed"
