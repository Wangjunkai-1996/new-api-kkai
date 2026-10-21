#!/usr/bin/env bash
# Only reads official state. A local record never authorizes a mutation.
# shellcheck source-path=SCRIPTDIR
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
die() { echo "release-resume-status: $*" >&2; exit 1; }

[[ $# == 2 && $1 == --metadata || $# == 6 && $1 == --metadata && $3 == --slot && $5 == --rollout-id ]] ||
  die 'usage: release-resume-status.sh --metadata METADATA.json [--slot blue|green --rollout-id ID]'
metadata=$2
expected_slot=${4:-}
expected_rollout=${6:-}
[[ -f ${metadata} ]] || die 'metadata is missing'
[[ -z ${expected_slot} || ${expected_slot} == blue || ${expected_slot} == green ]] || die 'invalid expected slot'
[[ -z ${expected_rollout} || ${expected_rollout} =~ ^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$ ]] || die 'invalid rollout ID'
for command_name in jq ssh; do command -v "${command_name}" >/dev/null || die "missing ${command_name}"; done
version="$(jq -er '.version' "${metadata}")"
source_sha="$(jq -er '.source_sha' "${metadata}")"
[[ ${source_sha} =~ ^[0-9a-f]{40}$ && ${version} =~ ^kkai-prod-[0-9]{8}\.[1-9][0-9]*-${source_sha:0:9}$ ]] || die 'invalid metadata identity'
KKAI_INFRA_SHA=''
KKAI_DEPLOYMENT_PROTOCOL=''
# shellcheck source=manual-deployment-contract.env
source "${ROOT}/scripts/kkai/manual-deployment-contract.env"
[[ ${KKAI_INFRA_SHA} =~ ^[0-9a-f]{40}$ && ${KKAI_DEPLOYMENT_PROTOCOL} == router-v3-staged ]] || die 'invalid deployment contract'
ssh_options=(
  -i "${HOME}/.ssh/ovh_sys1" -o IdentitiesOnly=yes -o BatchMode=yes
  -o ConnectTimeout=12 -o ServerAliveInterval=15 -o ServerAliveCountMax=3
  -o ProxyCommand=none -o ProxyJump=none -o KexAlgorithms=curve25519-sha256
)
# Candidate-status validates the installed controller and its persisted identity.
# No upload, stage, rollout apply, promote, rollback, cleanup or retry is invoked.
candidate="$(ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-manual-deploy candidate-status)" ||
  die 'candidate query failed; inspect official state before any recovery'
printf '%s\n' "${candidate}"
[[ $(grep -c '^KKAI_CANDIDATE_STATUS=' <<<"${candidate}") == 1 ]] || die 'ambiguous candidate status'
if grep -qx 'KKAI_CANDIDATE_STATUS=ready' <<<"${candidate}"; then
  if [[ $(grep -c '^KKAI_CANDIDATE_VERSION=' <<<"${candidate}") != 1 ]] ||
      ! grep -qx "KKAI_CANDIDATE_VERSION=${version}" <<<"${candidate}"; then
    die 'candidate version differs from metadata'
  fi
  if [[ -n ${expected_slot} ]]; then
    if [[ $(grep -c '^KKAI_CANDIDATE_SLOT=' <<<"${candidate}") != 1 ]] ||
        ! grep -qx "KKAI_CANDIDATE_SLOT=${expected_slot}" <<<"${candidate}"; then
      die 'candidate slot differs from recorded target'
    fi
  fi
elif ! grep -qx 'KKAI_CANDIDATE_STATUS=none' <<<"${candidate}"; then
  die 'candidate is not ready; use the runbook for diagnosis'
fi
rollout="$(ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-rollout status --json)" ||
  die 'rollout query failed; do not replay a mutation'
printf '%s\n' "${rollout}"
jq -e 'type == "object" and .result == "status" and (.drift | type == "array") and (.drift | length == 0) and (.status != "recovery_required")' <<<"${rollout}" >/dev/null ||
  die 'rollout state has drift or requires recovery'
if [[ -n ${expected_rollout} ]]; then
  jq -e --arg id "${expected_rollout}" --arg slot "${expected_slot}" --arg version "${version}" \
    '.healthy == true and .state.rollout_id == $id and .state.target_slot == $slot and .state.target_version == $version' \
    <<<"${rollout}" >/dev/null || die 'rollout ID/version/slot differs from recorded coordinates'
else
  jq -e '.status == "inactive" and .state == null' <<<"${rollout}" >/dev/null ||
    die 'rollout state exists; supply this release exact --slot and --rollout-id before recovery'
fi
printf '%s\n' 'RESUME_STATUS=read-only; follow the runbook after reviewing exact current state; this is not release acceptance'
