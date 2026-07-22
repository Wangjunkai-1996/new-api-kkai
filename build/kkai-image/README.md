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

Deploy the metadata file explicitly with
`scripts/kkai/deploy-manual-release.sh`. No GitHub workflow builds or deploys
KKAI production images.
