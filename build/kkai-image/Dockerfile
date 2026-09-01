# syntax=docker/dockerfile:1.8@sha256:dde28735fbb848299b7a43b7561d828b89c76f84f745c31896a7a7efbf93851a

ARG BUN_IMAGE=oven/bun:1.3.13@sha256:bb35eafd10b2e969809384850ff0474ba36a491239d715864bc87787b4cdf0a4
ARG GO_IMAGE=golang:1.26.5-alpine@sha256:111d79159b2326f7e80c4a4706e1ba166acb0e2611df853955f3621828cd49e8
# Multi-platform index; its linux/amd64 child remains pinned by GO_IMAGE.
ARG GO_BUILD_IMAGE=golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2
ARG BUSYBOX_IMAGE=busybox:1.37.0-musl@sha256:6be969a074d06073a62ad8ccfd2ad6ec2790cae25d519d8df255ba24819d96f0
ARG DISTROLESS_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:7a2bd171a18bdd39a4729600d0dca5f16e779d41156a6908b4f8a9a289e76d92
ARG KKAI_SCHEMA_CONTRACT=feature
ARG KKAI_FRONTEND_MODE=embedded
ARG MEDIA_BUILD_PARALLELISM=2
ARG FFMPEG_VERSION=7.1.1
ARG FFMPEG_SOURCE_SHA256=0c8da2f11579a01e014fc007cbacf5bb4da1d06afd0b43c7f8097ec7c0f143ba
ARG X264_REVISION=b35605ace3ddf7c1a5d67a2eb553f034aef41d55
ARG X264_SOURCE_SHA256=6eeb82934e69fd51e043bd8c5b0d152839638d1ce7aa4eea65a3fedcf83ff224
ARG VIDEO_MEDIA_SOURCE_DATE_EPOCH=1749399863
ARG FFMPEG_BINARY_SHA256=d003df00d4930e60f5e534c07e12064ec0abd5677fc638fd7202edfb01f8bc3f
ARG FFPROBE_BINARY_SHA256=4df45ffe1eb3ebed143136499616e894a06e6c0d3ccb3abf8e72f84bb1ac8c6e

FROM ${BUN_IMAGE} AS web-deps
WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
# BuildKit may schedule independent stages together. This locked, empty cache
# mount is a builder-local semaphore for the CPU/memory-heavy build steps.
RUN --mount=type=cache,id=kkai-newapi-build-gate-v1,target=/var/kkai-build-gate,sharing=locked \
    --mount=type=cache,id=kkai-newapi-bun-v1,target=/root/.bun/install/cache,sharing=locked \
    bun install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1

FROM web-deps AS web-default

ARG APP_VERSION
ARG KKAI_SCHEMA_CONTRACT
COPY web/default ./default
RUN --mount=type=cache,id=kkai-newapi-build-gate-v1,target=/var/kkai-build-gate,sharing=locked \
    test -n "${APP_VERSION}" \
    && cd default \
    && DISABLE_ESLINT_PLUGIN=true VITE_REACT_APP_VERSION="${APP_VERSION}" VITE_KKAI_SCHEMA_CONTRACT="${KKAI_SCHEMA_CONTRACT}" bun run build

FROM web-deps AS web-classic

ARG APP_VERSION
COPY web/classic ./classic
RUN --mount=type=cache,id=kkai-newapi-build-gate-v1,target=/var/kkai-build-gate,sharing=locked \
    test -n "${APP_VERSION}" \
    && cd classic \
    && VITE_REACT_APP_VERSION="${APP_VERSION}" bun run build

FROM --platform=$BUILDPLATFORM ${GO_BUILD_IMAGE} AS backend

ARG APP_VERSION
ARG KKAI_SCHEMA_CONTRACT
ARG KKAI_FRONTEND_MODE
ARG SOURCE_REVISION
ARG GO_BUILD_PARALLELISM=4
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0 \
    GO111MODULE=on \
    GOEXPERIMENT=greenteagc \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH}

WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,id=kkai-newapi-go-mod-v1,target=/go/pkg/mod,sharing=locked \
    go mod download && go mod verify
