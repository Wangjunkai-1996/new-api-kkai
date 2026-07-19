# KKAI production image

This directory is the single source of truth for the hardened KKAI NewAPI
image. It is fork-owned build and runtime tooling, not upstream NewAPI source.

Production artifacts are built only by
`.github/workflows/kkai-production-image.yml` from the current committed HEAD
of `production/kkrich`. The workflow publishes an immutable Linux AMD64 image
to GHCR, signs its digest, emits BuildKit provenance and SBOM attestations,
runs a vulnerability gate, verifies every runtime binary, and exercises a
PostgreSQL 18 plus Redis Compose smoke test.

`.github/workflows/kkai-image-candidate.yml` builds the same Dockerfile on
GitHub-hosted Linux runners for rebuild and integration branches. Candidate
images are loaded only into the ephemeral runner for verification and smoke;
the workflow has no package-write permission and cannot publish or deploy.

The smoke test applies the PostgreSQL v3 fixture over stdin and uses the dedicated
host-only `cmd/newapi-schema-bootstrap` command to initialize the otherwise empty
application schema. The command performs only the canonical GORM schema bootstrap
and prerequisite validation; it does not start HTTP, Redis, background jobs, root
setup, or application runtime resources, and it is not copied into the image.
The formal image is compiled with `schema_management=external`, so none of its
runtime roles can invoke GORM AutoMigrate. After initialization, the test records
the physical schema and migration-ledger fingerprints, runs the v3 no-op path,
starts explicit `leader` and `serving` roles, and proves both fingerprints remain
unchanged. The PostgreSQL smoke never applies or emulates the MySQL-only v4 DDL.

Local builds are development evidence only. A dirty local image must never be
used as a production release artifact.
