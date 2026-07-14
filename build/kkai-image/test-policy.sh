#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly BUILD_ROOT="${ROOT}/build/kkai-image"
readonly WORKFLOW="${ROOT}/.github/workflows/kkai-production-image.yml"
readonly CANDIDATE_WORKFLOW="${ROOT}/.github/workflows/kkai-image-candidate.yml"

fail() {
  echo "KKAI image policy: $*" >&2
  exit 1
}

for script in export-release.sh smoke-compose.sh verify-image.sh; do
  [[ -x "${BUILD_ROOT}/${script}" ]] || fail "${script} is not executable"
done

for image_arg in BUN_IMAGE GO_IMAGE BUSYBOX_IMAGE DISTROLESS_IMAGE; do
  rg -e "^ARG ${image_arg}=[^[:space:]]+@sha256:[0-9a-f]{64}$" \
    "${BUILD_ROOT}/Dockerfile" >/dev/null ||
    fail "${image_arg} is not pinned to an immutable manifest"
done
[[ "$(rg -c '^ARG BUN_IMAGE=' "${BUILD_ROOT}/Dockerfile")" -eq 1 ]] ||
  fail "Bun image definition is duplicated"
[[ "$(rg -c '^ARG GO_IMAGE=' "${BUILD_ROOT}/Dockerfile")" -eq 1 ]] ||
  fail "Go image definition is duplicated"

rg -F 'COPY --from=kkai_image' "${BUILD_ROOT}/Dockerfile" >/dev/null ||
  fail "runtime tools are not sourced from the repository build context"
rg -F -- '-o /out/new-api .' "${BUILD_ROOT}/Dockerfile" >/dev/null ||
  fail "image build does not compile the complete root package"
if rg -n 'go get|go mod tidy|./main\.go' "${BUILD_ROOT}/Dockerfile" >/dev/null; then
  fail "image build mutates dependencies or compiles a single source file"
fi

for role in leader serving; do
  rg -F "NEWAPI_NODE_ROLE=${role}" "${BUILD_ROOT}/smoke-compose.sh" >/dev/null ||
    fail "smoke test does not exercise ${role} startup"
done
rg -F -- '--dsn-stdin' "${BUILD_ROOT}/smoke-compose.sh" >/dev/null ||
  fail "smoke test does not exercise migration stdin"

rg -F 'refs/heads/production/kkrich' "${WORKFLOW}" >/dev/null ||
  fail "workflow is not restricted to production/kkrich"
rg -F 'packages: write' "${WORKFLOW}" >/dev/null ||
  fail "workflow cannot publish to GHCR"
rg -F 'sbom: true' "${WORKFLOW}" >/dev/null ||
  fail "workflow does not publish BuildKit SBOM provenance"
rg -F 'cosign sign --yes' "${WORKFLOW}" >/dev/null ||
  fail "workflow does not sign the immutable digest"
rg -F 'candidate-${{ steps.release.outputs.version }}' "${WORKFLOW}" >/dev/null ||
  fail "workflow does not isolate the unscanned candidate tag"
rg -F 'imagetools create' "${WORKFLOW}" >/dev/null ||
  fail "workflow does not promote the scanned digest"
if rg -n 'calciumion/new-api|docker\.io|ssh|ansible|workflow_run' "${WORKFLOW}" >/dev/null; then
  fail "production image workflow targets upstream or contains deployment behavior"
fi
if rg -n 'uses: [^ ]+@v[0-9]' "${WORKFLOW}" >/dev/null; then
  fail "workflow contains an unpinned action reference"
fi

for branch_pattern in "'rebuild/**'" "'integration/**'"; do
  rg -F -- "- ${branch_pattern}" "${CANDIDATE_WORKFLOW}" >/dev/null ||
    fail "candidate workflow does not build ${branch_pattern}"
done
rg -F 'load: true' "${CANDIDATE_WORKFLOW}" >/dev/null ||
  fail "candidate workflow does not load the image for smoke tests"
if rg -n 'packages: write|push: true|cosign sign|ssh|ansible' "${CANDIDATE_WORKFLOW}" >/dev/null; then
  fail "candidate workflow can publish or deploy"
fi
if rg -n 'uses: [^ ]+@v[0-9]' "${CANDIDATE_WORKFLOW}" >/dev/null; then
  fail "candidate workflow contains an unpinned action reference"
fi

echo "KKAI production image policy passed"
