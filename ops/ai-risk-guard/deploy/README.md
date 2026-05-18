# ai-risk-guard deployment scripts

These scripts are local artifacts for controlled deployment and rollback. They are not executed automatically and should be run only on the intended target host or a staging clone.

## Artifact layout

By default, `ARTIFACT_DIR` is the local `ai-risk-guard/` directory. It expects:

```text
ai-risk-guard/
  README.md
  bin/ai-risk-guardd
  bin/riskctl
  nginx/ai-risk-guard.http.conf
  nginx/ai-risk-guard.location.conf
  nginx/ai_risk_guard_access.lua
  rules/pre-risk-rules.json
  systemd/ai-risk-guard.service
```

## Deploy

```bash
CONFIRM_DEPLOY=install-ai-risk-guard \
ARTIFACT_DIR=/path/to/artifacts \
./ai-risk-guard/deploy/deploy-ai-risk-guard.sh
```

The deployment script:

- Backs up each target file into `/var/backups/ai-risk-guard/<timestamp>`.
- Installs the daemon, `riskctl`, rule file, Nginx snippets/Lua file, README, and systemd unit.
- Runs `nginx -t`.
- Reloads Nginx only after `nginx -t` succeeds.
- Does not restart `new-api` or `ai-bridge`.
- Skips `ai-risk-guard` restart unless `RESTART_AI_RISK_GUARD=1` is explicitly set.

## Rollback

Use the backup path printed by deploy:

```bash
CONFIRM_ROLLBACK=rollback-ai-risk-guard \
BACKUP_DIR=/var/backups/ai-risk-guard/20260517-120000 \
./ai-risk-guard/deploy/rollback-ai-risk-guard.sh
```

Rollback restores files from the backup manifest, runs `nginx -t`, and reloads Nginx only after the config test succeeds.
