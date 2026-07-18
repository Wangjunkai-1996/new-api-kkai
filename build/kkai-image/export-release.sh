#!/usr/bin/env bash
set -Eeuo pipefail

readonly IMAGE_REF="${1:?usage: $0 IMAGE_REF VERSION SOURCE_SHA REGISTRY_DIGEST OUTPUT_DIR}"
readonly VERSION="${2:?usage: $0 IMAGE_REF VERSION SOURCE_SHA REGISTRY_DIGEST OUTPUT_DIR}"
readonly SOURCE_SHA="${3:?usage: $0 IMAGE_REF VERSION SOURCE_SHA REGISTRY_DIGEST OUTPUT_DIR}"
readonly REGISTRY_DIGEST="${4:?usage: $0 IMAGE_REF VERSION SOURCE_SHA REGISTRY_DIGEST OUTPUT_DIR}"
readonly OUTPUT_DIR="${5:?usage: $0 IMAGE_REF VERSION SOURCE_SHA REGISTRY_DIGEST OUTPUT_DIR}"

[[ "${IMAGE_REF}" =~ ^ghcr\.io/[a-z0-9._/-]+@sha256:[0-9a-f]{64}$ ]]
[[ "${VERSION}" =~ ^[A-Za-z0-9._-]{8,96}$ ]]
[[ "${SOURCE_SHA}" =~ ^[0-9a-f]{40}$ ]]
[[ "${REGISTRY_DIGEST}" =~ ^sha256:[0-9a-f]{64}$ ]]

for command_name in docker sha256sum; do
  command -v "${command_name}" >/dev/null || {
    echo "required command not found: ${command_name}" >&2
    exit 69
  }
done

mkdir -p -- "${OUTPUT_DIR}"
readonly LOCAL_TAG="kkai-release:${VERSION}"
readonly ARCHIVE_NAME="new-api-${VERSION}-linux-amd64.tar"
readonly ARCHIVE_PATH="${OUTPUT_DIR}/${ARCHIVE_NAME}"
readonly MANIFEST_PATH="${OUTPUT_DIR}/offline-release.yml"
readonly SCHEMA_COMPATIBILITY_PATH="${OUTPUT_DIR}/schema-compatibility.json"
readonly UPSTREAM_SCHEMA_COMPATIBILITY_PATH="${OUTPUT_DIR}/upstream-schema-compatibility.json"

docker pull "${IMAGE_REF}" >/dev/null
docker tag "${IMAGE_REF}" "${LOCAL_TAG}"
docker run --rm --pull=never --platform linux/amd64 --read-only --network none \
  --cap-drop=ALL --security-opt=no-new-privileges:true \
  --entrypoint /kkai-schema-observe "${LOCAL_TAG}" -h >/dev/null
docker image save --output "${ARCHIVE_PATH}" "${LOCAL_TAG}"

ARTIFACT_SHA256="$(sha256sum "${ARCHIVE_PATH}" | awk '{print $1}')"
readonly ARTIFACT_SHA256
IMAGE_ID="$(docker image inspect --format '{{.Id}}' "${LOCAL_TAG}")"
readonly IMAGE_ID
ARCHITECTURE="$(docker image inspect --format '{{.Architecture}}' "${LOCAL_TAG}")"
readonly ARCHITECTURE
OS_NAME="$(docker image inspect --format '{{.Os}}' "${LOCAL_TAG}")"
readonly OS_NAME
IMAGE_USER="$(docker image inspect --format '{{.Config.User}}' "${LOCAL_TAG}")"
readonly IMAGE_USER

[[ "${IMAGE_ID}" =~ ^sha256:[0-9a-f]{64}$ ]]
[[ "${ARCHITECTURE}" == amd64 ]]
[[ "${OS_NAME}" == linux ]]
[[ "${IMAGE_USER}" == 10007:10007 ]]

{
  printf '%s\n' '---' 'schema: 1' 'source:'
  printf "  revision: '%s'\n" "${SOURCE_SHA}"
  printf '%s\n' 'image:'
  printf "  registry_ref: '%s'\n" "${IMAGE_REF}"
  printf "  registry_digest: '%s'\n" "${REGISTRY_DIGEST}"
  printf "  image_id: '%s'\n" "${IMAGE_ID}"
  printf "  architecture: '%s'\n" "${ARCHITECTURE}"
  printf "  os: '%s'\n" "${OS_NAME}"
  printf "  user: '%s'\n" "${IMAGE_USER}"
  printf "  version: '%s'\n" "${VERSION}"
  printf '%s\n' '  rootfs_diff_ids:'
  docker image inspect --format '{{range .RootFS.Layers}}{{println .}}{{end}}' "${LOCAL_TAG}" |
    sed '/^$/d; s/^/    - '\''/; s/$/'\''/'
  printf '%s\n' 'artifact:'
  printf "  filename: '%s'\n" "${ARCHIVE_NAME}"
  printf "  sha256: '%s'\n" "${ARTIFACT_SHA256}"
} >"${MANIFEST_PATH}"

printf '%s  %s\n' "${ARTIFACT_SHA256}" "${ARCHIVE_NAME}" >"${ARCHIVE_PATH}.sha256"
docker image inspect "${LOCAL_TAG}" >"${OUTPUT_DIR}/image-inspect.json"
container_id="$(docker create --pull=never "${LOCAL_TAG}")"
trap 'docker rm --force "${container_id}" >/dev/null 2>&1 || true' EXIT
docker cp "${container_id}:/schema-compatibility.json" "${SCHEMA_COMPATIBILITY_PATH}"
docker cp "${container_id}:/upstream-schema-compatibility.json" "${UPSTREAM_SCHEMA_COMPATIBILITY_PATH}"
docker rm --force "${container_id}" >/dev/null
trap - EXIT

echo "exported ${ARCHIVE_PATH}"
echo "manifest ${MANIFEST_PATH}"
echo "schema compatibility ${SCHEMA_COMPATIBILITY_PATH}"
echo "upstream schema compatibility ${UPSTREAM_SCHEMA_COMPATIBILITY_PATH}"
