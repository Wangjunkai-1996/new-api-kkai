#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly BUILD_SCRIPT="${ROOT}/scripts/kkai/build-manual-release.sh"

fail() {
  echo "build-manual-release test: $*" >&2
  exit 1
}

test_root="$(mktemp -d "${TMPDIR:-/tmp}/kkai-manual-build-test.XXXXXX")"
trap 'rm -rf -- "${test_root}"' EXIT
readonly test_root
readonly mock_bin="${test_root}/bin"
readonly call_log="${test_root}/calls.log"
readonly build_tmp="${test_root}/tmp"
readonly source_fixture="${test_root}/committed-source"
mkdir -p -- "${mock_bin}" "${build_tmp}" "${source_fixture}/common" "${source_fixture}/build/kkai-image" "${source_fixture}/scripts/kkai"
printf 'committed application\n' >"${source_fixture}/main.go"
printf 'committed image recipe\n' >"${source_fixture}/build/kkai-image/Dockerfile"
printf 'committed image tools\n' >"${source_fixture}/build/kkai-image/go.mod"
printf 'committed release preparation\n' >"${source_fixture}/scripts/kkai/prepare-maintenance-release.py"
printf '{"feature":{"format_version":1,"api_contracts":[2],"capabilities":["committed_feature"]},"bridge":{"format_version":1,"api_contracts":[2],"capabilities":["committed_bridge"]}}\n' \
  >"${source_fixture}/common/console_contract.json"
export KKAI_TEST_SOURCE_FIXTURE="${source_fixture}"
export KKAI_TEST_SOURCE_STATE="${test_root}/source-state"
export KKAI_TEST_GIT_DIR="${test_root}/object-db"
KKAI_TEST_REAL_PYTHON="$(command -v python3)"
export KKAI_TEST_REAL_PYTHON

readonly version=kkai-prod-20260830.1-111111111

cat >"${mock_bin}/git" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'git %s\n' "$*" >>"${KKAI_TEST_LOG}"
case "$*" in
  *'branch --show-current')
    printf 'production/kkrich\n'
    ;;
  *'status --porcelain=v1 --untracked-files=all')
    if [[ ${KKAI_TEST_DIRTY:-false} != false || -e ${KKAI_TEST_SOURCE_STATE} ]]; then
      printf ' M main.go\n'
    fi
    ;;
  *'rev-parse HEAD')
    if [[ -e ${KKAI_TEST_SOURCE_STATE} ]]; then
      printf '2222222222222222222222222222222222222222\n'
    else
      printf '1111111111111111111111111111111111111111\n'
    fi
    ;;
  *'rev-parse --absolute-git-dir')
    printf '%s\n' "${KKAI_TEST_GIT_DIR}"
    ;;
  *'cat-file -e '*)
    ;;
  *'archive --format=tar '*)
    [[ ${*: -1} == 1111111111111111111111111111111111111111 ]] || exit 89
    [[ ${KKAI_TEST_ARCHIVE_FAIL:-false} == false ]] || exit 88
    tar -cf - -C "${KKAI_TEST_SOURCE_FIXTURE}" .
    if [[ ${KKAI_TEST_CHANGE_CHECKOUT:-false} == true ]]; then
      touch "${KKAI_TEST_SOURCE_STATE}"
    fi
    ;;
  *)
    echo "unexpected git invocation: $*" >&2
    exit 90
    ;;
esac
EOF

cat >"${mock_bin}/docker" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'docker %s\n' "$*" >>"${KKAI_TEST_LOG}"

if [[ ${1:-} == buildx && ${2:-} == inspect ]]; then
  case "${KKAI_TEST_ENDPOINT_MODE}" in
    direct-unix)
      endpoint='unix:///tmp/kkai-docker.sock'
      ;;
    direct-npipe)
      endpoint='npipe:////./pipe/docker_engine'
      ;;
    context)
      endpoint='desktop-linux'
      ;;
    remote)
      endpoint='tcp://builder.example:2376'
      ;;
    *)
      echo "unknown endpoint mode" >&2
      exit 91
      ;;
  esac
  cat <<EOF_INSPECT
