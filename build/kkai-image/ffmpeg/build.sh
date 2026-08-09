#!/bin/sh
set -eu

: "${FFMPEG_VERSION:?missing FFMPEG_VERSION}"
: "${FFMPEG_SOURCE_SHA256:?missing FFMPEG_SOURCE_SHA256}"
: "${X264_REVISION:?missing X264_REVISION}"
: "${X264_SOURCE_SHA256:?missing X264_SOURCE_SHA256}"
: "${SOURCE_DATE_EPOCH:?missing SOURCE_DATE_EPOCH}"
: "${MEDIA_BUILD_IMAGE:?missing MEDIA_BUILD_IMAGE}"
: "${MEDIA_MATERIAL_DIR:?missing MEDIA_MATERIAL_DIR}"

readonly BUILD_ROOT=/build/video-media
readonly PREFIX="${BUILD_ROOT}/prefix"
readonly SOURCE_ROOT="${BUILD_ROOT}/sources"
readonly PACKAGE_ROOT=/package
readonly LICENSE_ROOT="${PACKAGE_ROOT}/licenses/ffmpeg"
readonly ARCHIVE_ROOT="${LICENSE_ROOT}/sources"
readonly OUTPUT_ROOT="${PACKAGE_ROOT}/usr/local/bin"

download_source() {
  url=$1
  destination=$2
  expected_sha256=$3
  (
    unset HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy
    if command -v curl >/dev/null 2>&1; then
      curl --fail --location --retry 5 --retry-all-errors --retry-delay 2 \
        --connect-timeout 20 --max-time 300 --output "${destination}" "${url}"
    else
      wget -O "${destination}" "${url}"
    fi
  )
  printf '%s  %s\n' "${expected_sha256}" "${destination}" | sha256sum -c -
}

build_x264() {
  x264_source="${SOURCE_ROOT}/x264-${X264_REVISION}"
  bzip2 -dc "${ARCHIVE_ROOT}/x264-${X264_REVISION}.tar.bz2" | tar -xf - -C "${SOURCE_ROOT}"
  cd "${x264_source}"
  bash ./configure \
    --prefix="${PREFIX}" \
    --enable-static \
    --disable-cli \
    --disable-opencl \
    --disable-avs \
    --disable-swscale \
    --disable-lavf \
    --disable-ffms \
    --disable-gpac \
    --disable-lsmash \
    --bit-depth=8 \
    --chroma-format=420 \
    --extra-cflags="-O2 -ffile-prefix-map=${BUILD_ROOT}=. -fdebug-prefix-map=${BUILD_ROOT}=." \
    --extra-ldflags="-Wl,--build-id=none"
  make -j4
  make install-lib-static
}

build_ffmpeg() {
  ffmpeg_source="${SOURCE_ROOT}/ffmpeg-${FFMPEG_VERSION}"
  bzip2 -dc "${ARCHIVE_ROOT}/ffmpeg-${FFMPEG_VERSION}.tar.bz2" | tar -xf - -C "${SOURCE_ROOT}"
  cd "${ffmpeg_source}"
  PKG_CONFIG_PATH="${PREFIX}/lib/pkgconfig" ./configure \
    --prefix="${PREFIX}" \
    --pkg-config-flags=--static \
    --extra-cflags="-I${PREFIX}/include -O2 -ffile-prefix-map=${BUILD_ROOT}=. -fdebug-prefix-map=${BUILD_ROOT}=." \
    --extra-ldflags="-L${PREFIX}/lib -static -Wl,--build-id=none" \
    --extra-libs="-lpthread -lm" \
    --enable-static \
    --disable-shared \
    --disable-doc \
    --disable-debug \
    --disable-stripping \
    --disable-network \
    --disable-autodetect \
    --disable-iconv \
    --disable-zlib \
    --disable-bzlib \
    --disable-lzma \
    --disable-sdl2 \
    --disable-postproc \
    --disable-swresample \
    --disable-everything \
    --enable-ffmpeg \
    --enable-ffprobe \
    --enable-gpl \
    --enable-version3 \
    --enable-libx264 \
    --enable-protocol=file \
    --enable-indev=lavfi \
    --enable-demuxer=mov,matroska,image2 \
    --enable-muxer=mp4,image2 \
    --enable-decoder=h264,hevc,av1,mpeg4,prores,vp8,vp9,mjpeg,wrapped_avframe \
    --enable-encoder=libx264,mjpeg \
    --enable-parser=h264,hevc,av1,mpeg4video,vp8,vp9,mjpeg \
    --enable-filter=scale,testsrc2
  make -j4 ffmpeg ffprobe
  cp ffmpeg ffprobe "${OUTPUT_ROOT}/"
  strip "${OUTPUT_ROOT}/ffmpeg" "${OUTPUT_ROOT}/ffprobe"
}

