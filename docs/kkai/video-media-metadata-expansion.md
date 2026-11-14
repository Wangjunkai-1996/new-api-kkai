# Video media metadata expansion

The multimodal Video Studio workflow stores ffprobe evidence in
`kkai_video_assets.media_metadata`. This is an additive expansion beside the
canonical migration ledger; it does not change canonical schema version 21.

## Current release gate (confirmed locally)

The current application contract requires both `rc49-online-v1` and
`video-media-metadata-v1` in `required_expansions`. The application's manual
deployment contract pins controller revision
`ec9a76512f84086e48936cdd10bf96705e33ccb0`; its objects are readable in the
confirmed local `../kkai-infra` checkout. That revision only accepts the frozen
`rc49-online-v1` expansion: its manual deploy contract comparison hard-codes
that singleton, and its schema runtime proof mount has no operation or proof
path for `video-media-metadata-v1`. Therefore this application tree cannot pass
the ordinary stage image-contract gate with that controller. Generic preflight
does not inspect the candidate image contract and cannot clear this blocker.
This is local source/pin evidence, not a claim about the installed controller,
live schema or a production receipt.

The blocker is resolved only by installing and pinning a controller revision
that explicitly validates this expansion's digest, proof protocol and runtime
contract, then completing the applicable read-only schema evidence. Do not
work around it by removing the expansion from the image contract, treating the
application CLI as an installed controller, or running the new `apply` command
directly on production.

Before an operator authorizes a production change, the installed controller
must generate a root-owned, mode `0400`, read-only proof at
`/run/video-media-metadata-expansion/proof.json`. The proof binds the
canonical-21 source digest, expansion plan checksum and digest, PostgreSQL
database identity, immutable image identities, and active/rollback/writer
process evidence. The application image must use external schema management.

Read-only inspection:

```sh
./kkai-migrate --dsn "$KKAI_MIGRATION_DSN" --json \
  --video-media-metadata-expand plan
./kkai-migrate --dsn "$KKAI_MIGRATION_DSN" --json \
  --video-media-metadata-expand status
./kkai-migrate --dsn "$KKAI_MIGRATION_DSN" --json \
  --video-media-metadata-expand check
```

The mutation command is intentionally proof-gated:

```sh
./kkai-migrate --dsn "$KKAI_MIGRATION_DSN" --json \
  --video-media-metadata-expand apply \
  --video-media-metadata-controller-proof \
  /run/video-media-metadata-expansion/proof.json
```

The library also supports `Options{DryRun:true}` and explicit isolated/offline
test execution. No production migration is implied by the CLI or this
document. A receipt table created before its `building` row is recoverable;
an existing `media_metadata` column without a matching receipt is rejected and
cannot be adopted.
