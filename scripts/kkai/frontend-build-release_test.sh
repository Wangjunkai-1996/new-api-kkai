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
backend_image_digest="sha256:$(printf '2%.0s' {1..64})"
readonly backend_image_digest
KKAI_TEST_PINNED_BUN_VERSION="$(awk -F= '$1 == "KKAI_BUN_VERSION" { print $2 }' "${ROOT}/docs/kkai/upstream-baseline.env")"
export KKAI_TEST_PINNED_BUN_VERSION
mkdir -p -- "${mock_bin}"

cat >"${mock_bin}/bun" <<'EOF_BUN'
#!/usr/bin/env bash
set -Eeuo pipefail

printf '%s\n' "$*" >>"${KKAI_TEST_LOG}"
case "${1:-}" in
  --version)
    printf '%s\n' "${KKAI_TEST_BUN_VERSION:-${KKAI_TEST_PINNED_BUN_VERSION}}"
    ;;
  install)
    : >"${KKAI_TEST_INSTALL_MARKER}"
    if [[ -n ${KKAI_TEST_SOURCE_REPO:-} ]]; then
      [[ ${PWD} != "${KKAI_TEST_SOURCE_REPO}/web" ]] || exit 96
      [[ ! -e default/.env && ! -e node_modules/injected ]] || exit 97
      case ${KKAI_TEST_MUTATE_ARCHIVE:-} in
        tracked) printf 'changed-lock\n' >bun.lock ;;
        untracked) printf 'unexpected-source\n' >default/injected.ts ;;
      esac
      if [[ ${KKAI_TEST_DRIFT_SOURCE:-0} == 1 ]]; then
        printf 'changed-input\n' >"${KKAI_TEST_SOURCE_REPO}/web/default/input.txt"
        git -C "${KKAI_TEST_SOURCE_REPO}" add web/default/input.txt
        git -C "${KKAI_TEST_SOURCE_REPO}" -c user.name=Test -c user.email=test@example.invalid commit --quiet -m concurrent-change
      fi
    fi
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
    if [[ -n ${KKAI_TEST_SOURCE_REPO:-} ]]; then
      [[ $(cat input.txt) == committed-input ]] || exit 98
      cat input.txt >"${KKAI_TEST_SOURCE_SEEN}"
    fi
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
      "${BUILD_SCRIPT}" --legacy-pair \
        --output-dir "${output_dir}" \
        --release-id "${id}" \
        --source-sha "${source_sha}" \
        --schema-contract bridge \
        --api-contract 7 \
        --backend-image-digest "${backend_image_digest}" \
        --build-timestamp 2026-09-01T00:00:00Z \
        --allow-dirty \
        --lock-dir "${lock_dir}" \
        "$@" 2>&1
  )" || fail "build ${id} failed\n${output}"
  printf '%s\n' "${output}"
}

out_both="${test_root}/out-both"
build_output="$(run_build "${out_both}" "${release_id}" \
  --theme both \
  --backend-source-sha "${source_sha}" \
  --backend-release-id backend-test-20260901-1)"

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
  --arg digest "${backend_image_digest}" \
  '.release_id == $id and .backend_release_id == "backend-test-20260901-1" and .backend_image_digest == $digest and .themes == ["default", "classic"] and .api_contract == 7 and .api_base_url == "relative"' \
  "${out_both}/${release_id}.json" >/dev/null || fail 'artifact metadata is incorrect'
jq -e '.build.install == "frozen" and .build.lockfile == "web/bun.lock"' \
  "${release_root}/frontend.json" >/dev/null || fail 'frontend build metadata is incorrect'
tar -tzf "${out_both}/${release_id}.tar.gz" | grep -F "frontend-releases/${release_id}/default/index.html" >/dev/null ||
  fail 'default frontend is absent from archive'
tar -tzf "${out_both}/${release_id}.tar.gz" | grep -F "frontend-releases/${release_id}/classic/index.html" >/dev/null ||
  fail 'classic frontend is absent from archive'
