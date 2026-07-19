#!/usr/bin/env bash
set -Eeuo pipefail

readonly IMAGE_REF="${1:?usage: $0 IMAGE_REF}"
readonly EXPECTED_SHA="${EXPECTED_SHA:-}"
readonly EXPECTED_VERSION="${EXPECTED_VERSION:-}"
readonly EXPECTED_REGISTRY_DIGEST="${EXPECTED_REGISTRY_DIGEST:-}"
readonly EXPECTED_SCHEMA_MANAGEMENT="${EXPECTED_SCHEMA_MANAGEMENT:-}"
readonly EXPECTED_SCHEMA_COMPATIBLE_PREFIXES="${EXPECTED_SCHEMA_COMPATIBLE_PREFIXES:-}"
readonly EXPECTED_SCHEMA_RUNTIME_MIN_VERSION="${EXPECTED_SCHEMA_RUNTIME_MIN_VERSION:-}"
readonly EXPECTED_SCHEMA_RUNTIME_MAX_VERSION="${EXPECTED_SCHEMA_RUNTIME_MAX_VERSION:-}"
readonly EXPECTED_SCHEMA_MIGRATION_TARGET="${EXPECTED_SCHEMA_MIGRATION_TARGET:-}"
readonly EXPECTED_SCHEMA_MIGRATION_KIND="${EXPECTED_SCHEMA_MIGRATION_KIND:-}"
readonly EXPECTED_SCHEMA_MIGRATION_SET_DIGEST="${EXPECTED_SCHEMA_MIGRATION_SET_DIGEST:-}"

for command_name in docker grep jq; do
  command -v "${command_name}" >/dev/null || {
    echo "required command not found: ${command_name}" >&2
    exit 69
  }
done

