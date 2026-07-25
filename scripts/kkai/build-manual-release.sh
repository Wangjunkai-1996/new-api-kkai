#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT

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

output_dir="${ROOT}/.local-releases"
version=''
build_http_proxy="${BUILD_HTTP_PROXY:-${HTTP_PROXY:-}}"
build_https_proxy="${BUILD_HTTPS_PROXY:-${HTTPS_PROXY:-}}"
build_no_proxy="${BUILD_NO_PROXY:-${NO_PROXY:-}}"
while (( $# > 0 )); do
  [[ $# -ge 2 ]] || die "missing value for $1"
  case "$1" in
    --output-dir) output_dir=$2 ;;
    --version) version=$2 ;;
    *) die "unsupported argument: $1" ;;
  esac
  shift 2
done

for command_name in docker git jq; do
  command -v "${command_name}" >/dev/null 2>&1 || die "missing ${command_name}"
done
[[ "$(git -C "${ROOT}" branch --show-current)" == production/kkrich ]] ||
  die "production builds require branch production/kkrich"
[[ -z "$(git -C "${ROOT}" status --porcelain=v1 --untracked-files=all)" ]] ||
  die "production builds require a clean worktree"

source_sha="$(git -C "${ROOT}" rev-parse HEAD)"
[[ "${source_sha}" =~ ^[0-9a-f]{40}$ ]] || die "invalid source SHA"
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
[[ ! -e "${archive}" && ! -e "${metadata}" ]] || die "release output already exists"

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
  --file "${ROOT}/build/kkai-image/Dockerfile" \
  --build-context "kkai_image=${ROOT}/build/kkai-image" \
  --build-arg "APP_VERSION=${version}" \
  --build-arg "SOURCE_REVISION=${source_sha}" \
  --tag "${image_tag}" \
  --output "type=docker,dest=${archive}" \
  "${ROOT}"

archive_sha256="$(sha256_file "${archive}")"
jq --null-input \
  --arg version "${version}" \
  --arg source_sha "${source_sha}" \
  --arg image_tag "${image_tag}" \
  --arg archive "$(basename -- "${archive}")" \
  --arg archive_sha256 "${archive_sha256}" \
  '{
    version: $version,
    source_sha: $source_sha,
    image_tag: $image_tag,
    archive: $archive,
    archive_sha256: $archive_sha256,
    platform: "linux/amd64"
  }' > "${metadata}"

echo "MANUAL_RELEASE_METADATA=${metadata}"