grep -F 'LICENSE' "${release_root}/manifest.sha256" >/dev/null || fail 'legal files are absent from manifest'
[[ ! -e ${lock_dir} ]] || fail 'frontend build lock was not removed'
record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${build_output}")"
archive_bytes="$(wc -c < "${out_both}/${release_id}.tar.gz" | tr -d '[:space:]')"
jq -e --arg id "${release_id}" --arg sha "${source_sha}" --arg bytes "${archive_bytes}" \
  --slurpfile metadata "${out_both}/${release_id}.json" \
  --slurpfile frontend "${release_root}/frontend.json" \
  '.kind == "frontend-build" and .release_id == $id and .source_sha == $sha and
   .result == "passed" and .exit_code == 0 and
   .identity.theme == "both" and .identity.frontend_format == "1" and
   .identity.bun_version == $frontend[0].build.bun_version and
   .identity.lockfile_sha256 == $frontend[0].build.lockfile_sha256 and
   .identity.archive_sha256 == $metadata[0].archive_sha256 and .identity.archive_bytes == $bytes and
   ([.phases[].name] == ["source-export", "bun-version", "dependency-install", "bundle", "source-verify", "package-digests", "archive", "package-metadata", "publish"]) and
   all(.phases[]; .result == "passed" and .duration_seconds >= 0)' \
  "${record_path}" >/dev/null || fail 'successful build record is incomplete or differs from its artifact'
if tar -tzf "${out_both}/${release_id}.tar.gz" | grep -F "$(basename -- "${record_path}")" >/dev/null; then
  fail 'local timing record entered the immutable artifact'
fi

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
  "${BUILD_SCRIPT}" --legacy-pair \
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
else
  failure_status=$?
fi
[[ ${failure_status} == 92 ]] || fail 'timing record changed the failed build exit status'
failed_record="$(sed -n 's/^RELEASE_RECORD=//p' "${test_root}/failed-build.log")"
jq -e '.result == "failed" and .exit_code == 92 and
  .phases[-1].name == "bundle" and .phases[-1].result == "failed" and .phases[-1].exit_code == 92' \
  "${failed_record}" >/dev/null || fail 'failed bundle did not retain its phase evidence'
[[ ! -e "${failed_output}/frontend-releases/${release_id}-failed" ]] ||
  fail 'failed theme build left a release directory'
[[ ! -e "${failed_output}/${release_id}-failed.tar.gz" ]] ||
  fail 'failed theme build left an archive'
[[ ! -e "${failed_output}/${release_id}-failed.json" ]] ||
  fail 'failed theme build left metadata'
[[ ! -e "${lock_dir}" ]] || fail 'failed theme build left its lock'

out_default="${test_root}/out-default"
run_build "${out_default}" "${release_id}-default" >/dev/null
default_root="${out_default}/frontend-releases/${release_id}-default"
[[ -f ${default_root}/default/index.html ]] || fail 'default-only artifact is missing default theme'
[[ ! -e ${default_root}/classic ]] || fail 'default-only artifact contains classic theme'
grep -Fx 'install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1 --filter ./default' \
  "${call_log}" >/dev/null || fail 'default build must install only the default workspace'
if tar -tzf "${out_default}/${release_id}-default.tar.gz" | grep -F '/classic/' >/dev/null; then
  fail 'default-only archive contains classic theme'
fi
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
  "${BUILD_SCRIPT}" --legacy-pair \
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
grep -Fx 'FRONTEND_THEMES=default' <<<"${dry_run_output}" >/dev/null || fail 'dry-run did not select only default'
[[ ! -e ${dry_output} ]] || fail 'dry-run wrote output files'

for unsupported_production_theme in classic both; do
  if production_theme_output="$(
    "${BUILD_SCRIPT}" --legacy-pair --dry-run --theme "${unsupported_production_theme}" \
      --schema-contract bridge --api-contract 1 2>&1
  )"; then
    fail "production ${unsupported_production_theme} build unexpectedly succeeded"
  fi
  grep -F 'production frontend builds require the default theme' \
    <<<"${production_theme_output}" >/dev/null || fail 'production theme rejection was not explicit'
done

if invalid_output="$({
  "${BUILD_SCRIPT}" --legacy-pair --source-sha "${source_sha}" --schema-contract invalid --allow-dirty 2>&1
} 2>&1)"; then
  fail 'invalid schema contract unexpectedly succeeded'
fi
grep -F 'schema contract must be bridge or feature' <<<"${invalid_output}" >/dev/null ||
  fail 'invalid schema contract error was not explicit'

if missing_digest_output="$({
  "${BUILD_SCRIPT}" --legacy-pair \
    --dry-run \
    --source-root "${ROOT}" \
    --source-sha "${source_sha}" \
    --release-id frontend-production-missing-digest \
    --backend-source-sha "${source_sha}" \
    --backend-release-id backend-test-20260901-1 \
    --schema-contract bridge \
    --api-contract 1 \
    --output-dir "${test_root}/missing-digest" 2>&1
} 2>&1)"; then
  fail 'production build without a backend image digest unexpectedly succeeded'