Name:          kkai-mirror-builder
Driver:        docker-container
Endpoint:      ${endpoint}
Platforms:     linux/amd64, linux/arm64
EOF_INSPECT
  exit 0
fi

if [[ ${1:-} == context && ${2:-} == inspect ]]; then
  if [[ ${KKAI_TEST_ENDPOINT_MODE} == direct-unix || ${KKAI_TEST_ENDPOINT_MODE} == direct-npipe ]]; then
    echo 'direct URI must not be sent to docker context inspect' >&2
    exit 92
  fi
  if [[ ${KKAI_TEST_ENDPOINT_MODE} == remote ]]; then
    printf 'tcp://builder.example:2376\n'
  else
    printf 'unix:///Users/test/.docker/run/docker.sock\n'
  fi
  exit 0
fi

if [[ ${1:-} == buildx && ${2:-} == build && ${3:-} == --help ]]; then
  printf '%s\n' '--resource stringArray'
  exit 0
fi

if [[ ${1:-} == buildx && ${2:-} == build ]]; then
  destination=''
  context=${*: -1}
  [[ $(<"${context}/main.go") == 'committed application' ]] || exit 96
  [[ $(<"${context}/build/kkai-image/Dockerfile") == 'committed image recipe' ]] || exit 96
  [[ $(<"${context}/build/kkai-image/go.mod") == 'committed image tools' ]] || exit 96
  [[ ! -e ${context}/.git && ! -e ${context}/.local-releases && ! -e ${context}/web/node_modules ]] || exit 96
  [[ " $* " == *" --file ${context}/build/kkai-image/Dockerfile "* ]] || exit 96
  [[ " $* " == *" --build-context kkai_image=${context}/build/kkai-image "* ]] || exit 96
  [[ " $* " == *'"capabilities":["committed_'* ]] || exit 96
  for argument in "$@"; do
    if [[ ${argument} == type=docker,dest=* ]]; then
      destination=${argument#type=docker,dest=}
    fi
  done
  [[ -n ${destination} ]] || {
    echo 'mock build did not receive an archive destination' >&2
    exit 93
  }
  if [[ ${KKAI_TEST_BUILD_FAIL:-false} == true ]]; then
    printf 'partial archive\n' >"${destination}"
    exit 97
  fi
  printf 'mock image archive\n' >"${destination}"
  exit 0
fi

echo "unexpected docker invocation: $*" >&2
exit 94
EOF
chmod 0755 "${mock_bin}/git" "${mock_bin}/docker"

cat >"${mock_bin}/python3" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ ${1:-} == */release-record.py ]]; then
  [[ ${KKAI_TEST_RECORD_FAIL:-false} == false ]] || exit 98
  exec "${KKAI_TEST_REAL_PYTHON}" "$@"
fi
printf 'python3 %s\n' "$*" >>"${KKAI_TEST_LOG}"
[[ $1 == */prepare-maintenance-release.py && $2 == create ]] || exit 95
[[ $1 == "${GIT_WORK_TREE}/scripts/kkai/prepare-maintenance-release.py" && ${GIT_DIR} == "${KKAI_TEST_GIT_DIR}" ]] || exit 95
[[ $(<"$1") == 'committed release preparation' ]] || exit 95
EOF
chmod 0755 "${mock_bin}/python3"

