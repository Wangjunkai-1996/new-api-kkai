#!/bin/sh
set -eu

readonly PACKAGE_ROOT="${1:-/package}"
readonly LICENSE_ROOT="${PACKAGE_ROOT}/licenses/ffmpeg"
readonly FFMPEG="${PACKAGE_ROOT}/usr/local/bin/ffmpeg"
readonly FFPROBE="${PACKAGE_ROOT}/usr/local/bin/ffprobe"
readonly FIXTURE_ROOT=/tmp/video-media-fixture

fail() {
  echo "FFmpeg package verification: $*" >&2
  exit 1
}

verify_manifest_and_configuration() {
  cd "${PACKAGE_ROOT}"
  sha256sum -c licenses/ffmpeg/SHA256SUMS
  # shellcheck disable=SC1091
  . "${LICENSE_ROOT}/VERSIONS.env"
  "${FFMPEG}" -version 2>&1 | grep -Fq "ffmpeg version ${FFMPEG_VERSION}" || fail "unexpected ffmpeg version"
  "${FFPROBE}" -version 2>&1 | grep -Fq "ffprobe version ${FFMPEG_VERSION}" || fail "unexpected ffprobe version"
  "${FFMPEG}" -hide_banner -buildconf > "${FIXTURE_ROOT}/BUILD-CONFIG.actual" 2>&1
  cmp "${LICENSE_ROOT}/BUILD-CONFIG.txt" "${FIXTURE_ROOT}/BUILD-CONFIG.actual" || fail "build configuration drifted"
  grep -Fq -- '--enable-libx264' "${LICENSE_ROOT}/BUILD-CONFIG.txt" || fail "x264 is not enabled"
  grep -Fq -- '--disable-network' "${LICENSE_ROOT}/BUILD-CONFIG.txt" || fail "network support is enabled"
  if grep -Fq -- '--enable-nonfree' "${LICENSE_ROOT}/BUILD-CONFIG.txt"; then
    fail "nonfree mode is enabled"
  fi
}

verify_static_binaries() {
  "${LICENSE_ROOT}/build/verify-static.sh" "${FFMPEG}" "${FFPROBE}"
}

verify_real_media_fixture() {
  "${FFMPEG}" -v error \
    -f lavfi -i testsrc2=size=320x180:rate=12 -t 1 -an \
    -c:v libx264 -preset veryfast -crf 28 -pix_fmt yuv420p \
    -movflags +faststart -f mp4 "${FIXTURE_ROOT}/fixture.mp4"
  probe="$(${FFPROBE} -v error -select_streams v:0 \
    -show_entries stream=codec_name,width,height -of csv=p=0 "${FIXTURE_ROOT}/fixture.mp4")"
  [ "${probe}" = 'h264,320,180' ] || fail "unexpected MP4 fixture metadata: ${probe}"
  "${FFMPEG}" -v error -i "${FIXTURE_ROOT}/fixture.mp4" \
    -map 0:v:0 -frames:v 1 -an -vf scale=160:-2 \
    -c:v mjpeg -f image2 "${FIXTURE_ROOT}/fixture.jpg"
  [ -s "${FIXTURE_ROOT}/fixture.jpg" ] || fail "poster fixture is empty"
  "${FFMPEG}" -v error -i "${FIXTURE_ROOT}/fixture.mp4" \
    -map 0:v:0 -t 0.5 -an -vf scale=160:-2 -r 12 \
    -c:v libx264 -preset veryfast -crf 32 -pix_fmt yuv420p \
    -movflags +faststart -f mp4 "${FIXTURE_ROOT}/preview.mp4"
  preview_codec="$(${FFPROBE} -v error -select_streams v:0 \
    -show_entries stream=codec_name -of csv=p=0 "${FIXTURE_ROOT}/preview.mp4")"
  [ "${preview_codec}" = h264 ] || fail "preview fixture is not H.264"
}

mkdir -p "${FIXTURE_ROOT}"
verify_manifest_and_configuration
verify_static_binaries
verify_real_media_fixture
echo "FFmpeg package verification passed"
