#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly BUILD_SCRIPT="${ROOT}/scripts/kkai/build-manual-release.sh"
readonly HEAD_VERIFIER="${ROOT}/scripts/kkai/require-production-head.sh"
readonly CANONICAL_ORIGIN=https://github.com/Wangjunkai-1996/new-api-kkai.git

fail() {
  echo "build-manual-release test: $*" >&2
  exit 1
}

test_root="$(mktemp -d "${TMPDIR:-/tmp}/kkai-manual-build-test.XXXXXX")"
trap 'rm -rf -- "${test_root}"' EXIT
readonly test_root
readonly source_repo="${test_root}/source"
readonly remote_repo="${test_root}/origin.git"
readonly remote_writer="${test_root}/remote-writer"
readonly mock_bin="${test_root}/bin"
readonly docker_log="${test_root}/docker.log"
real_git="$(command -v git)"
readonly real_git
mkdir -p -- "${source_repo}/scripts/kkai" "${source_repo}/build/kkai-image" \
  "${source_repo}/web" "${mock_bin}"
cp -- "${BUILD_SCRIPT}" "${HEAD_VERIFIER}" "${source_repo}/scripts/kkai/"
chmod 0755 "${source_repo}/scripts/kkai/build-manual-release.sh" \
  "${source_repo}/scripts/kkai/require-production-head.sh"
printf 'FROM scratch\n' > "${source_repo}/build/kkai-image/Dockerfile"
printf 'go.work\nweb/.env.production.local\n' > "${source_repo}/.gitignore"
printf 'production snapshot\n' > "${source_repo}/snapshot-marker.txt"

"${real_git}" init --quiet --initial-branch=production/kkrich "${source_repo}"
"${real_git}" -C "${source_repo}" config user.name 'KKAI Release Test'
"${real_git}" -C "${source_repo}" config user.email 'release-test@example.invalid'
"${real_git}" -C "${source_repo}" add .
"${real_git}" -C "${source_repo}" commit --quiet -m 'test fixture'
base_sha="$("${real_git}" -C "${source_repo}" rev-parse HEAD)"
readonly base_sha
"${real_git}" init --quiet --bare "${remote_repo}"
"${real_git}" -C "${source_repo}" remote add origin "${remote_repo}"
"${real_git}" -C "${source_repo}" push --quiet --set-upstream origin production/kkrich
"${real_git}" --git-dir="${remote_repo}" symbolic-ref HEAD refs/heads/production/kkrich
"${real_git}" clone --quiet "${remote_repo}" "${remote_writer}"
"${real_git}" -C "${remote_writer}" config user.name 'KKAI Remote Test'
"${real_git}" -C "${remote_writer}" config user.email 'remote-test@example.invalid'

cat > "${mock_bin}/git" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ " $* " == *' remote get-url origin '* ]]; then
  printf '%s\n' "${KKAI_TEST_ORIGIN_URL}"
  exit 0
fi
exec "${KKAI_TEST_REAL_GIT}" "$@"
EOF

