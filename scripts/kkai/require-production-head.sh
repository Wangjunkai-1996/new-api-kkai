#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly SOURCE_REPOSITORY=github.com/Wangjunkai-1996/new-api-kkai
readonly PRODUCTION_REF=refs/heads/production/kkrich

die() {
  echo "require-production-head: $*" >&2
  exit 1
}

[[ $# -eq 1 ]] || die "usage: require-production-head.sh <source-sha>"
readonly SOURCE_SHA=$1
[[ "${SOURCE_SHA}" =~ ^[0-9a-f]{40}$ ]] || die "invalid source SHA"

origin_url="$(git -C "${ROOT}" remote get-url origin)" || die "origin remote is missing"
case "${origin_url}" in
  https://github.com/Wangjunkai-1996/new-api-kkai | \
    https://github.com/Wangjunkai-1996/new-api-kkai.git | \
    git@github.com:Wangjunkai-1996/new-api-kkai | \
    git@github.com:Wangjunkai-1996/new-api-kkai.git | \
    ssh://git@github.com/Wangjunkai-1996/new-api-kkai | \
    ssh://git@github.com/Wangjunkai-1996/new-api-kkai.git) ;;
  *) die "origin is not the canonical ${SOURCE_REPOSITORY} repository" ;;
esac

remote_record="$(git -C "${ROOT}" ls-remote --exit-code --heads origin "${PRODUCTION_REF}")" ||
  die "cannot resolve remote production HEAD"
[[ -n "${remote_record}" && "${remote_record}" != *$'\n'* ]] ||
  die "remote production ref is ambiguous"
read -r remote_sha remote_ref extra <<< "${remote_record}"
[[ "${remote_sha}" =~ ^[0-9a-f]{40}$ ]] || die "remote production SHA is invalid"
[[ "${remote_ref}" == "${PRODUCTION_REF}" && -z "${extra:-}" ]] ||
  die "remote production ref is invalid"
[[ "${remote_sha}" == "${SOURCE_SHA}" ]] ||
  die "source SHA is no longer the production branch HEAD"