run_build() {
  local mode=$1
  local output_name=${2:-${mode}}
  local frontend_mode=${3:-embedded}
  local output_dir="${test_root}/out-${output_name}"
  local output record_path
  if (( $# >= 3 )); then shift 3; else set --; fi

  mkdir -p -- "${output_dir}"
  : >"${call_log}"
  if ! output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE="${mode}" \
      TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" \
        --schema-contract feature \
        --frontend-mode "${frontend_mode}" \
        --version "${version}" \
        --output-dir "${output_dir}" "$@" 2>&1
  )"; then
    fail "expected ${mode} endpoint to build successfully\n${output}"
  fi
  [[ -s "${output_dir}/${version}.tar" ]] || fail "${mode} build did not write an archive"
  [[ -s "${output_dir}/${version}.json" ]] || fail "${mode} build did not write metadata"
  [[ "$(jq --raw-output .frontend_mode "${output_dir}/${version}.json")" == "${frontend_mode}" ]] ||
    fail "${mode} build metadata did not record frontend mode ${frontend_mode}"
  jq -e '.source_sha == "1111111111111111111111111111111111111111" and .console_contract.capabilities == ["committed_feature"]' \
    "${output_dir}/${version}.json" >/dev/null || fail 'build metadata does not match the committed source'
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e --arg archive_sha "$(jq -r .archive_sha256 "${output_dir}/${version}.json")" \
    '.kind == "backend-build" and .result == "passed" and .exit_code == 0 and
     .source_sha == "1111111111111111111111111111111111111111" and
     .identity.archive_sha256 == $archive_sha and (.identity.archive_bytes | tonumber) > 0 and
     [.phases[].name] == ["source-export", "builder-preflight", "buildkit", "archive-metadata"] and
     all(.phases[]; .result == "passed" and .duration_seconds >= 0)' \
    "${record_path}" >/dev/null || fail 'successful build record is incomplete or does not match the artifact'
  [[ -z $(find "${build_tmp}" -mindepth 1 -maxdepth 1 -print -quit) ]] ||
    fail 'successful build left its source snapshot or lock behind'
  printf '%s\n' "${output}"
}

expect_checkout_changes_isolated() {
  KKAI_TEST_CHANGE_CHECKOUT=true run_build direct-unix checkout-changed external >/dev/null
  [[ -e ${KKAI_TEST_SOURCE_STATE} ]] || fail 'checkout change scenario did not run'
  rm -- "${KKAI_TEST_SOURCE_STATE}"
}

expect_failed_build_cleanup() {
  local phase=$1 output status record_path expected_phase=source-export
  : >"${call_log}"
  if output="$(
    PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=direct-unix TMPDIR="${build_tmp}" \
      KKAI_TEST_ARCHIVE_FAIL="$([[ ${phase} == export ]] && printf true || printf false)" \
      KKAI_TEST_BUILD_FAIL="$([[ ${phase} == build ]] && printf true || printf false)" \
      "${BUILD_SCRIPT}" --schema-contract feature --frontend-mode external \
      --version "${version}" --output-dir "${test_root}/out-failed-${phase}" 2>&1
  )"; then
    fail "${phase} failure unexpectedly succeeded"
  else
    status=$?
  fi
  [[ ! -e ${test_root}/out-failed-${phase}/${version}.json ]] || fail 'failed build emitted accepted metadata'
  [[ -z $(find "${build_tmp}" -mindepth 1 -maxdepth 1 -print -quit) ]] ||
    fail "${phase} failure left its source snapshot or lock behind"
  if [[ ${phase} == export ]]; then
    grep -F 'source tree cannot be archived' <<<"${output}" >/dev/null || fail 'archive failure was not reported'
    ! grep -q '^docker ' "${call_log}" || fail 'failed source export reached Docker'
  else
    [[ ${status} == 97 ]] || fail 'record cleanup masked the original build failure'
    expected_phase=buildkit
  fi
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e --arg phase "${expected_phase}" --argjson status "${status}" \
    '.result == "failed" and .exit_code == $status and
     .phases[-1].name == $phase and .phases[-1].result == "failed" and .phases[-1].exit_code == $status' \
    "${record_path}" >/dev/null || fail 'failure record lost the failing phase or exit code'
}

expect_record_failure_stops_build() {
  local output
  : >"${call_log}"
  if output="$(
    PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_RECORD_FAIL=true KKAI_TEST_ENDPOINT_MODE=direct-unix TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" --schema-contract feature --version "${version}" \
      --output-dir "${test_root}/out-record-failure" 2>&1
  )"; then
    fail "build proceeded without its required release record\n${output}"
  fi
  ! grep -q '^docker ' "${call_log}" || fail 'release record failure reached Docker'
  [[ -z $(find "${build_tmp}" -mindepth 1 -maxdepth 1 -print -quit) ]] ||
    fail 'record failure left its build lock behind'
}

