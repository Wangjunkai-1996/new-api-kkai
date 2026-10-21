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
readonly EXPECTED_DEPLOYMENT_PROTOCOL=router-v3-staged
readonly EXPECTED_HOST=sys1
export KKAI_TEST_EXPECTED_INFRA_SHA="${KKAI_INFRA_SHA}"
export KKAI_TEST_EXPECTED_PROTOCOL="${KKAI_DEPLOYMENT_PROTOCOL}"
export KKAI_TEST_EXPECTED_SCHEMA_CONTRACT=feature
export KKAI_TEST_EXPECTED_FRONTEND_MODE=embedded
KKAI_TEST_REAL_PYTHON="$(command -v python3)"
export KKAI_TEST_REAL_PYTHON
export KKAI_RELEASE_SSH_MULTIPLEX=0
export KKAI_RELEASE_SSH_COMPRESSION=0

cat > "${mock_bin}/ssh" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'ssh %s\n' "$*" >> "${KKAI_TEST_LOG}"
control_path=''
for argument in "$@"; do
  [[ ${argument} != ControlPath=* ]] || control_path=${argument#ControlPath=}
done
if [[ -n ${control_path} && ${KKAI_TEST_CONTROL_FILE:-none} != none && ! -e ${control_path} ]]; then
  "${KKAI_TEST_REAL_PYTHON}" -c '
import os, socket, stat, sys
path, kind = sys.argv[1:]
assert path.startswith("/tmp/kkai-release-ssh.") and path.endswith("/control")
assert stat.S_IMODE(os.stat(os.path.dirname(path)).st_mode) == 0o700
if kind == "socket":
    connection = socket.socket(socket.AF_UNIX)
    connection.bind(path)
    connection.close()
else:
    open(path, "x").close()
' "${control_path}" "${KKAI_TEST_CONTROL_FILE}"
fi
case "$*" in
  *' -O exit sys1')
    [[ -S ${control_path} ]] || exit 47
    exit 0
    ;;
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
exit "${KKAI_TEST_UPLOAD_EXIT:-0}"
EOF
chmod 0755 "${mock_bin}/ssh" "${mock_bin}/scp"

cat > "${mock_bin}/python3" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ ${1:-} == */release-record.py ]]; then
  if [[ ${2:-} == init && ${KKAI_TEST_RECORD_INIT_FAIL:-false} == true ]]; then exit 98; fi
  exec "${KKAI_TEST_REAL_PYTHON}" "$@"
fi
printf 'python3 %s\n' "$*" >> "${KKAI_TEST_LOG}"
[[ $1 == */prepare-maintenance-release.py && $2 == verify ]] || exit 95
[[ ${KKAI_TEST_ONLINE_PLAN:-valid} == valid ]] || exit 96
EOF
chmod 0755 "${mock_bin}/python3"

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
  [[ "${KKAI_INFRA_SHA}" =~ ^[0-9a-f]{40}$ ]] ||
    fail "deployment contract does not pin an exact infrastructure commit"
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
  local output record_path status

  if output="$(run_stage fail 2>&1)"; then
    fail "failed preflight unexpectedly allowed staging"
  else
    status=$?
  fi
  grep -F 'production preflight failed; archive was not uploaded' <<< "${output}" >/dev/null ||
    fail "failed preflight did not explain the upload boundary"
  ! grep -q '^scp ' "${call_log}" || fail "archive was uploaded after failed preflight"
  ! grep -q '/kkai-newapi-manual-deploy stage ' "${call_log}" ||
    fail "stage was invoked after failed preflight"
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e --argjson status "${status}" \
    '.result == "failed" and .exit_code == $status and .phases[-1].name == "preflight" and .phases[-1].result == "failed"' \
    "${record_path}" >/dev/null || fail 'failed preflight was not recorded accurately'
}

