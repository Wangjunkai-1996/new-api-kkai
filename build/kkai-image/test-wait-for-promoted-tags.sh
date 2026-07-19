#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly SCRIPT="${ROOT}/build/kkai-image/wait-for-promoted-tags.sh"
readonly EXPECTED_DIGEST="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
readonly WRONG_DIGEST="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
readonly VERSION_REF="ghcr.io/wangjunkai-1996/new-api-kkai:kkai-prod-test"
readonly SHA_REF="ghcr.io/wangjunkai-1996/new-api-kkai:sha-test"

fail() {
  echo "wait-for-promoted-tags test: $*" >&2
  exit 1
}

[[ -x "${SCRIPT}" ]] || fail "script is not executable"

test_root="$(mktemp -d)"
readonly test_root
trap 'rm -rf "${test_root}"' EXIT
fixture_bin="${test_root}/bin"
state_dir="${test_root}/state"
mkdir -p "${fixture_bin}" "${state_dir}"

cat >"${fixture_bin}/timeout" <<'PYTHON'
#!/usr/bin/env python3
import subprocess
import sys

raw_seconds = sys.argv[1]
seconds = float(raw_seconds[:-1] if raw_seconds.endswith("s") else raw_seconds)
try:
    result = subprocess.run(sys.argv[2:], timeout=seconds, check=False)
except subprocess.TimeoutExpired:
    raise SystemExit(124)
raise SystemExit(result.returncode)
PYTHON

cat >"${fixture_bin}/docker" <<'BASH'
#!/usr/bin/env bash
set -Eeuo pipefail

[[ "$#" == 4 && "$1" == buildx && "$2" == imagetools && "$3" == inspect ]]
ref="$4"
if [[ "${ref}" == "${FAKE_VERSION_REF}" ]]; then
  key=version
elif [[ "${ref}" == "${FAKE_SHA_REF}" ]]; then
  key=sha
else
  echo "invalid reference: ${ref}" >&2
  exit 1
fi
counter_file="${FAKE_STATE_DIR}/${key}.count"
count=0
[[ ! -f "${counter_file}" ]] || read -r count <"${counter_file}"
count=$((count + 1))
printf '%s\n' "${count}" >"${counter_file}"
printf '%s\n' "${ref}" >>"${FAKE_CALL_LOG}"

emit_digest() {
  local digest=$1
  printf 'Name: %s\nMediaType: application/vnd.oci.image.index.v1+json\nDigest: %s\n' \
    "${ref}" "${digest}"
}

case "${FAKE_MODE}" in
  immediate)
    emit_digest "${FAKE_EXPECTED_DIGEST}"
    ;;
  retry_once)
    if ((count == 1)); then
      echo "${FAKE_RETRY_MESSAGE}" >&2
      exit 1
    fi
    emit_digest "${FAKE_EXPECTED_DIGEST}"
    ;;
  wrong_digest)
    if [[ "${key}" == version ]]; then
      emit_digest "${FAKE_WRONG_DIGEST}"
    else
      emit_digest "${FAKE_EXPECTED_DIGEST}"
    fi
    ;;
  unauthorized)
    if [[ "${key}" == version ]]; then
      echo 'unauthorized: authentication required' >&2
      exit 1
    fi
    emit_digest "${FAKE_EXPECTED_DIGEST}"
    ;;
  invalid_reference)
    if [[ "${key}" == version ]]; then
      echo 'invalid reference format' >&2
      exit 1
    fi
    emit_digest "${FAKE_EXPECTED_DIGEST}"
    ;;
  invalid_output)
    if [[ "${key}" == version ]]; then
      echo 'Digest: not-a-digest'
    else
      emit_digest "${FAKE_EXPECTED_DIGEST}"
    fi
    ;;
  inspect_timeout)
    if ((count == 1)); then
      sleep 2
    fi
    emit_digest "${FAKE_EXPECTED_DIGEST}"
    ;;
  always_retry)
    echo "${FAKE_RETRY_MESSAGE}" >&2
    exit 1
    ;;
  *)
    echo "unknown fake mode: ${FAKE_MODE}" >&2
    exit 2
    ;;
esac
BASH
chmod +x "${fixture_bin}/timeout" "${fixture_bin}/docker"