fi
grep -F 'production frontend builds require --backend-image-digest' \
  <<<"${missing_digest_output}" >/dev/null ||
  fail 'missing backend image digest error was not explicit'

# The normal format builds without backend coordinates or a schema profile.
independent_id="${release_id}-independent"
PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
  KKAI_TEST_INSTALL_MARKER="${install_marker}" \
  "${BUILD_SCRIPT}" --release-id "${independent_id}" --allow-dirty \
  --output-dir "${test_root}/independent" --lock-dir "${lock_dir}" >/dev/null
jq -e --slurpfile contract "${ROOT}/web/default/console-contract.json" \
  '.format_version == 2 and .api_contract == $contract[0].api_contract and
  .required_capabilities == $contract[0].required_capabilities and
  (has("backend_release_id") | not) and (has("backend_source_sha") | not) and
  (has("backend_image_digest") | not) and (has("schema_contract") | not)' \
  "${test_root}/independent/${independent_id}.json" >/dev/null || fail 'independent artifact still binds a backend'
[[ ! -e "${test_root}/independent/frontend-releases/${independent_id}/release-pair.json" ]] ||
  fail 'independent artifact contains a legacy pair'

# A real, tiny Git fixture exercises immutable inputs without invoking Bun or
# changing the application's branch, index, source tree, or dependency cache.
fixture="${test_root}/source"
mkdir -p "${fixture}/web/default" "${fixture}/docs/kkai" "${fixture}/scripts/kkai"
cp "${ROOT}/LICENSE" "${ROOT}/NOTICE" "${ROOT}/THIRD-PARTY-LICENSES.md" "${fixture}/"
cp "${ROOT}/web/default/console-contract.json" "${fixture}/web/default/"
cp "${ROOT}/docs/kkai/upstream-baseline.env" "${fixture}/docs/kkai/"
cp "${ROOT}/scripts/kkai/frontend-build-release-package.sh" "${fixture}/scripts/kkai/"
cp "${ROOT}/scripts/kkai/release-record.sh" "${ROOT}/scripts/kkai/release-record.py" "${fixture}/scripts/kkai/"
printf 'committed-input\n' >"${fixture}/web/default/input.txt"
printf 'test-lock\n' >"${fixture}/web/bun.lock"
printf '.env\nnode_modules/\n' >"${fixture}/.gitignore"
git init --quiet "${fixture}"
git -C "${fixture}" symbolic-ref HEAD refs/heads/production/kkrich
git -C "${fixture}" add .
git -C "${fixture}" -c user.name=Test -c user.email=test@example.invalid commit --quiet -m fixture
fixture_sha="$(git -C "${fixture}" rev-parse HEAD)"
mkdir -p "${fixture}/web/node_modules"
printf 'must-not-reuse\n' >"${fixture}/web/node_modules/injected"
printf 'must-not-read\n' >"${fixture}/web/default/.env"

export KKAI_TEST_LOG="${call_log}" KKAI_TEST_INSTALL_MARKER="${install_marker}"
export KKAI_TEST_SOURCE_REPO="${fixture}" KKAI_TEST_SOURCE_SEEN="${test_root}/source-seen"
snapshot_output="${test_root}/snapshot"
PATH="${mock_bin}:${PATH}" "${BUILD_SCRIPT}" \
  --source-root "${fixture}" --release-id snapshot-test \
  --output-dir "${snapshot_output}" --lock-dir "${lock_dir}" >/dev/null
jq -e --arg sha "${fixture_sha}" '.source_sha == $sha and .format_version == 2' \
  "${snapshot_output}/snapshot-test.json" >/dev/null || fail 'snapshot provenance differs from source commit'
jq -e --arg version "${KKAI_TEST_PINNED_BUN_VERSION}" '.build.bun_version == $version' \
  "${snapshot_output}/frontend-releases/snapshot-test/frontend.json" >/dev/null || fail 'actual Bun version was not recorded'

# A spoofed metadata version must not override the actual production toolchain.
rm -f "${install_marker}"
if PATH="${mock_bin}:${PATH}" KKAI_TEST_BUN_VERSION=0.0.0 \
  KKAI_FRONTEND_BUN_VERSION="${KKAI_TEST_PINNED_BUN_VERSION}" \
  "${BUILD_SCRIPT}" --source-root "${fixture}" --release-id wrong-bun \
  --output-dir "${test_root}/wrong-bun" --lock-dir "${lock_dir}" \
  >"${test_root}/wrong-bun.log" 2>&1; then
  fail 'production build accepted an unpinned Bun version'
