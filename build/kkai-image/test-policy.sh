#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly DOCKERFILE="${ROOT}/build/kkai-image/Dockerfile"
readonly BUILD_SCRIPT="${ROOT}/scripts/kkai/build-manual-release.sh"
readonly DEPLOY_SCRIPT="${ROOT}/scripts/kkai/deploy-manual-release.sh"
readonly DEPLOY_CONTRACT="${ROOT}/scripts/kkai/manual-deployment-contract.env"
readonly DEPLOY_TEST="${ROOT}/scripts/kkai/deploy-manual-release_test.sh"
readonly FFMPEG_POLICY_TEST="${ROOT}/build/kkai-image/test-ffmpeg-policy.sh"
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
[[ -f "${DEPLOY_CONTRACT}" ]] || fail "manual deployment contract is missing"
[[ -x "${DEPLOY_TEST}" ]] || fail "manual deploy client tests are missing or not executable"
[[ -x "${FFMPEG_POLICY_TEST}" ]] || fail "FFmpeg source-build policy test is missing or not executable"

ruby -ryaml -e 'YAML.safe_load_file(ARGV.fetch(0), aliases: true)' "${QUALITY_WORKFLOW}" >/dev/null ||
  fail "invalid quality workflow YAML"
if grep -Eq 'uses: [^ ]+@v[0-9]' "${QUALITY_WORKFLOW}"; then
  fail "quality workflow contains an unpinned action reference"
fi

for image_arg in BUN_IMAGE GO_IMAGE BUSYBOX_IMAGE DISTROLESS_IMAGE; do
  grep -Eq "^ARG ${image_arg}=[^[:space:]]+@sha256:[0-9a-f]{64}$" "${DOCKERFILE}" ||
    fail "${image_arg} is not pinned to an immutable digest"
done
"${FFMPEG_POLICY_TEST}"
contains '-o /out/new-api .' "${DOCKERFILE}" || fail "Dockerfile does not build the application"
contains '-o /out/kkai-migrate ./cmd/kkai-migrate' "${DOCKERFILE}" ||
  fail "Dockerfile does not retain /kkai-migrate"
contains 'ARG KKAI_SCHEMA_CONTRACT=feature' "${DOCKERFILE}" ||
  fail "Dockerfile does not default to the feature schema contract"
contains 'GOFLAGS=-tags=kkai_bridge' "${DOCKERFILE}" ||
  fail "Dockerfile cannot compile the explicit bridge schema contract"
contains 'io.kkrich.schema-contract="${KKAI_SCHEMA_CONTRACT}"' "${DOCKERFILE}" ||
  fail "runtime image does not identify its schema contract"
[[ "$(grep -Fc 'common.SchemaManagementMode=external' "${DOCKERFILE}")" -eq 2 ]] ||
  fail "application and migrator must compile with external schema management"
[[ "$(grep -Fc -- 'bun install --frozen-lockfile --network-concurrency=1' "${DOCKERFILE}")" -eq 1 ]] ||
  fail "frontend dependencies must use one serialized, shared install stage"
contains 'id=kkai-newapi-bun-v1,target=/root/.bun/install/cache,sharing=locked' "${DOCKERFILE}" ||
  fail "frontend dependency downloads do not use a persistent locked cache"
contains 'FROM web-deps AS web-default' "${DOCKERFILE}" ||
  fail "default frontend does not reuse the shared dependency stage"
contains 'FROM web-deps AS web-classic' "${DOCKERFILE}" ||
  fail "classic frontend does not reuse the shared dependency stage"
contains '--platform linux/amd64' "${BUILD_SCRIPT}" || fail "manual build is not pinned to AMD64"
contains 'production/kkrich' "${BUILD_SCRIPT}" || fail "manual build does not require the production branch"
contains 'status --porcelain=v1 --untracked-files=all' "${BUILD_SCRIPT}" ||
  fail "manual build does not require a clean worktree"
contains '--output "type=docker,dest=${archive}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not export a Docker archive"
contains 'archive_sha256' "${BUILD_SCRIPT}" || fail "manual build omits archive integrity metadata"
contains 'schema_contract=feature' "${BUILD_SCRIPT}" ||
  fail "manual builds do not default to the feature schema contract"
contains '--schema-contract) schema_contract=$2' "${BUILD_SCRIPT}" ||
  fail "manual builds cannot explicitly select the bridge schema contract"
contains '--build-arg "KKAI_SCHEMA_CONTRACT=${schema_contract}"' "${BUILD_SCRIPT}" ||
  fail "manual builds do not bind the selected schema contract into the image"
contains 'schema_contract: $schema_contract' "${BUILD_SCRIPT}" ||
  fail "release metadata does not identify the schema contract"
contains 'BUILD_HTTP_PROXY' "${BUILD_SCRIPT}" || fail "manual build cannot accept an HTTP proxy"
contains '--build-arg "HTTP_PROXY=${build_http_proxy}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not forward the HTTP proxy into build stages"
contains '--build-arg "HTTPS_PROXY=${build_https_proxy}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not forward the HTTPS proxy into build stages"
contains '--build-arg "http_proxy=${build_http_proxy}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not forward the lowercase HTTP proxy into build stages"
contains '--build-arg "https_proxy=${build_https_proxy}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not forward the lowercase HTTPS proxy into build stages"

contains 'tokk@10.203.0.1' "${DEPLOY_SCRIPT}" || fail "manual deploy does not use the private host"
contains 'ProxyCommand=none' "${DEPLOY_SCRIPT}" || fail "manual deploy may use an SSH proxy"
contains 'usage: deploy-manual-release.sh --stage METADATA.json' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not require an explicit stage action"
contains 'kkai-newapi-manual-deploy stage' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not stage through the production controller"
contains '--schema-contract "${schema_contract}"' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not bind release metadata to the staged schema contract"
! contains 'kkai-newapi-manual-deploy deploy' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy still invokes the legacy one-step action"
contains 'kkai-newapi-manual-deploy preflight' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not run production preflight"
contains '--expected-infra-sha "${KKAI_INFRA_SHA}"' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not pin the infrastructure SHA"
contains '--deployment-protocol "${KKAI_DEPLOYMENT_PROTOCOL}"' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not pin the deployment protocol"
contains 'archive checksum mismatch' "${DEPLOY_SCRIPT}" || fail "manual deploy omits local archive verification"
contains 'KKAI_INFRA_SHA=3c3e5a6658bf6bdba82692843165d1880b3d583d' "${DEPLOY_CONTRACT}" ||
  fail "manual deployment contract does not pin the approved infrastructure commit"
contains 'KKAI_DEPLOYMENT_PROTOCOL=router-v3-staged' "${DEPLOY_CONTRACT}" ||
  fail "manual deployment contract does not pin the staged protocol"

if grep -Eiq 'github actions|ghcr\.io|cosign|repository_dispatch|newapi_image_ready' \
  "${BUILD_SCRIPT}" "${DEPLOY_SCRIPT}"; then
  fail "manual delivery scripts still contain automatic delivery behavior"
fi

echo "KKAI manual production image policy passed"
