# KKAI Billing Verification

This note records the fork-owned billing behavior accepted on top of upstream
commit `7c28993f6bd9e92616f3f578212577f8b7c40b45`.

## Completion Ratio Policy

- An exact administrator-configured completion ratio wins before the official
  model-family fallback.
- A configured ratio is reported as unlocked.
- Models without an exact configuration retain the official ratio and lock
  behavior.
- Provider-qualified model names keep exact-match behavior.
- `ModelPriceHelper` freezes the resolved ratio in `PriceData` for settlement.

The policy is isolated in
`setting/ratio_setting/kkai_completion_ratio_policy.go`. The official ratio
table and fallback function remain the single source of truth.

## Cache Token Billing

Upstream commit `48068ce9` is an ancestor of the pinned baseline and already
provides the production implementation. The fork does not duplicate it.

Acceptance tests confirm:

- Chat and Responses cache-write fields survive response conversion.
- Claude conversion preserves the original OpenAI billing usage.
- compatibility and native cache-creation fields use the larger value instead
  of being added together;
- negative cache-creation values resolve to zero;
- ratio billing clamps an overlapping uncached prompt remainder to zero;
- `tiered_expr` receives OpenAI cache writes through `cc`;
- frozen tiered pre-consume and settlement agree for identical token inputs.

## Verification Commands

```bash
go test ./service/relayconvert/... ./relay/channel/openai ./relay/channel/claude
go test ./pkg/billingexpr ./relay/helper ./setting/ratio_setting ./dto ./service \
  -run 'Test(KKAI|Cache|CalculateTextQuotaSummaryBillsOpenAICacheWriteTokens|TryTieredSettle|BuildTieredTokenParams|ComputeTieredQuota|ResponseUsage|BuildClaudeUsage)'
go test -race ./setting/ratio_setting ./relay/helper ./dto ./service \
  -run 'Test(KKAI|CacheWriteTokensTotal|CalculateTextQuotaSummaryBillsOpenAICacheWriteTokens|TryTieredSettle_PreConsumeMatchesPostConsume)'
```
