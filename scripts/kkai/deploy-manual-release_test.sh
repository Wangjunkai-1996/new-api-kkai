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
mkdir -p -- "${mock_bin}"

# shellcheck source=manual-deployment-contract.env
source "${CONTRACT}"
readonly KKAI_INFRA_SHA KKAI_DEPLOYMENT_PROTOCOL
readonly EXPECTED_INFRA_SHA=0b8076e423011b0d33745d30c52fafc280001563
readonly EXPECTED_DEPLOYMENT_PROTOCOL=router-v3-staged
readonly EXPECTED_HOST=sys1
export KKAI_TEST_EXPECTED_INFRA_SHA="${KKAI_INFRA_SHA}"
export KKAI_TEST_EXPECTED_PROTOCOL="${KKAI_DEPLOYMENT_PROTOCOL}"
export KKAI_TEST_EXPECTED_SCHEMA_CONTRACT=feature
export KKAI_TEST_EXPECTED_FRONTEND_MODE=embedded

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
        printf 'KKAI_FRONTEND_MODE=%s\n' "${KKAI_TEST_EXPECTED_FRONTEND_MODE}"
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
        printf 'KKAI_FRONTEND_MODE=%s\n' "${KKAI_TEST_EXPECTED_FRONTEND_MODE}"
        exit 0
        ;;
      wrong-frontend-mode)
        printf 'KKAI_PREFLIGHT_RESULT=ready\n'
        printf 'KKAI_DEPLOYMENT_PROTOCOL=%s\n' "${KKAI_TEST_EXPECTED_PROTOCOL}"
        printf 'KKAI_INFRA_SHA=%s\n' "${KKAI_TEST_EXPECTED_INFRA_SHA}"
        printf 'KKAI_SCHEMA_CONTRACT=%s\n' "${KKAI_TEST_EXPECTED_SCHEMA_CONTRACT}"
        printf 'KKAI_FRONTEND_MODE=external\n'
        exit 0
        ;;
      duplicate-result)
        printf 'KKAI_PREFLIGHT_RESULT=ready\n'
        printf 'KKAI_PREFLIGHT_RESULT=not-ready\n'
        printf 'KKAI_DEPLOYMENT_PROTOCOL=%s\n' "${KKAI_TEST_EXPECTED_PROTOCOL}"
        printf 'KKAI_INFRA_SHA=%s\n' "${KKAI_TEST_EXPECTED_INFRA_SHA}"
        printf 'KKAI_SCHEMA_CONTRACT=%s\n' "${KKAI_TEST_EXPECTED_SCHEMA_CONTRACT}"
        printf 'KKAI_FRONTEND_MODE=%s\n' "${KKAI_TEST_EXPECTED_FRONTEND_MODE}"
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
  *'/kkai-newapi-manual-deploy candidate-status')
    case "${KKAI_TEST_CANDIDATE_STATUS_MODE:-ready}" in
      ready)
        printf 'KKAI_CANDIDATE_STATUS=ready\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:3000\n'
        exit 0
        ;;
      none)
        printf 'KKAI_CANDIDATE_STATUS=none\n'
        exit 0
        ;;
      fail)
        printf 'candidate-status unavailable\n' >&2
        exit 45
        ;;
      *)
        exit 46
        ;;
    esac
    ;;
  *'/kkai-newapi-manual-deploy stage '*)
    case "${KKAI_TEST_STAGE_MODE:-ready}" in
      ready)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:3000\n'
        printf 'KKAI_CANDIDATE_EXPIRES_AT=1893456000\n'
        printf 'KKAI_CANDIDATE_FRONTEND_MODE=%s\n' "${KKAI_TEST_EXPECTED_FRONTEND_MODE}"
        exit 0
        ;;
      wrong-status)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=failed\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        exit 0
        ;;
      wrong-version)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=kkai-prod-20260726.1-222222222\n'
        exit 0
        ;;
      missing-status)
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        exit 0
        ;;
      missing-version)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        exit 0
        ;;
      missing-slot)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:3000\n'
        printf 'KKAI_CANDIDATE_EXPIRES_AT=1893456000\n'
        exit 0
        ;;
      missing-tunnel-target)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_EXPIRES_AT=1893456000\n'
        exit 0
        ;;
      missing-expires-at)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:3000\n'
        exit 0
        ;;
      missing-frontend-mode)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:3000\n'
        printf 'KKAI_CANDIDATE_EXPIRES_AT=1893456000\n'
        exit 0
        ;;
      wrong-frontend-mode)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:3000\n'
        printf 'KKAI_CANDIDATE_EXPIRES_AT=1893456000\n'
        printf 'KKAI_CANDIDATE_FRONTEND_MODE=external\n'
        exit 0
        ;;
      duplicate-frontend-mode)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:3000\n'
        printf 'KKAI_CANDIDATE_EXPIRES_AT=1893456000\n'
        printf 'KKAI_CANDIDATE_FRONTEND_MODE=%s\n' "${KKAI_TEST_EXPECTED_FRONTEND_MODE}"
        printf 'KKAI_CANDIDATE_FRONTEND_MODE=%s\n' "${KKAI_TEST_EXPECTED_FRONTEND_MODE}"
        exit 0
        ;;
      invalid-tunnel-ip)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=999.0.0.2:3000\n'
        printf 'KKAI_CANDIDATE_EXPIRES_AT=1893456000\n'
        exit 0
        ;;
      invalid-tunnel-port)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:65536\n'
        printf 'KKAI_CANDIDATE_EXPIRES_AT=1893456000\n'
        exit 0
        ;;
      command-fail)
        printf 'controller stage failed: candidate image rejected\n' >&2
        exit 42
        ;;
      stderr-forged)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=failed\n' >&2
        printf 'KKAI_CANDIDATE_VERSION=kkai-prod-20260726.1-222222222\n' >&2
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n'
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}"
        printf 'KKAI_CANDIDATE_SLOT=green\n'
        printf 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:3000\n'
        printf 'KKAI_CANDIDATE_EXPIRES_AT=1893456000\n'
        printf 'KKAI_CANDIDATE_FRONTEND_MODE=%s\n' "${KKAI_TEST_EXPECTED_FRONTEND_MODE}"
        exit 0
        ;;
      stderr-only)
        printf 'KKAI_CANDIDATE_STAGE_RESULT=staged\n' >&2
        printf 'KKAI_CANDIDATE_VERSION=%s\n' "${KKAI_TEST_EXPECTED_VERSION}" >&2
        exit 0
        ;;
      *)
        exit 43
        ;;
    esac
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
readonly schema_contract=feature
readonly frontend_mode=embedded
export KKAI_TEST_EXPECTED_VERSION="${version}"
readonly archive="${test_root}/${version}.tar"
readonly metadata="${test_root}/${version}.json"
printf 'immutable archive fixture\n' > "${archive}"
archive_sha256="$(shasum -a 256 "${archive}" | awk '{print $1}')"
readonly archive_sha256
jq --null-input \
  --arg version "${version}" \
  --arg source_sha "${source_sha}" \
  --arg image_tag "kkai-newapi-manual:${version}" \
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

