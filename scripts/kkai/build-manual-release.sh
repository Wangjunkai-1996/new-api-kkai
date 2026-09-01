#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
export GIT_NO_LAZY_FETCH=1

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
schema_contract=''
frontend_mode='embedded'
builder="${BUILDX_BUILDER:-kkai-mirror-builder}"
go_build_parallelism="${KKAI_GO_BUILD_PARALLELISM:-4}"
media_build_parallelism="${KKAI_MEDIA_BUILD_PARALLELISM:-2}"
build_cpu_quota="${KKAI_BUILDX_CPU_QUOTA:-200000}"
build_memory_limit="${KKAI_BUILDX_MEMORY:-3g}"
build_http_proxy="${BUILD_HTTP_PROXY:-${HTTP_PROXY:-}}"
build_https_proxy="${BUILD_HTTPS_PROXY:-${HTTPS_PROXY:-}}"
build_no_proxy="${BUILD_NO_PROXY:-${NO_PROXY:-}}"
build_lock_dir="${TMPDIR:-/tmp}/kkai-newapi-manual-build.lock"

release_build_lock() {
  rm -f -- "${build_lock_dir}/pid" 2>/dev/null || true
  rmdir -- "${build_lock_dir}" 2>/dev/null || true
}

acquire_build_lock() {
  local lock_pid=''

  if mkdir -m 700 -- "${build_lock_dir}" 2>/dev/null; then
    trap release_build_lock EXIT
    if ! printf '%s\n' "$$" > "${build_lock_dir}/pid"; then
      release_build_lock
      die "unable to record local build lock owner"
    fi
    return
  fi

  if [[ -r "${build_lock_dir}/pid" ]]; then
    lock_pid="$(<"${build_lock_dir}/pid")"
  fi
  if [[ "${lock_pid}" =~ ^[0-9]+$ ]] && kill -0 "${lock_pid}" 2>/dev/null; then
    die "another local New API build is running (pid ${lock_pid})"
  fi
  die "build lock exists at ${build_lock_dir}; verify and remove it before retrying"
}

set_build_resource() {
  local resource=$1
  local resource_value
  [[ "${resource}" == *=* ]] || die "build resource must use key=value"
  resource_value=${resource#*=}
  [[ -n "${resource_value}" ]] || die "build resource value must not be empty"
  case "${resource%%=*}" in
    cpu-quota) build_cpu_quota=${resource_value} ;;
    memory) build_memory_limit=${resource_value} ;;
    *) die "unsupported build resource: ${resource%%=*}" ;;
  esac
}

resolve_builder_endpoint_host() {
  local endpoint=$1

  # Buildx can report either a Docker context name or a direct local socket URI.
  # Resolve context names through Docker, while keeping direct URIs untouched.
  case "${endpoint}" in
    unix:///* | npipe:///*)
      printf '%s\n' "${endpoint}"
      ;;
    *)
      docker context inspect "${endpoint}" --format '{{.Endpoints.docker.Host}}'
      ;;
  esac
}

while (( $# > 0 )); do
  case "$1" in
    --no-resource-limits)
      build_cpu_quota=''
      build_memory_limit=''
      shift
      continue
      ;;
  esac
  [[ $# -ge 2 ]] || die "missing value for $1"
  case "$1" in
    --output-dir) output_dir=$2 ;;
    --schema-contract) schema_contract=$2 ;;
    --frontend-mode) frontend_mode=$2 ;;
    --version) version=$2 ;;
    --builder) builder=$2 ;;
    --go-build-parallelism) go_build_parallelism=$2 ;;
    --media-build-parallelism) media_build_parallelism=$2 ;;
    --resource) set_build_resource "$2" ;;
    --cpu-quota)
      [[ -n "$2" ]] || die "cpu quota must not be empty"
      build_cpu_quota=$2
      ;;
    --memory)
      [[ -n "$2" ]] || die "memory limit must not be empty"
      build_memory_limit=$2
      ;;
    *) die "unsupported argument: $1" ;;
  esac
  shift 2
done
[[ -n "${schema_contract}" ]] ||
  die "schema contract must be selected explicitly with --schema-contract bridge|feature"
case "${schema_contract}" in
  feature | bridge) ;;
  *) die "schema contract must be feature or bridge" ;;
esac
case "${frontend_mode}" in
  embedded | external) ;;
  *) die "frontend mode must be embedded or external" ;;
