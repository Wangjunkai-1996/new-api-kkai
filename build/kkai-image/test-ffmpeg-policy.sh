#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly ROOT
readonly DOCKERFILE="${ROOT}/build/kkai-image/Dockerfile"
readonly CONTEXT_IGNORE="${ROOT}/build/kkai-image/.dockerignore"
readonly MEDIA_BUILD_DIR="${ROOT}/build/kkai-image/ffmpeg"
readonly MEDIA_BUILD_SCRIPT="${MEDIA_BUILD_DIR}/build.sh"
readonly MEDIA_VERIFY_SCRIPT="${MEDIA_BUILD_DIR}/verify.sh"
readonly MEDIA_STATIC_VERIFY_SCRIPT="${MEDIA_BUILD_DIR}/verify-static.sh"
readonly MEDIA_STATIC_VERIFY_TEST="${MEDIA_BUILD_DIR}/verify-static_test.sh"
readonly MEDIA_PACKAGE_LOCK="${MEDIA_BUILD_DIR}/BUILD-PACKAGES.lock"
readonly MEDIA_PROVENANCE="${MEDIA_BUILD_DIR}/PROVENANCE.md"
readonly MEDIA_PROCESSOR="${ROOT}/service/video_media_processor.go"
readonly THIRD_PARTY_LICENSES="${ROOT}/THIRD-PARTY-LICENSES.md"

fail() {
  echo "KKAI FFmpeg policy: $*" >&2
  exit 1
}

contains() {
  grep -Fq -- "$1" "$2"
}

rejects() {
  if grep -Fq -- "$1" "$2"; then
    fail "$3"
  fi
}

