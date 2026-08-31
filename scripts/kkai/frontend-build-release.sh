#!/usr/bin/env bash
# Build an immutable, independently deployable frontend artifact.
#
# This script only builds and packages files. It never changes a live pointer,
# contacts a server, or invokes a deployment controller.
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
export GIT_NO_LAZY_FETCH=1

die() {
  echo "frontend-build-release: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF_USAGE'
Usage: frontend-build-release.sh [options]

Build default and classic frontend bundles into an immutable archive.

Required:
  --schema-contract bridge|feature
  --api-contract NUMBER             Integer API compatibility contract

Common options:
  --output-dir DIR                 Artifact output directory
  --release-id ID                  Stable release identifier
  --source-sha SHA                 Expected 40-character source commit SHA
  --backend-source-sha SHA         Backend commit paired with this frontend
  --backend-release-id ID          Backend release identifier
  --backend-image-digest DIGEST    Optional sha256:<64 hex> image digest
  --theme default|classic|both      Bundle selection (default: both)
  --build-timestamp RFC3339         Metadata timestamp (UTC, ending in Z)
  --source-root DIR                 Application checkout to build
  --bun-bin PATH                    Bun executable (or test double)
  --git-bin PATH                    Git executable (or test double)
  --jq-bin PATH                     jq executable (or test double)
  --tar-bin PATH                    tar executable (or test double)
  --lock-dir DIR                    Local build lock directory
  --skip-install                    Skip the frozen Bun install (local tests only)
  --dry-run                         Print the plan without building or writing
  --allow-non-production             Permit a branch other than production/kkrich
  --allow-dirty                     Permit a dirty source checkout (local only)
  -h, --help                        Show this help

Environment equivalents use the KKAI_FRONTEND_* prefix, for example
KKAI_FRONTEND_BUN_BIN and KKAI_FRONTEND_OUTPUT_DIR. The build command is
`bun run build -- --dist-path <temporary-directory>` for each selected theme.
EOF_USAGE
}

sha256_file() {
  local file=$1
  local digest

  if [[ -n "${sha256_bin}" ]]; then
    digest="$("${sha256_bin}" "${file}" | awk '{print $1}')"
  elif command -v sha256sum >/dev/null 2>&1; then
    digest="$(sha256sum "${file}" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    digest="$(shasum -a 256 "${file}" | awk '{print $1}')"
  else
    die "missing sha256sum, shasum, or KKAI_FRONTEND_SHA256_BIN"
  fi

  [[ "${digest}" =~ ^[0-9a-fA-F]{64}$ ]] ||
    die "invalid SHA-256 output for ${file}"
  printf '%s\n' "${digest}" | LC_ALL=C tr '[:upper:]' '[:lower:]'
}

require_command() {
  local command_name=$1
  command -v "${command_name}" >/dev/null 2>&1 || die "missing ${command_name}"
}

validate_sha() {
  local label=$1
  local value=$2
  [[ "${value}" =~ ^[0-9a-f]{40}$ ]] || die "invalid ${label}"
}

validate_release_id() {
  local label=$1
  local value=$2
  [[ "${value}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] ||
    die "invalid ${label}: use 1-128 letters, digits, '.', '_' or '-'"
}

source_root="${KKAI_FRONTEND_SOURCE_ROOT:-${ROOT}}"
output_dir="${KKAI_FRONTEND_OUTPUT_DIR:-${ROOT}/.local-releases/frontend}"
release_id="${KKAI_FRONTEND_RELEASE_ID:-}"
source_sha_arg="${KKAI_FRONTEND_SOURCE_SHA:-}"
backend_source_sha="${KKAI_FRONTEND_BACKEND_SOURCE_SHA:-}"
backend_release_id="${KKAI_FRONTEND_BACKEND_RELEASE_ID:-}"
backend_image_digest="${KKAI_FRONTEND_BACKEND_IMAGE_DIGEST:-}"
schema_contract="${KKAI_FRONTEND_SCHEMA_CONTRACT:-}"
api_contract="${KKAI_FRONTEND_API_CONTRACT:-}"
theme_selection="${KKAI_FRONTEND_THEME:-both}"
build_timestamp="${KKAI_FRONTEND_BUILD_TIMESTAMP:-}"
bun_bin="${KKAI_FRONTEND_BUN_BIN:-bun}"
git_bin="${KKAI_FRONTEND_GIT_BIN:-git}"
jq_bin="${KKAI_FRONTEND_JQ_BIN:-jq}"
tar_bin="${KKAI_FRONTEND_TAR_BIN:-tar}"
sha256_bin="${KKAI_FRONTEND_SHA256_BIN:-}"
lock_dir="${KKAI_FRONTEND_LOCK_DIR:-${TMPDIR:-/tmp}/kkai-newapi-frontend-build.lock}"
install_deps=1
case "${KKAI_FRONTEND_SKIP_INSTALL:-}" in
  1|true|TRUE|yes|YES) install_deps=0 ;;
  0|false|FALSE|no|NO) install_deps=1 ;;
  '') ;;
  *) die "KKAI_FRONTEND_SKIP_INSTALL must be 1/0, true/false, or yes/no" ;;
