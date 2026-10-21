# 代理站请求兼容修复验证记录

日期：2026-10-21。对应 [完整方案和验收矩阵](reseller-relay-parity-plan.md)。

本记录只证明所列本地回归与检查的结果。本轮未提交/推送、安装 controller、应用 Edge 计划、构建镜像或修改生产；不能把本地成功描述为生产已修复。

## 应用验证

以下命令均在 `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai` 执行。

| 精确命令 | 结果与覆盖 |
| --- | --- |
| `go test ./controller -run '^(TestTenantTokenGroup(WritesMatchPublicSupply\|ListingAndRelayUseSameSupply)\|TestTenantTokenStatusCannotEnableUnavailableGroup\|TestTenantPricingAndModelsInheritPublicGroupsAndRespectTokenLimits\|TestUpdateTokenGroup\|TestAutoGroup(ModelLists\|AuthAndDistribution))' -count=1 -timeout=120s` | PASS，0.159s；分组展示、Key 各写入口、模型列表与鉴权统一；主站 auto 回归 |
| `go test ./controller -run '^TestResponsesWebSocket' -count=1 -timeout=120s` | PASS，0.689s；原有主站 WebSocket 行为 |
| `go test ./controller ./relay -run '^(TestTenantResponsesWSRunner\|TestResponsesWSRequestRunner\|TestTenantResponsesWSPinnedConnection\|TestTenantResponsesWSPayload\|TestResponsesWSWalletClientCancellation)' -count=1 -timeout=120s` | PASS，0.242s/0.100s；逐轮 Host/Key/供给校验、固定渠道、取消与出站快照 |
| `go test ./middleware -run '^(TestTenantFeatureGate\|TestTenantOrigin)' -count=1 -timeout=120s` | PASS，0.065s；租户路径、WebSocket Upgrade、Origin 隔离和 compact 禁用 |
| `go test ./relay -run '^TestTenantResponsesWSDurableOutcomes$' -count=1 -timeout=120s` | PASS，0.104s；真实本地 WebSocket 的完成、明确拒绝、未知 500、客户取消及无 usage 取消 |
| `go test ./controller -run '^TestTenantResponsesWSPaidTurnsSettleOnceWithIndependentPrices$' -count=1 -timeout=60s` | PASS，0.091s；完整 HTTP 鉴权至真实本地 WS 两轮付费结算，无会员目录；第一轮预扣后改价，第二轮使用新价；用户/Key/渠道/支付批次/代理收益只记一次 |
| `go test ./relay -run '^(TestPrepareTenantRequestBillingUsesPlatformProtocolWithoutMembershipCatalog\|TestPrepareTenantResponsesRequestPreservesPlatformSemantics)$' -count=1 -timeout=120s` | PASS，0.088s；9 种普通协议、媒体和工具输入及 Responses 透传，无会员发布依赖 |
| `go test ./service -run '^TestTenantCatalogExcludesUnsupportedProviderEndpoints$' -count=1 -timeout=120s` | PASS，0.053s；仅展示已整合的协议/渠道 |
| `go test ./service -run 'TestTenant(Audio\|ResponsesImageTools)' -count=1` | PASS，0.080s；音频与 Responses 图片工具的冻结价格、换算单位、缺 usage 保留及幂等账务 |
| `go test ./service -run '^TestFixedPriceBillingDatabaseMatrix/sqlite$' -count=1` | PASS，0.069s；已有固定价格 SQLite 回归 |
| `go test ./model -run '^(TestTenantEstimatedReservationRetainsUnfundedActualAndResumesAfterTopup\|TestTenantConsumption(AtomicSettlementDatabaseMatrix\|SettlementFailureRetainsRetryableIntent\|FreeUnknownDoesNotBlockPaidReservation)\|TestTenantConsumptionReconciliationPreservesReservationUntilSettlement)$' -count=1 -timeout=120s` | PASS，0.108s；差额不足保存 actual、阻断下次付费、真实支付批次充值后重试、只结算一次。MySQL/PostgreSQL 分支因无 DSN 跳过 |
| `go test ./service -run '^(TestTenantBudget.*\|TestTenantOutbound.*\|TestTenantBilling.*\|TestMembership.*(Budget\|Bound\|Pricing)\|TestTenantCatalog.*)$' -count=1 -timeout=120s` | PASS，0.088s；共享预估、冻结报价与会员原有安全规则。此结果在最后免费附加项补充前取得，最终相关回归见后续表 |
| `go test ./service ./relay ./relay/common -run '^(TestTenantZeroBasePriceFundingIncludesAuxiliaryCharges\|TestTenantLargeResponsesBodyPreservesThirtyMiBPayload\|TestTenantCallerToolActualChargeUsesReservedPrice\|TestCountBillableCallerToolUsesFrozenPrice\|TestTenantBudgetClonesRequestMultipliers\|TestTenantFreeReservationRejectsPaidFeaturesAddedAtDispatch\|TestTenantBudgetUsesSharedEstimateWithoutMembershipConfiguration\|TestTenantBudgetFixedPriceAndRetryStayFrozen\|TestTenantBudgetFreezesTieredRequestPricing\|TestTenantOutboundRequiresPricingAndIntegratedTransport\|TestTenantBudgetFreeGroupIsDurableAndCannotBecomePositiveCharge)$' -count=1 -timeout=120s` | PASS，service 0.066s、relay 0.708s、relay/common 0.020s；11 项免费附加费与冻结工具价回归，含真实 30 MiB BodyStorage/账务准备和工具实际结算 |
| `go test ./service -run '^TestRelay(JSON\|Outbound)Quantities' -count=1` | PASS，0.109s；已知计费字段精确整数边界、科学计数、重复字段、schema/正文不递归、HTTP body/重放/close 语义及 30 MiB |
| `go test ./relay/helper -run 'Test(MaxTokensBounds\|OutputCountBounds)' -count=1` | PASS，0.040s；标准 DTO 和 Gemini batch 的 token/count 边界，缺省/null/0/有效上限语义保留 |
| `go test ./service ./controller ./relay -run '^(TestTenantBudget.*\|TestTenantOutbound.*\|TestTenantBilling.*\|TestMembership.*(Budget\|Bound\|Pricing)\|TestTenantResponsesWSPaidTurnsSettleOnceWithIndependentPrices\|TestTenantResponsesWSDurableOutcomes\|TestTenantResponsesWSPayload\|TestResponsesWebSocket.*)$' -count=1 -timeout=120s` | PASS，service 0.087s、controller 0.760s、relay 0.109s；最终数值校验接入后的报价/会员边界、原有主站 WS、代理付费两轮及异常结算回归 |
| `go test ./relay/channel -run '^(TestApplyUpstreamBodyMetadata.*\|TestUpstreamGetBody_.*\|TestTenantFreeTransportPreservesUnknownExecutionOutcome\|TestMembershipTransport.*)$' -count=1 -timeout=120s` | PASS，0.110s；最终 HTTP dispatcher 的重放元数据、HTTP/2 重试、免费资金未知结果和会员传输边界 |
| `git diff --check` | PASS；最终任务差异 |