test_record_initialization_failure_prevents_remote_calls() {
  local output status
  if output="$(KKAI_TEST_RECORD_INIT_FAIL=true run_stage ready 2>&1)"; then
    fail 'record initialization failure unexpectedly allowed staging'
  else
    status=$?
  fi
  [[ ${status} == 1 ]] || fail "record initialization failure returned an unexpected status\n${output}"
  [[ ! -s ${call_log} ]] || fail 'record initialization failure made a remote call'
}

test_invalid_ssh_options_prevent_remote_calls() {
  local output
  if output="$(KKAI_RELEASE_SSH_MULTIPLEX=yes run_stage ready 2>&1)"; then
    fail 'invalid SSH multiplex setting unexpectedly allowed staging'
  fi
  grep -F 'KKAI_RELEASE_SSH_MULTIPLEX must be 0 or 1' <<<"${output}" >/dev/null || fail 'invalid multiplex setting was not rejected explicitly'
  [[ ! -s ${call_log} ]] || fail 'invalid multiplex setting made a remote call'
  if output="$(KKAI_RELEASE_SSH_COMPRESSION=on run_stage ready 2>&1)"; then
    fail 'invalid SSH compression setting unexpectedly allowed staging'
  fi
  grep -F 'KKAI_RELEASE_SSH_COMPRESSION must be 0 or 1' <<<"${output}" >/dev/null || fail 'invalid compression setting was not rejected explicitly'
  [[ ! -s ${call_log} ]] || fail 'invalid compression setting made a remote call'
}

test_failed_upload_preserves_exit_and_phase() {
  local output status record_path
  if output="$(KKAI_TEST_UPLOAD_EXIT=73 run_stage ready 2>&1)"; then
    fail 'failed archive upload unexpectedly allowed staging'
  else
    status=$?
  fi
  [[ ${status} == 73 ]] || fail 'record finalization masked the original upload exit code'
  ! grep -q '/kkai-newapi-manual-deploy stage ' "${call_log}" || fail 'failed upload reached stage'
  ! grep -q '/kkai-newapi-manual-deploy candidate-status' "${call_log}" || fail 'failed upload queried an unstaged candidate'
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e '.result == "failed" and .exit_code == 73 and .phases[-1].name == "upload" and .phases[-1].result == "failed" and .phases[-1].exit_code == 73' \
    "${record_path}" >/dev/null || fail 'upload failure lost its recorded phase or exit code'
}

