#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly SOURCE_REPOSITORY=github.com/Wangjunkai-1996/new-api-kkai
readonly SOURCE_REF=refs/heads/production/kkrich
readonly PRODUCTION_UPSTREAM=refs/remotes/origin/production/kkrich
readonly REQUIRE_PRODUCTION_HEAD="${ROOT}/scripts/kkai/require-production-head.sh"

die() {
  echo "build-manual-release: $*" >&2
  exit 1
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  shasum -a 256 "$1" | awk '{print $1}'
}

require_clean_worktree() {
  [[ -z "$(git -C "${ROOT}" status --porcelain=v1 --untracked-files=all)" ]] ||
    die "production builds require a clean worktree"
}

output_dir="${ROOT}/.local-releases"
version=''
schema_contract=feature
build_http_proxy="${BUILD_HTTP_PROXY:-${HTTP_PROXY:-}}"
build_https_proxy="${BUILD_HTTPS_PROXY:-${HTTPS_PROXY:-}}"
build_no_proxy="${BUILD_NO_PROXY:-${NO_PROXY:-}}"
while (( $# > 0 )); do
  [[ $# -ge 2 ]] || die "missing value for $1"
  case "$1" in
    --output-dir) output_dir=$2 ;;
    --schema-contract) schema_contract=$2 ;;
    --version) version=$2 ;;
    *) die "unsupported argument: $1" ;;
  esac
  shift 2
done
case "${schema_contract}" in
  feature | bridge) ;;
  *) die "schema contract must be feature or bridge" ;;
esac

for command_name in docker git jq tar; do
  command -v "${command_name}" >/dev/null 2>&1 || die "missing ${command_name}"
done
[[ -x "${REQUIRE_PRODUCTION_HEAD}" ]] || die "production HEAD verifier is missing"
current_branch="$(git -C "${ROOT}" branch --show-current)"
if [[ -n "${current_branch}" ]]; then
  [[ "${current_branch}" == production/kkrich ]] ||
    die "attached production builds require branch production/kkrich"
  current_upstream="$(git -C "${ROOT}" rev-parse --symbolic-full-name '@{upstream}' 2>/dev/null || true)"
  [[ "${current_upstream}" == "${PRODUCTION_UPSTREAM}" ]] ||
    die "production/kkrich must track origin/production/kkrich"
fi
require_clean_worktree

source_sha="$(git -C "${ROOT}" rev-parse HEAD)"
[[ "${source_sha}" =~ ^[0-9a-f]{40}$ ]] || die "invalid source SHA"
"${REQUIRE_PRODUCTION_HEAD}" "${source_sha}"
if [[ -z "${version}" ]]; then
  version="kkai-prod-$(date -u +%Y%m%d).$(date -u +%s)-${source_sha:0:9}"
fi
[[ "${version}" =~ ^kkai-prod-[0-9]{8}\.[1-9][0-9]*-${source_sha:0:9}$ ]] ||
  die "invalid version for source ${source_sha}"

image_tag="kkai-newapi-manual:${version}"
mkdir -p -- "${output_dir}"
output_dir="$(cd -- "${output_dir}" && pwd)"
archive="${output_dir}/${version}.tar"
metadata="${output_dir}/${version}.json"
metadata_tmp="${metadata}.tmp.$$"
[[ ! -e "${archive}" && ! -e "${metadata}" ]] || die "release output already exists"
build_context=''
release_complete=0
cleanup_incomplete_release() {
  if [[ -n "${build_context}" ]]; then
    rm -rf -- "${build_context}"
  fi
  if (( release_complete == 0 )); then
    rm -f -- "${archive}" "${metadata}" "${metadata_tmp}"
  fi
}
trap cleanup_incomplete_release EXIT

build_context="$(mktemp -d "${TMPDIR:-/tmp}/kkai-manual-build-context.XXXXXX")"
readonly build_context
git -C "${ROOT}" archive --format=tar "${source_sha}" | tar -xf - -C "${build_context}"
[[ -f "${build_context}/build/kkai-image/Dockerfile" ]] ||
  die "production source snapshot is incomplete"

build_args=(buildx build)
if [[ -n "${build_http_proxy}" ]]; then
  build_args+=(--build-arg "HTTP_PROXY=${build_http_proxy}")
  build_args+=(--build-arg "http_proxy=${build_http_proxy}")
fi
if [[ -n "${build_https_proxy}" ]]; then
  build_args+=(--build-arg "HTTPS_PROXY=${build_https_proxy}")
  build_args+=(--build-arg "https_proxy=${build_https_proxy}")
fi
if [[ -n "${build_no_proxy}" ]]; then
  build_args+=(--build-arg "NO_PROXY=${build_no_proxy}")
  build_args+=(--build-arg "no_proxy=${build_no_proxy}")
fi

docker "${build_args[@]}" \
  --platform linux/amd64 \
  --file "${build_context}/build/kkai-image/Dockerfile" \
  --build-context "kkai_image=${build_context}/build/kkai-image" \
  --build-arg "APP_VERSION=${version}" \
  --build-arg "KKAI_SCHEMA_CONTRACT=${schema_contract}" \
  --build-arg "SOURCE_REVISION=${source_sha}" \
  --tag "${image_tag}" \
  --output "type=docker,dest=${archive}" \
  "${build_context}"

require_clean_worktree
"${REQUIRE_PRODUCTION_HEAD}" "${source_sha}"
archive_sha256="$(sha256_file "${archive}")"
jq --null-input \
  --arg version "${version}" \
  --arg source_repository "${SOURCE_REPOSITORY}" \
  --arg source_ref "${SOURCE_REF}" \
  --arg source_sha "${source_sha}" \
  --arg image_tag "${image_tag}" \
  --arg schema_contract "${schema_contract}" \
  --arg archive "$(basename -- "${archive}")" \
  --arg archive_sha256 "${archive_sha256}" \
  '{
    version: $version,
    source_repository: $source_repository,
    source_ref: $source_ref,
    source_sha: $source_sha,
    image_tag: $image_tag,
    schema_contract: $schema_contract,
    archive: $archive,
    archive_sha256: $archive_sha256,
    platform: "linux/amd64"
  }' > "${metadata_tmp}"
require_clean_worktree
"${REQUIRE_PRODUCTION_HEAD}" "${source_sha}"
mv -- "${metadata_tmp}" "${metadata}"
release_complete=1

echo "MANUAL_RELEASE_METADATA=${metadata}"
