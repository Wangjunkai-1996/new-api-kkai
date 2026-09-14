# Independent Frontend Artifact

`frontend-build-release.sh` builds the modern `default` Rsbuild bundle as a
separate, immutable production artifact. It is a packaging command only: it
does not upload files, change `current` or `previous` pointers, restart a
service, or call an infrastructure deployment controller.

## Production invocation

Run it from the clean local `production/kkrich` checkout after selecting the
schema contract from current production evidence. For local preparation, build
the external backend first and take its release/source/schema values from the
exact generated metadata and its image ID from the checksummed archive. Before
installation, recheck every value against the immutable staged backend manifest.
Production builds require the backend image digest so promotion can verify the
complete pair:

```bash
scripts/kkai/frontend-build-release.sh \
  --schema-contract bridge \
  --api-contract 1 \
  --theme default \
  --release-id kkai-frontend-20260901.175000-abcdef123 \
  --backend-release-id kkai-prod-20260901.175000-abcdef123 \
  --backend-source-sha <40-character-backend-sha> \
  --backend-image-digest <exact-sha256-image-id> \
  --output-dir .local-releases/frontend
```

The command installs only the selected workspace and builds the modern UI:

```text
bun install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1 --filter ./default
bun run build -- --dist-path <temporary-directory>
```

`--skip-install` is restricted to explicitly local runs (`--allow-dirty` or
`--allow-non-production`) and should not be used for a production release.

Production builds accept only `--theme default`, which is also the default
when no theme is specified. `classic` source stays available for upstream
integration; `--theme classic` and `--theme both` require an explicit local
`--allow-dirty` or `--allow-non-production` run. Existing artifacts remain
immutable and must retain their original theme metadata for rollback checks.

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
entry all agree. An `embedded` backend supports either edge mode during the
transition; an `external` backend requires an installed and verified artifact
plus an effective external edge configuration. The controller checks
the backend manifest before installation, so do not install an artifact against
a release with different source, version, image, schema, or API coordinates.

## Output

The output directory contains an archive, outer metadata, and an extracted
release directory for local inspection:

```text
frontend-releases/<release-id>/
  default/
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
change. For `embedded` to `external`, keep the active backend embedded while
the external backend remains staged, then:

1. Complete the local infrastructure gates, pin the exact infrastructure commit
   in the application contract, and build a unique external/bridge backend plus
   its exact paired frontend artifact. Local builds do not require staging.
2. After production preparation is authorized, install the matching controller
   and ACME lock support, then stage the backend. Respect the previous release's
   24-hour rollback retention before replacing its slot. Stage may defer the
   live Edge-mode gate and must leave the public router unchanged.
3. Validate and install the frontend with an explicit `--backend-manifest`
   pointing at that staged release while Edge is still embedded. Verify the
   selected release tree, pointer, theme entry page, and offline configuration.
   An artifact install changes its pointer; once Edge is external that is a
   public frontend change, not just an upload.
4. Follow `docs/runbooks/20-newapi-frontend.md` in the pinned infrastructure
   checkout to prepare the actual Edge plan. Confirm its SHA/diff, the one-time
   immutable-bind adoption exception, and any existing checksum drift before
   `apply-edge`. This activates a private static service through graceful HUP
   while retaining the Edge container and config inode. The ordinary
   `platform-edge` rebuilding path requires a separate maintenance decision.
   No backend canary may be active during adoption or restoration. Immediately
   verify the public static page, API/Invitations prefixes, login, cookies,
   existing SSE connections, and media paths.
5. Recheck the staged backend candidate, complete its normal canary, and
   promote. Promotion rechecks both the live Edge mode and the selected exact
   frontend/backend pair under the shared platform operation lock. Verify
   public status before treating the pair as active.

For the reverse switch, complete backend/canary rollback to a verified embedded
backend first, then use `rollback-edge` with the same accepted plan. Restore a
frontend pointer only through the controller after exact pair validation.
Never edit release files or symlinks by hand, or delete recovery evidence to
bypass the controller's guards.

Run the focused regression test with:

```bash
scripts/kkai/frontend-build-release_test.sh
```

The test mocks Bun, checks filtered frozen install forwarding, default-only
production selection, local legacy-theme compatibility, metadata, legal-file
checksums, archive contents,
duplicate release rejection, dry-run non-mutation, and cleanup after a failed
theme build.