COPY . .
COPY --from=web-default /build/web/default/dist ./web/default/dist
COPY --from=web-classic /build/web/classic/dist ./web/classic/dist
RUN --mount=type=cache,id=kkai-newapi-go-mod-v1,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=kkai-newapi-go-build-v1,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,id=kkai-newapi-build-gate-v1,target=/var/kkai-build-gate,sharing=locked \
    test -n "${APP_VERSION}" \
    && test -n "${SOURCE_REVISION}" \
    && case "${GO_BUILD_PARALLELISM}" in \
      [1-9]|[1-5][0-9]|6[0-4]) ;; \
      *) echo "invalid GO_BUILD_PARALLELISM: ${GO_BUILD_PARALLELISM}" >&2; exit 1 ;; \
    esac \
    && case "${KKAI_SCHEMA_CONTRACT}" in \
      feature) export GOFLAGS= ;; \
      bridge) export GOFLAGS=-tags=kkai_bridge ;; \
      *) echo "invalid KKAI schema contract: ${KKAI_SCHEMA_CONTRACT}" >&2; exit 1 ;; \
    esac \
    && test "${KKAI_FRONTEND_MODE}" = embedded \
    && go build -p "${GO_BUILD_PARALLELISM}" -trimpath \
      -ldflags "-s -w -buildid= -X 'github.com/QuantumNous/new-api/common.Version=${APP_VERSION}' -X 'github.com/QuantumNous/new-api/common.SchemaManagementMode=external'" \
      -o /out/new-api . \
    && go build -p "${GO_BUILD_PARALLELISM}" -trimpath \
      -ldflags "-s -w -buildid= -X 'github.com/QuantumNous/new-api/common.SchemaManagementMode=external'" \
      -o /out/kkai-migrate ./cmd/kkai-migrate \
    && go build -p "${GO_BUILD_PARALLELISM}" -trimpath \
      -ldflags "-s -w -buildid= -X 'github.com/QuantumNous/new-api/common.SchemaManagementMode=external'" \
      -o /out/kkai-video-archive-once ./cmd/kkai-video-archive-once

FROM --platform=$BUILDPLATFORM ${GO_BUILD_IMAGE} AS backend-external

ARG APP_VERSION
ARG KKAI_SCHEMA_CONTRACT
ARG KKAI_FRONTEND_MODE
ARG SOURCE_REVISION
ARG GO_BUILD_PARALLELISM=4
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0 \
    GO111MODULE=on \
    GOEXPERIMENT=greenteagc \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH}

WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,id=kkai-newapi-go-mod-v1,target=/go/pkg/mod,sharing=locked \
    go mod download && go mod verify
COPY . .
RUN --mount=type=cache,id=kkai-newapi-go-mod-v1,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=kkai-newapi-go-build-v1,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,id=kkai-newapi-build-gate-v1,target=/var/kkai-build-gate,sharing=locked \
    test -n "${APP_VERSION}" \
    && test -n "${SOURCE_REVISION}" \
    && test "${KKAI_FRONTEND_MODE}" = external \
    && case "${GO_BUILD_PARALLELISM}" in \
      [1-9]|[1-5][0-9]|6[0-4]) ;; \
      *) echo "invalid GO_BUILD_PARALLELISM: ${GO_BUILD_PARALLELISM}" >&2; exit 1 ;; \
    esac \
    && case "${KKAI_SCHEMA_CONTRACT}" in \
      feature) export GOFLAGS=-tags=external_frontend ;; \
      bridge) export GOFLAGS=-tags=kkai_bridge,external_frontend ;; \
      *) echo "invalid KKAI schema contract: ${KKAI_SCHEMA_CONTRACT}" >&2; exit 1 ;; \
    esac \
    && go build -p "${GO_BUILD_PARALLELISM}" -trimpath \
      -ldflags "-s -w -buildid= -X 'github.com/QuantumNous/new-api/common.Version=${APP_VERSION}' -X 'github.com/QuantumNous/new-api/common.SchemaManagementMode=external'" \
      -o /out/new-api . \
    && go build -p "${GO_BUILD_PARALLELISM}" -trimpath \
      -ldflags "-s -w -buildid= -X 'github.com/QuantumNous/new-api/common.SchemaManagementMode=external'" \
      -o /out/kkai-migrate ./cmd/kkai-migrate \
    && go build -p "${GO_BUILD_PARALLELISM}" -trimpath \
      -ldflags "-s -w -buildid= -X 'github.com/QuantumNous/new-api/common.SchemaManagementMode=external'" \
      -o /out/kkai-video-archive-once ./cmd/kkai-video-archive-once

