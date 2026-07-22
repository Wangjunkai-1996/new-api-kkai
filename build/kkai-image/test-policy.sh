#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly BUILD_ROOT="${ROOT}/build/kkai-image"
readonly DOCKERFILE="${BUILD_ROOT}/Dockerfile"
readonly PRODUCTION_WORKFLOW="${ROOT}/.github/workflows/kkai-production-image.yml"
readonly PRODUCTION_HEAD_CHECK="${ROOT}/scripts/kkai/require-production-head.sh"
readonly QUALITY_WORKFLOW="${ROOT}/.github/workflows/kkai-fork-quality.yml"

fail() {
  echo "KKAI image policy: $*" >&2
  exit 1
}

contains() {
  grep -Fq -- "$1" "$2"
}

rejects() {
  if grep -Eiq -- "$1" "$2"; then
    fail "$2 still contains retired delivery behavior matching: $1"
  fi
}

for workflow in "${PRODUCTION_WORKFLOW}" "${QUALITY_WORKFLOW}"; do
  ruby -ryaml -e 'YAML.safe_load_file(ARGV.fetch(0), aliases: true)' "${workflow}" >/dev/null ||
    fail "invalid workflow YAML: ${workflow}"
  if grep -Eq 'uses: [^ ]+@v[0-9]' "${workflow}"; then
    fail "workflow contains an unpinned action reference: ${workflow}"
  fi
done

ruby -ryaml -e '
  jobs = YAML.safe_load_file(ARGV.fetch(0), aliases: true).fetch("jobs").keys.sort
  exit(jobs == %w[build notify-infra] ? 0 : 1)
' "${PRODUCTION_WORKFLOW}" ||
  fail "production workflow must contain only build and notify-infra jobs"

for image_arg in BUN_IMAGE GO_IMAGE BUSYBOX_IMAGE DISTROLESS_IMAGE; do
  grep -Eq "^ARG ${image_arg}=[^[:space:]]+@sha256:[0-9a-f]{64}$" "${DOCKERFILE}" ||
    fail "${image_arg} is not pinned to an immutable digest"
done

contains '-o /out/new-api .' "${DOCKERFILE}" ||
  fail "Dockerfile does not build the application"
contains '-o /out/kkai-migrate ./cmd/kkai-migrate' "${DOCKERFILE}" ||
  fail "Dockerfile does not retain /kkai-migrate"
[[ "$(grep -Fc 'common.SchemaManagementMode=external' "${DOCKERFILE}")" -eq 2 ]] ||
  fail "application and migrator must compile with schema management fixed to external"
rejects 'ARG SCHEMA_|com\.kkai\.(runtime\.schema-management|schema\.)' "${DOCKERFILE}"
rejects 'go test|new-api-canary-probe|newapi-schema-bootstrap' "${DOCKERFILE}"

for retired_path in \
  build/kkai-image/export-release.sh \
  build/kkai-image/export-schema-contract.sh \
  build/kkai-image/smoke-compose.sh \
  build/kkai-image/smoke-compose.yml \
  build/kkai-image/verify-image.sh \
  build/kkai-image/wait-for-promoted-tags.sh \
  build/kkai-image/test-wait-for-promoted-tags.sh \
  build/kkai-image/cmd/canaryprobe \
  .github/workflows/kkai-image-candidate.yml \
  cmd/kkai-topup-recovery \
  cmd/newapi-schema-bootstrap \
  pkg/topuprecovery; do
  retired="${ROOT}/${retired_path}"
  if [[ -f "${retired}" ]] ||
    { [[ -d "${retired}" ]] && find "${retired}" -type f -print -quit | grep -q .; }; then
    fail "retired path still contains files: ${retired_path}"
  fi
done

contains 'refs/heads/production/kkrich' "${PRODUCTION_WORKFLOW}" ||
  fail "production workflow is not restricted to production/kkrich"
# This is GitHub expression syntax matched literally.
# shellcheck disable=SC2016
contains 'PUSH_TIMESTAMP: ${{ github.event.head_commit.timestamp }}' "${PRODUCTION_WORKFLOW}" ||
  fail "release version date is not bound to the immutable push event"
