# Legacy Fork Port Plan

This plan classifies every path in `legacy-fork-files.txt`. A path is covered
by the first matching workstream below. Historical commits are evidence only;
none of them may be cherry-picked as an implementation shortcut.

## Decision Vocabulary

- **Port**: preserve externally visible behavior, then adapt it to current
  upstream APIs with behavior tests.
- **Rewrite**: preserve the requirement but replace the implementation because
  the legacy ownership, security, or lifecycle model is unsafe.
- **Regenerate**: recreate from the new source of truth; do not copy the file.
- **Drop**: intentionally omit the legacy change.
- **Already rebuilt**: completed independently on the rebuild branch.

## Workstreams

| ID | Legacy paths | Decision | Required evidence |
| --- | --- | --- | --- |
| BUILD | `web/bun.lock`, both frontend package files, `web/classic/rsbuild.config.ts` | Already rebuilt | Frozen install, default/classic build, typecheck |
| POLICY | policy-named backend files; `controller/{misc,relay,token,public_error_response}*`; `middleware/{auth,distributor,utils}*`; `model/{token,user,ability}*`; `types/*`; `constant/context_key.go` | Rewrite | Single transaction owner, idempotency, public-error redaction, race tests |
| ATTRIBUTION | `relay/channel/{api_request,internal_attribution}*` | Rewrite | Exact origin allowlist, HMAC, timestamp, nonce, redirect and forged-header tests |
| RISK-EDGE | all `ops/ai-risk-guard/**` | Rewrite and move deployment ownership to `kkai-infra` | Redis Stream delivery, no DB credentials/writes, fixture replay |
| INVITE | all internal-balance-adjustment backend files and `web/default/src/features/invitations/**` | Port then harden | Ledger idempotency, outbox retry, concurrency, frontend tests |
| GROUP | `controller/group_status*`, `service/group_status*`, `pkg/perf_metrics/**`, `web/default/src/features/group-status/**` | Rewrite freshness and multi-instance merge; port UI | Real sample timestamp, stale state, Redis failure, aggregation tests |
| NODE | `main*`, `model/{option,channel_cache,standby_sync}*` and background-flush changes | Rewrite | Job registry, leader lease, read-only standby, two-process zero-DML test |
| MIGRATION | `model/{main,channel,database_type}*` migration changes and authorization startup workaround | Drop workaround; replace with versioned KKAI migrations | Checksums, additive schema, three-database tests, production-clone diff |
| BILLING | `setting/ratio_setting/**`, `relay/helper/price_test.go`, billing-specific task tests | Port | Configured completion ratio precedence, cache write/read, tier `cc`, pre/post charge parity |
| FRT | `dto/task.go`, `model/{log,perf_metric}.go`, `relay/common/**`, `service/log_info_generate*`, stream scanner/response timing tests, FRT guard | Port | Header parsing and timing behavior tests plus patch guard |
| RESPONSES | `relay/channel/openai/{chat_via_responses,relay_responses,responses_stream_status_test}.go` and related relay task changes | Port only if behavior test fails on upstream | Stream terminal-state tests on current converter structure |
| CC | both CC Switch dialogs, key row action, classic helper, default verification script | Rewrite | One-time ticket, 60-second TTL, single use, replay rejection, no key in URI/logs |
| PRICING | classic pricing hook; default pricing cards/details/constants/filters; recharge display | Port | Pricing hash/display tests and browser verification |
| WALLET | default wallet and affiliate component changes | Port after API contract review | Recharge, rebate, redemption and Waffo browser tests |
| NAV | command menu, sidebar hooks, section layout, maintenance module registration, route files | Port minimal integration only | Permission-aware navigation tests and route build generation |
| CHANNEL-UI | default channel columns/table changes | Review and port only KKAI operational behavior | Channel edit regression and default frontend tests |
| TABLE-UI | data-table sizing/row/hook changes | Drop unless required by a ported KKAI screen | Visual regression evidence |
| THEME | `setting/system_setting/theme.go`, frontend theme-only changes | Drop unless a KKAI brand requirement is documented | Build and screenshot evidence |
| API | `router/api-router.go`, `docs/openapi/api.json`, `web/default/src/lib/api.ts` | Regenerate from rebuilt endpoints | Router tests and generated schema diff |
| ROUTES | `web/default/src/routeTree.gen.ts` | Regenerate | Router generator produces a clean tree |
| I18N | all legacy locale JSON changes | Drop legacy translation churn; add only keys required by rebuilt UI | Default build; translation completeness is outside this remediation |
| DOCS | `README.md`, `AGENTS.md`, legacy `docs/ops/**` | Drop or rewrite from final architecture | Final runbooks match tested commands and immutable artifacts |
| LEGACY-CI | `.github/workflows/codex-build-ghcr-amd64.yml` | Drop | Clean local Linux AMD64 archive build and checksummed `router-v3-staged` delivery; no GitHub production build or deploy |

## Known Legacy Implementations That Must Not Return

1. `ai-risk-guardd` or any sidecar directly updating users, tokens, channels, or
   policy tables.
2. JSONL files as a reliable cross-process security event queue.
3. Trusting all private IP destinations for internal attribution.
4. Unsigned attribution headers or attribution containing token names/secrets.
5. `DISABLE_BACKGROUND_TASKS` as an undifferentiated global switch.
6. Startup `AutoMigrate` as the owner of KKAI schema.
7. API keys, reusable credentials, or full provider payloads in CC Switch URIs.
8. A single image variable shared by blue and green slots.
9. Release manifests that can drift from the containers actually serving.
10. Repeated policy hotfix cherry-picks, broad revert/restore commits, or copied
    generated frontend routes.

## Port Order

1. Versioned migrations, outbox primitives, and risk-action persistence.
2. Redis Stream ingestion and signed internal attribution.
3. Node roles, job registry, leader lease, and standby cache synchronization.
4. Billing/FRT/Responses compatibility patches on current upstream structures.
5. Invitation and group-status services.
6. CC Switch ticket service and shared provider payload builder.
7. Default/classic frontend integrations, generated routes, and API schema.
8. Infrastructure slot identity, idle-slot routing, build provenance, and runbooks.

No frontend port may define a second business contract that differs from the
backend. No infrastructure port may regain business-database write access.