esac
dry_run=0
allow_nonproduction=0
allow_dirty=0

while (( $# > 0 )); do
  case "$1" in
    --dry-run)
      dry_run=1
      shift
      ;;
    --allow-non-production)
      allow_nonproduction=1
      shift
      ;;
    --allow-dirty)
      allow_dirty=1
      shift
      ;;
    --skip-install)
      install_deps=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --output-dir|--release-id|--source-sha|--backend-source-sha|\
    --backend-release-id|--backend-image-digest|--schema-contract|\
    --api-contract|--theme|--build-timestamp|--source-root|--bun-bin|\
    --git-bin|--jq-bin|--tar-bin|--lock-dir)
      [[ $# -ge 2 ]] || die "missing value for $1"
      case "$1" in
        --output-dir) output_dir=$2 ;;
        --release-id) release_id=$2 ;;
        --source-sha) source_sha_arg=$2 ;;
        --backend-source-sha) backend_source_sha=$2 ;;
        --backend-release-id) backend_release_id=$2 ;;
        --backend-image-digest) backend_image_digest=$2 ;;
        --schema-contract) schema_contract=$2 ;;
        --api-contract) api_contract=$2 ;;
        --theme) theme_selection=$2 ;;
        --build-timestamp) build_timestamp=$2 ;;
        --source-root) source_root=$2 ;;
        --bun-bin) bun_bin=$2 ;;
        --git-bin) git_bin=$2 ;;
        --jq-bin) jq_bin=$2 ;;
        --tar-bin) tar_bin=$2 ;;
        --lock-dir) lock_dir=$2 ;;
      esac
      shift 2
      ;;
    *)
      die "unsupported argument: $1"
      ;;
  esac
done

case "${theme_selection}" in
  default)
    themes=(default)
    themes_json='["default"]'
    default_theme=default
    ;;
  classic)
    themes=(classic)
    themes_json='["classic"]'
    default_theme=classic
    ;;
  both)
    themes=(default classic)
    themes_json='["default","classic"]'
    default_theme=default
    ;;
  *)
    die "theme must be default, classic, or both"
    ;;
esac

[[ -n "${schema_contract}" ]] ||
  die "schema contract must be selected explicitly with --schema-contract bridge|feature"
case "${schema_contract}" in
  bridge|feature) ;;
  *) die "schema contract must be bridge or feature" ;;
esac
[[ -n "${api_contract}" ]] ||
  die "API contract must be selected explicitly with --api-contract NUMBER"
[[ "${api_contract}" =~ ^[1-9][0-9]*$ ]] ||
  die "API contract must be a positive integer"
if [[ -n "${backend_image_digest}" ]]; then
  [[ "${backend_image_digest}" =~ ^sha256:[0-9a-f]{64}$ ]] ||
    die "backend image digest must match sha256:<64 lowercase hex>"
fi