require_binary_digest() {
  local argument_name=$1
  local value
  value="$(sed -n "s/^ARG ${argument_name}=\\([0-9a-f]\\{64\\}\\)$/\\1/p" "${DOCKERFILE}")"
  [[ ${#value} -eq 64 ]] || fail "${argument_name} is not a lowercase SHA-256 digest"
  [[ "${value}" != ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff ]] ||
    fail "${argument_name} still uses the temporary placeholder digest"
  [[ "${value}" != 0000000000000000000000000000000000000000000000000000000000000000 ]] ||
    fail "${argument_name} uses an empty placeholder digest"
}

for file in \
  "${MEDIA_BUILD_SCRIPT}" \
  "${MEDIA_STATIC_VERIFY_SCRIPT}" \
  "${MEDIA_VERIFY_SCRIPT}" \
  "${MEDIA_PACKAGE_LOCK}" \
  "${MEDIA_PROVENANCE}"; do
  [[ -s "${file}" ]] || fail "missing FFmpeg build artifact: ${file#"${ROOT}/"}"
done
[[ -x "${MEDIA_STATIC_VERIFY_TEST}" ]] || fail "static linkage verifier tests are not executable"

contains '!ffmpeg/' "${CONTEXT_IGNORE}" || fail "FFmpeg build context directory is excluded"
contains '!ffmpeg/**' "${CONTEXT_IGNORE}" || fail "FFmpeg build context files are excluded"

contains 'ARG FFMPEG_VERSION=7.1.1' "${DOCKERFILE}" || fail "FFmpeg version is not pinned"
contains 'ARG FFMPEG_SOURCE_SHA256=0c8da2f11579a01e014fc007cbacf5bb4da1d06afd0b43c7f8097ec7c0f143ba' "${DOCKERFILE}" ||
  fail "FFmpeg source archive digest is not pinned"
contains 'ARG X264_REVISION=b35605ace3ddf7c1a5d67a2eb553f034aef41d55' "${DOCKERFILE}" ||
  fail "x264 source revision is not pinned"
contains 'ARG X264_SOURCE_SHA256=6eeb82934e69fd51e043bd8c5b0d152839638d1ce7aa4eea65a3fedcf83ff224' "${DOCKERFILE}" ||
  fail "x264 source archive digest is not pinned"
require_binary_digest FFMPEG_BINARY_SHA256
require_binary_digest FFPROBE_BINARY_SHA256
contains "FROM \${GO_IMAGE} AS video-media-build" "${DOCKERFILE}" || fail "controlled media build stage is missing"
contains 'COPY --from=kkai_image ffmpeg/BUILD-PACKAGES.lock' "${DOCKERFILE}" ||
  fail "media build package lock is not copied"
contains 'COPY --from=kkai_image ffmpeg/build.sh' "${DOCKERFILE}" || fail "media build script is not copied"
contains 'COPY --from=kkai_image ffmpeg/verify-static.sh' "${DOCKERFILE}" ||
  fail "fail-closed static linkage verifier is not copied"
contains 'COPY --from=kkai_image ffmpeg/verify.sh' "${DOCKERFILE}" || fail "media verifier is not copied"
contains 'FROM scratch AS video-media-package' "${DOCKERFILE}" || fail "audited media package export stage is missing"
contains 'FFMPEG_BINARY_SHA256}" /package/usr/local/bin/ffmpeg' "${DOCKERFILE}" ||
  fail "build audit does not bind the pinned FFmpeg digest to the packaged binary"
contains 'FFPROBE_BINARY_SHA256}" /package/usr/local/bin/ffprobe' "${DOCKERFILE}" ||
  fail "build audit does not bind the pinned FFprobe digest to the packaged binary"
contains '| sha256sum -c -' "${DOCKERFILE}" || fail "build audit does not verify the pinned media digests"
contains 'COPY --from=video-media-package --chown=0:0 /package/licenses/ffmpeg /licenses/ffmpeg' "${DOCKERFILE}" ||
  fail "runtime image omits FFmpeg corresponding-source materials"
contains 'VIDEO_STUDIO_FFMPEG_PATH=/usr/local/bin/ffmpeg' "${DOCKERFILE}" ||
  fail "runtime FFmpeg path contract is missing"
contains 'VIDEO_STUDIO_FFPROBE_PATH=/usr/local/bin/ffprobe' "${DOCKERFILE}" ||
  fail "runtime FFprobe path contract is missing"
contains "VIDEO_STUDIO_FFMPEG_VERSION=\${FFMPEG_VERSION}" "${DOCKERFILE}" ||
  fail "runtime FFmpeg version contract is missing"
contains "VIDEO_STUDIO_FFMPEG_SHA256=\${FFMPEG_BINARY_SHA256}" "${DOCKERFILE}" ||
  fail "runtime FFmpeg digest contract is missing"
contains "VIDEO_STUDIO_FFPROBE_SHA256=\${FFPROBE_BINARY_SHA256}" "${DOCKERFILE}" ||
  fail "runtime FFprobe digest contract is missing"
contains 'org.opencontainers.image.licenses="AGPL-3.0 AND GPL-3.0-or-later"' "${DOCKERFILE}" ||
  fail "runtime image license metadata omits FFmpeg"

rejects 'mwader/static-ffmpeg' "${DOCKERFILE}" "third-party static FFmpeg object code remains"
rejects 'FFMPEG_IMAGE=' "${DOCKERFILE}" "third-party FFmpeg image argument remains"
rejects 'VIDEO_STUDIO_FFMPEG_LICENSE' "${DOCKERFILE}" "license-string runtime gate remains in the image"
rejects 'GNU General Public License' "${DOCKERFILE}" "license output is still used as a release gate"
rejects 'RELEASE_STATUS' "${DOCKERFILE}" "manual FFmpeg release status state machine remains"
rejects 'BLOCKED' "${DOCKERFILE}" "manual FFmpeg BLOCKED state remains"
rejects 'CLEARED' "${DOCKERFILE}" "manual FFmpeg CLEARED state remains"
rejects 'VIDEO_STUDIO_FFMPEG_LICENSE' "${MEDIA_PROCESSOR}" "license-string runtime gate remains in service code"
rejects '"-L"' "${MEDIA_PROCESSOR}" "runtime still trusts FFmpeg license output"

contains "https://ffmpeg.org/releases/ffmpeg-\${FFMPEG_VERSION}.tar.bz2" "${MEDIA_BUILD_SCRIPT}" ||
  fail "FFmpeg source URL is not the fixed official release archive"
contains "https://code.videolan.org/videolan/x264/-/archive/\${X264_REVISION}/x264-\${X264_REVISION}.tar.bz2" "${MEDIA_BUILD_SCRIPT}" ||
  fail "x264 source URL is not bound to the pinned official revision"
contains 'curl --fail --location --retry' "${MEDIA_BUILD_SCRIPT}" ||
  fail "source downloads do not use the fail-closed curl transport when available"
contains 'curl=8.21.0-r0' "${MEDIA_PACKAGE_LOCK}" ||
  fail "the locked media build toolchain does not include curl"
contains 'sha256sum -c' "${MEDIA_BUILD_SCRIPT}" || fail "source archives are not checksum-verified"
contains '--disable-network' "${MEDIA_BUILD_SCRIPT}" || fail "FFmpeg network support is not disabled"
contains '--disable-autodetect' "${MEDIA_BUILD_SCRIPT}" || fail "FFmpeg dependency autodetection is not disabled"
contains '--enable-libx264' "${MEDIA_BUILD_SCRIPT}" || fail "the required x264 encoder is not enabled"
contains '--enable-decoder=h264,hevc,av1,mpeg4,prores,vp8,vp9,mjpeg,wrapped_avframe' "${MEDIA_BUILD_SCRIPT}" ||
  fail "the controlled decoder allowlist omits the lavfi verification decoder"
contains '--enable-gpl' "${MEDIA_BUILD_SCRIPT}" || fail "GPL mode is not explicit"
contains '--enable-version3' "${MEDIA_BUILD_SCRIPT}" || fail "GPLv3-compatible mode is not explicit"
rejects '--enable-nonfree' "${MEDIA_BUILD_SCRIPT}" "nonfree FFmpeg mode is enabled"
contains 'COPYING.GPLv3' "${MEDIA_BUILD_SCRIPT}" || fail "FFmpeg GPLv3 text is not retained"
contains 'licenses/x264' "${MEDIA_BUILD_SCRIPT}" || fail "x264 license directory is not retained"
contains '/COPYING"' "${MEDIA_BUILD_SCRIPT}" || fail "x264 GPL text is not retained"
contains 'BUILD-CONFIG.txt' "${MEDIA_BUILD_SCRIPT}" || fail "actual FFmpeg build configuration is not retained"
contains 'SHA256SUMS' "${MEDIA_BUILD_SCRIPT}" || fail "source and binary SHA manifest is not generated"

contains 'sha256sum -c licenses/ffmpeg/SHA256SUMS' "${MEDIA_VERIFY_SCRIPT}" ||
  fail "packaged source and binary manifest is not verified"
contains 'build/verify-static.sh' "${MEDIA_VERIFY_SCRIPT}" ||
  fail "package verification bypasses the fail-closed static linkage verifier"
contains 'command -v scanelf' "${MEDIA_STATIC_VERIFY_SCRIPT}" || fail "scanelf availability is not verified"
contains 'needed_status=$?' "${MEDIA_STATIC_VERIFY_SCRIPT}" || fail "scanelf NEEDED failures are not captured"
contains 'interp_status=$?' "${MEDIA_STATIC_VERIFY_SCRIPT}" || fail "scanelf INTERP failures are not captured"
contains '-buildconf' "${MEDIA_VERIFY_SCRIPT}" || fail "FFmpeg build configuration is not verified"
contains 'fixture.mp4' "${MEDIA_VERIFY_SCRIPT}" || fail "real MP4 fixture is not generated"
contains 'libx264' "${MEDIA_VERIFY_SCRIPT}" || fail "real fixture does not exercise x264 encoding"
contains 'fixture.jpg' "${MEDIA_VERIFY_SCRIPT}" || fail "real fixture does not exercise poster extraction"
contains 'ffprobe' "${MEDIA_VERIFY_SCRIPT}" || fail "real fixture is not independently probed"

contains "\`FFmpeg\`" "${MEDIA_PROVENANCE}" || fail "FFmpeg provenance entry is missing"
contains "\`x264\`" "${MEDIA_PROVENANCE}" || fail "x264 provenance entry is missing"
contains '/licenses/ffmpeg/sources/' "${MEDIA_PROVENANCE}" || fail "corresponding-source location is undocumented"
contains "\`FFmpeg\`" "${THIRD_PARTY_LICENSES}" || fail "FFmpeg is missing from the third-party inventory"
contains "\`x264\`" "${THIRD_PARTY_LICENSES}" || fail "x264 is missing from the third-party inventory"
contains '/licenses/ffmpeg/PROVENANCE.md' "${THIRD_PARTY_LICENSES}" ||
  fail "the third-party inventory does not locate the container media materials"
contains '/licenses/ffmpeg/SHA256SUMS' "${THIRD_PARTY_LICENSES}" ||
  fail "the third-party inventory does not locate the generated integrity manifest"
contains '/licenses/ffmpeg/BUILD-ENVIRONMENT.txt' "${THIRD_PARTY_LICENSES}" ||
  fail "the third-party inventory does not locate the build manifest"
if sed -n '/^## Container Media Materials$/,/^## License Texts$/p' "${THIRD_PARTY_LICENSES}" |
  grep -Eq '[0-9a-f]{64}'; then
  fail "the third-party inventory duplicates a generated binary digest"
fi

"${MEDIA_STATIC_VERIFY_TEST}"

echo "KKAI FFmpeg source-build policy passed"
