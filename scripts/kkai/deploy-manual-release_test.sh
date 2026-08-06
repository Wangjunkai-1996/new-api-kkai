#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
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
readonly remote_source_state="${test_root}/remote-source-sha"
real_git="$(command -v git)"
readonly real_git
mkdir -p -- "${mock_bin}"

# shellcheck source=manual-deployment-contract.env
source "${CONTRACT}"
readonly KKAI_INFRA_SHA KKAI_DEPLOYMENT_PROTOCOL
readonly EXPECTED_INFRA_SHA=2b4d149f4b8b778d6a3f2c997fd021b45484dad4
readonly EXPECTED_DEPLOYMENT_PROTOCOL=router-v3-staged
export KKAI_TEST_EXPECTED_INFRA_SHA="${KKAI_INFRA_SHA}"
export KKAI_TEST_EXPECTED_PROTOCOL="${KKAI_DEPLOYMENT_PROTOCOL}"
export KKAI_TEST_EXPECTED_SCHEMA_CONTRACT=feature

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
        printf 'KKAI_SCHEMA_CONTRACT=%s\n' "${KKAI_TEST_EXPECTED_SCHEMA_CONTRACT}"
        exit 0
        ;;
      advance-source)
        printf 'KKAI_PREFLIGHT_RESULT=ready\n'
        printf 'KKAI_DEPLOYMENT_PROTOCOL=%s\n' "${KKAI_TEST_EXPECTED_PROTOCOL}"
        printf 'KKAI_INFRA_SHA=%s\n' "${KKAI_TEST_EXPECTED_INFRA_SHA}"
        printf 'KKAI_SCHEMA_CONTRACT=%s\n' "${KKAI_TEST_EXPECTED_SCHEMA_CONTRACT}"
        printf '%s\n' "${KKAI_TEST_ADVANCED_SOURCE_SHA}" > "${KKAI_TEST_REMOTE_SOURCE_STATE}"
        exit 0
        ;;
      wrong-sha)
        printf 'KKAI_PREFLIGHT_RESULT=ready\n'
        printf 'KKAI_DEPLOYMENT_PROTOCOL=%s\n' "${KKAI_TEST_EXPECTED_PROTOCOL}"
        printf 'KKAI_INFRA_SHA=%040d\n' 0
        exit 0
        ;;
      wrong-protocol)
        printf 'KKAI_PREFLIGHT_RESULT=ready\n'
        printf 'KKAI_DEPLOYMENT_PROTOCOL=router-v2\n'
        printf 'KKAI_INFRA_SHA=%s\n' "${KKAI_TEST_EXPECTED_INFRA_SHA}"
        exit 0
        ;;
      wrong-schema-contract)
        printf 'KKAI_PREFLIGHT_RESULT=ready\n'
        printf 'KKAI_DEPLOYMENT_PROTOCOL=%s\n' "${KKAI_TEST_EXPECTED_PROTOCOL}"
        printf 'KKAI_INFRA_SHA=%s\n' "${KKAI_TEST_EXPECTED_INFRA_SHA}"
        printf 'KKAI_SCHEMA_CONTRACT=bridge\n'
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
  *'/kkai-newapi-manual-deploy stage '*)
    printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
    printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
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
if [[ -n "${KKAI_TEST_ADVANCE_AFTER_SCP:-}" ]]; then
  printf '%s\n' "${KKAI_TEST_ADVANCED_SOURCE_SHA}" > "${KKAI_TEST_REMOTE_SOURCE_STATE}"
fi
EOF

cat > "${mock_bin}/git" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ " $* " == *' remote get-url origin '* ]]; then
  printf '%s\n' 'https://github.com/Wangjunkai-1996/new-api-kkai.git'
  exit 0
fi
if [[ " $* " == *' ls-remote --exit-code --heads origin refs/heads/production/kkrich '* ]]; then
  printf '%s\t%s\n' "$(<"${KKAI_TEST_REMOTE_SOURCE_STATE}")" refs/heads/production/kkrich
  exit 0
fi
exec "${KKAI_TEST_REAL_GIT}" "$@"
EOF
chmod 0755 "${mock_bin}/ssh" "${mock_bin}/scp" "${mock_bin}/git"

readonly source_sha=1111111111111111111111111111111111111111
readonly source_repository=github.com/Wangjunkai-1996/new-api-kkai
readonly source_ref=refs/heads/production/kkrich
readonly version=kkai-prod-20260726.1-111111111
readonly schema_contract=feature
export KKAI_TEST_EXPECTED_VERSION="${version}"
readonly archive="${test_root}/${version}.tar"
readonly metadata="${test_root}/${version}.json"
printf 'immutable archive fixture\n' > "${archive}"
archive_sha256="$(shasum -a 256 "${archive}" | awk '{print $1}')"
readonly archive_sha256
jq --null-input \
  --arg version "${version}" \
  --arg source_repository "${source_repository}" \
  --arg source_ref "${source_ref}" \
  --arg source_sha "${source_sha}" \
  --arg image_tag "kkai-newapi-manual:${version}" \
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
  }' > "${metadata}"

