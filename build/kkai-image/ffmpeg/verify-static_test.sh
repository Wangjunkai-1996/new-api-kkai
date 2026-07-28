#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly ROOT
readonly VERIFY_STATIC_SCRIPT="${ROOT}/verify-static.sh"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kkai-ffmpeg-static-test.XXXXXX")"
readonly TEST_ROOT
readonly FAKE_BIN="${TEST_ROOT}/bin"
readonly EMPTY_BIN="${TEST_ROOT}/empty-bin"

cleanup() {
  rm -rf -- "${TEST_ROOT}"
}
trap cleanup EXIT

fail() {
  echo "FFmpeg static verifier test: $*" >&2
  exit 1
}

mkdir -p "${FAKE_BIN}" "${EMPTY_BIN}"
touch "${TEST_ROOT}/dynamic-elf" "${TEST_ROOT}/static-elf" "${TEST_ROOT}/static-pie"

printf '%s\n' \
  '#!/bin/sh' \
  "mode=\${SCANELF_TEST_MODE:-static}" \
  "inspection=\${2:-}" \
  "case \"\${mode}:\${inspection}\" in" \
  '  needed-error:-n) exit 7 ;;' \
  '  interp-error:-i) exit 9 ;;' \
  '  dynamic-needed:-n) printf "%s\\n" "ET_DYN libexample.so" ;;' \
  '  dynamic-interp:-i) printf "%s\\n" "/lib/ld-musl-x86_64.so.1" ;;' \
  'esac' \
  'exit 0' \
  > "${FAKE_BIN}/scanelf"
chmod 0755 "${FAKE_BIN}/scanelf"

run_expect_failure() {
  local name=$1
  local path=$2
  local mode=$3
  local expected_message=$4
  local fixture=$5
  local output_file="${TEST_ROOT}/${name}.log"
  local status=0

  env PATH="${path}" SCANELF_TEST_MODE="${mode}" \
    /bin/sh "${VERIFY_STATIC_SCRIPT}" "${fixture}" >"${output_file}" 2>&1 || status=$?
  [[ ${status} -ne 0 ]] || fail "${name} unexpectedly passed"
  grep -Fq -- "${expected_message}" "${output_file}" ||
    fail "${name} did not report: ${expected_message}"
}

run_expect_success() {
  local name=$1
  local mode=$2
  local fixture=$3

  env PATH="${FAKE_BIN}:/usr/bin:/bin" SCANELF_TEST_MODE="${mode}" \
    /bin/sh "${VERIFY_STATIC_SCRIPT}" "${fixture}" || fail "${name} unexpectedly failed"
}

run_expect_failure missing-scanelf "${EMPTY_BIN}" static 'scanelf is unavailable' "${TEST_ROOT}/static-elf"
run_expect_failure needed-error "${FAKE_BIN}:/usr/bin:/bin" needed-error \
  'NEEDED inspection failed with status 7' "${TEST_ROOT}/static-elf"
run_expect_failure interp-error "${FAKE_BIN}:/usr/bin:/bin" interp-error \
  'INTERP inspection failed with status 9' "${TEST_ROOT}/static-elf"
run_expect_failure dynamic-needed "${FAKE_BIN}:/usr/bin:/bin" dynamic-needed \
  'dynamic dependencies' "${TEST_ROOT}/dynamic-elf"
run_expect_failure dynamic-interp "${FAKE_BIN}:/usr/bin:/bin" dynamic-interp \
  'dynamic interpreter' "${TEST_ROOT}/dynamic-elf"
run_expect_success static-elf static "${TEST_ROOT}/static-elf"
run_expect_success static-pie static-pie "${TEST_ROOT}/static-pie"

echo "FFmpeg static verifier tests passed"