case "${source_root}" in
  /*) ;;
  *) source_root="${ROOT}/${source_root}" ;;
esac

# Resolve the source commit before deriving the release ID. A supplied SHA is
# useful for deterministic tests, but production builds still verify HEAD.
source_sha=''
head_sha=''
git_available=0
if command -v "${git_bin}" >/dev/null 2>&1; then
  git_available=1
fi
if [[ -n "${source_sha_arg}" ]]; then
  source_sha="${source_sha_arg}"
elif (( git_available )) && [[ -d "${source_root}" ]]; then
  source_sha="$("${git_bin}" -C "${source_root}" rev-parse HEAD 2>/dev/null)" ||
    die "unable to resolve source HEAD"
else
  die "source SHA is required when git is unavailable or source root is missing"
fi
validate_sha "source SHA" "${source_sha}"

if [[ -z "${release_id}" ]]; then
  release_stamp="${KKAI_FRONTEND_RELEASE_STAMP:-}"
  if [[ -z "${release_stamp}" ]]; then
    release_stamp="$(date -u +%Y%m%d.%s)"
  fi
  [[ "${release_stamp}" =~ ^[0-9]{8}\.[0-9]+$ ]] ||
    die "invalid release timestamp"
  release_id="kkai-frontend-${release_stamp}-${source_sha:0:9}"
fi
validate_release_id "release ID" "${release_id}"

if [[ -z "${backend_source_sha}" ]]; then
  if [[ "${allow_nonproduction}" -ne 1 && "${allow_dirty}" -ne 1 ]]; then
    die "production frontend builds require --backend-source-sha"
  fi
  backend_source_sha="${source_sha}"
fi
validate_sha "backend source SHA" "${backend_source_sha}"
if [[ -z "${backend_release_id}" ]]; then
  if [[ "${allow_nonproduction}" -ne 1 && "${allow_dirty}" -ne 1 ]]; then
    die "production frontend builds require --backend-release-id"
  fi
  backend_release_id="${release_id}"
fi
validate_release_id "backend release ID" "${backend_release_id}"

if [[ -z "${build_timestamp}" ]]; then
  build_timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi
[[ "${build_timestamp}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] ||
  die "build timestamp must be UTC RFC3339 (for example 2026-08-30T12:34:56Z)"

if (( git_available )) && [[ -d "${source_root}" ]]; then
  head_sha="$("${git_bin}" -C "${source_root}" rev-parse HEAD 2>/dev/null)" ||
    die "unable to resolve source HEAD"
  validate_sha "HEAD SHA" "${head_sha}"
  if [[ -n "${source_sha_arg}" && "${source_sha}" != "${head_sha}" ]]; then
    die "source SHA does not match HEAD"
  fi
  if [[ "${allow_nonproduction}" -ne 1 ]]; then
    branch="$("${git_bin}" -C "${source_root}" branch --show-current)" ||
      die "unable to resolve source branch"
    [[ "${branch}" == "production/kkrich" ]] ||
      die "frontend production builds require branch production/kkrich"
  fi
  if [[ "${allow_dirty}" -ne 1 ]]; then
    worktree_state="$("${git_bin}" -C "${source_root}" status --porcelain=v1 --untracked-files=all)" ||
      die "unable to inspect source worktree"
    [[ -z "${worktree_state}" ]] ||
      die "frontend production builds require a clean worktree"
  fi
  "${git_bin}" -C "${source_root}" cat-file -e "${source_sha}^{commit}" >/dev/null 2>&1 ||
    die "source commit object is unavailable"
  "${git_bin}" -C "${source_root}" archive --format=tar "${source_sha}" >/dev/null 2>&1 ||
    die "source tree cannot be archived"
elif (( ! dry_run )); then
  die "a real build requires an accessible git checkout"
fi

if (( ! install_deps && ! allow_dirty && ! allow_nonproduction )); then
  die "--skip-install is restricted to local builds with --allow-dirty or --allow-non-production"
fi

if (( dry_run )); then
  printf 'DRY_RUN=1\n'
  printf 'FRONTEND_RELEASE_ID=%s\n' "${release_id}"
  printf 'FRONTEND_SOURCE_SHA=%s\n' "${source_sha}"
  printf 'FRONTEND_BACKEND_SOURCE_SHA=%s\n' "${backend_source_sha}"
  printf 'FRONTEND_SCHEMA_CONTRACT=%s\n' "${schema_contract}"
  printf 'FRONTEND_API_CONTRACT=%s\n' "${api_contract}"
  printf 'FRONTEND_THEMES=%s\n' "${theme_selection}"
  if (( install_deps )); then
    printf 'INSTALL cwd=%q %q install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1\n' \
      "${source_root}/web" "${bun_bin}"
  else
    printf 'INSTALL=skipped\n'
  fi
  for theme in "${themes[@]}"; do
    printf 'BUILD theme=%s cwd=%q env=KKAI_EXTERNAL_FRONTEND_BUILD=1,VITE_REACT_APP_SERVER_URL="",VITE_REACT_APP_VERSION=%q,VITE_KKAI_SCHEMA_CONTRACT=%q %q run build -- --dist-path %q\n' \
      "${theme}" "${source_root}/web/${theme}" "${release_id}" "${schema_contract}" \
      "${bun_bin}" "<temporary-dir>/${theme}"
  done
  printf 'ARCHIVE=%s/%s.tar.gz\n' "${output_dir}" "${release_id}"
  printf 'METADATA=%s/%s.json\n' "${output_dir}" "${release_id}"
  exit 0
fi

for command_name in "${bun_bin}" "${git_bin}" "${jq_bin}" "${tar_bin}" mktemp find sort awk tr mv cp; do
  require_command "${command_name}"
done
[[ -d "${source_root}" ]] || die "source root does not exist: ${source_root}"
for theme in "${themes[@]}"; do
  [[ -d "${source_root}/web/${theme}" ]] || die "missing ${theme} frontend"
done

release_lock() {
  rm -f -- "${lock_dir}/pid" 2>/dev/null || true
  rmdir -- "${lock_dir}" 2>/dev/null || true
}

lock_parent="$(dirname -- "${lock_dir}")"
mkdir -p -- "${lock_parent}"
if ! mkdir -m 700 -- "${lock_dir}" 2>/dev/null; then
  lock_pid=''
  [[ -r "${lock_dir}/pid" ]] && lock_pid="$(<"${lock_dir}/pid")"
  if [[ "${lock_pid}" =~ ^[0-9]+$ ]] && kill -0 "${lock_pid}" 2>/dev/null; then
    die "another frontend build is running (pid ${lock_pid})"
  fi
  die "frontend build lock exists at ${lock_dir}; verify and remove it before retrying"
fi
trap release_lock EXIT
printf '%s\n' "$$" >"${lock_dir}/pid" || die "unable to record frontend build lock owner"

case "${output_dir}" in
  /*) ;;
  *) output_dir="${ROOT}/${output_dir}" ;;
esac
mkdir -p -- "${output_dir}"
output_dir="$(cd -- "${output_dir}" && pwd -P)"
release_parent="${output_dir}/frontend-releases"
final_release="${release_parent}/${release_id}"
archive="${output_dir}/${release_id}.tar.gz"
metadata="${output_dir}/${release_id}.json"
for existing_path in "${final_release}" "${archive}" "${metadata}"; do
  [[ ! -e "${existing_path}" && ! -L "${existing_path}" ]] ||
    die "release output already exists: ${existing_path}"
done
mkdir -p -- "${release_parent}"

tmp_root=""
tmp_release=""
moved_release=0
moved_archive=0
moved_metadata=0
committed=0
cleanup() {
  local status=$?
  if (( ! committed )); then
    (( moved_release )) && rm -rf -- "${final_release}" || true
    (( moved_archive )) && rm -f -- "${archive}" || true
    (( moved_metadata )) && rm -f -- "${metadata}" || true
  fi
  if [[ -n "${tmp_root}" && -d "${tmp_root}" ]]; then
    rm -rf -- "${tmp_root}"
  fi
  release_lock
  exit "${status}"
}
trap cleanup EXIT

tmp_root="$(mktemp -d "${output_dir}/.frontend-build-${release_id}.XXXXXX")"
tmp_release="${tmp_root}/frontend-releases/${release_id}"
mkdir -p -- "${tmp_release}" "${tmp_root}/build"

if (( install_deps )); then
  (
    cd -- "${source_root}/web"
    "${bun_bin}" install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1
  )
fi

legal_files=(LICENSE NOTICE THIRD-PARTY-LICENSES.md)
for legal_file in "${legal_files[@]}"; do
  legal_source="${source_root}/${legal_file}"
  [[ -f "${legal_source}" && ! -L "${legal_source}" ]] ||
    die "required legal notice is missing: ${legal_file}"
  cp -p "${legal_source}" "${tmp_release}/${legal_file}"
done

for theme in "${themes[@]}"; do
  theme_source_dir="${source_root}/web/${theme}"
  build_dist="${tmp_root}/build/${theme}"
  mkdir -p -- "${build_dist}"
  (
    cd -- "${theme_source_dir}"
    export VITE_REACT_APP_VERSION="${release_id}"
    export VITE_KKAI_SCHEMA_CONTRACT="${schema_contract}"
    export VITE_REACT_APP_SERVER_URL=""
    export KKAI_EXTERNAL_FRONTEND_BUILD=1
    if [[ "${theme}" == "default" ]]; then
      export DISABLE_ESLINT_PLUGIN=true
    fi
    "${bun_bin}" run build -- --dist-path "${build_dist}"
  )
  [[ -f "${build_dist}/index.html" && -s "${build_dist}/index.html" ]] ||
    die "${theme} build did not produce a non-empty index.html"
  symlink_path="$(find "${build_dist}" -type l -print -quit)"
  [[ -z "${symlink_path}" ]] || die "${theme} build contains an unexpected symlink"
  mv -- "${build_dist}" "${tmp_release}/${theme}"
done

# A build should only write ignored dependency/output paths. Recheck the
# checkout before packaging so an install hook or build plugin cannot silently
# enter the release provenance.
if [[ "${allow_dirty}" -ne 1 ]]; then
  post_build_state="$("${git_bin}" -C "${source_root}" status --porcelain=v1 --untracked-files=all)" ||
    die "unable to inspect source worktree after frontend build"
  [[ -z "${post_build_state}" ]] ||
    die "frontend build changed the source worktree"
fi

lockfile="${source_root}/web/bun.lock"
[[ -f "${lockfile}" ]] || die "frontend lockfile is missing: ${lockfile}"
lockfile_sha256="$(sha256_file "${lockfile}")"
bun_version="${KKAI_FRONTEND_BUN_VERSION:-}"
if [[ -z "${bun_version}" ]]; then
  bun_version="$("${bun_bin}" --version 2>/dev/null || true)"
fi
[[ -n "${bun_version}" ]] || bun_version="unknown"

# shellcheck source-path=SCRIPTDIR
# shellcheck source=frontend-build-release-package.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/frontend-build-release-package.sh"
package_frontend_release
