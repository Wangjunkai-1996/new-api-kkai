# ai-risk-guard

Local-only guard artifacts for consuming high-risk Nginx Lua JSONL events and applying durable containment actions.

## Scope and defaults

- Event input: `/var/lib/ai-risk-guard/events.jsonl`
- ai-bridge cyber_policy fallback input: `/var/lib/ai-bridge/black_log`
- Evidence/state: `/var/lib/ai-risk-guard`
- IP blacklist: `/var/lib/ai-bridge/black_ip`
- Database: podman container `new-api-postgres`, database `newapi`, user `newapi`
- Token table: `tokens(id,user_id,key,status,...)`; keys are stored without the `sk-` prefix
- User table: `users(id,username,email,status,...)`
- Enabled status: `1`; disabled status: `2`

The daemon is intentionally local. These files do not SSH to, deploy to, or mutate remote production by themselves.

## Event format

Each line in the event file must be one JSON object. Field names are tolerant so Nginx Lua can evolve without daemon edits:

```json
{"case_id":"risk-20260517-001","ip":"203.0.113.10","api_key_hash":"d41d8cd98f00b204e9800998ecf8427e","api_key_suffix":"token123","user_id":123,"reason":"lua risk rule matched"}
```

Recognized IP fields: `ip`, `client_ip`, `remote_addr`, `remote_ip`, `source_ip`.

Recognized raw token fields, accepted only for legacy bridge-derived in-memory events: `token_key`, `token`, `key`, `api_key`, `api_key_full`, `authorization`. Nginx Lua events must not persist raw token fields. They should use `token_id`, or `api_key_hash` plus `api_key_suffix` for database lookup.

Recognized user fields: `user_id`, `userId`, `uid`.

## Behavior

For every valid event, `ai-risk-guardd`:

- Persists redacted evidence under `/var/lib/ai-risk-guard/cases/<case_id>/raw_event.redacted.jsonl`; raw Authorization/client token values are not written.
- Writes a redacted evidence view under `/var/lib/ai-risk-guard/cases/<case_id>/event.redacted.json`.
- Appends case metadata to `/var/lib/ai-risk-guard/cases.jsonl`.
- Adds the source IP to `/var/lib/ai-bridge/black_ip` only if absent.
- Looks up `tokens.key` without the `sk-` prefix, sets `tokens.status=2` if not already disabled, and then disables the owning `users.id`.
- Appends every durable containment action to `/var/lib/ai-risk-guard/actions.jsonl`.

The daemon also tails ai-bridge's `/var/lib/ai-bridge/black_log` for future
`cyber_policy_black` records. On first startup it begins at the current end of
that file, so historical investigation records are not replayed accidentally.
New upstream-confirmed cyber_policy records are converted into the same evidence
and ban workflow, using Nginx-provided `X-Real-IP`/`X-Forwarded-For` from the
trusted local ai-bridge hop.

Medium-confidence rules should use `enforcement: "observe"` with scored
`signals` and `score_threshold`. Observe rules write evidence only; they never
return local 403, update the in-memory block cache, or trigger durable account
actions. Only high-confidence `enforcement: "block"` rules should auto-block and
auto-ban.

The Nginx Lua scanner is versioned with the deployed rule bundle. Version
`2026-05-18.role-aware.v3` scans only endpoint-appropriate user text:

- `/v1/responses` and `/v1beta/responses`: current/user `input` text only;
  top-level `instructions`, system/developer messages, assistant history, tool
  output, and patch output are excluded.
- `/v1/chat/completions` and `/v1beta/chat/completions`: `role=user` message
  text only.
- `/v1/completions` and `/v1beta/completions`: `prompt` text only.
- `multipart/form-data`: text fields only; uploaded file parts are ignored.

The daemon uses a single process lock, keeps an offset file for JSONL consumption, writes append-only JSONL logs, and creates state files with restricted permissions.

## Install sketch

Review paths before installing:

```sh
sudo install -d -m 0750 /opt/ai-risk-guard/bin /var/lib/ai-risk-guard
sudo install -m 0750 ai-risk-guard/bin/ai-risk-guardd /opt/ai-risk-guard/bin/ai-risk-guardd
sudo install -m 0750 ai-risk-guard/bin/riskctl /opt/ai-risk-guard/bin/riskctl
sudo install -m 0644 ai-risk-guard/README.md /opt/ai-risk-guard/README.md
sudo install -m 0644 ai-risk-guard/systemd/ai-risk-guard.service /etc/systemd/system/ai-risk-guard.service
sudo systemctl daemon-reload
sudo systemctl enable --now ai-risk-guard.service
```

If Nginx writes the event file directly, create `/var/lib/ai-risk-guard/events.jsonl` with permissions that let its worker user append without broadening access more than needed, for example by using a shared group and `0660` on the event file. The daemon waits when the event file is absent; it does not create that input file.

## Commands

Process existing events once without DB writes:

```sh
sudo /opt/ai-risk-guard/bin/ai-risk-guardd --once --no-db
```

List cases:

```sh
sudo /opt/ai-risk-guard/bin/riskctl cases
```

Audit active durable actions and likely false positives:

```sh
sudo /opt/ai-risk-guard/bin/riskctl audit --days 7
```

Show evidence and actions for one case:

```sh
sudo /opt/ai-risk-guard/bin/riskctl show risk-20260517-001
```

Prune old per-case evidence directories, leaving append-only JSONL logs intact:

```sh
sudo /opt/ai-risk-guard/bin/riskctl prune --days 30
```

This bundle intentionally does not include a case rollback command. Suspected false positives should be reviewed manually from the case evidence and corrected through the normal administrative tools.
