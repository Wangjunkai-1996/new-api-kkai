# KKAI production image

This directory contains the Dockerfile and runtime helpers used for the KKAI
New API image.

A push with runtime changes on `production/kkrich` builds the Linux AMD64 image
once, pushes version and source-SHA tags for the same digest, signs that digest,
and sends `source_sha`, `version`, and `digest` to the private infrastructure
repository. Markdown-only pushes do not start the workflow.

The application and `/kkai-migrate` are compiled with
`common.SchemaManagementMode=external`. The image therefore cannot run GORM
AutoMigrate when it starts in the read-only idle slot. Database maintenance is
separate from ordinary application delivery.

Formal images are built only by `.github/workflows/kkai-production-image.yml`.
Local builds are for development and must not be published as production
releases.