run_stage() {
  local mode=$1
  local metadata_path=${2:-${metadata}}
  local stage_mode=${3:-ready}

  : > "${call_log}"
  PATH="${mock_bin}:${PATH}" \
    KKAI_TEST_LOG="${call_log}" \
    KKAI_TEST_PREFLIGHT_MODE="${mode}" \
    KKAI_TEST_STAGE_MODE="${stage_mode}" \
    KKAI_TEST_CANDIDATE_STATUS_MODE=ready \
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

test_invalid_frontend_mode_prevents_remote_calls() {
  local invalid_metadata="${test_root}/invalid-frontend-mode.json" output

  jq '.frontend_mode = "remote"' "${metadata}" > "${invalid_metadata}"
  if output="$(run_stage ready "${invalid_metadata}" 2>&1)"; then
    fail "invalid frontend mode unexpectedly allowed staging"
  fi
  grep -F 'invalid frontend mode' <<< "${output}" >/dev/null ||
    fail "invalid frontend mode was not rejected explicitly"
  [[ ! -s "${call_log}" ]] || fail "invalid frontend mode made a remote call"
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

test_preflight_frontend_mode_must_match_release() {
  local output

  if output="$(run_stage wrong-frontend-mode 2>&1)"; then
    fail "mismatched preflight frontend mode unexpectedly allowed staging"
  fi
  grep -F 'production preflight frontend mode mismatch' <<< "${output}" >/dev/null ||
    fail "mismatched preflight frontend mode was not rejected"
  ! grep -q '^scp ' "${call_log}" ||
    fail "archive was uploaded after a preflight frontend mode mismatch"
}

test_preflight_fields_must_be_unique() {
  local output

  if output="$(run_stage duplicate-result 2>&1)"; then
    fail "duplicate preflight result unexpectedly allowed staging"
  fi
  grep -F 'production preflight did not report ready' <<< "${output}" >/dev/null ||
    fail "duplicate preflight result was not rejected explicitly"
  ! grep -q '^scp ' "${call_log}" || fail "archive was uploaded after a duplicate preflight result"
}

test_stage_status_must_be_staged() {
  local output

  if output="$(run_stage ready "${metadata}" wrong-status 2>&1)"; then
    fail "non-staged candidate result unexpectedly allowed staging"
  fi
  grep -F 'candidate stage did not report KKAI_CANDIDATE_STAGE_RESULT=staged exactly once' <<< "${output}" >/dev/null ||
    fail "non-staged candidate result was not rejected explicitly"
  [[ "$(grep -Fc '/kkai-newapi-manual-deploy candidate-status' "${call_log}")" -eq 1 ]] ||
    fail "invalid stage output did not trigger exactly one candidate-status query"
}

test_stage_version_must_match_metadata() {
  local output

  if output="$(run_stage ready "${metadata}" wrong-version 2>&1)"; then
    fail "mismatched candidate version unexpectedly allowed staging"
  fi
  grep -F "candidate stage did not report KKAI_CANDIDATE_VERSION=${version} exactly once" <<< "${output}" >/dev/null ||
    fail "mismatched candidate version was not rejected explicitly"
}

test_stage_output_requires_status_field() {
  local output

  if output="$(run_stage ready "${metadata}" missing-status 2>&1)"; then
    fail "candidate output without a stage result unexpectedly allowed staging"
  fi
  grep -F 'candidate stage did not report KKAI_CANDIDATE_STAGE_RESULT=staged exactly once' <<< "${output}" >/dev/null ||
    fail "missing candidate stage result was not rejected explicitly"
}

test_stage_output_requires_version_field() {
  local output

  if output="$(run_stage ready "${metadata}" missing-version 2>&1)"; then
    fail "candidate output without a version unexpectedly allowed staging"
  fi
  grep -F "candidate stage did not report KKAI_CANDIDATE_VERSION=${version} exactly once" <<< "${output}" >/dev/null ||
    fail "missing candidate version was not rejected explicitly"
}

test_stage_output_requires_slot_field() {
  local output

  if output="$(run_stage ready "${metadata}" missing-slot 2>&1)"; then
    fail "candidate output without a slot unexpectedly allowed staging"
  fi
  grep -F 'candidate stage did not report a valid KKAI_CANDIDATE_SLOT exactly once' <<< "${output}" >/dev/null ||
    fail "missing candidate slot was not rejected explicitly"
}

test_stage_output_requires_tunnel_target_field() {
  local output

  if output="$(run_stage ready "${metadata}" missing-tunnel-target 2>&1)"; then
    fail "candidate output without a tunnel target unexpectedly allowed staging"
  fi
  grep -F 'candidate stage did not report a valid KKAI_CANDIDATE_TUNNEL_TARGET exactly once' <<< "${output}" >/dev/null ||
    fail "missing candidate tunnel target was not rejected explicitly"
}

test_stage_output_requires_expires_at_field() {
  local output

  if output="$(run_stage ready "${metadata}" missing-expires-at 2>&1)"; then
    fail "candidate output without an expiry time unexpectedly allowed staging"
  fi
  grep -F 'candidate stage did not report a valid KKAI_CANDIDATE_EXPIRES_AT exactly once' <<< "${output}" >/dev/null ||
    fail "missing candidate expiry time was not rejected explicitly"
}

test_stage_output_requires_frontend_mode_field() {
  local output

  if output="$(run_stage ready "${metadata}" missing-frontend-mode 2>&1)"; then
    fail "candidate output without a frontend mode unexpectedly allowed staging"
  fi
  grep -F "candidate stage did not report KKAI_CANDIDATE_FRONTEND_MODE=${frontend_mode} exactly once" <<< "${output}" >/dev/null ||
    fail "missing candidate frontend mode was not rejected explicitly"
}

test_stage_output_frontend_mode_must_match_metadata() {
  local output

  if output="$(run_stage ready "${metadata}" wrong-frontend-mode 2>&1)"; then
    fail "mismatched candidate frontend mode unexpectedly allowed staging"
  fi
  grep -F "candidate stage did not report KKAI_CANDIDATE_FRONTEND_MODE=${frontend_mode} exactly once" <<< "${output}" >/dev/null ||
    fail "mismatched candidate frontend mode was not rejected explicitly"
}

test_stage_output_frontend_mode_must_be_unique() {
  local output

  if output="$(run_stage ready "${metadata}" duplicate-frontend-mode 2>&1)"; then
    fail "duplicate candidate frontend mode unexpectedly allowed staging"
  fi
  grep -F "candidate stage did not report KKAI_CANDIDATE_FRONTEND_MODE=${frontend_mode} exactly once" <<< "${output}" >/dev/null ||
    fail "duplicate candidate frontend mode was not rejected explicitly"
}

test_stage_output_rejects_invalid_tunnel_target() {
  local mode=$1 output

  if output="$(run_stage ready "${metadata}" "${mode}" 2>&1)"; then
    fail "invalid tunnel target (${mode}) unexpectedly allowed staging"
  fi
  grep -F 'candidate stage did not report a valid KKAI_CANDIDATE_TUNNEL_TARGET exactly once' <<< "${output}" >/dev/null ||
    fail "invalid tunnel target (${mode}) was not rejected explicitly"
}

test_stage_command_failure_preserves_diagnostics() {
  local output

  if output="$(run_stage ready "${metadata}" command-fail 2>&1)"; then
    fail "failed candidate stage command unexpectedly succeeded"
  fi
  grep -F 'controller stage failed: candidate image rejected' <<< "${output}" >/dev/null ||
    fail "candidate stage stderr was not preserved"
  grep -F 'candidate stage command failed' <<< "${output}" >/dev/null ||
    fail "candidate stage command failure was not reported"
  grep -Fx 'KKAI_CANDIDATE_STATUS=ready' <<< "${output}" >/dev/null ||
    fail "candidate status was not queried after a stage command failure"
  [[ "$(grep -Fc '/kkai-newapi-manual-deploy candidate-status' "${call_log}")" -eq 1 ]] ||
    fail "candidate status was not queried exactly once after a stage command failure"
}

test_candidate_status_failure_stops_without_retry() {
  local output

  : > "${call_log}"
  if output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_PREFLIGHT_MODE=ready \
      KKAI_TEST_STAGE_MODE=command-fail \
      KKAI_TEST_CANDIDATE_STATUS_MODE=fail \
      "${DEPLOY_SCRIPT}" --stage "${metadata}" 2>&1
  )"; then
    fail "candidate-status failure unexpectedly allowed stage handling to continue"
  fi
  grep -F 'candidate-status failed' <<< "${output}" >/dev/null ||
    fail "candidate-status failure was not reported"
  grep -F 'candidate-status failed (exit 45)' <<< "${output}" >/dev/null ||
    fail "candidate-status failure did not preserve its exit status"
  grep -F 'candidate-status failed; preserve evidence and stop' <<< "${output}" >/dev/null ||
    fail "candidate-status failure did not stop in the uncertain-state path"
  [[ "$(grep -Fc '/kkai-newapi-manual-deploy candidate-status' "${call_log}")" -eq 1 ]] ||
    fail "candidate-status was retried after its first failure"
  [[ "$(grep -c '^scp ' "${call_log}")" -eq 1 ]] ||
    fail "candidate-status failure changed the single archive upload boundary"
}

test_stage_stderr_is_not_parsed_as_result() {
  local output

  if ! output="$(run_stage ready "${metadata}" stderr-forged 2>&1)"; then
    fail "valid candidate stdout was rejected because stderr contained forged fields"
  fi
  grep -Fx 'KKAI_CANDIDATE_STAGE_RESULT=staged' <<< "${output}" >/dev/null ||
    fail "valid candidate stage result was not preserved"
  grep -Fx "KKAI_CANDIDATE_VERSION=${version}" <<< "${output}" >/dev/null ||
    fail "valid candidate version was not preserved"
  grep -Fx 'KKAI_CANDIDATE_STAGE_RESULT=failed' <<< "${output}" >/dev/null ||
    fail "stage stderr diagnostics were not preserved"
}

test_stage_stdout_is_required() {
  local output

  if output="$(run_stage ready "${metadata}" stderr-only 2>&1)"; then
    fail "candidate fields emitted only on stderr unexpectedly allowed staging"
  fi
  grep -F 'candidate stage did not report KKAI_CANDIDATE_STAGE_RESULT=staged exactly once' <<< "${output}" >/dev/null ||
    fail "stderr-only candidate fields were not rejected"
}

test_successful_preflight_precedes_upload_and_stage() {
  local output preflight_line upload_line stage_line contract_arguments stage_arguments

  output="$(run_stage ready)"
  grep -Fx 'KKAI_PREFLIGHT_RESULT=ready' <<< "${output}" >/dev/null ||
    fail "ready preflight output was not preserved"
  grep -Fx 'KKAI_CANDIDATE_STAGE_RESULT=staged' <<< "${output}" >/dev/null ||
    fail "candidate stage output was not preserved"
  grep -Fx "KKAI_CANDIDATE_VERSION=${version}" <<< "${output}" >/dev/null ||
    fail "candidate stage version output was not preserved"
  grep -Fx 'KKAI_CANDIDATE_SLOT=green' <<< "${output}" >/dev/null ||
    fail "candidate stage slot output was not preserved"
  grep -Fx 'KKAI_CANDIDATE_TUNNEL_TARGET=10.0.0.2:3000' <<< "${output}" >/dev/null ||
    fail "candidate stage tunnel target output was not preserved"
  grep -Fx 'KKAI_CANDIDATE_EXPIRES_AT=1893456000' <<< "${output}" >/dev/null ||
    fail "candidate stage expiry output was not preserved"
  grep -Fx "KKAI_CANDIDATE_FRONTEND_MODE=${frontend_mode}" <<< "${output}" >/dev/null ||
    fail "candidate stage frontend mode output was not preserved"
  preflight_line="$(grep -n '/kkai-newapi-manual-deploy preflight ' "${call_log}" | cut -d: -f1)"
  upload_line="$(grep -n '^scp ' "${call_log}" | cut -d: -f1)"
  stage_line="$(grep -n '/kkai-newapi-manual-deploy stage ' "${call_log}" | cut -d: -f1)"
  [[ "${preflight_line}" -lt "${upload_line}" && "${upload_line}" -lt "${stage_line}" ]] ||
    fail "preflight, upload, and stage order is invalid"
  contract_arguments="--expected-infra-sha ${KKAI_INFRA_SHA} --deployment-protocol ${KKAI_DEPLOYMENT_PROTOCOL} --schema-contract ${schema_contract} --frontend-mode ${frontend_mode}"
  [[ "$(grep -Fc -- "${contract_arguments}" "${call_log}")" -eq 2 ]] ||
    fail "preflight and stage did not share the pinned contract"
  [[ "$(grep -Ec -- "^ssh .* ${EXPECTED_HOST} sudo " "${call_log}")" -eq 2 ]] ||
    fail "preflight and stage did not use the primary sys1 route"
  grep -E -- "^scp .* ${EXPECTED_HOST}:/tmp/newapi-manual-${version}\\.tar$" "${call_log}" >/dev/null ||
    fail "archive upload did not use the primary sys1 route"
  ! grep -F '10.203.0.1' "${call_log}" >/dev/null ||
    fail "manual deploy used the retired WireGuard address"
  for ssh_option in \
    '-o IdentitiesOnly=yes' \
    '-o ServerAliveInterval=15' \
    '-o ServerAliveCountMax=3'; do
    [[ "$(grep -Fc -- "${ssh_option}" "${call_log}")" -eq 3 ]] ||
      fail "SSH option ${ssh_option} was not applied to preflight, upload, and stage"
  done
  stage_arguments="--archive /tmp/newapi-manual-${version}.tar --archive-sha256 ${archive_sha256} --source-sha ${source_sha} --version ${version} --image-tag kkai-newapi-manual:${version} ${contract_arguments}"
  grep -F -- "${stage_arguments}" "${call_log}" >/dev/null ||
    fail "stage did not receive the verified release metadata"
  ! grep -q '/kkai-newapi-manual-deploy deploy ' "${call_log}" ||
    fail "legacy deploy action was invoked"
  [[ "$(grep -Fc '/kkai-newapi-manual-deploy candidate-status' "${call_log}")" -eq 0 ]] ||
    fail "candidate-status was queried after a successful stage"
}

test_contract_pins_staged_controller
test_requires_explicit_stage_action
test_preflight_failure_prevents_upload
test_invalid_schema_contract_prevents_remote_calls
test_invalid_frontend_mode_prevents_remote_calls
test_preflight_output_must_match_contract
test_preflight_protocol_must_match_contract
test_preflight_schema_contract_must_match_release
test_preflight_frontend_mode_must_match_release
test_preflight_fields_must_be_unique
test_stage_status_must_be_staged
test_stage_version_must_match_metadata
test_stage_output_requires_status_field
test_stage_output_requires_version_field
test_stage_output_requires_slot_field
test_stage_output_requires_tunnel_target_field
test_stage_output_requires_expires_at_field
test_stage_output_requires_frontend_mode_field
test_stage_output_frontend_mode_must_match_metadata
test_stage_output_frontend_mode_must_be_unique
test_stage_output_rejects_invalid_tunnel_target invalid-tunnel-ip
test_stage_output_rejects_invalid_tunnel_target invalid-tunnel-port
test_stage_command_failure_preserves_diagnostics
test_candidate_status_failure_stops_without_retry
test_stage_stderr_is_not_parsed_as_result
test_stage_stdout_is_required
test_successful_preflight_precedes_upload_and_stage

echo 'New API manual deploy client tests passed'
