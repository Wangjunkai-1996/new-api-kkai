#!/usr/bin/env bash
# Packaging helper for frontend-build-release.sh.
#
# This file is sourced by the entrypoint after all build and provenance
# variables have been validated. It deliberately has no deployment behavior.
# shellcheck disable=SC2154,SC2034

package_frontend_release() {
  local manifest_path
  local file
  local relative_path
  local manifest_sha256
  local tmp_archive
  local archive_sha256
  local tmp_metadata

  # Keep provenance next to the bundles so a controller can verify an
  # extracted release without trusting the outer archive metadata.
  # shellcheck disable=SC2016
  "${jq_bin}" --null-input \
    --arg artifact "new-api-frontend" \
    --arg release_id "${release_id}" \
    --arg source_sha "${source_sha}" \
    --arg backend_source_sha "${backend_source_sha}" \
    --arg backend_release_id "${backend_release_id}" \
    --arg schema_contract "${schema_contract}" \
    --arg build_timestamp "${build_timestamp}" \
    --arg bun_version "${bun_version}" \
    --arg lockfile_sha256 "${lockfile_sha256}" \
    --arg theme_selection "${theme_selection}" \
    --arg default_theme "${default_theme}" \
    --arg install_mode "$( (( install_deps )) && printf 'frozen' || printf 'skipped' )" \
    --argjson legal_files '["LICENSE","NOTICE","THIRD-PARTY-LICENSES.md"]' \
    --argjson api_contract "${api_contract}" \
    --argjson themes "${themes_json}" \
    '{
      artifact: $artifact,
      format_version: 1,
      release_id: $release_id,
      source_sha: $source_sha,
      backend_source_sha: $backend_source_sha,
      backend_release_id: $backend_release_id,
      api_contract: $api_contract,
      schema_contract: $schema_contract,
      themes: $themes,
      default_theme: $default_theme,
      theme_selection: $theme_selection,
      api_base_url: "relative",
      legal_files: $legal_files,
      build: {
        package_manager: "bun",
        install: $install_mode,
        bun_version: $bun_version,
        lockfile: "web/bun.lock",
        lockfile_sha256: $lockfile_sha256
      },
      generated_at: $build_timestamp
    }' >"${tmp_release}/frontend.json"

  # The pair record is deliberately separate from the frontend manifest: it
  # lets promotion code reject a frontend/backend mismatch before serving it.
  # shellcheck disable=SC2016
  "${jq_bin}" --null-input \
    --arg frontend_release_id "${release_id}" \
    --arg frontend_source_sha "${source_sha}" \
    --arg backend_release_id "${backend_release_id}" \
    --arg backend_source_sha "${backend_source_sha}" \
    --arg backend_image_digest "${backend_image_digest}" \
    --arg schema_contract "${schema_contract}" \
    --arg build_timestamp "${build_timestamp}" \
    --argjson api_contract "${api_contract}" \
    '{
      format_version: 1,
      frontend_release_id: $frontend_release_id,
      frontend_source_sha: $frontend_source_sha,
      backend_release_id: $backend_release_id,
      backend_source_sha: $backend_source_sha,
      backend_image_digest: (if $backend_image_digest == "" then null else $backend_image_digest end),
      api_contract: $api_contract,
      schema_contract: $schema_contract,
      generated_at: $build_timestamp
    }' >"${tmp_release}/release-pair.json"

  manifest_path="${tmp_release}/manifest.sha256"
  : >"${manifest_path}"
  while IFS= read -r file; do
    [[ "${file}" == "${manifest_path}" ]] && continue
    relative_path="${file#"${tmp_release}/"}"
    printf '%s  %s\n' "$(sha256_file "${file}")" "${relative_path}" >>"${manifest_path}"
  done < <(find "${tmp_release}" -type f -print | LC_ALL=C sort)
  [[ -s "${manifest_path}" ]] || die "frontend release contains no files"
  manifest_sha256="$(sha256_file "${manifest_path}")"

  tmp_archive="${tmp_root}/${release_id}.tar.gz"
  "${tar_bin}" -czf "${tmp_archive}" -C "${tmp_root}" "frontend-releases/${release_id}"
  [[ -s "${tmp_archive}" ]] || die "frontend archive is empty"
  archive_sha256="$(sha256_file "${tmp_archive}")"

  tmp_metadata="${tmp_root}/${release_id}.json"
  # shellcheck disable=SC2016
  "${jq_bin}" --null-input \
    --arg artifact "new-api-frontend" \
    --arg release_id "${release_id}" \
    --arg source_sha "${source_sha}" \
    --arg backend_source_sha "${backend_source_sha}" \
    --arg backend_release_id "${backend_release_id}" \
    --arg backend_image_digest "${backend_image_digest}" \
    --arg schema_contract "${schema_contract}" \
    --arg theme_selection "${theme_selection}" \
    --arg archive "${release_id}.tar.gz" \
    --arg archive_sha256 "${archive_sha256}" \
    --arg manifest "frontend-releases/${release_id}/manifest.sha256" \
    --arg manifest_sha256 "${manifest_sha256}" \
    --arg release_path "frontend-releases/${release_id}" \
    --arg build_timestamp "${build_timestamp}" \
    --arg default_theme "${default_theme}" \
    --argjson legal_files '["LICENSE","NOTICE","THIRD-PARTY-LICENSES.md"]' \
    --argjson api_contract "${api_contract}" \
    --argjson themes "${themes_json}" \
    '{
      artifact: $artifact,
      format_version: 1,
      release_id: $release_id,
      source_sha: $source_sha,
      backend_source_sha: $backend_source_sha,
      backend_release_id: $backend_release_id,
      backend_image_digest: (if $backend_image_digest == "" then null else $backend_image_digest end),
      api_contract: $api_contract,
      schema_contract: $schema_contract,
      themes: $themes,
      default_theme: $default_theme,
      theme_selection: $theme_selection,
      api_base_url: "relative",
      legal_files: $legal_files,
      archive: $archive,
      archive_sha256: $archive_sha256,
      manifest: $manifest,
      manifest_sha256: $manifest_sha256,
      release_path: $release_path,
      generated_at: $build_timestamp,
      platform: "web"
    }' >"${tmp_metadata}"

  # Each final move is atomic on the same filesystem. If a later move fails,
  # the EXIT trap removes any earlier move so callers never see a partial release.
  mv -- "${tmp_release}" "${final_release}"
  moved_release=1
  mv -- "${tmp_archive}" "${archive}"
  moved_archive=1
  mv -- "${tmp_metadata}" "${metadata}"
  moved_metadata=1
  committed=1

  printf 'FRONTEND_RELEASE_METADATA=%s\n' "${metadata}"
  printf 'FRONTEND_RELEASE_ARCHIVE=%s\n' "${archive}"
  printf 'FRONTEND_RELEASE_ID=%s\n' "${release_id}"
  printf 'FRONTEND_SOURCE_SHA=%s\n' "${source_sha}"
}