test_checksum_failure_is_recorded_without_remote_calls() {
  local invalid_metadata="${test_root}/wrong-checksum.json" output record_path
  jq '.archive_sha256 = ("0" * 64)' "${metadata}" >"${invalid_metadata}"
  if output="$(run_stage ready "${invalid_metadata}" 2>&1)"; then
    fail 'archive checksum mismatch unexpectedly allowed staging'
  fi
  [[ ! -s ${call_log} ]] || fail 'invalid archive checksum made a remote call'
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e '.result == "failed" and .phases[-1].name == "artifact-validation" and .phases[-1].result == "failed"' \
    "${record_path}" >/dev/null || fail 'archive checksum failure was not recorded'
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

test_maintenance_preparation_prevents_remote_calls() {
  local maintenance_metadata="${test_root}/maintenance.json" output

  jq '.release_purpose = "maintenance-preparation"' "${metadata}" > "${maintenance_metadata}"
  if output="$(run_stage ready "${maintenance_metadata}" 2>&1)"; then
    fail "maintenance preparation unexpectedly allowed ordinary staging"
  fi
  grep -F 'ordinary stage is forbidden' <<< "${output}" >/dev/null ||
    fail "maintenance preparation was not rejected explicitly"
  [[ ! -s "${call_log}" ]] || fail "maintenance preparation made a remote call"
}

test_online_stage_requires_explicit_action_and_verified_plan() {
  local online_metadata="${test_root}/online.json" output verify_line preflight_line
  jq '.release_purpose = "tenant-online-preparation" | .schema_contract = "bridge" | .frontend_mode = "external"' \
    "${metadata}" > "${online_metadata}"
  if output="$(run_stage ready "${online_metadata}" 2>&1)"; then
    fail "online artifact bypassed the explicit stage-online action"
  fi
  [[ ! -s ${call_log} ]] || fail 'ordinary online stage made a remote call'
  : > "${call_log}"
  if output="$(PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" KKAI_TEST_ONLINE_PLAN=invalid \
    "${DEPLOY_SCRIPT}" --stage-online "${online_metadata}" 2>&1)"; then
    fail 'invalid online plan reached staging'
  fi
  ! grep -Eq '^ssh |^scp ' "${call_log}" || fail 'invalid online plan made a remote call'
  : > "${call_log}"
  output="$(PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
    KKAI_TEST_EXPECTED_SCHEMA_CONTRACT=bridge KKAI_TEST_EXPECTED_FRONTEND_MODE=external \
    "${DEPLOY_SCRIPT}" --stage-online "${online_metadata}")"
  grep -Fx 'KKAI_CANDIDATE_STAGE_RESULT=staged' <<<"${output}" >/dev/null || fail 'verified online bridge did not stage'
  grep -F -- "--planned-infra-sha ${KKAI_INFRA_SHA} --planned-deployment-protocol tenant-online-v1" "${call_log}" >/dev/null ||
    fail 'online artifact verification did not bind the exact installed controller'
  verify_line="$(grep -n '^python3 .*prepare-maintenance-release.py verify ' "${call_log}" | cut -d: -f1)"
  preflight_line="$(grep -n '/kkai-newapi-manual-deploy preflight ' "${call_log}" | cut -d: -f1)"
  [[ ${verify_line} -lt ${preflight_line} ]] || fail 'online plan verification ran after production preflight'
}