run_stage() {
  local mode=$1
  local metadata_path=${2:-${metadata}}
  local remote_source_sha=${3:-${source_sha}}
  local advance_after_scp=${4:-}

  : > "${call_log}"
  printf '%s\n' "${remote_source_sha}" > "${remote_source_state}"
  PATH="${mock_bin}:${PATH}" \
    KKAI_TEST_REAL_GIT="${real_git}" \
    KKAI_TEST_LOG="${call_log}" \
    KKAI_TEST_PREFLIGHT_MODE="${mode}" \
    KKAI_TEST_REMOTE_SOURCE_STATE="${remote_source_state}" \
    KKAI_TEST_ADVANCED_SOURCE_SHA=2222222222222222222222222222222222222222 \
    KKAI_TEST_ADVANCE_AFTER_SCP="${advance_after_scp}" \
    "${DEPLOY_SCRIPT}" --stage "${metadata_path}"
}

test_contract_pins_staged_controller() {
  [[ "${KKAI_INFRA_SHA}" == "${EXPECTED_INFRA_SHA}" ]] ||
    fail "deployment contract does not pin the approved infrastructure commit"
  [[ "${KKAI_DEPLOYMENT_PROTOCOL}" == "${EXPECTED_DEPLOYMENT_PROTOCOL}" ]] ||
    fail "deployment contract does not pin the staged protocol"
}

test_requires_explicit_stage_action() {
  local output

  : > "${call_log}"
  if output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      "${DEPLOY_SCRIPT}" "${metadata}" 2>&1
  )"; then
    fail "legacy one-step invocation unexpectedly succeeded"
  fi
  grep -F 'usage: deploy-manual-release.sh --stage METADATA.json' <<< "${output}" >/dev/null ||
    fail "usage does not require the stage action"
  [[ ! -s "${call_log}" ]] || fail "invalid invocation made a remote call"
}

test_preflight_failure_prevents_upload() {
  local output

  if output="$(run_stage fail 2>&1)"; then
    fail "failed preflight unexpectedly allowed staging"
  fi
  grep -F 'production preflight failed; archive was not uploaded' <<< "${output}" >/dev/null ||
    fail "failed preflight did not explain the upload boundary"
  ! grep -q '^scp ' "${call_log}" || fail "archive was uploaded after failed preflight"
  ! grep -q '/kkai-newapi-manual-deploy stage ' "${call_log}" ||
    fail "stage was invoked after failed preflight"
}

test_invalid_schema_contract_prevents_remote_calls() {
  local invalid_metadata="${test_root}/invalid-schema-contract.json" output

  jq '.schema_contract = "invalid"' "${metadata}" > "${invalid_metadata}"
  if output="$(run_stage ready "${invalid_metadata}" 2>&1)"; then
    fail "invalid schema contract unexpectedly allowed staging"
  fi
  grep -F 'invalid schema contract' <<< "${output}" >/dev/null ||
    fail "invalid schema contract was not rejected explicitly"
  [[ ! -s "${call_log}" ]] || fail "invalid schema contract made a remote call"
}

test_invalid_source_provenance_prevents_remote_calls() {
  local invalid_repository="${test_root}/invalid-source-repository.json"
  local invalid_ref="${test_root}/invalid-source-ref.json" output

  jq '.source_repository = "github.com/example/not-new-api"' "${metadata}" > "${invalid_repository}"
  if output="$(run_stage ready "${invalid_repository}" 2>&1)"; then
    fail "invalid source repository unexpectedly allowed staging"
  fi
  grep -F 'invalid source repository' <<< "${output}" >/dev/null ||
    fail "invalid source repository was not rejected explicitly"
  [[ ! -s "${call_log}" ]] || fail "invalid source repository made a remote call"

  jq '.source_ref = "refs/heads/release/temporary"' "${metadata}" > "${invalid_ref}"
  if output="$(run_stage ready "${invalid_ref}" 2>&1)"; then
    fail "invalid source ref unexpectedly allowed staging"
  fi
  grep -F 'invalid source ref' <<< "${output}" >/dev/null ||
    fail "invalid source ref was not rejected explicitly"
  [[ ! -s "${call_log}" ]] || fail "invalid source ref made a remote call"
}

test_stale_source_sha_prevents_remote_calls() {
  local output
  if output="$(run_stage ready "${metadata}" 2222222222222222222222222222222222222222 2>&1)"; then
    fail "stale production source unexpectedly allowed staging"
  fi
  grep -F 'source SHA is no longer the production branch HEAD' <<< "${output}" >/dev/null ||
    fail "stale production source was not rejected explicitly"
  [[ ! -s "${call_log}" ]] || fail "stale production source made a remote call"
}

test_preflight_output_must_match_contract() {
  local output

  if output="$(run_stage wrong-sha 2>&1)"; then
    fail "mismatched preflight SHA unexpectedly allowed staging"
  fi
  grep -F 'production preflight infrastructure SHA mismatch' <<< "${output}" >/dev/null ||
    fail "mismatched preflight SHA was not rejected"
  ! grep -q '^scp ' "${call_log}" || fail "archive was uploaded after a preflight SHA mismatch"
}