package_build_materials() {
  mkdir -p "${LICENSE_ROOT}/build" "${LICENSE_ROOT}/licenses/ffmpeg" "${LICENSE_ROOT}/licenses/x264"
  cp "${SOURCE_ROOT}/ffmpeg-${FFMPEG_VERSION}/COPYING.GPLv3" "${LICENSE_ROOT}/licenses/ffmpeg/"
  cp "${SOURCE_ROOT}/x264-${X264_REVISION}/COPYING" "${LICENSE_ROOT}/licenses/x264/"
  cp "${MEDIA_MATERIAL_DIR}/BUILD-PACKAGES.lock" \
    "${MEDIA_MATERIAL_DIR}/build.sh" \
    "${MEDIA_MATERIAL_DIR}/verify-static.sh" \
    "${MEDIA_MATERIAL_DIR}/verify.sh" \
    "${LICENSE_ROOT}/build/"
  cp "${MEDIA_MATERIAL_DIR}/PROVENANCE.md" "${LICENSE_ROOT}/"
  "${OUTPUT_ROOT}/ffmpeg" -hide_banner -buildconf > "${LICENSE_ROOT}/BUILD-CONFIG.txt" 2>&1
  {
    printf 'build_image=%s\n' "${MEDIA_BUILD_IMAGE}"
    printf 'source_date_epoch=%s\n' "${SOURCE_DATE_EPOCH}"
    apk info -vv | LC_ALL=C sort
  } > "${LICENSE_ROOT}/BUILD-ENVIRONMENT.txt"
  {
    printf 'FFMPEG_VERSION=%s\n' "${FFMPEG_VERSION}"
    printf 'FFMPEG_SOURCE_SHA256=%s\n' "${FFMPEG_SOURCE_SHA256}"
    printf 'X264_REVISION=%s\n' "${X264_REVISION}"
    printf 'X264_SOURCE_SHA256=%s\n' "${X264_SOURCE_SHA256}"
  } > "${LICENSE_ROOT}/VERSIONS.env"
  cd "${PACKAGE_ROOT}"
  find usr/local/bin licenses/ffmpeg -type f ! -name SHA256SUMS -print \
    | LC_ALL=C sort \
    | xargs sha256sum > licenses/ffmpeg/SHA256SUMS
}

main() {
  export SOURCE_DATE_EPOCH LC_ALL=C TZ=UTC
  mkdir -p "${SOURCE_ROOT}" "${ARCHIVE_ROOT}" "${OUTPUT_ROOT}"
  download_source \
    "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.bz2" \
    "${ARCHIVE_ROOT}/ffmpeg-${FFMPEG_VERSION}.tar.bz2" \
    "${FFMPEG_SOURCE_SHA256}"
  download_source \
    "https://code.videolan.org/videolan/x264/-/archive/${X264_REVISION}/x264-${X264_REVISION}.tar.bz2" \
    "${ARCHIVE_ROOT}/x264-${X264_REVISION}.tar.bz2" \
    "${X264_SOURCE_SHA256}"
  build_x264
  build_ffmpeg
  package_build_materials
}

main "$@"
