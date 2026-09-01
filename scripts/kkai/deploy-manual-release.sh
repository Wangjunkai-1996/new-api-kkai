#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly CONTRACT="${ROOT}/scripts/kkai/manual-deployment-contract.env"

die() {
  echo "deploy-manual-release: $*" >&2
  exit 1
}

require_single_exact_line() {
  local output=$1
  local key=$2
  local expected=$3
  local error_message=$4
  local key_count exact_count

  # These protocol keys are fixed, regex-safe names. Reject duplicate or
  # conflicting values before any archive upload or candidate mutation.
  key_count="$(grep -Ec "^${key}=" <<<"${output}" || true)"
  exact_count="$(grep -Fxc -- "${expected}" <<<"${output}" || true)"
  [[ "${key_count}" -eq 1 && "${exact_count}" -eq 1 ]] || die "${error_message}"
}

valid_ipv4() {
  local address=$1 octet
  local -a octets

  IFS=. read -r -a octets <<<"${address}"
  (( ${#octets[@]} == 4 )) || return 1
  for octet in "${octets[@]}"; do
    [[ "${octet}" =~ ^[0-9]{1,3}$ ]] || return 1
    (( 10#${octet} <= 255 )) || return 1
  done
  [[ "${address}" != 0.0.0.0 ]]
}

valid_tunnel_target() {
  local target=$1 address port

  [[ "${target}" == *:* ]] || return 1
  address=${target%:*}
  port=${target##*:}
  valid_ipv4 "${address}" || return 1
  [[ "${port}" =~ ^[1-9][0-9]{0,4}$ ]] || return 1
  (( 10#${port} <= 65535 ))
}

stage_validation_error=''

validate_stage_output() {
  local output=$1
  local expected_version=$2
  local expected_frontend_mode=$3
  local stage_result_count stage_version_count exact_stage_result_count exact_stage_version_count
  local stage_slot_count stage_tunnel_count stage_expires_count stage_frontend_mode_count
  local exact_stage_frontend_mode_count
  local stage_slot stage_tunnel stage_expires stage_tunnel_value

  stage_validation_error=''
  stage_result_count="$(grep -Ec '^KKAI_CANDIDATE_STAGE_RESULT=' <<<"${output}" || true)"
  exact_stage_result_count="$(grep -Fxc -- 'KKAI_CANDIDATE_STAGE_RESULT=staged' <<<"${output}" || true)"
  if [[ "${stage_result_count}" -ne 1 || "${exact_stage_result_count}" -ne 1 ]]; then
    stage_validation_error='candidate stage did not report KKAI_CANDIDATE_STAGE_RESULT=staged exactly once'
    return 1
  fi

  stage_version_count="$(grep -Ec '^KKAI_CANDIDATE_VERSION=' <<<"${output}" || true)"
  exact_stage_version_count="$(grep -Fxc -- "KKAI_CANDIDATE_VERSION=${expected_version}" <<<"${output}" || true)"
  if [[ "${stage_version_count}" -ne 1 || "${exact_stage_version_count}" -ne 1 ]]; then
    stage_validation_error="candidate stage did not report KKAI_CANDIDATE_VERSION=${expected_version} exactly once"
    return 1
  fi

  stage_slot_count="$(grep -Ec '^KKAI_CANDIDATE_SLOT=' <<<"${output}" || true)"
  stage_slot="$(grep -E '^KKAI_CANDIDATE_SLOT=' <<<"${output}" || true)"
  if [[ "${stage_slot_count}" -ne 1 || ! "${stage_slot}" =~ ^KKAI_CANDIDATE_SLOT=(blue|green)$ ]]; then
    stage_validation_error='candidate stage did not report a valid KKAI_CANDIDATE_SLOT exactly once'
    return 1
  fi

  stage_tunnel_count="$(grep -Ec '^KKAI_CANDIDATE_TUNNEL_TARGET=' <<<"${output}" || true)"
  stage_tunnel="$(grep -E '^KKAI_CANDIDATE_TUNNEL_TARGET=' <<<"${output}" || true)"
  stage_tunnel_value=${stage_tunnel#KKAI_CANDIDATE_TUNNEL_TARGET=}
  if [[ "${stage_tunnel_count}" -ne 1 ]] || ! valid_tunnel_target "${stage_tunnel_value}"; then
    stage_validation_error='candidate stage did not report a valid KKAI_CANDIDATE_TUNNEL_TARGET exactly once'
    return 1
  fi

  stage_expires_count="$(grep -Ec '^KKAI_CANDIDATE_EXPIRES_AT=' <<<"${output}" || true)"
  stage_expires="$(grep -E '^KKAI_CANDIDATE_EXPIRES_AT=' <<<"${output}" || true)"
  if [[ "${stage_expires_count}" -ne 1 || ! "${stage_expires}" =~ ^KKAI_CANDIDATE_EXPIRES_AT=[1-9][0-9]*$ ]]; then
    stage_validation_error='candidate stage did not report a valid KKAI_CANDIDATE_EXPIRES_AT exactly once'
    return 1
  fi

  stage_frontend_mode_count="$(grep -Ec '^KKAI_CANDIDATE_FRONTEND_MODE=' <<<"${output}" || true)"
  exact_stage_frontend_mode_count="$(grep -Fxc -- "KKAI_CANDIDATE_FRONTEND_MODE=${expected_frontend_mode}" <<<"${output}" || true)"
  if [[ "${stage_frontend_mode_count}" -ne 1 || "${exact_stage_frontend_mode_count}" -ne 1 ]]; then
    stage_validation_error="candidate stage did not report KKAI_CANDIDATE_FRONTEND_MODE=${expected_frontend_mode} exactly once"
    return 1
  fi
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  shasum -a 256 "$1" | awk '{print $1}'
}

[[ $# -eq 2 && $1 == --stage ]] ||
  die "usage: deploy-manual-release.sh --stage METADATA.json"
METADATA="$(cd -- "$(dirname -- "$2")" && pwd)/$(basename -- "$2")"
readonly METADATA
[[ -f "${METADATA}" ]] || die "metadata file is missing"
[[ -f "${CONTRACT}" && ! -L "${CONTRACT}" ]] || die "deployment contract is missing or unsafe"
for command_name in jq scp ssh; do
  command -v "${command_name}" >/dev/null 2>&1 || die "missing ${command_name}"
done

KKAI_INFRA_SHA=''
KKAI_DEPLOYMENT_PROTOCOL=''
# shellcheck source=manual-deployment-contract.env
source "${CONTRACT}"
readonly KKAI_INFRA_SHA KKAI_DEPLOYMENT_PROTOCOL
[[ "${KKAI_INFRA_SHA}" =~ ^[0-9a-f]{40}$ ]] || die "invalid infrastructure SHA in deployment contract"
[[ "${KKAI_DEPLOYMENT_PROTOCOL}" == router-v3-staged ]] ||
  die "invalid deployment protocol in deployment contract"

version="$(jq --exit-status --raw-output '.version' "${METADATA}")"
source_sha="$(jq --exit-status --raw-output '.source_sha' "${METADATA}")"
image_tag="$(jq --exit-status --raw-output '.image_tag' "${METADATA}")"
schema_contract="$(jq --exit-status --raw-output '.schema_contract' "${METADATA}")"
frontend_mode="$(jq --exit-status --raw-output '.frontend_mode' "${METADATA}")"
archive_name="$(jq --exit-status --raw-output '.archive' "${METADATA}")"
archive_sha256="$(jq --exit-status --raw-output '.archive_sha256' "${METADATA}")"
platform="$(jq --exit-status --raw-output '.platform' "${METADATA}")"

[[ "${source_sha}" =~ ^[0-9a-f]{40}$ ]] || die "invalid source SHA"
[[ "${version}" =~ ^kkai-prod-[0-9]{8}\.[1-9][0-9]*-${source_sha:0:9}$ ]] ||
  die "invalid release version"
[[ "${image_tag}" == "kkai-newapi-manual:${version}" ]] || die "invalid image tag"
case "${schema_contract}" in
  feature | bridge) ;;
  *) die "invalid schema contract" ;;
esac
case "${frontend_mode}" in
  embedded | external) ;;
  *) die "invalid frontend mode" ;;
esac
[[ "${archive_name}" == "$(basename -- "${archive_name}")" ]] || die "invalid archive name"
[[ "${archive_sha256}" =~ ^[0-9a-f]{64}$ ]] || die "invalid archive checksum"
[[ "${platform}" == linux/amd64 ]] || die "invalid release platform"

archive="$(dirname -- "${METADATA}")/${archive_name}"
[[ -f "${archive}" ]] || die "release archive is missing"
[[ "$(sha256_file "${archive}")" == "${archive_sha256}" ]] || die "archive checksum mismatch"

readonly HOST=sys1
readonly KEY="${HOME}/.ssh/ovh_sys1"
readonly REMOTE_ARCHIVE="/tmp/newapi-manual-${version}.tar"
readonly -a SSH_OPTIONS=(
  -i "${KEY}"
  -o IdentitiesOnly=yes
  -o BatchMode=yes
  -o ConnectTimeout=12
  -o ServerAliveInterval=15
  -o ServerAliveCountMax=3
  -o ProxyCommand=none
  -o ProxyJump=none
  -o KexAlgorithms=curve25519-sha256
)

query_candidate_status_once() {
  local status_output='' status_rc=1

  if status_output="$(
    ssh "${SSH_OPTIONS[@]}" "${HOST}" \
      sudo -n /usr/local/sbin/kkai-newapi-manual-deploy candidate-status 2>&1
  )"; then
    printf '%s\n' "${status_output}" >&2
    return 0
  else
    status_rc=$?
  fi
  [[ -z "${status_output}" ]] || printf '%s\n' "${status_output}" >&2
  printf 'candidate-status failed (exit %s); preserve the stage evidence and stop.\n' \
    "${status_rc}" >&2
  return "${status_rc}"
}

handle_uncertain_stage() {
  local reason=$1
  local stage_output=${2:-}

  [[ -z "${stage_output}" ]] || printf '%s\n' "${stage_output}" >&2
  printf '%s\n' "${reason}" >&2
  printf '%s\n' \
    'The remote stage may have completed; querying candidate-status exactly once.' >&2
  if query_candidate_status_once; then
    die 'candidate stage outcome is uncertain; use the candidate-status result above and do not retry stage'
  fi
  die 'candidate stage outcome is uncertain and candidate-status failed; preserve evidence and stop'
}

preflight_output=''
if ! preflight_output="$(
  ssh "${SSH_OPTIONS[@]}" "${HOST}" \
    sudo -n /usr/local/sbin/kkai-newapi-manual-deploy preflight \
      --expected-infra-sha "${KKAI_INFRA_SHA}" \
      --deployment-protocol "${KKAI_DEPLOYMENT_PROTOCOL}" \
      --schema-contract "${schema_contract}" \
      --frontend-mode "${frontend_mode}"
)"; then
  die "production preflight failed; archive was not uploaded"
fi
require_single_exact_line \
  "${preflight_output}" \
  KKAI_PREFLIGHT_RESULT \
  KKAI_PREFLIGHT_RESULT=ready \
  "production preflight did not report ready"
require_single_exact_line \
  "${preflight_output}" \
  KKAI_INFRA_SHA \
  "KKAI_INFRA_SHA=${KKAI_INFRA_SHA}" \
  "production preflight infrastructure SHA mismatch"
require_single_exact_line \
  "${preflight_output}" \
  KKAI_DEPLOYMENT_PROTOCOL \
  "KKAI_DEPLOYMENT_PROTOCOL=${KKAI_DEPLOYMENT_PROTOCOL}" \
  "production preflight protocol mismatch"
require_single_exact_line \
  "${preflight_output}" \
  KKAI_SCHEMA_CONTRACT \
  "KKAI_SCHEMA_CONTRACT=${schema_contract}" \
  "production preflight schema contract mismatch"
require_single_exact_line \
  "${preflight_output}" \
  KKAI_FRONTEND_MODE \
  "KKAI_FRONTEND_MODE=${frontend_mode}" \
  "production preflight frontend mode mismatch"
printf '%s\n' "${preflight_output}"

stage_stdout="$(mktemp "${TMPDIR:-/tmp}/kkai-newapi-stage-stdout.XXXXXX")" ||
  die "unable to create temporary stage output file"
stage_stderr="$(mktemp "${TMPDIR:-/tmp}/kkai-newapi-stage-stderr.XXXXXX")" || {
  rm -f -- "${stage_stdout}"
  die "unable to create temporary stage diagnostics file"
}
trap 'rm -f -- "${stage_stdout}" "${stage_stderr}"' EXIT
scp "${SSH_OPTIONS[@]}" -- "${archive}" "${HOST}:${REMOTE_ARCHIVE}"
stage_statuses=()
if ssh "${SSH_OPTIONS[@]}" "${HOST}" \
    sudo -n /usr/local/sbin/kkai-newapi-manual-deploy stage \
      --archive "${REMOTE_ARCHIVE}" \
      --archive-sha256 "${archive_sha256}" \
      --source-sha "${source_sha}" \
      --version "${version}" \
      --image-tag "${image_tag}" \
      --expected-infra-sha "${KKAI_INFRA_SHA}" \
      --deployment-protocol "${KKAI_DEPLOYMENT_PROTOCOL}" \
      --schema-contract "${schema_contract}" \
      --frontend-mode "${frontend_mode}" 2>"${stage_stderr}" |
    tee "${stage_stdout}"; then
  stage_statuses=("${PIPESTATUS[@]}")
else
  stage_statuses=("${PIPESTATUS[@]}")
fi
if [[ "${stage_statuses[0]:-1}" -ne 0 || "${stage_statuses[1]:-1}" -ne 0 ]]; then
  [[ ! -s "${stage_stderr}" ]] || cat -- "${stage_stderr}" >&2
  handle_uncertain_stage 'candidate stage command failed'
fi
[[ ! -s "${stage_stderr}" ]] || cat -- "${stage_stderr}" >&2
stage_output="$(<"${stage_stdout}")"
if ! validate_stage_output "${stage_output}" "${version}" "${frontend_mode}"; then
  handle_uncertain_stage "${stage_validation_error}" "${stage_output}"
fi
