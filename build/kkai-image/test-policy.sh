#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly DOCKERFILE="${ROOT}/build/kkai-image/Dockerfile"
readonly ROOT_CONTEXT_IGNORE="${ROOT}/.dockerignore"
readonly MAKEFILE="${ROOT}/makefile"
readonly ENTRYPOINT_SOURCE="${ROOT}/build/kkai-image/cmd/entrypoint/main.go"
readonly BUILD_SCRIPT="${ROOT}/scripts/kkai/build-manual-release.sh"
readonly BUILD_TEST="${ROOT}/scripts/kkai/build-manual-release_test.sh"
readonly MEDIA_BUILD_SCRIPT="${ROOT}/build/kkai-image/ffmpeg/build.sh"
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
[[ -x "${BUILD_TEST}" ]] || fail "manual build endpoint tests are missing or not executable"
[[ -x "${DEPLOY_SCRIPT}" ]] || fail "manual deploy script is missing or not executable"
[[ -f "${DEPLOY_CONTRACT}" ]] || fail "manual deployment contract is missing"
[[ -x "${DEPLOY_TEST}" ]] || fail "manual deploy client tests are missing or not executable"
[[ -x "${FFMPEG_POLICY_TEST}" ]] || fail "FFmpeg source-build policy test is missing or not executable"

ruby -ryaml -e 'YAML.safe_load_file(ARGV.fetch(0), aliases: true)' "${QUALITY_WORKFLOW}" >/dev/null ||
  fail "invalid quality workflow YAML"
if grep -Eq 'uses: [^ ]+@v[0-9]' "${QUALITY_WORKFLOW}"; then
  fail "quality workflow contains an unpinned action reference"
fi

for image_arg in BUN_IMAGE GO_IMAGE GO_BUILD_IMAGE BUSYBOX_IMAGE DISTROLESS_IMAGE; do
  grep -Eq "^ARG ${image_arg}=[^[:space:]]+@sha256:[0-9a-f]{64}$" "${DOCKERFILE}" ||
    fail "${image_arg} is not pinned to an immutable digest"
done
[[ "$(grep -Fc -- 'FROM --platform=$BUILDPLATFORM ${GO_BUILD_IMAGE} AS backend' "${DOCKERFILE}")" -eq 1 ]] ||
  fail "backend Go compilation does not use the native build platform"
[[ "$(grep -Fc -- 'FROM --platform=$BUILDPLATFORM ${GO_BUILD_IMAGE} AS runtime-tools' "${DOCKERFILE}")" -eq 1 ]] ||
  fail "runtime tool compilation does not use the native build platform"
[[ "$(grep -Fc -- 'id=kkai-newapi-build-gate-v1,target=/var/kkai-build-gate,sharing=locked' "${DOCKERFILE}")" -eq 6 ]] ||
  fail "CPU/memory-heavy build stages must share the serialized BuildKit gate"
"${FFMPEG_POLICY_TEST}"
contains '-o /out/new-api .' "${DOCKERFILE}" || fail "Dockerfile does not build the application"
contains '-o /out/kkai-migrate ./cmd/kkai-migrate' "${DOCKERFILE}" ||
  fail "Dockerfile does not retain /kkai-migrate"
contains '-o /out/kkai-video-archive-once ./cmd/kkai-video-archive-once' "${DOCKERFILE}" ||
  fail "Dockerfile does not build /kkai-video-archive-once"
contains '/out/kkai-video-archive-once /kkai-video-archive-once' "${DOCKERFILE}" ||
  fail "runtime image does not contain /kkai-video-archive-once"
contains 'arguments[0] == "kkai-video-archive-once"' "${ENTRYPOINT_SOURCE}" ||
  fail "entrypoint does not dispatch the exact archive command"
contains 'ARG KKAI_SCHEMA_CONTRACT=feature' "${DOCKERFILE}" ||
  fail "Dockerfile does not default to the feature schema contract"
contains 'GOFLAGS=-tags=kkai_bridge' "${DOCKERFILE}" ||
  fail "Dockerfile cannot compile the explicit bridge schema contract"
contains 'io.kkrich.schema-contract="${KKAI_SCHEMA_CONTRACT}"' "${DOCKERFILE}" ||
  fail "runtime image does not identify its schema contract"
