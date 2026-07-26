#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly DEPLOY_SCRIPT="${ROOT}/scripts/kkai/deploy-manual-release.sh"
readonly CONTRACT="${ROOT}/scripts/kkai/manual-deployment-contract.env"

fail() {
  echo "deploy-manual-release test: $*" >&2
  exit 1
}

test_root="$(mktemp -d "${TMPDIR:-/tmp}/kkai-manual-deploy-test.XXXXXX")"
trap 'rm -rf -- "${test_root}"' EXIT
readonly test_root
readonly mock_bin="${test_root}/bin"
readonly call_log="${test_root}/calls.log"
mkdir -p -- "${mock_bin}"

# shellcheck source=manual-deployment-contract.env
source "${CONTRACT}"
readonly KKAI_INFRA_SHA KKAI_DEPLOYMENT_PROTOCOL
export KKAI_TEST_EXPECTED_INFRA_SHA="${KKAI_INFRA_SHA}"
export KKAI_TEST_EXPECTED_PROTOCOL="${KKAI_DEPLOYMENT_PROTOCOL}"

cat > "${mock_bin}/ssh" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'ssh %s\n' "$*" >> "${KKAI_TEST_LOG}"
case "$*" in
  *'/kkai-newapi-manual-deploy preflight '*)
    case "${KKAI_TEST_PREFLIGHT_MODE:-ready}" in
      ready)
        printf 'KKAI_PREFLIGHT_RESULT=ready\n'
        printf 'KKAI_DEPLOYMENT_PROTOCOL=%s\n' "${KKAI_TEST_EXPECTED_PROTOCOL}"
        printf 'KKAI_INFRA_SHA=%s\n' "${KKAI_TEST_EXPECTED_INFRA_SHA}"
        exit 0
        ;;
      wrong-sha)
        printf 'KKAI_PREFLIGHT_RESULT=ready\n'
        printf 'KKAI_DEPLOYMENT_PROTOCOL=%s\n' "${KKAI_TEST_EXPECTED_PROTOCOL}"
        printf 'KKAI_INFRA_SHA=%040d\n' 0
        exit 0
        ;;
      fail)
        exit 42
        ;;
      *)
        exit 43
        ;;
    esac
    ;;
  *'/kkai-newapi-manual-deploy deploy '*)
    printf 'KKAI_DEPLOY_RESULT=deployed\n'
    exit 0
    ;;
  *)
    exit 44
    ;;
esac
EOF

cat > "${mock_bin}/scp" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'scp %s\n' "$*" >> "${KKAI_TEST_LOG}"
EOF
chmod 0755 "${mock_bin}/ssh" "${mock_bin}/scp"

readonly source_sha=1111111111111111111111111111111111111111
readonly version=kkai-prod-20260726.1-111111111
readonly archive="${test_root}/${version}.tar"
readonly metadata="${test_root}/${version}.json"
printf 'immutable archive fixture\n' > "${archive}"
archive_sha256="$(shasum -a 256 "${archive}" | awk '{print $1}')"
readonly archive_sha256
jq --null-input \
  --arg version "${version}" \
  --arg source_sha "${source_sha}" \
  --arg image_tag "kkai-newapi-manual:${version}" \
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

run_deploy() {
  local mode=$1

  : > "${call_log}"
  PATH="${mock_bin}:${PATH}" \
    KKAI_TEST_LOG="${call_log}" \
    KKAI_TEST_PREFLIGHT_MODE="${mode}" \
    "${DEPLOY_SCRIPT}" "${metadata}"
}

test_preflight_failure_prevents_upload() {
  local output

  if output="$(run_deploy fail 2>&1)"; then
    fail "failed preflight unexpectedly allowed deployment"
  fi
  grep -F 'production preflight failed; archive was not uploaded' <<< "${output}" >/dev/null ||
    fail "failed preflight did not explain the upload boundary"
  ! grep -q '^scp ' "${call_log}" || fail "archive was uploaded after failed preflight"
  ! grep -q '/kkai-newapi-manual-deploy deploy ' "${call_log}" ||
    fail "deploy was invoked after failed preflight"
}

test_preflight_output_must_match_contract() {
  local output

  if output="$(run_deploy wrong-sha 2>&1)"; then
    fail "mismatched preflight SHA unexpectedly allowed deployment"
  fi
  grep -F 'production preflight infrastructure SHA mismatch' <<< "${output}" >/dev/null ||
    fail "mismatched preflight SHA was not rejected"
  ! grep -q '^scp ' "${call_log}" || fail "archive was uploaded after a preflight SHA mismatch"
}

test_successful_preflight_precedes_upload_and_deploy() {
  local output preflight_line upload_line deploy_line contract_arguments

  output="$(run_deploy ready)"
  grep -Fx 'KKAI_PREFLIGHT_RESULT=ready' <<< "${output}" >/dev/null ||
    fail "ready preflight output was not preserved"
  preflight_line="$(grep -n '/kkai-newapi-manual-deploy preflight ' "${call_log}" | cut -d: -f1)"
  upload_line="$(grep -n '^scp ' "${call_log}" | cut -d: -f1)"
  deploy_line="$(grep -n '/kkai-newapi-manual-deploy deploy ' "${call_log}" | cut -d: -f1)"
  [[ "${preflight_line}" -lt "${upload_line}" && "${upload_line}" -lt "${deploy_line}" ]] ||
    fail "preflight, upload, and deploy order is invalid"
  contract_arguments="--expected-infra-sha ${KKAI_INFRA_SHA} --deployment-protocol ${KKAI_DEPLOYMENT_PROTOCOL}"
  [[ "$(grep -Fc -- "${contract_arguments}" "${call_log}")" -eq 2 ]] ||
    fail "preflight and deploy did not share the pinned contract"
}

test_preflight_failure_prevents_upload
test_preflight_output_must_match_contract
test_successful_preflight_precedes_upload_and_deploy

echo 'New API manual deploy client tests passed'
