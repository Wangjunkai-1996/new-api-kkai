#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly BUILD_SCRIPT="${ROOT}/scripts/kkai/frontend-build-release.sh"

fail() {
  echo "frontend-build-release test: $*" >&2
  exit 1
}

test_root="$(mktemp -d "${TMPDIR:-/tmp}/kkai-frontend-build-test.XXXXXX")"
trap 'rm -rf -- "${test_root}"' EXIT
readonly test_root
readonly mock_bin="${test_root}/bin"
readonly call_log="${test_root}/calls.log"
readonly install_marker="${test_root}/install.marker"
readonly lock_dir="${test_root}/build.lock"
source_sha="$(git -C "${ROOT}" rev-parse HEAD)"
readonly source_sha
readonly release_id="frontend-test-20260901-1"
mkdir -p -- "${mock_bin}"

cat >"${mock_bin}/bun" <<'EOF_BUN'
#!/usr/bin/env bash
set -Eeuo pipefail

printf '%s\n' "$*" >>"${KKAI_TEST_LOG}"
case "${1:-}" in
  --version)
    printf '%s\n' '1.3.13'
    ;;
  install)
    : >"${KKAI_TEST_INSTALL_MARKER}"
    ;;
  run)
    [[ ${2:-} == build ]] || {
      echo "unexpected bun run command: $*" >&2
      exit 91
    }
    [[ ${KKAI_TEST_FAIL_BUILD:-0} != 1 ]] || exit 92
    [[ ${KKAI_EXTERNAL_FRONTEND_BUILD:-} == 1 ]] || {
      echo 'external frontend build marker is missing' >&2
      exit 95
    }
    dist=''
    previous=''
    for argument in "$@"; do
      if [[ ${previous} == --dist-path ]]; then
        dist=${argument}
        break
      fi
      previous=${argument}
    done
    [[ -n ${dist} ]] || {
      echo 'mock bun did not receive --dist-path' >&2
      exit 93
    }
    theme=$(basename -- "${PWD}")
    mkdir -p -- "${dist}/static"
    printf '<!doctype html><title>%s</title>\n' "${theme}" >"${dist}/index.html"
    printf '%s\n' "${theme}-bundle" >"${dist}/static/${theme}.js"
    ;;
  *)
    echo "unexpected bun invocation: $*" >&2
    exit 94
    ;;
esac
EOF_BUN
chmod 0755 "${mock_bin}/bun"

run_build() {
  local output_dir=$1
  local id=$2
  local output
  shift 2

  mkdir -p -- "${output_dir}"
  : >"${call_log}"
  output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_INSTALL_MARKER="${install_marker}" \
      "${BUILD_SCRIPT}" \
        --output-dir "${output_dir}" \
        --release-id "${id}" \
        --source-sha "${source_sha}" \
        --schema-contract bridge \
        --api-contract 7 \
        --build-timestamp 2026-09-01T00:00:00Z \
        --allow-dirty \
        --lock-dir "${lock_dir}" \
        "$@" 2>&1
  )" || fail "build ${id} failed\n${output}"
  printf '%s\n' "${output}"
}

out_both="${test_root}/out-both"
run_build "${out_both}" "${release_id}" \
  --backend-source-sha "${source_sha}" \
  --backend-release-id backend-test-20260901-1 >/dev/null

release_root="${out_both}/frontend-releases/${release_id}"
for required_file in \
  "${release_root}/default/index.html" \
  "${release_root}/classic/index.html" \
  "${release_root}/LICENSE" \
  "${release_root}/NOTICE" \
  "${release_root}/THIRD-PARTY-LICENSES.md" \
  "${release_root}/frontend.json" \
  "${release_root}/release-pair.json" \
  "${release_root}/manifest.sha256" \
  "${out_both}/${release_id}.tar.gz" \
  "${out_both}/${release_id}.json"; do
  [[ -s ${required_file} ]] || fail "missing artifact file: ${required_file}"
done
[[ -e ${install_marker} ]] || fail 'the default build did not run frozen install'
grep -F 'install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1' "${call_log}" >/dev/null ||
  fail 'frozen install arguments were not forwarded'
grep -F 'run build -- --dist-path' "${call_log}" >/dev/null ||
  fail 'frontend build command was not invoked'

jq -e --arg id "${release_id}" \
  '.release_id == $id and .backend_release_id == "backend-test-20260901-1" and .themes == ["default", "classic"] and .api_contract == 7 and .api_base_url == "relative"' \
  "${out_both}/${release_id}.json" >/dev/null || fail 'artifact metadata is incorrect'
jq -e '.build.install == "frozen" and .build.lockfile == "web/bun.lock"' \
  "${release_root}/frontend.json" >/dev/null || fail 'frontend build metadata is incorrect'
tar -tzf "${out_both}/${release_id}.tar.gz" | grep -F "frontend-releases/${release_id}/default/index.html" >/dev/null ||
  fail 'default frontend is absent from archive'
