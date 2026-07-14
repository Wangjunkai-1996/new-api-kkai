#!/usr/bin/env bash
set -Eeuo pipefail

readonly IMAGE_REF="${1:?usage: $0 IMAGE_REF}"
readonly EXPECTED_SHA="${EXPECTED_SHA:-}"
readonly EXPECTED_VERSION="${EXPECTED_VERSION:-}"
readonly EXPECTED_REGISTRY_DIGEST="${EXPECTED_REGISTRY_DIGEST:-}"

if [[ ! "${EXPECTED_SHA}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "EXPECTED_SHA must be an explicit 40-character commit" >&2
  exit 64
fi
if [[ -z "${EXPECTED_VERSION}" ]]; then
  echo "EXPECTED_VERSION is required" >&2
  exit 64
fi

for command_name in docker grep; do
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
risk_guard_probe="$(
  docker run --rm --pull=never --platform linux/amd64 --entrypoint /newapi-risk-guard "${IMAGE_REF}" 2>&1 || true
)"
readonly risk_guard_probe
grep -F 'invalid risk guard configuration' <<<"${risk_guard_probe}" >/dev/null

echo "verified ${IMAGE_REF}"
echo "image ID: ${IMAGE_ID}"