expect_resource_profiles() {
  local output record_path
  output="$(run_build direct-unix balanced external --resource-profile balanced)"
  grep -F -- '--resource cpu-quota=400000 --resource memory=4g' "${call_log}" >/dev/null || fail 'balanced profile was not applied'
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e '.identity.profile == "balanced" and (.identity.cpu_quota | tonumber) == 400000 and .identity.memory_limit == "4g"' \
    "${record_path}" >/dev/null || fail 'balanced resource identity was not recorded'

  run_build direct-unix overrides-before external --cpu-quota 300000 --memory 3500m --resource-profile balanced >/dev/null
  grep -F -- '--resource cpu-quota=300000 --resource memory=3500m' "${call_log}" >/dev/null || fail 'profile overwrote preceding explicit resources'
  KKAI_BUILDX_CPU_QUOTA=250000 KKAI_BUILDX_MEMORY=2500m \
    run_build direct-unix overrides-after external --resource-profile balanced --resource cpu-quota=300000 --resource memory=3500m >/dev/null
  grep -F -- '--resource cpu-quota=300000 --resource memory=3500m' "${call_log}" >/dev/null || fail 'environment overwrote explicit resources'
  KKAI_BUILDX_CPU_QUOTA=250000 KKAI_BUILDX_MEMORY=2500m \
    run_build direct-unix environment-resources external --resource-profile balanced >/dev/null
  grep -F -- '--resource cpu-quota=250000 --resource memory=2500m' "${call_log}" >/dev/null || fail 'profile overwrote environment resource overrides'
}

expect_direct_uri() {
  local mode=$1
  run_build "${mode}" >/dev/null
  ! grep -q '^docker context inspect ' "${call_log}" ||
    fail "${mode} endpoint was incorrectly resolved as a Docker context"
}

expect_context_name() {
  run_build context >/dev/null
  grep -q '^docker context inspect desktop-linux ' "${call_log}" ||
    fail 'context endpoint was not resolved through docker context inspect'
}

expect_build_defaults() {
  run_build direct-unix defaults >/dev/null
  grep -F -- 'docker buildx build --builder kkai-mirror-builder --resource cpu-quota=200000 --resource memory=3g' "${call_log}" >/dev/null ||
    fail 'default Buildx resource limits were not applied'
  grep -F -- '--build-arg GO_BUILD_PARALLELISM=4' "${call_log}" >/dev/null ||
    fail 'default Go build parallelism was not forwarded'
  grep -F -- '--build-arg MEDIA_BUILD_PARALLELISM=2' "${call_log}" >/dev/null ||
    fail 'default media build parallelism was not forwarded'
  grep -F -- '--target runtime-embedded' "${call_log}" >/dev/null ||
    fail 'default build did not select the embedded runtime target'
  grep -F -- '--build-arg KKAI_FRONTEND_MODE=embedded' "${call_log}" >/dev/null ||
    fail 'default embedded frontend mode was not forwarded'
}

expect_external_frontend_mode() {
  run_build direct-unix external external >/dev/null
  grep -F -- '--target runtime-external' "${call_log}" >/dev/null ||
    fail 'external build did not select the external runtime target'
  grep -F -- '--build-arg KKAI_FRONTEND_MODE=external' "${call_log}" >/dev/null ||
    fail 'external frontend mode was not forwarded'
}

expect_invalid_frontend_mode_rejected() {
  local output

  : >"${call_log}"
  if output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=direct-unix \
      TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" \
        --schema-contract feature \
        --frontend-mode remote \
        --version "${version}" \
        --output-dir "${test_root}/out-invalid-frontend" 2>&1
  )"; then
    fail 'invalid frontend mode unexpectedly succeeded'
  fi
  grep -F 'frontend mode must be embedded or external' <<<"${output}" >/dev/null ||
    fail 'invalid frontend mode was not rejected explicitly'
  [[ ! -s "${call_log}" ]] || fail 'invalid frontend mode reached an external command'
}

