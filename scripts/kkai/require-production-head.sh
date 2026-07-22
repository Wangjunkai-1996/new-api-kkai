#!/usr/bin/env bash
set -Eeuo pipefail

die() {
  echo "require-production-head: $*" >&2
  exit 1
}

[[ $# -eq 1 ]] || die "usage: require-production-head.sh <source-sha>"
readonly SOURCE_SHA=$1
readonly PRODUCTION_REF=refs/heads/production/kkrich

[[ "${SOURCE_SHA}" =~ ^[0-9a-f]{40}$ ]] || die "invalid source SHA"
remote_record="$(git ls-remote --exit-code origin refs/heads/production/kkrich)"
read -r remote_sha remote_ref extra <<< "${remote_record}"
[[ "${remote_sha}" =~ ^[0-9a-f]{40}$ ]] || die "remote production SHA is invalid"
[[ "${remote_ref}" == "${PRODUCTION_REF}" && -z "${extra:-}" ]] ||
  die "remote production ref is invalid"
[[ "${remote_sha}" == "${SOURCE_SHA}" ]] ||
  die "source SHA is no longer the production branch HEAD"
