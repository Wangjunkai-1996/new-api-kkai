# FFmpeg Runtime Provenance

The production image builds its media tools from the exact upstream source
archives listed below. It does not copy FFmpeg object code from a third-party
binary image.

## Pinned Sources

- `FFmpeg` 7.1.1:
  `https://ffmpeg.org/releases/ffmpeg-7.1.1.tar.bz2`
  (`0c8da2f11579a01e014fc007cbacf5bb4da1d06afd0b43c7f8097ec7c0f143ba`)
- `x264` commit `b35605ace3ddf7c1a5d67a2eb553f034aef41d55`:
  `https://code.videolan.org/videolan/x264/-/archive/b35605ace3ddf7c1a5d67a2eb553f034aef41d55/x264-b35605ace3ddf7c1a5d67a2eb553f034aef41d55.tar.bz2`
  (`6eeb82934e69fd51e043bd8c5b0d152839638d1ce7aa4eea65a3fedcf83ff224`)

The unchanged archives are distributed in `/licenses/ffmpeg/sources/` in the
same image as the binaries. Their upstream license texts, the exact build and
verification scripts, the pinned Alpine package list, the resolved package
inventory, the actual `ffmpeg -buildconf` output, and a SHA-256 manifest are
also retained under `/licenses/ffmpeg/`.

## Build Scope

The build enables the native demuxers and decoders needed to inspect supported
MP4/WebM inputs, the native MJPEG encoder needed for posters, and `libx264` for
short H.264 previews. Network protocols, dependency autodetection, shared
libraries, documentation, and unrelated external codecs are disabled.

The source-build verifier checks the manifest, versions, exact build
configuration, static linkage, MP4 creation and probing, H.264 decoding, x264
re-encoding, scaling, and JPEG poster extraction before the binaries can enter
the runtime image.

This engineering record is not legal advice or an independent legal approval.