fi
grep -F 'Bun version mismatch' "${test_root}/wrong-bun.log" >/dev/null || fail 'Bun mismatch was not explicit'
[[ ! -e ${install_marker} ]] || fail 'Bun mismatch ran dependency installation'

# Install/build hooks may write only ignored outputs, even inside the snapshot.
for mutation in tracked untracked; do
  if PATH="${mock_bin}:${PATH}" KKAI_TEST_MUTATE_ARCHIVE="${mutation}" \
    "${BUILD_SCRIPT}" --source-root "${fixture}" --release-id "mutated-${mutation}" \
    --output-dir "${test_root}/mutated-${mutation}" --lock-dir "${lock_dir}" \
    >"${test_root}/mutated-${mutation}.log" 2>&1; then
    fail "frontend build accepted ${mutation} source mutation from an install hook"
  fi
  grep -E 'frontend build (modified|added untracked) archived source' \
    "${test_root}/mutated-${mutation}.log" >/dev/null || fail 'archive mutation rejection was not explicit'
  [[ ! -e "${test_root}/mutated-${mutation}/mutated-${mutation}.json" ]] || fail 'archive mutation left metadata'
done
[[ $(cat "${fixture}/web/bun.lock") == test-lock ]] || fail 'isolated source check changed the live checkout'

# Local opt-outs retain the existing checkout/dependency workflow, but record
# the actual executable version instead of a metadata-only override.
PATH="${mock_bin}:${PATH}" KKAI_TEST_BUN_VERSION=0.0.0 \
  "${BUILD_SCRIPT}" --source-root "${fixture}" --release-id local-toolchain \
  --allow-non-production --skip-install \
  --output-dir "${test_root}/local" --lock-dir "${lock_dir}" >/dev/null
jq -e '.build.bun_version == "0.0.0" and .build.install == "skipped"' \
  "${test_root}/local/frontend-releases/local-toolchain/frontend.json" >/dev/null || fail 'local toolchain opt-out did not preserve actual provenance'

# If local recording cannot initialize, stop before invoking Bun at all.
real_python3="$(command -v python3)"
cat >"${mock_bin}/python3" <<'EOF_PYTHON'
#!/usr/bin/env bash
if [[ ${2:-} == init ]]; then exit 73; fi
exec "${KKAI_TEST_REAL_PYTHON3}" "$@"
EOF_PYTHON
chmod 0755 "${mock_bin}/python3"
: >"${call_log}"
rm -f "${install_marker}"
if PATH="${mock_bin}:${PATH}" KKAI_TEST_REAL_PYTHON3="${real_python3}" \
  "${BUILD_SCRIPT}" --source-root "${fixture}" --release-id record-unavailable \
  --allow-dirty --output-dir "${test_root}/record-unavailable" --lock-dir "${lock_dir}" \
  >"${test_root}/record-unavailable.log" 2>&1; then
  fail 'frontend build continued after recording initialization failed'
fi
rm -f "${mock_bin}/python3"
[[ ! -s ${call_log} && ! -e ${install_marker} ]] || fail 'recording initialization failure still invoked Bun'
[[ ! -e ${lock_dir} ]] || fail 'recording initialization failure left the build lock'

# The live checkout can become a different clean commit during installation.
# Build inputs and metadata must stay at the original SHA without blocking
# unrelated work in the original checkout.
rm -f "${KKAI_TEST_SOURCE_SEEN}"
PATH="${mock_bin}:${PATH}" KKAI_TEST_DRIFT_SOURCE=1 \
  "${BUILD_SCRIPT}" --source-root "${fixture}" --release-id drift-test \
  --output-dir "${test_root}/drift" --lock-dir "${lock_dir}" \
  >"${test_root}/drift.log" 2>&1 || fail 'archived build was blocked by an unrelated source commit'
[[ $(cat "${KKAI_TEST_SOURCE_SEEN}") == committed-input ]] || fail 'build read the changed checkout instead of archived inputs'
[[ $(git -C "${fixture}" rev-parse HEAD) != "${fixture_sha}" ]] || fail 'concurrent source change fixture did not run'
jq -e --arg sha "${fixture_sha}" '.source_sha == $sha' \
  "${test_root}/drift/drift-test.json" >/dev/null || fail 'metadata followed the changed live HEAD'
[[ -s "${test_root}/drift/drift-test.tar.gz" ]] || fail 'archived build did not publish its artifact'
[[ ! -e ${lock_dir} ]] || fail 'source drift left the build lock'

echo 'Frontend artifact build regression tests passed.'