FROM --platform=$BUILDPLATFORM ${GO_BUILD_IMAGE} AS runtime-tools

ARG GO_BUILD_PARALLELISM=4
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
WORKDIR /tools
COPY --from=kkai_image go.mod go.sum ./
RUN --mount=type=cache,id=kkai-newapi-go-mod-v1,target=/go/pkg/mod,sharing=locked \
    go mod download && go mod verify
COPY --from=kkai_image cmd ./cmd
COPY --from=kkai_image internal ./internal
RUN --mount=type=cache,id=kkai-newapi-go-mod-v1,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=kkai-newapi-go-build-v1,target=/root/.cache/go-build,sharing=locked \
    --mount=type=cache,id=kkai-newapi-build-gate-v1,target=/var/kkai-build-gate,sharing=locked \
    case "${GO_BUILD_PARALLELISM}" in \
      [1-9]|[1-5][0-9]|6[0-4]) ;; \
      *) echo "invalid GO_BUILD_PARALLELISM: ${GO_BUILD_PARALLELISM}" >&2; exit 1 ;; \
    esac \
    && go build -p "${GO_BUILD_PARALLELISM}" -trimpath -ldflags '-s -w -buildid=' -o /out/new-api-entrypoint ./cmd/entrypoint \
    && go build -p "${GO_BUILD_PARALLELISM}" -trimpath -ldflags '-s -w -buildid=' -o /out/newapi-risk-guard ./cmd/riskguard

FROM ${BUSYBOX_IMAGE} AS health-tools

FROM ${GO_IMAGE} AS video-media-build

ARG GO_IMAGE
ARG MEDIA_BUILD_PARALLELISM=2
ARG FFMPEG_VERSION
ARG FFMPEG_SOURCE_SHA256
ARG X264_REVISION
ARG X264_SOURCE_SHA256
ARG VIDEO_MEDIA_SOURCE_DATE_EPOCH
COPY --from=kkai_image ffmpeg/BUILD-PACKAGES.lock /opt/video-media-build/BUILD-PACKAGES.lock
RUN xargs apk add --no-cache < /opt/video-media-build/BUILD-PACKAGES.lock
COPY --from=kkai_image ffmpeg/build.sh /opt/video-media-build/build.sh
COPY --from=kkai_image ffmpeg/verify-static.sh /opt/video-media-build/verify-static.sh
COPY --from=kkai_image ffmpeg/verify.sh /opt/video-media-build/verify.sh
COPY --from=kkai_image ffmpeg/PROVENANCE.md /opt/video-media-build/PROVENANCE.md
RUN --mount=type=cache,id=kkai-newapi-build-gate-v1,target=/var/kkai-build-gate,sharing=locked \
    chmod 0755 \
      /opt/video-media-build/build.sh \
      /opt/video-media-build/verify-static.sh \
      /opt/video-media-build/verify.sh \
    && FFMPEG_VERSION="${FFMPEG_VERSION}" \
       FFMPEG_SOURCE_SHA256="${FFMPEG_SOURCE_SHA256}" \
       X264_REVISION="${X264_REVISION}" \
       X264_SOURCE_SHA256="${X264_SOURCE_SHA256}" \
       SOURCE_DATE_EPOCH="${VIDEO_MEDIA_SOURCE_DATE_EPOCH}" \
       MEDIA_BUILD_IMAGE="${GO_IMAGE}" \
       MEDIA_BUILD_PARALLELISM="${MEDIA_BUILD_PARALLELISM}" \
       MEDIA_MATERIAL_DIR=/opt/video-media-build \
       /opt/video-media-build/build.sh

