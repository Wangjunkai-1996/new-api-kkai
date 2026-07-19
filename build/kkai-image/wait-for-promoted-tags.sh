#!/usr/bin/env bash
set -Eeuo pipefail

die() {
  echo "wait-for-promoted-tags: $*" >&2
  exit 1
}

[[ "$#" == 3 ]] || die "usage: $0 EXPECTED_DIGEST VERSION_REF SHA_REF"
readonly EXPECTED_DIGEST=$1
readonly VERSION_REF=$2
readonly SHA_REF=$3
readonly DEADLINE_SECONDS="${KKAI_PROMOTED_TAG_DEADLINE_SECONDS:-120}"
readonly INSPECT_TIMEOUT_SECONDS="${KKAI_PROMOTED_TAG_INSPECT_TIMEOUT_SECONDS:-10}"
readonly RETRY_INTERVAL_SECONDS="${KKAI_PROMOTED_TAG_RETRY_INTERVAL_SECONDS:-2}"

[[ "${EXPECTED_DIGEST}" =~ ^sha256:[0-9a-f]{64}$ ]] ||
  die "expected digest must be sha256 followed by 64 lowercase hex characters"
[[ -n "${VERSION_REF}" && -n "${SHA_REF}" ]] || die "tag references must not be empty"
if [[ ! "${DEADLINE_SECONDS}" =~ ^[0-9]+$ ]] ||
  ((DEADLINE_SECONDS < 1 || DEADLINE_SECONDS > 120)); then
  die "deadline must be between 1 and 120 seconds"
fi
if [[ ! "${INSPECT_TIMEOUT_SECONDS}" =~ ^[0-9]+$ ]] ||
  ((INSPECT_TIMEOUT_SECONDS < 1 || INSPECT_TIMEOUT_SECONDS > 10)); then
  die "inspect timeout must be between 1 and 10 seconds"
fi
if [[ ! "${RETRY_INTERVAL_SECONDS}" =~ ^[0-9]+$ ]] ||
  ((RETRY_INTERVAL_SECONDS > 10)); then
  die "retry interval must be between 0 and 10 seconds"
fi
command -v docker >/dev/null || die "docker is required"
command -v timeout >/dev/null || die "timeout is required"

temporary="$(mktemp -d)"
readonly temporary
trap 'rm -rf "${temporary}"' EXIT

run_inspect() {
  local ref=$1 output_file=$2 status_file=$3 timeout_seconds=$4 status=0
  if timeout "${timeout_seconds}s" \
    docker buildx imagetools inspect "${ref}" >"${output_file}" 2>&1; then
    status=0
  else
    status=$?
  fi
  printf '%s\n' "${status}" >"${status_file}"
}

classify_inspect() {
  local output_file=$1 status_file=$2 status lower digest_count digest
  read -r status <"${status_file}"
  if [[ "${status}" == 124 || "${status}" == 137 ]]; then
    echo retry_inspect_timeout
    return
  fi
  if [[ "${status}" != 0 ]]; then
    lower="$(tr '[:upper:]' '[:lower:]' <"${output_file}")"
    if [[ "${lower}" =~ unauthorized|authentication[[:space:]]+required ]]; then
      echo fatal_unauthorized
    elif [[ "${lower}" =~ denied|forbidden ]]; then
      echo fatal_denied
    elif [[ "${lower}" =~ invalid[[:space:]]+(reference|tag)|invalid[[:space:]]+reference[[:space:]]+format ]]; then
      echo fatal_invalid_reference
    elif [[ "${lower}" =~ manifest[[:space:]]+unknown ]]; then
      echo retry_manifest_unknown
    elif [[ "${lower}" =~ (^|[^0-9])404([^0-9]|$) ]]; then
      echo retry_http_404
    elif [[ "${lower}" =~ (^|[^0-9])429([^0-9]|$)|too[[:space:]]+many[[:space:]]+requests ]]; then
      echo retry_http_429
    elif [[ "${lower}" =~ (^|[^0-9])5[0-9]{2}([^0-9]|$) ]]; then
      echo retry_http_5xx
    elif [[ "${lower}" =~ i/o[[:space:]]+timeout|connection[[:space:]]+timed[[:space:]]+out|tls[[:space:]]+handshake[[:space:]]+timeout|context[[:space:]]+deadline[[:space:]]+exceeded|client\.timeout[[:space:]]+exceeded|request[[:space:]]+canceled[[:space:]]+while[[:space:]]+waiting[[:space:]]+for[[:space:]]+connection ]]; then
      echo retry_network_timeout
    else
      echo fatal_inspect_error
    fi
    return
  fi

  digest_count="$(grep -c '^Digest:[[:space:]]' "${output_file}" || true)"
  if [[ "${digest_count}" != 1 ]]; then
    echo fatal_invalid_output
    return
  fi
  digest="$(sed -n 's/^Digest:[[:space:]]*//p' "${output_file}")"
  if [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo fatal_invalid_output
  elif [[ "${digest}" != "${EXPECTED_DIGEST}" ]]; then
    echo "fatal_digest_mismatch:${digest}"
  else
    echo exact
  fi
}

fail_on_fatal() {
  local ref=$1 classification=$2 actual
  case "${classification}" in
    fatal_digest_mismatch:*)
      actual=${classification#fatal_digest_mismatch:}
      die "${ref}: digest_mismatch expected=${EXPECTED_DIGEST} actual=${actual}"
      ;;
    fatal_*) die "${ref}: ${classification}" ;;
  esac
}

readonly deadline=$((SECONDS + DEADLINE_SECONDS))
version_last=not_inspected
sha_last=not_inspected
while ((SECONDS < deadline)); do
  remaining=$((deadline - SECONDS))
  ((remaining > 0)) || break
  call_timeout=${INSPECT_TIMEOUT_SECONDS}
  ((call_timeout <= remaining)) || call_timeout=${remaining}

  run_inspect "${VERSION_REF}" "${temporary}/version.out" "${temporary}/version.status" \
    "${call_timeout}" &
  version_pid=$!
  run_inspect "${SHA_REF}" "${temporary}/sha.out" "${temporary}/sha.status" \
    "${call_timeout}" &
  sha_pid=$!
  wait "${version_pid}"
  wait "${sha_pid}"

  version_last="$(classify_inspect "${temporary}/version.out" "${temporary}/version.status")"
  sha_last="$(classify_inspect "${temporary}/sha.out" "${temporary}/sha.status")"
  fail_on_fatal "${VERSION_REF}" "${version_last}"
  fail_on_fatal "${SHA_REF}" "${sha_last}"
  if [[ "${version_last}" == exact && "${sha_last}" == exact ]]; then
    exit 0
  fi

  remaining=$((deadline - SECONDS))
  ((remaining > 0)) || break
  sleep_seconds=${RETRY_INTERVAL_SECONDS}
  ((sleep_seconds <= remaining)) || sleep_seconds=${remaining}
  ((sleep_seconds == 0)) || sleep "${sleep_seconds}"
done

echo "wait-for-promoted-tags: tags did not converge within ${DEADLINE_SECONDS}s" >&2
[[ "${version_last}" == exact ]] || echo "${VERSION_REF}: ${version_last}" >&2
[[ "${sha_last}" == exact ]] || echo "${SHA_REF}: ${sha_last}" >&2
exit 1