run_wait() {
  local expected_digest=$1 mode=$2 deadline=$3 inspect_timeout=$4 interval=$5
  local retry_message=${6:-manifest unknown}
  rm -rf "${state_dir}"
  mkdir -p "${state_dir}"
  : >"${test_root}/calls"
  set +e
  PATH="${fixture_bin}:${PATH}" \
    FAKE_MODE="${mode}" \
    FAKE_RETRY_MESSAGE="${retry_message}" \
    FAKE_EXPECTED_DIGEST="${EXPECTED_DIGEST}" \
    FAKE_WRONG_DIGEST="${WRONG_DIGEST}" \
    FAKE_VERSION_REF="${VERSION_REF}" \
    FAKE_SHA_REF="${SHA_REF}" \
    FAKE_STATE_DIR="${state_dir}" \
    FAKE_CALL_LOG="${test_root}/calls" \
    KKAI_PROMOTED_TAG_DEADLINE_SECONDS="${deadline}" \
    KKAI_PROMOTED_TAG_INSPECT_TIMEOUT_SECONDS="${inspect_timeout}" \
    KKAI_PROMOTED_TAG_RETRY_INTERVAL_SECONDS="${interval}" \
    "${SCRIPT}" "${expected_digest}" "${VERSION_REF}" "${SHA_REF}" \
    >"${test_root}/stdout" 2>"${test_root}/stderr"
  run_status=$?
  set -e
}

call_count() {
  wc -l <"${test_root}/calls" | tr -d ' '
}

run_wait "${EXPECTED_DIGEST}" immediate 5 1 0
[[ "${run_status}" == 0 ]] || fail "immediate visibility did not succeed"
[[ "$(call_count)" == 2 ]] || fail "immediate visibility did not inspect both tags once"

for retry_message in \
  'manifest unknown' \
  'unexpected status from HEAD request: 404 Not Found' \
  '429 Too Many Requests' \
  '503 Service Unavailable' \
  'net/http: request canceled while waiting for connection (Client.Timeout exceeded)'; do
  run_wait "${EXPECTED_DIGEST}" retry_once 5 1 0 "${retry_message}"
  [[ "${run_status}" == 0 ]] || fail "retryable error did not converge: ${retry_message}"
  [[ "$(call_count)" == 4 ]] || fail "retryable error did not inspect both tags per round"
done

run_wait "${EXPECTED_DIGEST}" wrong_digest 5 1 0
[[ "${run_status}" != 0 ]] || fail "wrong digest was accepted"
[[ "$(call_count)" == 2 ]] || fail "wrong digest was retried"
grep -Fq "digest_mismatch" "${test_root}/stderr" || fail "wrong digest classification is missing"

run_wait "${EXPECTED_DIGEST}" unauthorized 5 1 0
[[ "${run_status}" != 0 ]] || fail "unauthorized error was retried"
grep -Fq "fatal_unauthorized" "${test_root}/stderr" || fail "unauthorized classification is missing"

run_wait "${EXPECTED_DIGEST}" invalid_reference 5 1 0
[[ "${run_status}" != 0 ]] || fail "invalid reference error was retried"
grep -Fq "fatal_invalid_reference" "${test_root}/stderr" || fail "invalid reference classification is missing"

run_wait "${EXPECTED_DIGEST}" invalid_output 5 1 0
[[ "${run_status}" != 0 ]] || fail "invalid inspect output was retried"
grep -Fq "fatal_invalid_output" "${test_root}/stderr" || fail "invalid output classification is missing"

run_wait "${EXPECTED_DIGEST}" inspect_timeout 5 1 0
[[ "${run_status}" == 0 ]] || fail "single-call timeout did not retry"
[[ "$(call_count)" == 4 ]] || fail "single-call timeout did not retry both tags"

run_wait "${EXPECTED_DIGEST}" always_retry 1 1 1 'manifest unknown'
[[ "${run_status}" != 0 ]] || fail "overall deadline did not fail closed"
grep -Fq "${VERSION_REF}: retry_manifest_unknown" "${test_root}/stderr" ||
  fail "deadline diagnostic omits the version tag classification"
grep -Fq "${SHA_REF}: retry_manifest_unknown" "${test_root}/stderr" ||
  fail "deadline diagnostic omits the SHA tag classification"

run_wait 'sha256:invalid' immediate 5 1 0
[[ "${run_status}" != 0 ]] || fail "invalid expected digest was accepted"
[[ "$(call_count)" == 0 ]] || fail "invalid expected digest reached the registry"
grep -Fq 'expected digest must be sha256' "${test_root}/stderr" ||
  fail "invalid expected digest diagnostic is missing"

echo "wait-for-promoted-tags behavior tests passed"