test_preflight_protocol_must_match_contract() {
  local output

  if output="$(run_stage wrong-protocol 2>&1)"; then
    fail "mismatched preflight protocol unexpectedly allowed staging"
  fi
  grep -F 'production preflight protocol mismatch' <<< "${output}" >/dev/null ||
    fail "mismatched preflight protocol was not rejected"
  ! grep -q '^scp ' "${call_log}" || fail "archive was uploaded after a preflight protocol mismatch"
}

test_preflight_schema_contract_must_match_release() {
  local output

  if output="$(run_stage wrong-schema-contract 2>&1)"; then
    fail "mismatched preflight schema contract unexpectedly allowed staging"
  fi
  grep -F 'production preflight schema contract mismatch' <<< "${output}" >/dev/null ||
    fail "mismatched preflight schema contract was not rejected"
  ! grep -q '^scp ' "${call_log}" ||
    fail "archive was uploaded after a preflight schema contract mismatch"
}

test_remote_advance_after_preflight_prevents_upload() {
  local output

  if output="$(run_stage advance-source 2>&1)"; then
    fail "production advance after preflight unexpectedly allowed upload"
  fi
  grep -F 'source SHA is no longer the production branch HEAD' <<< "${output}" >/dev/null ||
    fail "post-preflight production advance was not reported"
  grep -q '/kkai-newapi-manual-deploy preflight ' "${call_log}" ||
    fail "post-preflight race test did not reach preflight"
  ! grep -q '^scp ' "${call_log}" || fail "archive was uploaded after production advanced"
  ! grep -q '/kkai-newapi-manual-deploy stage ' "${call_log}" ||
    fail "stage was invoked after production advanced"
}

test_remote_advance_after_upload_prevents_stage() {
  local output

  if output="$(run_stage ready "${metadata}" "${source_sha}" advance-after-scp 2>&1)"; then
    fail "production advance during upload unexpectedly allowed staging"
  fi
  grep -F 'production advanced after archive upload' <<< "${output}" >/dev/null ||
    fail "post-upload production advance did not report the retained archive"
  grep -q '^scp ' "${call_log}" || fail "post-upload race test did not upload the archive"
  ! grep -q '/kkai-newapi-manual-deploy stage ' "${call_log}" ||
    fail "stage was invoked after production advanced during upload"
}

test_successful_preflight_precedes_upload_and_stage() {
  local output preflight_line upload_line stage_line contract_arguments stage_arguments

  output="$(run_stage ready)"
  grep -Fx 'KKAI_PREFLIGHT_RESULT=ready' <<< "${output}" >/dev/null ||
    fail "ready preflight output was not preserved"
  grep -Fx 'KKAI_CANDIDATE_STAGE_RESULT=staged' <<< "${output}" >/dev/null ||
    fail "candidate stage output was not preserved"
  preflight_line="$(grep -n '/kkai-newapi-manual-deploy preflight ' "${call_log}" | cut -d: -f1)"
  upload_line="$(grep -n '^scp ' "${call_log}" | cut -d: -f1)"
  stage_line="$(grep -n '/kkai-newapi-manual-deploy stage ' "${call_log}" | cut -d: -f1)"
  [[ "${preflight_line}" -lt "${upload_line}" && "${upload_line}" -lt "${stage_line}" ]] ||
    fail "preflight, upload, and stage order is invalid"
  contract_arguments="--expected-infra-sha ${KKAI_INFRA_SHA} --deployment-protocol ${KKAI_DEPLOYMENT_PROTOCOL} --schema-contract ${schema_contract}"
  [[ "$(grep -Fc -- "${contract_arguments}" "${call_log}")" -eq 2 ]] ||
    fail "preflight and stage did not share the pinned contract"
  stage_arguments="--archive /tmp/newapi-manual-${version}.tar --archive-sha256 ${archive_sha256} --source-sha ${source_sha} --version ${version} --image-tag kkai-newapi-manual:${version} ${contract_arguments}"
  grep -F -- "${stage_arguments}" "${call_log}" >/dev/null ||
    fail "stage did not receive the verified release metadata"
  ! grep -q '/kkai-newapi-manual-deploy deploy ' "${call_log}" ||
    fail "legacy deploy action was invoked"
}

test_contract_pins_staged_controller
test_requires_explicit_stage_action
test_preflight_failure_prevents_upload
test_invalid_schema_contract_prevents_remote_calls
test_invalid_source_provenance_prevents_remote_calls
test_stale_source_sha_prevents_remote_calls
test_preflight_output_must_match_contract
test_preflight_protocol_must_match_contract
test_preflight_schema_contract_must_match_release
test_remote_advance_after_preflight_prevents_upload
test_remote_advance_after_upload_prevents_stage
test_successful_preflight_precedes_upload_and_stage

echo 'New API manual deploy client tests passed'
