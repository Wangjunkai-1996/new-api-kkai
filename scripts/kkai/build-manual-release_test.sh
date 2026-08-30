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
mkdir -p -- "${mock_bin}" "${build_tmp}"

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
    ;;
  *'rev-parse HEAD')
    printf '1111111111111111111111111111111111111111\n'
    ;;
  *'cat-file -e '* | *'archive --format=tar '*)
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
  for argument in "$@"; do
    if [[ ${argument} == type=docker,dest=* ]]; then
      destination=${argument#type=docker,dest=}
    fi
  done
  [[ -n ${destination} ]] || {
    echo 'mock build did not receive an archive destination' >&2
    exit 93
  }
  printf 'mock image archive\n' >"${destination}"
  exit 0
fi

echo "unexpected docker invocation: $*" >&2
exit 94
EOF
chmod 0755 "${mock_bin}/git" "${mock_bin}/docker"

run_build() {
  local mode=$1
  local output_name=${2:-${mode}}
  local output_dir="${test_root}/out-${output_name}"
  local output

  mkdir -p -- "${output_dir}"
  : >"${call_log}"
  if ! output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE="${mode}" \
      TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" \
        --schema-contract bridge \
        --version "${version}" \
        --output-dir "${output_dir}" 2>&1
  )"; then
    fail "expected ${mode} endpoint to build successfully\n${output}"
  fi
  [[ -s "${output_dir}/${version}.tar" ]] || fail "${mode} build did not write an archive"
  [[ -s "${output_dir}/${version}.json" ]] || fail "${mode} build did not write metadata"
  printf '%s\n' "${output}"
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
}

expect_no_resource_limits() {
  local output_dir="${test_root}/out-no-resource" output

  mkdir -p -- "${output_dir}"
  : >"${call_log}"
  if ! output="$(
    PATH="${mock_bin}:${PATH}" \
      KKAI_TEST_LOG="${call_log}" \
      KKAI_TEST_ENDPOINT_MODE=direct-unix \
      TMPDIR="${build_tmp}" \
      "${BUILD_SCRIPT}" \
        --schema-contract bridge \
        --version "${version}" \
        --output-dir "${output_dir}" \
        --no-resource-limits 2>&1
  )"; then
    fail "--no-resource-limits build unexpectedly failed\n${output}"
  fi
  ! grep -F -- ' --resource ' "${call_log}" >/dev/null ||
    fail '--no-resource-limits still passed a Buildx resource flag'
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
        --schema-contract bridge \
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
        --schema-contract bridge \
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
        --schema-contract bridge \
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

expect_direct_uri direct-unix
expect_direct_uri direct-npipe
expect_context_name
expect_build_defaults
expect_no_resource_limits
expect_invalid_parallelism_rejected
expect_build_lock_rejected
expect_remote_rejected
echo 'Manual build endpoint regression tests passed.'