[[ "$(grep -Fc 'common.SchemaManagementMode=external' "${DOCKERFILE}")" -eq 3 ]] ||
  fail "application, migrator, and archive executor must compile with external schema management"
[[ "$(grep -Fc -- 'bun install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1' "${DOCKERFILE}")" -eq 1 ]] ||
  fail "frontend dependencies must use one serialized, shared install stage"
[[ "$(grep -Fc -- 'bun install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1' "${MAKEFILE}")" -eq 1 ]] ||
  fail "local all-web builds must install dependencies once with bounded lifecycle concurrency"
contains 'build-web: web-install' "${MAKEFILE}" ||
  fail "the default local web build does not reuse the shared dependency install"
contains 'build-web-classic: web-install' "${MAKEFILE}" ||
  fail "the classic local web build does not reuse the shared dependency install"
! contains '$(cat ../../VERSION)' "${MAKEFILE}" ||
  fail "local web builds still use the broken Make variable version expression"
contains 'id=kkai-newapi-bun-v1,target=/root/.bun/install/cache,sharing=locked' "${DOCKERFILE}" ||
  fail "frontend dependency downloads do not use a persistent locked cache"
contains 'FROM web-deps AS web-default' "${DOCKERFILE}" ||
  fail "default frontend does not reuse the shared dependency stage"
contains 'VITE_KKAI_SCHEMA_CONTRACT="${KKAI_SCHEMA_CONTRACT}"' "${DOCKERFILE}" ||
  fail "default frontend is not bound to the immutable schema contract"
contains 'FROM web-deps AS web-classic' "${DOCKERFILE}" ||
  fail "classic frontend does not reuse the shared dependency stage"
for ignored_path in '.local-releases/' '/docs-site/' '/electron/' '/scripts/kkai/manual-deployment-contract.env' '/web/default/.tanstack/'; do
  contains "${ignored_path}" "${ROOT_CONTEXT_IGNORE}" ||
    fail "Docker build context does not exclude ${ignored_path}"
done
contains 'id=kkai-newapi-go-build-v1,target=/root/.cache/go-build,sharing=locked' "${DOCKERFILE}" ||
  fail "Go compiler cache mount is missing"
[[ "$(grep -Fc -- 'go build -p "${GO_BUILD_PARALLELISM}"' "${DOCKERFILE}")" -eq 5 ]] ||
  fail "all Go production tools must honor the bounded build parallelism"
contains 'ARG GO_BUILD_PARALLELISM=4' "${DOCKERFILE}" ||
  fail "Dockerfile does not define a bounded Go build parallelism"
contains 'ARG MEDIA_BUILD_PARALLELISM=2' "${DOCKERFILE}" ||
  fail "Dockerfile does not define a bounded media build parallelism"
contains 'MEDIA_BUILD_PARALLELISM="${MEDIA_BUILD_PARALLELISM}"' "${DOCKERFILE}" ||
  fail "Dockerfile does not forward the media build parallelism"
contains 'media_build_parallelism="${KKAI_MEDIA_BUILD_PARALLELISM:-2}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not set a conservative media build parallelism"
contains '--media-build-parallelism) media_build_parallelism=$2' "${BUILD_SCRIPT}" ||
  fail "manual build cannot tune media build parallelism"
contains '--build-arg "MEDIA_BUILD_PARALLELISM=${media_build_parallelism}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not pass media build parallelism"
contains 'media_build_parallelism=${MEDIA_BUILD_PARALLELISM:-2}' "${MEDIA_BUILD_SCRIPT}" ||
  fail "FFmpeg build does not define the media parallelism default"
[[ "$(grep -Fc -- 'make -j"${media_build_parallelism}"' "${MEDIA_BUILD_SCRIPT}")" -eq 2 ]] ||
  fail "x264 and FFmpeg builds must honor the bounded media parallelism"
contains '--platform linux/amd64' "${BUILD_SCRIPT}" || fail "manual build is not pinned to AMD64"
contains 'builder="${BUILDX_BUILDER:-kkai-mirror-builder}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not select the configured Buildx builder"
contains 'docker buildx inspect --bootstrap "${builder}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not validate the selected Buildx builder"
contains '--builder "${builder}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not pass the selected Buildx builder"
contains "exactly one node endpoint" "${BUILD_SCRIPT}" ||
  fail "manual build does not reject multi-node builders"
