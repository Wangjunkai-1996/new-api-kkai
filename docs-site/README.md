# KKRICH documentation site

This directory is the maintained VitePress source for
`https://api.kkrich.ltd/docs/`.

The original July 2026 source was built from a temporary, non-Git directory and
was later deleted. The non-Seedance pages in this directory were recovered from
the public production HTML on 2026-08-25. Their source uses `v-pre` HTML to
preserve the live rendering without pretending that the original Markdown was
recoverable. New and actively maintained pages should use normal Markdown.

## Development

```bash
bun install
bun run docs:dev
bun run docs:build
bun run docs:preview
```

The production base path is always `/docs/`. A build writes to
`docs/.vitepress/dist-docs-path`.

## Recovery script

`bun run recover:production` refreshes mechanically recovered pages and public
assets from the live site. It deliberately skips these maintained pages:

- `docs/index.md`
- `docs/apps/index.md`
- `docs/apps/image2.md`
- `docs/apps/codex-image.md`
- `docs/api/index.md`
- `docs/api/image-generation.md`
- `docs/apps/seedance.md`
- `docs/api/video-generation.md`
- `docs/support/seedance-video.md`

Pass `--include-curated` only when intentionally replacing those files with the
current production rendering.

## Production note

The live static `docs/` directory may contain a separately managed subscription
YAML file. When present, a production candidate must preserve that file
byte-for-byte, including its owner, mode, and hash. Building this package does
not authorize or perform a production deployment.
