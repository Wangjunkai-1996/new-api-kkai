# KKAI production image

This directory contains the Dockerfile and runtime helpers used for the KKAI
New API image.

Production images are built manually from a clean `production/kkrich` checkout
with `scripts/kkai/build-manual-release.sh`. The script emits one Linux AMD64
Docker archive and a metadata file under `.local-releases/`.

The application and `/kkai-migrate` are compiled with
`common.SchemaManagementMode=external`. The image therefore cannot run GORM
AutoMigrate when it starts in the read-only idle slot. Database maintenance is
separate from ordinary application delivery.

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

This replaces only the inactive blue/green slot and exposes the candidate on
the production host's loopback interface. It does not switch public traffic;
promotion remains a separate operator action after acceptance. No GitHub
workflow builds or deploys KKAI production images.
