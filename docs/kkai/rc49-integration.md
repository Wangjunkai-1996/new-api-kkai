# rc.49 本地集成记录

已将固定官方目标 `48a6c87f9e7757fdc58e5be9bcb4f8281ad0dfd0` 的适用增量整合到本地 `production/kkrich` 工作区。实施起点为 `5eb8a92581df793be4ca108aaa27391bdef68442`，官方内容基线为 rc.41 `2035a82aeb5414253a728bd937d4b8f97aa99b9b`。

本记录描述最终代码、完整测试后的定向修复与真实旧新版本混跑。没有创建分支/worktree或推送；生产进度与独立门禁以[发布准备记录](rc49-release-readiness.md)为准，不能由本地测试推断上线完成。

## 增量处置

[完整台账](rc49-integration-ledger.json)记录 **878 个净变化路径、215 个上游提交**及本地路径映射、处置理由和内容摘要。提交与路径的关联仅证明来源，不代表每个提交逐字套用。不存在未处置的源代码冲突；保留项和验证限制不是被隐藏的“通过”。

| 范围 | 本地结果 |
| --- | --- |
| 依赖与 TokenKit | Go 1.26.10 / Bun 1.4.4、workspace 锁文件、独立模块及手工构建入口同步；保留精确 BPE 和有界缓存，修复供应商前缀选错 codec |
| Redis / 鉴权 | 保留 standalone DB0、旧键、原额度与撤权协议；增加 MAC/绝对期限、可靠回源修复及并发刷新事务保护；限流使用旧 list 的原子滑动窗口 |
| 持久化 | PostgreSQL canonical21 + 独立 rc49 回执在线扩展、令牌余额可见权限、缓存读写 Token 计数、独立日志库索引/回执；external 启动不执行 DDL |
| 账务与任务 | 保留 int64 钱包、租户/会员冻结授权、Studio 报价、资金来源、outbox/CAS/fence；重试提交前严格补扣；支持 12h 缓存计费与相应预算上界 |
| 协议与插件 | DeepSeek、供应商搜索/embedding、Responses/图片及 native 插件升级；有效终态/显式零/缺失用量分开处理；结果未知不重试、不误退款、不追加第二个响应 |
| 默认前端 | 304 个映射路径已处置；日志 CSV、缓存统计、定价确认/撤销、渠道重排、通知、插件界面及七语种资源；新功能使用 capabilities 门禁 |
| API 与发布契约 | 保留 console API 3 和后端 `[2,3]`、租户隔离及手工 external 发布路径；更新适用 OpenAPI/插件文档 |

明确保留：本地旧 Wanx 图像支持、会员分组配置和定价展示语义、精确 tokenizer、旧 Redis 键及原子额度通道；不引入 Redis Cluster 改键，也不新增 SQLite/MySQL 兼容工作。Electron 和通用 README/代理规则保留本地布局。24 个上游旧后端测试路径沿用本地定向夹具，另一个 SQLite 专用测试未引入；没有声称这些旧测试逐项等价覆盖。

## 审查中修正的真实问题

- 覆盖参数/透传后图片数量未重新预留：校验实际提交数量并在发送前补足；multipart 同步有效数量，Studio 冻结约束保持。
- 实际图片数量未传到表达式结算：统一输出数量与冻结输入的分工，兼容对象/数组响应和本地交付计数。
- durable 原生流提前 EOF、completion hook/usage 失败被视为成功：改为未知结果，轮询遵循同一规则；可信冻结事实、合法零用量仍有效。
- 12h 缓存计价新增变量未进入会员预算/冻结表达式：补齐 `cc12h`、长 TTL 折入的安全上界与动态谓词保护。
- DeepSeek finalizer 被租户 transport 包装器遮蔽：转发可选接口，参数覆盖后的推理历史规则实际生效。
- RawMessage 深拷贝把 nil 改为空：保留 nil/empty 语义并隔离字节存储。
- 带 `openai/` 等供应商前缀的旧 GPT 模型误用 o200k：规范模型身份后选 codec，保持精确计数。
- Doubao 布尔 duration 被当作 1 秒：非数值输入使用模型最大时长估算，保留时长上限。

心跳写入期限审查告警经调用链复核撤回：既有 `streamResponseWriter` 已设置 deadline。只补观察测试，没有留下冗余生产改动。

## 验证与证据

[验证清单](rc49-verification.json)保存精确命令、工作目录、结果和历史失败后的定向重跑关系；[后端说明](rc49-backend-integration.md)与[Redis 记录](rc49-redis-implementation.md)提供持久化边界详情。原始日志保留在本机 `.local-releases/rc49-integration/`。

- 主程序编译检查通过；图片/任务严格预留、实际输出数量、WebSocket/Realtime、参数覆盖、供应商协议、未知终态、显式零、权限和跨租户路径等定向回归通过。
- 使用本机新建 PostgreSQL 18.4 隔离 schema 和 Redis 8.6.3 standalone 验证迁移、缓存修复、撤权、并发刷新和持久化账务；没有复用生产服务。
- TokenKit 精确计数、长串与 codec 选择、12h 预算、日志/限流/HTTP transport 范围通过。
- 前端类型检查覆盖 977 个依赖文件；scoped lint 0 errors / 4 warnings。93 个不同测试路径分域执行，历史失败经过缩小范围修复重跑；末批 31 项和本地差异回归 60 项通过。七种 locale 各 9902 个键一致，源码 7506 个引用键检查通过。
- 本地完整 diff 检查通过。没有以单次全套绿灯替代分域证据，也没有把未匹配到用例的包记作行为测试通过。

后续已按完整授权执行 Go 根模块、四个独立模块及前端 typecheck/test/lint/format/build；原失败和定向重跑见[完整验证清单](rc49-full-validation.json)。最终迁移与CLI包通过，历史版本和外部环境skip逐项列明；前端全局lint/format存在未修改文件基线缺口。浏览器fixture与真实本机HTTP混跑分别记录，不作为生产验收或容量结论。

## 发布门禁

当前方案已落为 **canonical21 + `rc49-online-v1` 严格扩展回执**，两个 profile 都声明该要求，未扩展21会被拒绝。主表 NOWAIT、日志并发索引及独立修复入口遵循专用流程。保留 canonical21 使旧 max21 版本仍可回退；本次不推进22，不靠 profile 名称绕过兼容性。

精确旧当前版、旧回退版与新版真实服务混跑已通过登录/刷新、撤权、普通钱包扣费退款、在途SSE和候选退出后的连续性验收；发现的旧版注销后新版500已修为数据库确认撤销后的401，拒绝守卫未放宽。缓存测量证明小规模回源有界，暖缓存不额外读取users/tokens；未声称durable任务和会员writer交接已由HTTP矩阵覆盖。生产仍需受控 build、扩展、stage、canary、promote 与独立发布验收。

初期两个测试容器的清理属于历史记录；后续全套及混跑新建资源的最终清理回执由本轮验证记录单独保存。保留工具链、依赖缓存与验证日志，不清理其他项目资源。