独立交叉审查覆盖免费附加项分类、工具价与倍率快照、pending/充值重试、WS 身份及数值校验边界；所列范围无未解决的阻断项。未来新增独立收费项仍须同步资金分类和实际结算回归，不能靠当前白名单假定免费。

## 基础设施验证

以下定向命令在 `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra` 执行。

| 精确命令 | 结果与覆盖 |
| --- | --- |
| `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts.tests.test_tenant_business_config scripts.tests.test_tenant_business_edge scripts.tests.test_tenant_domain_edge` | PASS，27 tests，0.185s；渲染、事务、路由与原有拒绝入口 |
| `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts.tests.test_tenant_business_upgrade` | PASS，初始 4 tests，0.086s；历史 v1 计划仍可验证、升级、重复执行、失败恢复及精确回退。后续 v2 父事务覆盖并入 policy |
| `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts.tests.test_tenant_business_config` | PASS，5 tests，0.003s；增加唯一 Upgrade map 后的最终配置校验。先前联合运行的 1 个错误是旧断言预期的校验顺序，已修正并运行最近范围 |
| `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts.tests.test_tenant_business_nginx` | PASS，8.036s；实际缓存 OpenResty 容器、禁用网络与镜像拉取：5 个路径的 2 MiB 请求、真实 30 MiB Responses 字节完整性、共同 500 MiB 上限、SSE、WS 101、compact 仍拒绝、品牌静态资源与后端 assets 区分 |
| `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts.tests.test_newapi_installed_cli` | PASS，4 tests，2.508s；已安装命令接口 |
| `python3 scripts/check_source_size.py --root . --baseline scripts/source-size-baseline.tsv` | PASS；455 files，30 baselines，250-line 默认上限 |
| `make -C /Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra policy` | PASS，exit 0；在用户明确批准后完整运行，包含安全、controller/Edge 事务、历史 v1/v2 升级、实际本地容器演练和 ShellCheck；没有生产部署 |
| `git diff --check` | PASS；最终基础设施任务差异 |

## 剩余验证边界

- 应用全套测试未运行；按风险仅运行受影响包的定向回归。基础设施完整 policy 是强制门禁，单独取得明确批准并全部通过。
- 本轮未改变 Ansible 文件，未触发或运行 `make check`；未改变依赖、前端、schema 或生产价格配置。
- SQLite 账务流程有实际数据库验证；MySQL/PostgreSQL 未设置测试 DSN，不能声称三数据库集成全部通过。
- 实际生产的 Edge 新计划、controller 安装与 self-test、精确后端候选、蓝绿门禁和用户客户端推理尚未执行。30 MiB/WS/账务端到端证明来自隔离本地测试，不是生产请求。
- 主站估算式预扣保留首笔超预估及已经授权并发的差额风险。`settlement_pending` 代表待收款事实，不代表费用已追缴；已验证其不会产生负余额或无资金代理收益。