tar -tzf "${out_both}/${release_id}.tar.gz" | grep -F "frontend-releases/${release_id}/classic/index.html" >/dev/null ||
  fail 'classic frontend is absent from archive'
grep -F 'LICENSE' "${release_root}/manifest.sha256" >/dev/null || fail 'legal files are absent from manifest'
[[ ! -e ${lock_dir} ]] || fail 'frontend build lock was not removed'

if duplicate_output="$({
  run_build "${out_both}" "${release_id}"
} 2>&1)"; then
  fail 'duplicate release unexpectedly succeeded'
fi
grep -F 'release output already exists' <<<"${duplicate_output}" >/dev/null ||
  fail 'duplicate release error was not explicit'

failed_output="${test_root}/out-failed"
mkdir -p -- "${failed_output}"
if PATH="${mock_bin}:${PATH}" \
  KKAI_TEST_LOG="${call_log}" \
  KKAI_TEST_INSTALL_MARKER="${install_marker}" \
  KKAI_TEST_FAIL_BUILD=1 \
  "${BUILD_SCRIPT}" \
    --output-dir "${failed_output}" \
    --release-id "${release_id}-failed" \
    --source-sha "${source_sha}" \
    --backend-source-sha "${source_sha}" \
    --backend-release-id backend-test-20260901-1 \
    --schema-contract bridge \
    --api-contract 7 \
    --build-timestamp 2026-09-01T00:00:00Z \
    --allow-dirty \
    --skip-install \
    --lock-dir "${lock_dir}" \
    >"${test_root}/failed-build.log" 2>&1; then
  fail 'failed theme build unexpectedly succeeded'
fi
[[ ! -e "${failed_output}/frontend-releases/${release_id}-failed" ]] ||
  fail 'failed theme build left a release directory'
[[ ! -e "${failed_output}/${release_id}-failed.tar.gz" ]] ||
  fail 'failed theme build left an archive'
[[ ! -e "${failed_output}/${release_id}-failed.json" ]] ||
  fail 'failed theme build left metadata'
[[ ! -e "${lock_dir}" ]] || fail 'failed theme build left its lock'

out_default="${test_root}/out-default"
run_build "${out_default}" "${release_id}-default" --theme default --skip-install >/dev/null
default_root="${out_default}/frontend-releases/${release_id}-default"
[[ -f ${default_root}/default/index.html ]] || fail 'default-only artifact is missing default theme'
[[ ! -e ${default_root}/classic ]] || fail 'default-only artifact contains classic theme'
jq -e '.themes == ["default"] and .theme_selection == "default"' \
  "${out_default}/${release_id}-default.json" >/dev/null || fail 'default-only metadata is incorrect'
jq -e '.default_theme == "default"' \
  "${out_default}/${release_id}-default.json" >/dev/null || fail 'default-only default theme is incorrect'

out_classic="${test_root}/out-classic"
run_build "${out_classic}" "${release_id}-classic" --theme classic --skip-install >/dev/null
classic_root="${out_classic}/frontend-releases/${release_id}-classic"
[[ -f ${classic_root}/classic/index.html ]] || fail 'classic-only artifact is missing classic theme'
[[ ! -e ${classic_root}/default ]] || fail 'classic-only artifact contains default theme'
jq -e '.themes == ["classic"] and .theme_selection == "classic" and .default_theme == "classic"' \
  "${out_classic}/${release_id}-classic.json" >/dev/null || fail 'classic-only metadata is incorrect'

false_env_output="${test_root}/out-false-env"
rm -f -- "${install_marker}"
KKAI_FRONTEND_SKIP_INSTALL=0 run_build "${false_env_output}" "${release_id}-false-env" --theme default >/dev/null
[[ -e ${install_marker} ]] || fail 'false skip-install value incorrectly skipped install'

dry_output="${test_root}/dry-output"
dry_run_output="$(
  "${BUILD_SCRIPT}" \
    --dry-run \
    --skip-install \
    --allow-non-production \
    --allow-dirty \
    --source-root "${test_root}/missing-source" \
    --source-sha "${source_sha}" \
    --release-id dry-run-test \
    --schema-contract bridge \
    --api-contract 7 \
    --output-dir "${dry_output}"
)" || fail 'dry-run unexpectedly failed'
grep -F 'DRY_RUN=1' <<<"${dry_run_output}" >/dev/null || fail 'dry-run marker is missing'
[[ ! -e ${dry_output} ]] || fail 'dry-run wrote output files'

if invalid_output="$({
  "${BUILD_SCRIPT}" --source-sha "${source_sha}" --schema-contract invalid --allow-dirty 2>&1
} 2>&1)"; then
  fail 'invalid schema contract unexpectedly succeeded'
fi
grep -F 'schema contract must be bridge or feature' <<<"${invalid_output}" >/dev/null ||
  fail 'invalid schema contract error was not explicit'

echo 'Frontend artifact build regression tests passed.'