contains 'docker context inspect "${endpoint}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not inspect the selected Docker context"
contains 'unix:///* | npipe:///*' "${BUILD_SCRIPT}" ||
  fail "manual build does not require a local Docker socket"
contains 'resolve_builder_endpoint_host' "${BUILD_SCRIPT}" ||
  fail "manual build does not resolve builder endpoints centrally"
contains '--resource "cpu-quota=${build_cpu_quota}"' "${BUILD_SCRIPT}" ||
  fail "manual build cannot apply an optional CPU quota"
contains '--resource "memory=${build_memory_limit}"' "${BUILD_SCRIPT}" ||
  fail "manual build cannot apply an optional memory limit"
contains 'KKAI_BUILDX_CPU_QUOTA' "${BUILD_SCRIPT}" ||
  fail "manual build does not expose the optional CPU quota setting"
contains 'KKAI_BUILDX_MEMORY' "${BUILD_SCRIPT}" ||
  fail "manual build does not expose the optional memory setting"
contains 'build_cpu_quota="${KKAI_BUILDX_CPU_QUOTA:-200000}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not set a conservative CPU limit by default"
contains 'build_memory_limit="${KKAI_BUILDX_MEMORY:-3g}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not set a conservative memory limit by default"
contains '--no-resource-limits' "${BUILD_SCRIPT}" ||
  fail "manual build cannot explicitly disable resource limits"
contains 'kkai-newapi-manual-build.lock' "${BUILD_SCRIPT}" ||
  fail "manual build does not serialize local builds"
contains 'export GIT_NO_LAZY_FETCH=1' "${BUILD_SCRIPT}" ||
  fail "manual build does not disable lazy Git fetches"
contains 'cat-file -e "${source_sha}^{commit}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not validate the source commit object"
contains 'archive --format=tar "${source_sha}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not validate the source tree archive"
contains '--build-arg "GO_BUILD_PARALLELISM=${go_build_parallelism}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not pass the bounded Go parallelism"
contains 'production/kkrich' "${BUILD_SCRIPT}" || fail "manual build does not require the production branch"
contains 'status --porcelain=v1 --untracked-files=all' "${BUILD_SCRIPT}" ||
  fail "manual build does not require a clean worktree"
contains '--output "type=docker,dest=${archive}"' "${BUILD_SCRIPT}" ||
  fail "manual build does not export a Docker archive"
contains 'archive_sha256' "${BUILD_SCRIPT}" || fail "manual build omits archive integrity metadata"
contains "schema_contract=''" "${BUILD_SCRIPT}" ||
  fail "manual builds retain an implicit schema contract"
contains 'schema contract must be selected explicitly with --schema-contract bridge|feature' "${BUILD_SCRIPT}" ||
  fail "manual builds do not require explicit schema contract selection"
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

contains 'readonly HOST=sys1' "${DEPLOY_SCRIPT}" || fail "manual deploy does not use the primary sys1 route"
contains 'ProxyCommand=none' "${DEPLOY_SCRIPT}" || fail "manual deploy may use an SSH proxy"
contains 'usage: deploy-manual-release.sh --stage METADATA.json' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not require an explicit stage action"
contains 'kkai-newapi-manual-deploy stage' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not stage through the production controller"
contains 'candidate-status exactly once' "${DEPLOY_SCRIPT}" ||
  fail "manual deploy does not route uncertain stage outcomes through candidate-status"
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
contains 'KKAI_INFRA_SHA=97cbe7d4b24a324dcdeb84d94d3617087007a638' "${DEPLOY_CONTRACT}" ||
  fail "manual deployment contract does not pin the approved infrastructure commit"
contains 'KKAI_DEPLOYMENT_PROTOCOL=router-v3-staged' "${DEPLOY_CONTRACT}" ||
  fail "manual deployment contract does not pin the staged protocol"

if grep -Eiq 'github actions|ghcr\.io|cosign|repository_dispatch|newapi_image_ready' \
  "${BUILD_SCRIPT}" "${DEPLOY_SCRIPT}"; then
  fail "manual delivery scripts still contain automatic delivery behavior"
fi

"${BUILD_TEST}"

echo "KKAI manual production image policy passed"
