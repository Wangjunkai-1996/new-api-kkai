#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly DOCKERFILE="${ROOT}/build/kkai-image/Dockerfile"
readonly BUILD_SCRIPT="${ROOT}/scripts/kkai/build-manual-release.sh"
readonly DEPLOY_SCRIPT="${ROOT}/scripts/kkai/deploy-manual-release.sh"
readonly RETIRED_WORKFLOW="${ROOT}/.github/workflows/kkai-production-image.yml"
readonly RETIRED_HEAD_CHECK="${ROOT}/scripts/kkai/require-production-head.sh"
readonly QUALITY_WORKFLOW="${ROOT}/.github/workflows/kkai-fork-quality.yml"

fail() {
  echo "KKAI image policy: $*" >&2
  exit 1
}

contains() {
  grep -Fq -- "$1" "$2"
}

[[ ! -e "${RETIRED_WORKFLOW}" ]] || fail "automatic production workflow still exists"
[[ ! -e "${RETIRED_HEAD_CHECK}" ]] || fail "automatic production head check still exists"
[[ -x "${BUILD_SCRIPT}" ]] || fail "manual build script is missing or not executable"
[[ -x "${DEPLOY_SCRIPT}" ]] || fail "manual deploy script is missing or not executable"

ruby -ryaml -e 'YAML.safe_load_file(ARGV.fetch(0), aliases: true)' "${QUALITY_WORKFLOW}" >/dev/null ||
  fail "invalid quality workflow YAML"
if grep -Eq 'uses: [^ ]+@v[0-9]' "${QUALITY_WORKFLOW}"; then
  fail "quality workflow contains an unpinned action reference"
fi

for image_arg in BUN_IMAGE GO_IMAGE BUSYBOX_IMAGE DISTROLESS_IMAGE; do
  grep -Eq "^ARG ${image_arg}=[^[:space:]]+@sha256:[0-9a-f]{64}$" "${DOCKERFILE}" ||
    fail "${image_arg} is not pinned to an immutable digest"
done
contains '-o /out/new-api .' "${DOCKERFILE}" || fail "Dockerfile does not build the application"
contains '-o /out/kkai-migrate ./cmd/kkai-migrate' "${DOCKERFILE}" ||
  fail "Dockerfile does not retain /kkai-migrate"
[[ "$(grep -Fc 'common.SchemaManagementMode=external' "${DOCKERFILE}")" -eq 2 ]] ||
  fail "application and migrator must compile with external schema management"

contains '--platform linux/amd64' "${BUILD_SCRIPT}" || fail "manual build is not pinned to AMD64"
contains 'production/kkrich' "${BUILD_SCRIPT}" || fail "manual build does not require the production branch"
contains 'status --porcelain=v1 --untracked-files=all' "${BUILD_SCRIPT}" ||
  fail "manual build does not require a clean worktree"
contains '--output "type=docker,dest=${archive}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not export a Docker archive"
contains 'archive_sha256' "${BUILD_SCRIPT}" || fail "manual build omits archive integrity metadata"

contains 'tokk@10.203.0.1' "${DEPLOY_SCRIPT}" || fail "manual deploy does not use the private host"
contains 'ProxyCommand=none' "${DEPLOY_SCRIPT}" || fail "manual deploy may use an SSH proxy"
contains 'kkai-newapi-manual-deploy deploy' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not use the production controller"
contains 'archive checksum mismatch' "${DEPLOY_SCRIPT}" || fail "manual deploy omits local archive verification"

if grep -Eiq 'github actions|ghcr\.io|cosign|repository_dispatch|newapi_image_ready' \
  "${BUILD_SCRIPT}" "${DEPLOY_SCRIPT}"; then
  fail "manual delivery scripts still contain automatic delivery behavior"
fi

echo "KKAI manual production image policy passed"