FROM ${GO_IMAGE} AS video-media-audit
ARG FFMPEG_BINARY_SHA256
ARG FFPROBE_BINARY_SHA256
COPY --from=video-media-build /package /package
RUN printf '%s  %s\n%s  %s\n' \
      "${FFMPEG_BINARY_SHA256}" /package/usr/local/bin/ffmpeg \
      "${FFPROBE_BINARY_SHA256}" /package/usr/local/bin/ffprobe \
      | sha256sum -c - \
    && /package/licenses/ffmpeg/build/verify.sh /package

FROM scratch AS video-media-package
COPY --from=video-media-audit /package /package

FROM ${DISTROLESS_IMAGE} AS runtime-base

ARG APP_VERSION
ARG KKAI_SCHEMA_CONTRACT
ARG SOURCE_REVISION
ARG FFMPEG_VERSION
ARG FFMPEG_BINARY_SHA256
ARG FFPROBE_BINARY_SHA256
LABEL org.opencontainers.image.title="KKAI New API" \
      org.opencontainers.image.description="KKAI production New API image" \
      org.opencontainers.image.source="https://github.com/Wangjunkai-1996/new-api-kkai" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}" \
      org.opencontainers.image.licenses="AGPL-3.0 AND GPL-3.0-or-later" \
      io.kkrich.schema-contract="${KKAI_SCHEMA_CONTRACT}"

COPY --from=runtime-tools --chown=10007:10007 /out/new-api-entrypoint /new-api-entrypoint
COPY --from=runtime-tools --chown=10007:10007 /out/newapi-risk-guard /newapi-risk-guard
COPY --from=health-tools --chown=10007:10007 /bin/busybox /usr/bin/wget
COPY --from=video-media-package --chown=0:0 /package/usr/local/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --from=video-media-package --chown=0:0 /package/usr/local/bin/ffprobe /usr/local/bin/ffprobe
COPY --from=video-media-package --chown=0:0 /package/licenses/ffmpeg /licenses/ffmpeg
COPY --chown=0:0 LICENSE NOTICE THIRD-PARTY-LICENSES.md /licenses/

ENV TZ=Asia/Shanghai \
    VIDEO_STUDIO_FFMPEG_PATH=/usr/local/bin/ffmpeg \
    VIDEO_STUDIO_FFPROBE_PATH=/usr/local/bin/ffprobe \
    VIDEO_STUDIO_FFMPEG_VERSION=${FFMPEG_VERSION} \
    VIDEO_STUDIO_FFMPEG_SHA256=${FFMPEG_BINARY_SHA256} \
    VIDEO_STUDIO_FFPROBE_SHA256=${FFPROBE_BINARY_SHA256}
WORKDIR /data
USER 10007:10007
EXPOSE 3000
STOPSIGNAL SIGTERM
ENTRYPOINT ["/new-api-entrypoint"]

FROM runtime-base AS runtime-embedded

LABEL io.kkrich.frontend-mode="embedded"
ENV FRONTEND_MODE=embedded
COPY --from=backend --chown=10007:10007 /out/new-api /new-api
COPY --from=backend --chown=10007:10007 /out/kkai-migrate /kkai-migrate
COPY --from=backend --chown=10007:10007 /out/kkai-video-archive-once /kkai-video-archive-once

FROM runtime-base AS runtime-external

LABEL io.kkrich.frontend-mode="external"
ENV FRONTEND_MODE=external
COPY --from=backend-external --chown=10007:10007 /out/new-api /new-api
COPY --from=backend-external --chown=10007:10007 /out/kkai-migrate /kkai-migrate
COPY --from=backend-external --chown=10007:10007 /out/kkai-video-archive-once /kkai-video-archive-once

# Keep an unqualified build backward-compatible with the existing embedded
# production image. The manual release script selects the external target
# explicitly when requested.
FROM runtime-embedded AS runtime