expect_no_resource_limits() {
  local output_dir="${test_root}/out-no-resource" output record_path

  mkdir -p -- "${output_dir}"
  : >"${call_log}"
  if ! output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=direct-unix \
      TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" \
        --schema-contract feature \
        --version "${version}" \
        --output-dir "${output_dir}" \
        --no-resource-limits --resource-profile balanced 2>&1
  )"; then
    fail "--no-resource-limits build unexpectedly failed\n${output}"
  fi
  ! grep -F -- ' --resource ' "${call_log}" >/dev/null ||
    fail '--no-resource-limits still passed a Buildx resource flag'
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e '.identity.cpu_quota == "builder-default" and .identity.memory_limit == "builder-default" and .result == "passed"' \
    "${record_path}" >/dev/null || fail 'disabled resource limits were not recorded accurately'
}

expect_invalid_parallelism_rejected() {
  local output

  : >"${call_log}"
  if output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=direct-unix \
      TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" \
        --schema-contract feature \
        --version "${version}" \
        --output-dir "${test_root}/out-invalid" \
        --go-build-parallelism 0 2>&1
  )"; then
    fail 'invalid Go build parallelism unexpectedly succeeded'
  fi
  grep -F 'Go build parallelism must be an integer from 1 to 64' <<<"${output}" >/dev/null ||
    fail 'invalid Go build parallelism was not rejected explicitly'
  [[ ! -s "${call_log}" ]] || fail 'invalid parallelism reached an external command'
}

expect_build_lock_rejected() {
  local output

  mkdir -m 700 -- "${build_tmp}/kkai-newapi-manual-build.lock"
  printf '%s\n' "$$" >"${build_tmp}/kkai-newapi-manual-build.lock/pid"
  if output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=direct-unix \
      TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" \
        --schema-contract feature \
        --version "${version}" \
        --output-dir "${test_root}/out-locked" 2>&1
  )"; then
    fail 'a live local build lock was unexpectedly ignored'
  fi
  grep -F 'another local New API build is running' <<<"${output}" >/dev/null ||
    fail 'live local build lock was not reported explicitly'
  rm -f -- "${build_tmp}/kkai-newapi-manual-build.lock/pid"
  rmdir -- "${build_tmp}/kkai-newapi-manual-build.lock"
}

expect_remote_rejected() {
  local output

  : >"${call_log}"
  if output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=remote \
      TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" \
        --schema-contract feature \
        --version "${version}" \
        --output-dir "${test_root}/out-remote" 2>&1
  )"; then
    fail 'remote endpoint unexpectedly passed local-builder validation'
  fi
  grep -F 'must use a local Docker context' <<<"${output}" >/dev/null ||
    fail "remote endpoint failure was not explicit\n${output}"
  ! grep -F 'docker buildx inspect --bootstrap ' "${call_log}" >/dev/null ||
    fail 'remote endpoint was bootstrapped before local endpoint validation'
}

expect_maintenance_preparation() {
  local protocol=$1
  local output_dir="${test_root}/out-${protocol}" output
  local planned_sha=3333333333333333333333333333333333333333
  local profile=feature flag=--prepare-maintenance purpose=maintenance-preparation
  if [[ ${protocol} == tenant-online-v1 ]]; then
    profile=bridge flag=--prepare-online purpose=tenant-online-preparation
  fi

  : >"${call_log}"
  if ! output="$(
    PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=direct-unix TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" --schema-contract "${profile}" --frontend-mode external \
      "${flag}" --planned-infra-sha "${planned_sha}" \
      --planned-deployment-protocol "${protocol}" \
      --version "${version}" --output-dir "${output_dir}" 2>&1
  )"; then
    fail "maintenance preparation failed\n${output}"
  fi
  output_dir="$(cd -- "${output_dir}" && pwd)"
  jq -e --arg purpose "${purpose}" --arg profile "${profile}" \
    '.release_purpose == $purpose and .schema_contract == $profile and .frontend_mode == "external"' \
    "${output_dir}/${version}.json" >/dev/null || fail 'maintenance metadata is not marked'
  grep -F -- "prepare-maintenance-release.py create --metadata ${output_dir}/${version}.json --planned-infra-sha ${planned_sha} --planned-deployment-protocol ${protocol}" \
    "${call_log}" >/dev/null || fail 'maintenance plan did not receive exact release coordinates'
  [[ $(grep -c '^docker buildx build --builder ' "${call_log}") == 1 ]] ||
    fail 'maintenance preparation did not build exactly one backend image'
  local record_path
  record_path="$(sed -n 's/^RELEASE_RECORD=//p' <<<"${output}")"
  jq -e '.result == "passed" and .phases[-1].name == "maintenance" and .phases[-1].result == "passed"' \
    "${record_path}" >/dev/null || fail 'maintenance phase was not recorded'
}

