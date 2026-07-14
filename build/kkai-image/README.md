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

The smoke test applies KKAI migrations over stdin, starts an isolated leader
with write background tasks disabled so the existing upstream schema path can
initialize an empty fixture, then force-recreates the application as the
writer-disabled `serving` role. This fixture behavior is not a production
migration procedure.

Local builds are development evidence only. A dirty local image must never be
used as a production release artifact.