test_tenant_routing_override_is_explicit_and_validated_before_remote_calls() {
  local external_metadata="${test_root}/tenant-feature.json" output value
  jq '.frontend_mode = "external"' "${metadata}" > "${external_metadata}"
  for value in true false; do
    : > "${call_log}"
    output="$(PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_EXPECTED_FRONTEND_MODE=external \
      "${DEPLOY_SCRIPT}" --stage "${external_metadata}" --tenant-routing-enabled "${value}")"
    grep -Fx 'KKAI_CANDIDATE_STAGE_RESULT=staged' <<<"${output}" >/dev/null || fail 'tenant routing override did not stage'
    grep -E -- "/kkai-newapi-manual-deploy stage .* --tenant-routing-enabled ${value}$" "${call_log}" >/dev/null ||
      fail 'tenant routing override was not forwarded to stage'
    ! grep -E -- '/kkai-newapi-manual-deploy preflight .* --tenant-routing-enabled' "${call_log}" >/dev/null ||
      fail 'stage runtime override was forwarded to readonly preflight'
  done
  for value in invalid TRUE; do
    : > "${call_log}"
    if output="$(PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
      "${DEPLOY_SCRIPT}" --stage "${external_metadata}" --tenant-routing-enabled "${value}" 2>&1)"; then
      fail 'invalid tenant routing override was accepted'
    fi
    [[ ! -s ${call_log} ]] || fail 'invalid tenant override made a remote call'
  done
  : > "${call_log}"
  if output="$(PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
    "${DEPLOY_SCRIPT}" --stage "${metadata}" --tenant-routing-enabled true 2>&1)"; then
    fail 'embedded release enabled tenant routing'
  fi
  [[ ! -s ${call_log} ]] || fail 'embedded tenant routing override made a remote call'
  : > "${call_log}"
  if output="$(PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
    "${DEPLOY_SCRIPT}" --stage-online "${external_metadata}" --tenant-routing-enabled true 2>&1)"; then
    fail 'online bridge stage enabled tenant routing'
  fi
  [[ ! -s ${call_log} ]] || fail 'online tenant routing override made a remote call'
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
  local output status record_path

  if output="$(run_stage ready "${metadata}" command-fail 2>&1)"; then
    fail "failed candidate stage command unexpectedly succeeded"
  else
    status=$?
  fi
  grep -F 'controller stage failed: candidate image rejected' <<< "${output}" >/dev/null ||
    fail "candidate stage stderr was not preserved"
  grep -F 'candidate stage command failed' <<< "${output}" >/dev/null ||
    fail "candidate stage command failure was not reported"
  grep -Fx 'KKAI_CANDIDATE_STATUS=ready' <<< "${output}" >/dev/null ||
    fail "candidate status was not queried after a stage command failure"
  [[ "$(grep -Fc '/kkai-newapi-manual-deploy candidate-status' "${call_log}")" -eq 1 ]] ||
    fail "candidate status was not queried exactly once after a stage command failure"
  [[ $(grep -Fc '/kkai-newapi-manual-deploy stage ' "${call_log}") == 1 ]] || fail 'uncertain stage was retried'
  [[ ${status} == 1 ]] || fail 'uncertain stage handling changed its failure result'
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e '.result == "failed" and .exit_code == 1 and .phases[-1].name == "load-stage" and .phases[-1].result == "failed" and (.identity | has("target_slot") | not)' \
    "${record_path}" >/dev/null || fail 'uncertain stage record claimed success or a verified slot'
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
  [[ $(grep -Fc '/kkai-newapi-manual-deploy stage ' "${call_log}") == 1 ]] || fail 'candidate-status failure retried stage'
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
  local output preflight_line upload_line stage_line contract_arguments stage_arguments record_path

  output="$(run_stage ready 2>&1)"
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
  ! grep -E -- 'ControlMaster=|ControlPersist=|ControlPath=|Compression=| -C | -O exit ' "${call_log}" >/dev/null ||
    fail 'default deployment unexpectedly enabled SSH experiments'
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e --arg source "${source_sha}" --arg version "${version}" --arg digest "${archive_sha256}" --arg infra "${KKAI_INFRA_SHA}" \
    --arg bytes "$(wc -c <"${archive}" | tr -d '[:space:]')" \
    '.kind == "stage" and .source_sha == $source and .release_id == $version and .result == "passed" and .exit_code == 0 and
     .identity.archive_sha256 == $digest and .identity.archive_bytes == $bytes and .identity.infra_sha == $infra and
     .identity.target_slot == "green" and .identity.compression == "0" and .identity.ssh_multiplex == "0" and
     [.phases[].name] == ["artifact-validation", "preflight", "upload", "load-stage"] and
     all(.phases[]; .result == "passed" and .duration_seconds >= 0)' \
    "${record_path}" >/dev/null || fail 'successful stage record lost its identity, artifact or phase results'
}

