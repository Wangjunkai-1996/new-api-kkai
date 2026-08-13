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
  -o BatchMode=yes
  -o ConnectTimeout=12
  -o ProxyCommand=none
  -o ProxyJump=none
  -o KexAlgorithms=curve25519-sha256
)

preflight_output=''
if ! preflight_output="$(
  ssh "${SSH_OPTIONS[@]}" "${HOST}" \
    sudo -n /usr/local/sbin/kkai-newapi-manual-deploy preflight \
      --expected-infra-sha "${KKAI_INFRA_SHA}" \
      --deployment-protocol "${KKAI_DEPLOYMENT_PROTOCOL}" \
      --schema-contract "${schema_contract}"
)"; then
  die "production preflight failed; archive was not uploaded"
fi
grep -Fx "KKAI_PREFLIGHT_RESULT=ready" <<< "${preflight_output}" >/dev/null ||
  die "production preflight did not report ready"
grep -Fx "KKAI_INFRA_SHA=${KKAI_INFRA_SHA}" <<< "${preflight_output}" >/dev/null ||
  die "production preflight infrastructure SHA mismatch"
grep -Fx "KKAI_DEPLOYMENT_PROTOCOL=${KKAI_DEPLOYMENT_PROTOCOL}" <<< "${preflight_output}" >/dev/null ||
  die "production preflight protocol mismatch"
grep -Fx "KKAI_SCHEMA_CONTRACT=${schema_contract}" <<< "${preflight_output}" >/dev/null ||
  die "production preflight schema contract mismatch"
printf '%s\n' "${preflight_output}"

scp "${SSH_OPTIONS[@]}" -- "${archive}" "${HOST}:${REMOTE_ARCHIVE}"
ssh "${SSH_OPTIONS[@]}" "${HOST}" \
  sudo -n /usr/local/sbin/kkai-newapi-manual-deploy stage \
    --archive "${REMOTE_ARCHIVE}" \
    --archive-sha256 "${archive_sha256}" \
    --source-sha "${source_sha}" \
    --version "${version}" \
    --image-tag "${image_tag}" \
    --expected-infra-sha "${KKAI_INFRA_SHA}" \
    --deployment-protocol "${KKAI_DEPLOYMENT_PROTOCOL}" \
    --schema-contract "${schema_contract}"
