# KKAI production image

This directory contains the Dockerfile and runtime helpers used for the KKAI
New API image.

Production images are built manually from a clean `production/kkrich` checkout
with `scripts/kkai/build-manual-release.sh`. The script emits one Linux AMD64
Docker archive and a metadata file under `.local-releases/`.

There is no production-safe profile default for every live schema. Production
commands must pass `--schema-contract` explicitly after the live schema has
been established:

- `bridge` is the current `(7,8,7)` profile. Use it for an application-only
  release while the database remains at v7.
- `feature` is the current `(8,8,8)` profile. Use it only after schema v8 has
  been independently observed.

Without exact v8 evidence, select `bridge`; do not omit the explicit profile or
use generic deployment preflight to infer the live schema.

Build the v7-to-v8 bridge profile with:

```bash
scripts/kkai/build-manual-release.sh --schema-contract bridge
```

Use `--schema-contract feature` only after the v8 schema gate passes. The
profile is recorded in release metadata and in the image's
`io.kkrich.schema-contract` label. The staging client validates the metadata
value and forwards it to the production controller, which must match it against
the loaded image before accepting the candidate. That generic preflight does
not observe the database schema, so its `ready` result is not compatibility
evidence. The profile cannot be changed at runtime.

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
scripts/kkai/build-manual-release.sh --schema-contract bridge
```

Stage the metadata file explicitly with:

```bash
scripts/kkai/deploy-manual-release.sh --stage .local-releases/<version>.json
```

This replaces only the inactive blue/green slot and exposes the candidate on
the production host's loopback interface. It does not switch public traffic;
promotion remains a separate operator action after acceptance. Production
images are built and deployed only through this local/manual path.
