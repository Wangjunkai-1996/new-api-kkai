# syntax=docker/dockerfile:1.8@sha256:dde28735fbb848299b7a43b7561d828b89c76f84f745c31896a7a7efbf93851a

ARG BUN_IMAGE=oven/bun:1.3.13@sha256:bb35eafd10b2e969809384850ff0474ba36a491239d715864bc87787b4cdf0a4
ARG GO_IMAGE=golang:1.26.5-alpine@sha256:111d79159b2326f7e80c4a4706e1ba166acb0e2611df853955f3621828cd49e8
ARG BUSYBOX_IMAGE=busybox:1.37.0-musl@sha256:6be969a074d06073a62ad8ccfd2ad6ec2790cae25d519d8df255ba24819d96f0
ARG DISTROLESS_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:7a2bd171a18bdd39a4729600d0dca5f16e779d41156a6908b4f8a9a289e76d92

FROM ${BUN_IMAGE} AS web-default

ARG APP_VERSION
WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
RUN bun install --frozen-lockfile
COPY web/default ./default
RUN test -n "${APP_VERSION}" \
    && cd default \
    && DISABLE_ESLINT_PLUGIN=true VITE_REACT_APP_VERSION="${APP_VERSION}" bun run build

FROM ${BUN_IMAGE} AS web-classic

ARG APP_VERSION
WORKDIR /build/web
COPY web/package.json web/bun.lock ./
COPY web/default/package.json ./default/package.json
COPY web/classic/package.json ./classic/package.json
RUN bun install --filter ./classic --frozen-lockfile
COPY web/classic ./classic
RUN test -n "${APP_VERSION}" \
    && cd classic \
    && VITE_REACT_APP_VERSION="${APP_VERSION}" bun run build

FROM ${GO_IMAGE} AS backend

ARG APP_VERSION
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ENV CGO_ENABLED=0 \
    GO111MODULE=on \
    GOEXPERIMENT=greenteagc \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH}

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
COPY --from=web-default /build/web/default/dist ./web/default/dist
COPY --from=web-classic /build/web/classic/dist ./web/classic/dist
RUN go test ./...
RUN test -n "${APP_VERSION}" \
    && go build -trimpath \
      -ldflags "-s -w -buildid= -X 'github.com/QuantumNous/new-api/common.Version=${APP_VERSION}'" \
      -o /out/new-api . \
    && go build -trimpath -ldflags '-s -w -buildid=' \
      -o /out/kkai-migrate ./cmd/kkai-migrate

FROM ${GO_IMAGE} AS runtime-tools

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
WORKDIR /tools
COPY --from=kkai_image go.mod go.sum ./
RUN go mod download && go mod verify
COPY --from=kkai_image cmd ./cmd
COPY --from=kkai_image internal ./internal
RUN go test ./... \
    && go build -trimpath -ldflags '-s -w -buildid=' -o /out/new-api-entrypoint ./cmd/entrypoint \
    && go build -trimpath -ldflags '-s -w -buildid=' -o /out/new-api-canary-probe ./cmd/canaryprobe \
    && go build -trimpath -ldflags '-s -w -buildid=' -o /out/newapi-risk-guard ./cmd/riskguard

FROM ${BUSYBOX_IMAGE} AS health-tools

FROM ${DISTROLESS_IMAGE}

ARG APP_VERSION
ARG SOURCE_REVISION
LABEL org.opencontainers.image.title="KKAI New API" \
      org.opencontainers.image.description="Hardened KKAI production New API image" \
      org.opencontainers.image.source="https://github.com/Wangjunkai-1996/new-api-kkai" \
      org.opencontainers.image.version="${APP_VERSION}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}" \
      org.opencontainers.image.licenses="AGPL-3.0"

COPY --from=backend --chown=10007:10007 /out/new-api /new-api
COPY --from=backend --chown=10007:10007 /out/kkai-migrate /kkai-migrate
COPY --from=runtime-tools --chown=10007:10007 /out/new-api-entrypoint /new-api-entrypoint
COPY --from=runtime-tools --chown=10007:10007 /out/new-api-canary-probe /new-api-canary-probe
COPY --from=runtime-tools --chown=10007:10007 /out/newapi-risk-guard /newapi-risk-guard
COPY --from=health-tools --chown=10007:10007 /bin/busybox /usr/bin/wget
COPY --chown=0:0 LICENSE NOTICE THIRD-PARTY-LICENSES.md /licenses/

ENV TZ=Asia/Shanghai
WORKDIR /data
USER 10007:10007
EXPOSE 3000
STOPSIGNAL SIGTERM
ENTRYPOINT ["/new-api-entrypoint"]