cat > "${mock_bin}/docker" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'docker %s\n' "$*" >> "${KKAI_TEST_DOCKER_LOG}"
archive=''
dockerfile=''
kkai_image_context=''
previous=''
for argument in "$@"; do
  case "${previous}" in
    --file) dockerfile=${argument} ;;
    --build-context) kkai_image_context=${argument} ;;
  esac
  case "${argument}" in
    type=docker,dest=*) archive=${argument#type=docker,dest=} ;;
  esac
  previous=${argument}
done
[[ -n "${archive}" ]] || exit 41
context=${!#}
if [[ -n "${KKAI_TEST_ASSERT_SNAPSHOT:-}" ]]; then
  [[ -d "${context}" && "${context}" != "${KKAI_TEST_SOURCE_REPO}" ]] || exit 42
  [[ "${dockerfile}" == "${context}/build/kkai-image/Dockerfile" ]] || exit 43
  [[ "${kkai_image_context}" == "kkai_image=${context}/build/kkai-image" ]] || exit 44
  [[ "$(<"${context}/snapshot-marker.txt")" == 'production snapshot' ]] || exit 45
  [[ ! -e "${context}/go.work" && ! -e "${context}/web/.env.production.local" ]] || exit 46
fi
printf 'immutable image archive\n' > "${archive}"
if [[ -n "${KKAI_TEST_MUTATE_SOURCE_REPO:-}" ]]; then
  printf 'changed during build\n' > "${KKAI_TEST_MUTATE_SOURCE_REPO}/snapshot-marker.txt"
fi
if [[ -n "${KKAI_TEST_ADVANCE_REMOTE_REPO:-}" ]]; then
  "${KKAI_TEST_REAL_GIT}" -C "${KKAI_TEST_ADVANCE_REMOTE_REPO}" fetch --quiet origin
  "${KKAI_TEST_REAL_GIT}" -C "${KKAI_TEST_ADVANCE_REMOTE_REPO}" checkout --quiet -B production/kkrich origin/production/kkrich
  printf 'remote advance\n' > "${KKAI_TEST_ADVANCE_REMOTE_REPO}/race.txt"
  "${KKAI_TEST_REAL_GIT}" -C "${KKAI_TEST_ADVANCE_REMOTE_REPO}" add race.txt
  "${KKAI_TEST_REAL_GIT}" -C "${KKAI_TEST_ADVANCE_REMOTE_REPO}" commit --quiet -m 'advance during build'
  "${KKAI_TEST_REAL_GIT}" -C "${KKAI_TEST_ADVANCE_REMOTE_REPO}" push --quiet origin production/kkrich
fi
EOF
chmod 0755 "${mock_bin}/git" "${mock_bin}/docker"

restore_base() {
  "${real_git}" -C "${source_repo}" checkout --quiet -B production/kkrich "${base_sha}"
  "${real_git}" -C "${source_repo}" push --quiet --force origin production/kkrich
  "${real_git}" -C "${source_repo}" fetch --quiet origin production/kkrich
  "${real_git}" -C "${source_repo}" branch --set-upstream-to=origin/production/kkrich production/kkrich >/dev/null
  "${real_git}" -C "${remote_writer}" fetch --quiet origin
  "${real_git}" -C "${remote_writer}" checkout --quiet -B production/kkrich origin/production/kkrich
  : > "${docker_log}"
}

advance_remote() {
  local marker=$1
  "${real_git}" -C "${remote_writer}" fetch --quiet origin
  "${real_git}" -C "${remote_writer}" checkout --quiet -B production/kkrich origin/production/kkrich
  printf '%s\n' "${marker}" > "${remote_writer}/${marker}.txt"
  "${real_git}" -C "${remote_writer}" add "${marker}.txt"
  "${real_git}" -C "${remote_writer}" commit --quiet -m "${marker}"
  "${real_git}" -C "${remote_writer}" push --quiet origin production/kkrich
}

run_build() {
  local output_dir=$1
  local origin_url=${2:-${CANONICAL_ORIGIN}}
  local advance_repo=${3:-}
  local assert_snapshot=${4:-}
  local mutate_source_repo=${5:-}
  local source_sha version
  source_sha="$("${real_git}" -C "${source_repo}" rev-parse HEAD)"
  version="kkai-prod-20260806.1-${source_sha:0:9}"
  PATH="${mock_bin}:${PATH}" \
    KKAI_TEST_REAL_GIT="${real_git}" \
    KKAI_TEST_ORIGIN_URL="${origin_url}" \
    KKAI_TEST_DOCKER_LOG="${docker_log}" \
    KKAI_TEST_ADVANCE_REMOTE_REPO="${advance_repo}" \
    KKAI_TEST_ASSERT_SNAPSHOT="${assert_snapshot}" \
    KKAI_TEST_SOURCE_REPO="${source_repo}" \
    KKAI_TEST_MUTATE_SOURCE_REPO="${mutate_source_repo}" \
    "${source_repo}/scripts/kkai/build-manual-release.sh" \
      --output-dir "${output_dir}" \
      --schema-contract feature \
      --version "${version}"
}

assert_build_rejected_before_docker() {
  local expected=$1
  local output_dir=$2
  local output
  if output="$(run_build "${output_dir}" 2>&1)"; then
    fail "build unexpectedly succeeded: ${expected}"
  fi
  grep -F "${expected}" <<< "${output}" >/dev/null ||
    fail "build failure did not report: ${expected}"
  [[ ! -s "${docker_log}" ]] || fail "rejected source started Docker"
}

test_attached_and_detached_production_head() {
  local attached_output="${test_root}/attached" detached_output="${test_root}/detached"
  local metadata
  restore_base
  run_build "${attached_output}" >/dev/null
  metadata="$(find "${attached_output}" -name '*.json' -type f)"
  [[ "$(jq -r '.source_repository' "${metadata}")" == github.com/Wangjunkai-1996/new-api-kkai ]] ||
    fail "metadata source repository is missing"
  [[ "$(jq -r '.source_ref' "${metadata}")" == refs/heads/production/kkrich ]] ||
    fail "metadata source ref is missing"
  [[ "$(jq -r '.source_sha' "${metadata}")" == "${base_sha}" ]] ||
    fail "metadata source SHA is wrong"

  restore_base
  "${real_git}" -C "${source_repo}" checkout --quiet --detach "${base_sha}"
  run_build "${detached_output}" >/dev/null
  [[ -n "$(find "${detached_output}" -name '*.json' -type f)" ]] ||
    fail "detached production HEAD did not produce metadata"
}

test_attached_identity_gates() {
  local output_dir="${test_root}/identity"
  restore_base
  "${real_git}" -C "${source_repo}" checkout --quiet -b release/test
  assert_build_rejected_before_docker 'attached production builds require branch production/kkrich' "${output_dir}"

  restore_base
  "${real_git}" -C "${source_repo}" push --quiet origin "${base_sha}:refs/heads/release/test"
  "${real_git}" -C "${source_repo}" fetch --quiet origin release/test:refs/remotes/origin/release/test
  "${real_git}" -C "${source_repo}" config branch.production/kkrich.merge refs/heads/release/test
  assert_build_rejected_before_docker 'production/kkrich must track origin/production/kkrich' "${output_dir}"

  restore_base
  local output
  if output="$(run_build "${output_dir}" https://github.com/example/not-new-api.git 2>&1)"; then
    fail "non-canonical origin unexpectedly built"
  fi
  grep -F 'origin is not the canonical' <<< "${output}" >/dev/null ||
    fail "non-canonical origin was not rejected explicitly"
  [[ ! -s "${docker_log}" ]] || fail "non-canonical origin started Docker"
}

test_ahead_behind_and_diverged_are_rejected() {
  local output_dir="${test_root}/divergence"
  restore_base
  printf 'local ahead\n' > "${source_repo}/ahead.txt"
  "${real_git}" -C "${source_repo}" add ahead.txt
  "${real_git}" -C "${source_repo}" commit --quiet -m 'local ahead'
  assert_build_rejected_before_docker 'source SHA is no longer the production branch HEAD' "${output_dir}"

  restore_base
  advance_remote behind
  assert_build_rejected_before_docker 'source SHA is no longer the production branch HEAD' "${output_dir}"

  restore_base
  advance_remote diverged-remote
  printf 'local diverged\n' > "${source_repo}/diverged-local.txt"
  "${real_git}" -C "${source_repo}" add diverged-local.txt
  "${real_git}" -C "${source_repo}" commit --quiet -m 'local diverged'
  assert_build_rejected_before_docker 'source SHA is no longer the production branch HEAD' "${output_dir}"
}

test_remote_advance_during_build_removes_outputs() {
  local output_dir="${test_root}/race" output
  restore_base
  if output="$(run_build "${output_dir}" "${CANONICAL_ORIGIN}" "${remote_writer}" 2>&1)"; then
    fail "build succeeded after production advanced during Docker build"
  fi
  grep -F 'source SHA is no longer the production branch HEAD' <<< "${output}" >/dev/null ||
    fail "post-build production advance was not reported"
  [[ -s "${docker_log}" ]] || fail "race test did not reach Docker"
  [[ -z "$(find "${output_dir}" -type f -print -quit 2>/dev/null)" ]] ||
    fail "failed race build left a deployable artifact"
}

test_build_uses_immutable_committed_snapshot() {
  local output_dir="${test_root}/snapshot"
  restore_base
  printf 'ignored workspace\n' > "${source_repo}/go.work"
  printf 'VITE_UNCOMMITTED_VALUE=ignored\n' > "${source_repo}/web/.env.production.local"
  run_build "${output_dir}" "${CANONICAL_ORIGIN}" '' assert-snapshot >/dev/null
  [[ -n "$(find "${output_dir}" -name '*.json' -type f)" ]] ||
    fail "immutable production snapshot did not produce metadata"
}

test_worktree_change_during_build_removes_outputs() {
  local output_dir="${test_root}/worktree-race" output
  restore_base
  if output="$(run_build "${output_dir}" "${CANONICAL_ORIGIN}" '' assert-snapshot "${source_repo}" 2>&1)"; then
    fail "build succeeded after the worktree changed during Docker build"
  fi
  grep -F 'production builds require a clean worktree' <<< "${output}" >/dev/null ||
    fail "worktree race was not reported explicitly"
  [[ -s "${docker_log}" ]] || fail "worktree race test did not reach Docker"
  [[ -z "$(find "${output_dir}" -type f -print -quit 2>/dev/null)" ]] ||
    fail "failed worktree race left a deployable artifact"
}

test_attached_and_detached_production_head
test_attached_identity_gates
test_ahead_behind_and_diverged_are_rejected
test_remote_advance_during_build_removes_outputs
test_build_uses_immutable_committed_snapshot
test_worktree_change_during_build_removes_outputs

echo 'New API manual build provenance tests passed'
