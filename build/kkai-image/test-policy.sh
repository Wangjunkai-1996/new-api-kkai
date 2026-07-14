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

contains_fixed() {
  grep -Fq -- "$1" "$2"
}

contains_regex() {
  grep -Eq -- "$1" "$2"
}

for script in export-release.sh smoke-compose.sh verify-image.sh; do
  [[ -x "${BUILD_ROOT}/${script}" ]] || fail "${script} is not executable"
done

for image_arg in BUN_IMAGE GO_IMAGE BUSYBOX_IMAGE DISTROLESS_IMAGE; do
  contains_regex "^ARG ${image_arg}=[^[:space:]]+@sha256:[0-9a-f]{64}$" \
    "${BUILD_ROOT}/Dockerfile" ||
    fail "${image_arg} is not pinned to an immutable manifest"
done
[[ "$(grep -Ec '^ARG BUN_IMAGE=' "${BUILD_ROOT}/Dockerfile")" -eq 1 ]] ||
  fail "Bun image definition is duplicated"
[[ "$(grep -Ec '^ARG GO_IMAGE=' "${BUILD_ROOT}/Dockerfile")" -eq 1 ]] ||
  fail "Go image definition is duplicated"

contains_fixed 'COPY --from=kkai_image' "${BUILD_ROOT}/Dockerfile" ||
  fail "runtime tools are not sourced from the repository build context"
contains_fixed '-o /out/new-api .' "${BUILD_ROOT}/Dockerfile" ||
  fail "image build does not compile the complete root package"
if contains_regex 'go get|go mod tidy|./main\.go' "${BUILD_ROOT}/Dockerfile"; then
  fail "image build mutates dependencies or compiles a single source file"
fi

for role in leader serving; do
  contains_fixed "NEWAPI_NODE_ROLE=${role}" "${BUILD_ROOT}/smoke-compose.sh" ||
    fail "smoke test does not exercise ${role} startup"
done
contains_fixed '--dsn-stdin' "${BUILD_ROOT}/smoke-compose.sh" ||
  fail "smoke test does not exercise migration stdin"

contains_fixed 'refs/heads/production/kkrich' "${WORKFLOW}" ||
  fail "workflow is not restricted to production/kkrich"
contains_fixed 'packages: write' "${WORKFLOW}" ||
  fail "workflow cannot publish to GHCR"
contains_fixed 'sbom: true' "${WORKFLOW}" ||
  fail "workflow does not publish BuildKit SBOM provenance"
contains_fixed 'cosign sign --yes' "${WORKFLOW}" ||
  fail "workflow does not sign the immutable digest"
contains_fixed "candidate-\${{ steps.release.outputs.version }}" "${WORKFLOW}" ||
  fail "workflow does not isolate the unscanned candidate tag"
contains_fixed 'imagetools create' "${WORKFLOW}" ||
  fail "workflow does not promote the scanned digest"
if contains_regex 'calciumion/new-api|docker\.io|ssh|ansible|workflow_run' "${WORKFLOW}"; then
  fail "production image workflow targets upstream or contains deployment behavior"
fi
if contains_regex 'uses: [^ ]+@v[0-9]' "${WORKFLOW}"; then
  fail "workflow contains an unpinned action reference"
fi

for branch_pattern in "'rebuild/**'" "'integration/**'"; do
  contains_fixed "- ${branch_pattern}" "${CANDIDATE_WORKFLOW}" ||
    fail "candidate workflow does not build ${branch_pattern}"
done
contains_fixed 'load: true' "${CANDIDATE_WORKFLOW}" ||
  fail "candidate workflow does not load the image for smoke tests"
if contains_regex 'packages: write|push: true|cosign sign|ssh|ansible' "${CANDIDATE_WORKFLOW}"; then
  fail "candidate workflow can publish or deploy"
fi
if contains_regex 'uses: [^ ]+@v[0-9]' "${CANDIDATE_WORKFLOW}"; then
  fail "candidate workflow contains an unpinned action reference"
fi

echo "KKAI production image policy passed"
