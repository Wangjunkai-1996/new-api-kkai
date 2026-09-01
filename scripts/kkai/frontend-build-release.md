# Independent Frontend Artifact

`frontend-build-release.sh` builds the `default` and `classic` Rsbuild
bundles as a separate, immutable artifact. It is a packaging command only: it
does not upload files, change `current` or `previous` pointers, restart a
service, or call an infrastructure deployment controller.

## Production invocation

Run it from the clean local `production/kkrich` checkout after selecting the
schema contract from current production evidence. The backend release values
are required so an operator can verify the frontend/backend pair before a
promotion:

```bash
scripts/kkai/frontend-build-release.sh \
  --schema-contract bridge \
  --api-contract 1 \
  --release-id kkai-frontend-20260901.175000-abcdef123 \
  --backend-release-id kkai-prod-20260901.175000-abcdef123 \
  --backend-source-sha <40-character-backend-sha> \
  --output-dir .local-releases/frontend
```

The command runs the following frozen dependency install before building both
themes:

```text
bun install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1
bun run build -- --dist-path <temporary-directory>
```

`--skip-install` is restricted to explicitly local runs (`--allow-dirty` or
`--allow-non-production`) and should not be used for a production release.

The browser artifact always uses relative API URLs. The production edge must
proxy NewAPI paths and route `/invitations/api/` to the independent KKAI
Invitations service, rewriting the public prefix to `/api/` upstream. For local
Rsbuild development, set `VITE_INVITATIONS_API_URL` to that service; it
defaults to `http://localhost:6212`, applies the same rewrite, and is used only
by the dev proxy.

The artifact intentionally does not contain a `frontend_mode` field. It records
the frontend source and the exact backend release/source/schema/API coordinates;
`frontend_mode` is a coordinated backend/edge contract. Pair an artifact with a
backend release whose metadata, image label, and `FRONTEND_MODE` environment
entry all agree. An `embedded` backend must be paired with the embedded edge
configuration; an `external` backend must be paired with an installed and
verified artifact plus the external edge configuration. The controller checks
the backend manifest before installation, so do not install an artifact against
a release with different source, version, image, schema, or API coordinates.

## Output

The output directory contains an archive, outer metadata, and an extracted
release directory for local inspection:

```text
frontend-releases/<release-id>/
  default/
  classic/
  LICENSE
  NOTICE
  THIRD-PARTY-LICENSES.md
  frontend.json
  release-pair.json
  manifest.sha256
<release-id>.tar.gz
<release-id>.json
```

The archive and extracted directory are created in a temporary sibling and
renamed into place only after all builds, metadata, hashes, and archive checks
pass. A later rename failure is cleaned up by the exit trap. The script never
creates or changes `current`/`previous`; pointer changes belong to the pinned
infrastructure controller.

`frontend.json` records the frontend source SHA, paired backend values, API and
schema contracts, selected themes, Bun version, lockfile SHA-256, and the
relative API policy. `release-pair.json` is the promotion-time compatibility
record; it deliberately does not replace the backend/edge mode contract.
`manifest.sha256` covers every release file except the manifest itself,
including the three legal-notice files. The outer JSON additionally records the
archive and manifest digests.

## Reproducible/testable interface

Pass `--release-id` and `--build-timestamp` to make metadata stable. The
`--source-sha`, `--source-root`, `--bun-bin`, `--git-bin`, `--jq-bin`,
`--tar-bin`, and `--lock-dir` options (and their `KKAI_FRONTEND_*` environment
counterparts) allow deterministic test doubles without changing production
behavior. `--dry-run` validates the arguments and prints install/build/archive
paths without running Bun or writing output.

The script verifies the source commit, branch, and clean worktree before the
build and checks the worktree again after the build. Local experiments can
explicitly opt out with `--allow-non-production` and/or `--allow-dirty`.

## Mode switch and rollback

Use the pinned frontend controller for every artifact install and pointer
change. For `embedded` to `external`, keep the backend embedded while the
matching artifact is installed and checked, then:

1. Validate and install the artifact; verify its selected theme, API proxy,
   login, cookies, SSE, and media paths through the private edge path.
2. Converge the platform manifest to `frontend_mode: external` and verify the
   public edge serves the artifact and proxies every approved NewAPI prefix.
3. Build, stage, and promote a backend release with
   `--frontend-mode external`; the NewAPI planner requires the edge manifest to
   already report `external` and rejects a mode-only reuse of an old release.
4. Verify the candidate and public status before treating the pair as active.

For the reverse switch, install/verify the embedded-capable backend first, then
converge the edge back to `embedded`. If either side fails, restore the backend
to `embedded` before restoring the edge mode, and only then move the frontend
`current` pointer to the verified `previous` artifact. Use controller rollback
operations for these changes; never edit release files or symlinks by hand.

Run the focused regression test with:

```bash
scripts/kkai/frontend-build-release_test.sh
```

The test mocks Bun, checks frozen install forwarding, both-theme output,
metadata, legal-file checksums, archive contents, single-theme selection,
duplicate release rejection, dry-run non-mutation, and cleanup after a failed
theme build.