test_ssh_opt_in_options_are_shared() {
  local multiplex=$1 compression=$2 control_kind=$3
  local output record_path control_path line remote_calls=0 close_calls=0
  output="$(KKAI_RELEASE_SSH_MULTIPLEX="${multiplex}" KKAI_RELEASE_SSH_COMPRESSION="${compression}" \
    KKAI_TEST_CONTROL_FILE="${control_kind}" run_stage ready 2>&1)" || fail "opt-in stage failed\n${output}"
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e --arg multiplex "${multiplex}" --arg compression "${compression}" \
    '.result == "passed" and .identity.ssh_multiplex == $multiplex and .identity.compression == $compression' \
    "${record_path}" >/dev/null || fail 'SSH experiment identity was not recorded'
  control_path="$(sed -n 's/.* -o ControlPath=\([^ ]*\).*/\1/p' "${call_log}" | head -1)"
  while IFS= read -r line; do
    case "${line}" in ssh\ *|scp\ *) ;; *) continue ;; esac
    remote_calls=$((remote_calls + 1))
    if [[ ${multiplex} == 1 ]]; then
      [[ ${line} == *' -o ControlMaster=auto '* && ${line} == *' -o ControlPersist=60 '* && ${line} == *" -o ControlPath=${control_path} "* ]] ||
        fail 'SSH and SCP did not share the same multiplex options'
    else
      [[ ${line} != *ControlMaster=* && ${line} != *ControlPersist=* && ${line} != *ControlPath=* ]] || fail 'compression enabled multiplexing'
    fi
    if [[ ${compression} == 1 ]]; then
      [[ ${line} == *' -o Compression=yes '* ]] || fail 'SSH and SCP did not share compression options'
    else
      [[ ${line} != *Compression=* ]] || fail 'multiplexing enabled compression'
    fi
    [[ ${line} != *' -C '* && ${line} != *' -L '* && ${line} != *' -N '* ]] || fail 'stage experiment changed tunnel or SSH option semantics'
    case "${line}" in
      scp\ *) [[ ${line} == *" ${EXPECTED_HOST}:/tmp/newapi-manual-${version}.tar" ]] || fail 'opt-in upload changed the sys1 alias or archive' ;;
      *' -O exit '*)
        close_calls=$((close_calls + 1))
        [[ ${line} == *" -O exit ${EXPECTED_HOST}" ]] || fail 'multiplex cleanup changed the sys1 alias'
        ;;
      *) [[ ${line} == *" ${EXPECTED_HOST} sudo "* ]] || fail 'opt-in deployment changed the sys1 alias' ;;
    esac
  done <"${call_log}"
  if [[ ${control_kind} == socket ]]; then
    [[ ${close_calls} == 1 && ${remote_calls} == 4 ]] || fail 'existing control socket was not closed exactly once'
  else
    [[ ${close_calls} == 0 && ${remote_calls} == 3 ]] || fail 'cleanup sent control commands without a socket'
  fi
  if [[ ${multiplex} == 1 ]]; then
    [[ ${control_path} == /tmp/kkai-release-ssh.*/control && ! -e ${control_path%/control} ]] || fail 'private control directory was not removed'
  fi
}

test_uncertain_stage_uses_opt_in_status_once() {
  local output
  if output="$(KKAI_RELEASE_SSH_MULTIPLEX=1 KKAI_RELEASE_SSH_COMPRESSION=1 \
    KKAI_TEST_CONTROL_FILE=socket run_stage ready "${metadata}" command-fail 2>&1)"; then
    fail 'uncertain opt-in stage unexpectedly succeeded'
  fi
  grep -F 'candidate stage outcome is uncertain' <<<"${output}" >/dev/null || fail 'uncertain opt-in stage did not preserve its recovery boundary'
  [[ $(grep -Fc '/kkai-newapi-manual-deploy candidate-status' "${call_log}") == 1 &&
     $(grep -Fc '/kkai-newapi-manual-deploy stage ' "${call_log}") == 1 &&
     $(grep -c '^scp ' "${call_log}") == 1 ]] || fail 'opt-in uncertainty retried upload, stage or status'
  [[ $(grep -Fc -- '-o Compression=yes' "${call_log}") == 5 &&
     $(grep -Fc -- '-o ControlMaster=auto' "${call_log}") == 5 ]] || fail 'uncertain stage status or cleanup lost opt-in SSH settings'
}

test_tenant_routing_override_is_explicit_and_validated_before_remote_calls
test_contract_pins_staged_controller
test_requires_explicit_stage_action
test_preflight_failure_prevents_upload
test_record_initialization_failure_prevents_remote_calls
test_invalid_ssh_options_prevent_remote_calls
test_failed_upload_preserves_exit_and_phase
test_checksum_failure_is_recorded_without_remote_calls
test_invalid_schema_contract_prevents_remote_calls
test_maintenance_preparation_prevents_remote_calls
test_online_stage_requires_explicit_action_and_verified_plan
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
test_ssh_opt_in_options_are_shared 1 0 socket
test_ssh_opt_in_options_are_shared 0 1 none
test_ssh_opt_in_options_are_shared 1 1 regular
test_uncertain_stage_uses_opt_in_status_once

echo 'New API manual deploy client tests passed'