expect_maintenance_rejected() {
  local reason=$1 output
  shift
  : >"${call_log}"
  if output="$(
    PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=direct-unix TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" --schema-contract feature --frontend-mode external \
      --version "${version}" --output-dir "${test_root}/out-rejected" "$@" 2>&1
  )"; then
    fail "invalid maintenance preparation succeeded: ${reason}"
  fi
  grep -F "${reason}" <<<"${output}" >/dev/null ||
    fail "maintenance preparation failed for the wrong reason\n${output}"
  ! grep -Eq '^docker |^python3 ' "${call_log}" || fail 'invalid maintenance preparation reached build or artifact inspection'
}

expect_bridge_build() {
  local output output_dir="${test_root}/out-bridge"
  : >"${call_log}"
  if ! output="$(
    PATH="${mock_bin}:${PATH}" KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=direct-unix TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" --schema-contract bridge --frontend-mode external \
      --version "${version}" --output-dir "${output_dir}" 2>&1
  )"; then
    fail "schema9-to10 bridge build failed\n${output}"
  fi
  jq -e '.schema_contract == "bridge" and .frontend_mode == "external"' \
    "${output_dir}/${version}.json" >/dev/null || fail 'bridge metadata is not preserved'
  grep -F -- '--build-arg KKAI_SCHEMA_CONTRACT=bridge' "${call_log}" >/dev/null ||
    fail 'bridge profile did not reach the backend build'
}

expect_direct_uri direct-unix
expect_direct_uri direct-npipe
expect_context_name
expect_build_defaults
expect_resource_profiles
expect_external_frontend_mode
expect_checkout_changes_isolated
expect_failed_build_cleanup export
expect_failed_build_cleanup build
expect_record_failure_stops_build
expect_invalid_frontend_mode_rejected
expect_no_resource_limits
expect_invalid_parallelism_rejected
expect_build_lock_rejected
expect_remote_rejected
expect_maintenance_preparation rc41-maintenance-v1
expect_maintenance_preparation tenant-maintenance-v1
expect_maintenance_preparation tenant-maintenance-v2
expect_maintenance_preparation tenant-online-v1
expect_bridge_build
expect_maintenance_rejected 'requires planned infrastructure SHA' --prepare-maintenance
expect_maintenance_rejected 'requires planned deployment protocol' --prepare-maintenance \
  --planned-infra-sha 3333333333333333333333333333333333333333
expect_maintenance_rejected 'requires feature schema and external frontend' --prepare-maintenance --schema-contract bridge
expect_maintenance_rejected 'requires feature schema and external frontend' --prepare-maintenance --frontend-mode embedded
expect_maintenance_rejected 'online preparation requires bridge schema' --prepare-online
expect_maintenance_rejected 'online preparation requires exact planned infrastructure SHA' --prepare-online --schema-contract bridge
expect_maintenance_rejected 'online preparation requires bridge schema' --prepare-online --prepare-maintenance --schema-contract bridge
expect_maintenance_rejected 'require --prepare-maintenance' --planned-infra-sha 3333333333333333333333333333333333333333
expect_maintenance_rejected 'resource profile must be conservative or balanced' --resource-profile unbounded
KKAI_TEST_DIRTY=true expect_maintenance_rejected 'production builds require a clean worktree' \
  --prepare-maintenance --planned-infra-sha 3333333333333333333333333333333333333333 \
  --planned-deployment-protocol rc41-maintenance-v1
KKAI_TEST_DIRTY=true expect_maintenance_rejected 'production builds require a clean worktree'
echo 'Manual build endpoint regression tests passed.'