if [[ ! "${EXPECTED_SHA}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "EXPECTED_SHA must be an explicit 40-character commit" >&2
  exit 64
fi
if [[ -z "${EXPECTED_VERSION}" ]]; then
  echo "EXPECTED_VERSION is required" >&2
  exit 64
fi
[[ "${EXPECTED_SCHEMA_MANAGEMENT}" == external ]] || {
  echo "formal images require EXPECTED_SCHEMA_MANAGEMENT=external" >&2
  exit 64
}
for version in \
  "${EXPECTED_SCHEMA_RUNTIME_MIN_VERSION}" \
  "${EXPECTED_SCHEMA_RUNTIME_MAX_VERSION}" \
  "${EXPECTED_SCHEMA_MIGRATION_TARGET}"; do
  [[ "${version}" =~ ^[1-9][0-9]*$ ]] || {
    echo "expected schema versions must be positive canonical integers" >&2
    exit 64
  }
done
[[ "${EXPECTED_SCHEMA_MIGRATION_KIND}" == none || "${EXPECTED_SCHEMA_MIGRATION_KIND}" == expand ]] || {
  echo "EXPECTED_SCHEMA_MIGRATION_KIND is invalid" >&2
  exit 64
}
((EXPECTED_SCHEMA_RUNTIME_MIN_VERSION <= EXPECTED_SCHEMA_MIGRATION_TARGET)) || {
  echo "schema migration target is below the runtime minimum" >&2
  exit 64
}
((EXPECTED_SCHEMA_MIGRATION_TARGET <= EXPECTED_SCHEMA_RUNTIME_MAX_VERSION)) || {
  echo "schema migration target exceeds the runtime maximum" >&2
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
SCHEMA_MANAGEMENT="$(docker image inspect --format '{{index .Config.Labels "com.kkai.runtime.schema-management"}}' "${IMAGE_REF}")"
readonly SCHEMA_MANAGEMENT
SCHEMA_COMPATIBLE_PREFIXES="$(docker image inspect --format '{{index .Config.Labels "com.kkai.schema.compatible-prefixes"}}' "${IMAGE_REF}")"
readonly SCHEMA_COMPATIBLE_PREFIXES
canonical_image_compatible_prefixes="$(jq --compact-output --sort-keys . <<<"${SCHEMA_COMPATIBLE_PREFIXES}")"
readonly canonical_image_compatible_prefixes
[[ "${SCHEMA_COMPATIBLE_PREFIXES}" == "${canonical_image_compatible_prefixes}" ]] || {
  echo "image compatible-prefixes label is not canonical compact JSON" >&2
  exit 1
}
SCHEMA_RUNTIME_MIN_VERSION="$(docker image inspect --format '{{index .Config.Labels "com.kkai.schema.min-compatible"}}' "${IMAGE_REF}")"
readonly SCHEMA_RUNTIME_MIN_VERSION
SCHEMA_RUNTIME_MAX_VERSION="$(docker image inspect --format '{{index .Config.Labels "com.kkai.schema.max-compatible"}}' "${IMAGE_REF}")"
readonly SCHEMA_RUNTIME_MAX_VERSION
SCHEMA_MIGRATION_TARGET="$(docker image inspect --format '{{index .Config.Labels "com.kkai.schema.migration-target"}}' "${IMAGE_REF}")"
readonly SCHEMA_MIGRATION_TARGET
SCHEMA_MIGRATION_KIND="$(docker image inspect --format '{{index .Config.Labels "com.kkai.schema.migration-kind"}}' "${IMAGE_REF}")"
readonly SCHEMA_MIGRATION_KIND
SCHEMA_MIGRATION_SET_DIGEST="$(docker image inspect --format '{{index .Config.Labels "com.kkai.schema.migration-set-digest"}}' "${IMAGE_REF}")"
readonly SCHEMA_MIGRATION_SET_DIGEST
IMAGE_ID="$(docker image inspect --format '{{.Id}}' "${IMAGE_REF}")"
readonly IMAGE_ID
ROOTFS_DIFF_IDS="$(docker image inspect --format '{{range .RootFS.Layers}}{{println .}}{{end}}' "${IMAGE_REF}")"
readonly ROOTFS_DIFF_IDS

[[ "${ARCHITECTURE}" == "amd64" ]]
[[ "${OS_NAME}" == "linux" ]]
[[ "${IMAGE_USER}" == "10007:10007" ]]
[[ "${IMAGE_SHA}" == "${EXPECTED_SHA}" ]]
[[ "${IMAGE_VERSION}" == "${EXPECTED_VERSION}" ]]
[[ "${SCHEMA_MANAGEMENT}" == "${EXPECTED_SCHEMA_MANAGEMENT}" ]]
[[ "${canonical_image_compatible_prefixes}" == "${canonical_compatible_prefixes}" ]]
[[ "${SCHEMA_RUNTIME_MIN_VERSION}" == "${EXPECTED_SCHEMA_RUNTIME_MIN_VERSION}" ]]
[[ "${SCHEMA_RUNTIME_MAX_VERSION}" == "${EXPECTED_SCHEMA_RUNTIME_MAX_VERSION}" ]]
[[ "${SCHEMA_MIGRATION_TARGET}" == "${EXPECTED_SCHEMA_MIGRATION_TARGET}" ]]
[[ "${SCHEMA_MIGRATION_KIND}" == "${EXPECTED_SCHEMA_MIGRATION_KIND}" ]]
[[ "${SCHEMA_MIGRATION_SET_DIGEST}" == "${EXPECTED_SCHEMA_MIGRATION_SET_DIGEST}" ]]
[[ "${IMAGE_ID}" =~ ^sha256:[0-9a-f]{64}$ ]]
[[ -n "${ROOTFS_DIFF_IDS}" ]]

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
docker run --rm --pull=never --platform linux/amd64 --entrypoint /kkai-topup-recovery "${IMAGE_REF}" plan -h >/dev/null
contract_json="$(
  docker run --rm --pull=never --platform linux/amd64 --entrypoint /kkai-migrate "${IMAGE_REF}" \
    --describe-contract --dialect postgres --json
)"
readonly contract_json
jq --exit-status \
  --argjson prefixes "${EXPECTED_SCHEMA_COMPATIBLE_PREFIXES}" \
  --argjson minimum "${EXPECTED_SCHEMA_RUNTIME_MIN_VERSION}" \
  --argjson maximum "${EXPECTED_SCHEMA_RUNTIME_MAX_VERSION}" \
  --argjson target "${EXPECTED_SCHEMA_MIGRATION_TARGET}" \
  --arg kind "${EXPECTED_SCHEMA_MIGRATION_KIND}" \
  --arg digest "${EXPECTED_SCHEMA_MIGRATION_SET_DIGEST}" \
  --arg schema_management "${EXPECTED_SCHEMA_MANAGEMENT}" \
  'keys == ["compatible_prefixes", "migration_kind", "migration_set_digest", "migration_target_version", "runtime_max_version", "runtime_min_version", "schema_management"] and
   .compatible_prefixes == $prefixes and
   .schema_management == $schema_management and
   .runtime_min_version == $minimum and .runtime_max_version == $maximum and
   .migration_target_version == $target and .migration_kind == $kind and
   .migration_set_digest == $digest' <<<"${contract_json}" >/dev/null
risk_guard_probe="$(
  docker run --rm --pull=never --platform linux/amd64 --entrypoint /newapi-risk-guard "${IMAGE_REF}" 2>&1 || true
)"
readonly risk_guard_probe
grep -F 'invalid risk guard configuration' <<<"${risk_guard_probe}" >/dev/null

echo "verified ${IMAGE_REF}"
echo "image ID: ${IMAGE_ID}"