esac
[[ "${builder}" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] ||
  die "invalid Buildx builder name"
[[ "${go_build_parallelism}" =~ ^([1-9]|[1-5][0-9]|6[0-4])$ ]] ||
  die "Go build parallelism must be an integer from 1 to 64"
[[ "${media_build_parallelism}" =~ ^([1-9]|[1-5][0-9]|6[0-4])$ ]] ||
  die "media build parallelism must be an integer from 1 to 64"
if [[ -n "${build_cpu_quota}" ]]; then
  [[ "${build_cpu_quota}" =~ ^[1-9][0-9]{0,6}$ ]] ||
    die "cpu quota must be a positive integer (microseconds per 100ms)"
fi
if [[ -n "${build_memory_limit}" ]]; then
  [[ "${build_memory_limit}" =~ ^[1-9][0-9]*([bBkKmMgGtTpP][iI]?[bB]?)?$ ]] ||
    die "memory limit must be a positive Docker size such as 4g"
fi

for command_name in docker git jq; do
  command -v "${command_name}" >/dev/null 2>&1 || die "missing ${command_name}"
done
[[ "$(git -C "${ROOT}" branch --show-current)" == production/kkrich ]] ||
  die "production builds require branch production/kkrich"
[[ -z "$(git -C "${ROOT}" status --porcelain=v1 --untracked-files=all)" ]] ||
  die "production builds require a clean worktree"

source_sha="$(git -C "${ROOT}" rev-parse HEAD)"
[[ "${source_sha}" =~ ^[0-9a-f]{40}$ ]] || die "invalid source SHA"
git -C "${ROOT}" cat-file -e "${source_sha}^{commit}" ||
  die "source commit object is unavailable"
git -C "${ROOT}" archive --format=tar "${source_sha}" >/dev/null ||
  die "source tree cannot be archived"
if [[ -z "${version}" ]]; then
  version="kkai-prod-$(date -u +%Y%m%d).$(date -u +%s)-${source_sha:0:9}"
fi
[[ "${version}" =~ ^kkai-prod-[0-9]{8}\.[1-9][0-9]*-${source_sha:0:9}$ ]] ||
  die "invalid version for source ${source_sha}"

image_tag="kkai-newapi-manual:${version}"
case "${frontend_mode}" in
  embedded) docker_target=runtime-embedded ;;
  external) docker_target=runtime-external ;;
esac
mkdir -p -- "${output_dir}"
output_dir="$(cd -- "${output_dir}" && pwd)"
archive="${output_dir}/${version}.tar"
metadata="${output_dir}/${version}.json"

acquire_build_lock
[[ ! -e "${archive}" && ! -e "${metadata}" ]] || die "release output already exists"

if ! builder_info="$(docker buildx inspect "${builder}" 2>&1)"; then
  die "Buildx builder ${builder} is unavailable"
fi
grep -Eq '^Driver:[[:space:]]+docker-container$' <<<"${builder_info}" ||
  die "Buildx builder ${builder} must use the docker-container driver"
[[ "$(grep -Ec '^Endpoint:[[:space:]]+' <<<"${builder_info}")" -eq 1 ]] ||
  die "Buildx builder ${builder} must have exactly one node endpoint"
builder_endpoint="$(sed -n 's/^Endpoint:[[:space:]]*//p' <<<"${builder_info}")"
builder_endpoint_host="$(resolve_builder_endpoint_host "${builder_endpoint}" 2>/dev/null || true)"
case "${builder_endpoint_host}" in
  unix:///* | npipe:///*) ;;
  *) die "Buildx builder ${builder} must use a local Docker context (got ${builder_endpoint})" ;;
esac
if ! builder_info="$(docker buildx inspect --bootstrap "${builder}" 2>&1)"; then
  die "Buildx builder ${builder} failed to bootstrap"
fi
grep -Eq 'linux/amd64([[:space:],/]|$)' <<<"${builder_info}" ||
  die "Buildx builder ${builder} does not advertise linux/amd64"
if [[ -n "${build_cpu_quota}" || -n "${build_memory_limit}" ]] &&
  ! docker buildx build --help 2>&1 | grep -Fq -- '--resource'; then
  die "this Docker Buildx does not support --resource; upgrade Docker Desktop or use --no-resource-limits"
fi
echo "Using Buildx builder: ${builder}"
echo "Go build parallelism: ${go_build_parallelism}"
echo "Media build parallelism: ${media_build_parallelism}"
echo "Frontend mode: ${frontend_mode}"
if [[ -n "${build_cpu_quota}" || -n "${build_memory_limit}" ]]; then
  echo "Build resource limits: cpu-quota=${build_cpu_quota:-default}, memory=${build_memory_limit:-default}"
else
  echo "Build resource limits: builder defaults"
fi

build_args=(buildx build --builder "${builder}")
if [[ -n "${build_cpu_quota}" ]]; then
  build_args+=(--resource "cpu-quota=${build_cpu_quota}")
fi
if [[ -n "${build_memory_limit}" ]]; then
  build_args+=(--resource "memory=${build_memory_limit}")
fi
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
  --target "${docker_target}" \
  --file "${ROOT}/build/kkai-image/Dockerfile" \
  --build-context "kkai_image=${ROOT}/build/kkai-image" \
  --build-arg "APP_VERSION=${version}" \
  --build-arg "KKAI_SCHEMA_CONTRACT=${schema_contract}" \
  --build-arg "KKAI_FRONTEND_MODE=${frontend_mode}" \
  --build-arg "SOURCE_REVISION=${source_sha}" \
  --build-arg "GO_BUILD_PARALLELISM=${go_build_parallelism}" \
  --build-arg "MEDIA_BUILD_PARALLELISM=${media_build_parallelism}" \
  --tag "${image_tag}" \
  --output "type=docker,dest=${archive}" \
  "${ROOT}"

archive_sha256="$(sha256_file "${archive}")"
jq --null-input \
  --arg version "${version}" \
  --arg source_sha "${source_sha}" \
  --arg image_tag "${image_tag}" \
  --arg schema_contract "${schema_contract}" \
  --arg frontend_mode "${frontend_mode}" \
  --arg archive "$(basename -- "${archive}")" \
  --arg archive_sha256 "${archive_sha256}" \
  '{
    version: $version,
    source_sha: $source_sha,
    image_tag: $image_tag,
    schema_contract: $schema_contract,
    frontend_mode: $frontend_mode,
    archive: $archive,
    archive_sha256: $archive_sha256,
    platform: "linux/amd64"
  }' > "${metadata}"

echo "MANUAL_RELEASE_METADATA=${metadata}"
