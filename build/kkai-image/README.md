# KKAI production image

This directory contains the Dockerfile and runtime helpers used for the KKAI
New API image.

Production images are built manually from a clean `production/kkrich` checkout
with `scripts/kkai/build-manual-release.sh`. The script emits one Linux AMD64
Docker archive and a metadata file under `.local-releases/`.

The image builds the single frontend rooted at `web/` and installs its output
at `web/dist`. The backend dependency stage also includes the local
`relaykit/go.mod` manifest before downloading modules so Docker cache misses do
not break the local module replacement.

The default image requires the complete KKAI schema v8. The legacy `bridge`
selector remains available so existing build automation fails closed, but this
source revision compiles both profiles with the same v8-only runtime contract:

```bash
scripts/kkai/build-manual-release.sh --schema-contract bridge
```

It is not a v7-to-v8 transition image. Such a bridge must be built separately
from the previously deployed v7-compatible source. Use `--schema-contract
feature` only after the v8 schema gate passes. The profile is recorded in
release metadata and in the image's
`io.kkrich.schema-contract` label. The staging client validates the metadata
value and forwards it to the production controller, which must match it against
the loaded image before accepting the candidate. The profile cannot be changed
at runtime.

The application, `/kkai-migrate`, and `/kkai-video-archive-once` are compiled
with `common.SchemaManagementMode=external`. The image therefore cannot run GORM
AutoMigrate in any production role. Database maintenance is separate from
ordinary application delivery.

When Docker build stages require the operator workstation proxy, pass it
explicitly without changing the image definition:

```bash
BUILD_HTTP_PROXY=http://host.docker.internal:17897 \
BUILD_HTTPS_PROXY=http://host.docker.internal:17897 \
BUILDX_BUILDER=kkai-mirror-builder \
scripts/kkai/build-manual-release.sh
```

Stage the metadata file explicitly with:

```bash
scripts/kkai/deploy-manual-release.sh --stage .local-releases/<version>.json
```

This replaces only the inactive blue/green slot. The candidate runs in
`serving` mode with background tasks disabled and the production writer database
identity, so acceptance requests can change production business data. It is
reachable only through a private Docker-network proxy and an explicit SSH tunnel
bound to the Mac's loopback interface. Production publishes no candidate host port.
Staging does not switch public traffic. Promotion remains a separate
operator action after acceptance. No GitHub workflow builds or deploys KKAI
production images.