# This is workflow shell source matched literally.
# shellcheck disable=SC2016
contains 'RELEASE_DATE="$(date -u --date="${PUSH_TIMESTAMP}" +%Y%m%d)"' "${PRODUCTION_WORKFLOW}" ||
  fail "release version date does not use the immutable push timestamp"
rejects 'date[[:space:]]+-u[[:space:]]+\+%Y%m%d' "${PRODUCTION_WORKFLOW}"
[[ -x "${PRODUCTION_HEAD_CHECK}" ]] ||
  fail "production branch freshness check is missing or not executable"
contains 'git ls-remote --exit-code origin refs/heads/production/kkrich' "${PRODUCTION_HEAD_CHECK}" ||
  fail "production branch freshness check does not read the remote production HEAD"
# This is workflow shell source matched literally.
# shellcheck disable=SC2016
[[ "$(grep -Fc 'scripts/kkai/require-production-head.sh "${SOURCE_SHA}"' "${PRODUCTION_WORKFLOW}")" -eq 3 ]] ||
  fail "build, signing, and dispatch must each verify the remote production HEAD"
contains '    paths-ignore:' "${PRODUCTION_WORKFLOW}" ||
  fail "production workflow does not ignore documentation-only pushes"
contains "      - '**/*.md'" "${PRODUCTION_WORKFLOW}" ||
  fail "Markdown-only pushes can trigger a production image"
contains '  cancel-in-progress: false' "${PRODUCTION_WORKFLOW}" ||
  fail "production queue can cancel an in-progress release"
contains 'permissions: {}' "${PRODUCTION_WORKFLOW}" ||
  fail "production workflow lacks a deny-by-default permission boundary"
contains '      packages: write' "${PRODUCTION_WORKFLOW}" ||
  fail "production build cannot push to GHCR"
contains '      id-token: write' "${PRODUCTION_WORKFLOW}" ||
  fail "production build cannot obtain a Cosign identity token"
contains '          push: true' "${PRODUCTION_WORKFLOW}" ||
  fail "production build does not push its image"
# These literals are GitHub expression syntax, not shell expansions.
# shellcheck disable=SC2016
contains '${{ env.IMAGE }}:${{ steps.release.outputs.version }}' "${PRODUCTION_WORKFLOW}" ||
  fail "production build omits the version tag"
# shellcheck disable=SC2016
contains '${{ env.IMAGE }}:sha-${{ steps.release.outputs.source_sha }}' "${PRODUCTION_WORKFLOW}" ||
  fail "production build omits the source SHA tag"
# shellcheck disable=SC2016
contains 'run: cosign sign --yes "${IMAGE}@${DIGEST}"' "${PRODUCTION_WORKFLOW}" ||
  fail "production workflow does not sign the pushed digest"
contains 'event_type: "newapi_image_ready"' "${PRODUCTION_WORKFLOW}" ||
  fail "production workflow does not notify infrastructure intake"
[[ "$(grep -Ec '^[[:space:]]+--arg (source_sha|version|digest) ' "${PRODUCTION_WORKFLOW}")" -eq 3 ]] ||
  fail "infrastructure dispatch must contain source_sha, version, and digest only"
rejects 'build_run|candidate-|imagetools|schema.contract|schema_contract|schema-bootstrap|check-fork-quality|verify-image|smoke-compose|trivy|sbom:[[:space:]]+true|upload-artifact|export-release|cosign verify|offline|evidence' "${PRODUCTION_WORKFLOW}"
rejects 'ssh|ansible|workflow_run|workflow_dispatch|kkai-prod-deploy|kkai-newapi-deploy|systemctl|docker[[:space:]]+compose' "${PRODUCTION_WORKFLOW}"
if contains '      - production/kkrich' "${QUALITY_WORKFLOW}"; then
  fail "fork quality still runs beside every production release"
fi
contains '  pull_request:' "${QUALITY_WORKFLOW}" ||
  fail "fork quality no longer covers pull requests"

echo "KKAI production image policy passed"
