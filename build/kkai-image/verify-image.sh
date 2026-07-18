#!/usr/bin/env bash
set -Eeuo pipefail

readonly IMAGE_REF="${1:?usage: $0 IMAGE_REF}"
readonly EXPECTED_SHA="${EXPECTED_SHA:-}"
readonly EXPECTED_VERSION="${EXPECTED_VERSION:-}"
readonly EXPECTED_REGISTRY_DIGEST="${EXPECTED_REGISTRY_DIGEST:-}"
readonly EXPECTED_SCHEMA_COMPATIBILITY="${EXPECTED_SCHEMA_COMPATIBILITY:-}"
readonly EXPECTED_UPSTREAM_SCHEMA_COMPATIBILITY="${EXPECTED_UPSTREAM_SCHEMA_COMPATIBILITY:-}"

if [[ ! "${EXPECTED_SHA}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "EXPECTED_SHA must be an explicit 40-character commit" >&2
  exit 64
fi

if [[ -z "${EXPECTED_VERSION}" ]]; then
  echo "EXPECTED_VERSION is required" >&2
  exit 64
fi

for command_name in docker grep jq mktemp; do
  command -v "${command_name}" >/dev/null || {
    echo "required command not found: ${command_name}" >&2
    exit 69
  }
done

ARCHITECTURE="$(docker image inspect --format '{{.Architecture}}' "${IMAGE_REF}")"
readonly ARCHITECTURE
OS_NAME="$(docker image inspect --format '{{.Os}}' "${IMAGE_REF}")"
readonly OS_NAME
IMAGE_USER="$(docker image inspect --format '{{.Config.User}}' "${IMAGE_REF}")"
readonly IMAGE_USER
IMAGE_SHA="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "${IMAGE_REF}")"
readonly IMAGE_SHA
IMAGE_VERSION="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "${IMAGE_REF}")"
readonly IMAGE_VERSION
IMAGE_ID="$(docker image inspect --format '{{.Id}}' "${IMAGE_REF}")"
readonly IMAGE_ID
ROOTFS_DIFF_IDS="$(docker image inspect --format '{{range .RootFS.Layers}}{{println .}}{{end}}' "${IMAGE_REF}")"
readonly ROOTFS_DIFF_IDS

[[ "${ARCHITECTURE}" == "amd64" ]]
[[ "${OS_NAME}" == "linux" ]]
[[ "${IMAGE_USER}" == "10007:10007" ]]
[[ "${IMAGE_SHA}" == "${EXPECTED_SHA}" ]]
[[ "${IMAGE_VERSION}" == "${EXPECTED_VERSION}" ]]
[[ "${IMAGE_ID}" =~ ^sha256:[0-9a-f]{64}$ ]]
[[ -n "${ROOTFS_DIFF_IDS}" ]]

if [[ -n "${EXPECTED_SCHEMA_COMPATIBILITY}" ]]; then
  [[ -s "${EXPECTED_SCHEMA_COMPATIBILITY}" ]]
  expected_contract="$(tr -d '\n' < "${EXPECTED_SCHEMA_COMPATIBILITY}")"
  readonly expected_contract
  declare -A schema_labels=(
    [runtime_min_version]=com.kkai.schema.min-compatible
    [runtime_max_version]=com.kkai.schema.max-compatible
    [migration_target_version]=com.kkai.schema.migration-target
    [migration_kind]=com.kkai.schema.migration-kind
    [migration_set_digest]=com.kkai.schema.migration-set-digest
  )
  for field in runtime_min_version runtime_max_version migration_target_version migration_kind migration_set_digest; do
    label_name="${schema_labels[${field}]}"
    expected_value="$(jq --raw-output ".${field}" "${EXPECTED_SCHEMA_COMPATIBILITY}")"
    actual_value="$(docker image inspect --format "{{index .Config.Labels \"${label_name}\"}}" "${IMAGE_REF}")"
    [[ "${actual_value}" == "${expected_value}" ]]
  done
  image_contract="$(docker run --rm --pull=never --platform linux/amd64 --entrypoint /kkai-migrate "${IMAGE_REF}" --describe --json)"
  [[ "${image_contract}" == "${expected_contract}" ]]
  container_id="$(docker create --pull=never "${IMAGE_REF}")"
  contract_dir="$(mktemp --directory)"
  contract_file="${contract_dir}/schema-compatibility.json"
  trap 'docker rm --force "${container_id}" >/dev/null 2>&1 || true; rm -rf "${contract_dir}"' EXIT
  docker cp "${container_id}:/schema-compatibility.json" "${contract_dir}"
  [[ "$(tr -d '\n' < "${contract_file}")" == "${expected_contract}" ]]
  docker rm --force "${container_id}" >/dev/null
  trap - EXIT
  rm -rf "${contract_dir}"
fi

if [[ -n "${EXPECTED_UPSTREAM_SCHEMA_COMPATIBILITY}" ]]; then
  [[ -s "${EXPECTED_UPSTREAM_SCHEMA_COMPATIBILITY}" ]]
  expected_upstream_contract="$(tr -d '\n' < "${EXPECTED_UPSTREAM_SCHEMA_COMPATIBILITY}")"
  readonly expected_upstream_contract
  image_upstream_contract="$(
    docker run --rm --pull=never --platform linux/amd64 --entrypoint /kkai-migrate \
      "${IMAGE_REF}" --describe-upstream-schema --json
  )"
  [[ "${image_upstream_contract}" == "${expected_upstream_contract}" ]]
  container_id="$(docker create --pull=never "${IMAGE_REF}")"
  contract_dir="$(mktemp --directory)"
  contract_file="${contract_dir}/upstream-schema-compatibility.json"
  trap 'docker rm --force "${container_id}" >/dev/null 2>&1 || true; rm -rf "${contract_dir}"' EXIT
  docker cp "${container_id}:/upstream-schema-compatibility.json" "${contract_dir}"
  [[ "$(tr -d '\n' < "${contract_file}")" == "${expected_upstream_contract}" ]]
  docker rm --force "${container_id}" >/dev/null
  trap - EXIT
  rm -rf "${contract_dir}"
fi

if [[ -n "${EXPECTED_REGISTRY_DIGEST}" ]]; then
  [[ "${EXPECTED_REGISTRY_DIGEST}" =~ ^sha256:[0-9a-f]{64}$ ]]
  docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "${IMAGE_REF}" |
    grep -Fx "${IMAGE_REF%@*}@${EXPECTED_REGISTRY_DIGEST}" >/dev/null
fi

docker run --rm --pull=never --platform linux/amd64 --entrypoint /new-api "${IMAGE_REF}" --version |
  grep -Fx "${EXPECTED_VERSION}" >/dev/null
docker run --rm --pull=never --platform linux/amd64 --entrypoint /usr/bin/wget "${IMAGE_REF}" --help >/dev/null
docker run --rm --pull=never --platform linux/amd64 --entrypoint /new-api-canary-probe "${IMAGE_REF}" -h >/dev/null
docker run --rm --pull=never --platform linux/amd64 --entrypoint /kkai-migrate "${IMAGE_REF}" -h >/dev/null
observer_help="$(
  docker run --rm --pull=never --platform linux/amd64 --entrypoint /kkai-schema-observe "${IMAGE_REF}" -h 2>&1
)"
readonly observer_help
grep -Eq '^[[:space:]]+-current([[:space:]=]|$)' <<<"${observer_help}"
grep -Eq '^[[:space:]]+-check-upstream-baseline([[:space:]=]|$)' <<<"${observer_help}"
for forbidden_observer_flag in apply bootstrap-empty check describe dry-run min-version; do
  if grep -Eq "^[[:space:]]+-${forbidden_observer_flag}([[:space:]=]|$)" <<<"${observer_help}"; then
    echo "schema observer exposes forbidden migration flag --${forbidden_observer_flag}" >&2
    exit 1
  fi
done
docker run --rm --pull=never --platform linux/amd64 --entrypoint /kkai-topup-recovery "${IMAGE_REF}" plan -h >/dev/null
risk_guard_probe="$(
  docker run --rm --pull=never --platform linux/amd64 --entrypoint /newapi-risk-guard "${IMAGE_REF}" 2>&1 || true
)"
readonly risk_guard_probe
grep -F 'invalid risk guard configuration' <<<"${risk_guard_probe}" >/dev/null

echo "verified ${IMAGE_REF}"
echo "image ID: ${IMAGE_ID}"
