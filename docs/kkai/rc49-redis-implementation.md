# Redis MAC 本地实施记录

2026-11-10；应用基线 `production/kkrich` / `5eb8a92581df793be4ca108aaa27391bdef68442`。
改动保留在当前工作区，未提交、构建发布镜像、执行生产迁移或部署。本轮没有访问或改变生产状态。

## 已实现

- User、Token、Session 鉴权元数据增加 HMAC 和绝对有效期，绑定完整 Redis key、TenantID、身份与授权字段。Token 的 AutoGroups、用户 AuthVersion 均被明确签入。
- 保持原 Redis key、DB 0、user/session schema 2、go-redis/v9、秘密及额度协议。新字段不进入 PostgreSQL schema；本轮验证只使用 PostgreSQL，没有做 SQLite/MySQL 兼容工作。
- 缺签、坏签、过期签名回源 PostgreSQL。用户和 Token 的修复先观察缓存，再读数据库，以 Lua 比较全部 metadata，避免旧 writer 在 AuthVersion/MAC 未改变时被覆盖。身份和租户必须匹配，实时计数由 Redis 原生 signed-int64 校验后保留。
- 保留 Token generation、前后失效 fence，用户永久版本下界，以及 Session deny/version/deadline。竞争最多三轮；后续观察失败不能降级为忽略撤权门禁的读取。用户成功发布后再次核对完整数据库 metadata，不一致则撤去本次签名、保留余额，不续租无效缓存。
- 用户局部设置、用户名、分组更新统一读取完整可信 metadata；Quota 专用对账继续独立，不重签任意缓存内容。
- Session 缺签/坏签作为缓存未命中；未确认的旧撤销墓碑仍拒绝请求，但返回临时错误。只有可信失效信息走永久鉴权失败，避免把数据库故障当作退出并清 Cookie。
- Session 回填门禁失败、过期观察不能用先前 active 快照放行。刷新密钥的条件 UPDATE 和缓存门禁位于同一 PostgreSQL 事务内；门禁失败回滚密钥，使客户端保留的原 Cookie 可以重试。原有 refresh race / replay 语义保持。

## 本地验证

使用新建、仅绑定 `127.0.0.1` 的 PostgreSQL `18.4` 和 Redis `8.6.3` standalone / DB 0。通过 `TEST_AUTH_CACHE_POSTGRES_DSN` 启用独立随机测试 schema，通过 `TEST_AUTH_CACHE_REDIS_ADDR` 指向新建 Redis。没有复用已有业务数据库、清空共享 Redis 或注入生产故障。

数据库夹具限定 loopback、`kkai_test_` 数据库前缀、随机 schema 和 SQL/连接超时。Go 命令另有 240 秒进程总时限；没有运行全仓测试、全仓 lint、发布构建或压测。

执行命令与结果：

```sh
go test ./model ./service -run '^Test(TokenCache|UserCache|UserSession(Cache|RefreshCache)|AuthCacheRedis86|TaskBillingCacheReconcile|TryReserveQuota|RedisBatchReserve|ReserveFallsBack|BatchReserve|SynchronousReserve|TenantTransitionLegacy.*Cache|RefreshLoginSessionPreservesTemporaryLookupErrors)' -count=1 -timeout=120s
```

其中 service 通过；model 的额度预留/补偿、撤权、generation、租户与真实 Redis 验证通过，刷新回滚测试暴露测试夹具的短 hash 被 PostgreSQL `char(64)` 补空格问题。已把测试输入修为真实 64 字符 HMAC，没有放宽生产校验。另一次早期运行发现 miniredis 会接受 `01` 并规范化，而真实 Redis 会拒绝；这类 canonical integer 边界保留在真实 Redis 验证，不为模拟器修改生产实现。

修正夹具并补齐重试断连门禁后，只复测受影响范围：

```sh
go test ./model -run '^Test(TokenCacheIntegrity|UserCache|UserSession(Cache|RefreshCache))' -count=1 -timeout=120s
go test ./service -run '^TestRefreshLoginSessionPreservesTemporaryLookupErrors$' -count=1 -timeout=60s
```

均通过。覆盖签名修复、同版本旧 writer 竞争、合法负额度、int64 边界、身份错配、绝对过期、撤权期间断连、失败不续租，以及刷新失败后原密钥仍可重试。service 定向测试通过错误注入验证公开刷新流程的 HTTP 错误分类。

真实 Redis 与 PostgreSQL 并发刷新验证：

```sh
go test ./model -run '^Test(AuthCacheRedis86MetadataRepair|UserSessionRefreshConcurrentCurrentSecretHasSingleWinner)$' -count=1 -timeout=30s -v
```

通过，两个主测试及全部 Redis 子案例均为 PASS（无跳过）。验证 Redis 8.6.3 的 signed-int64 两端、负值、非法表示与越界行为，以及共享 hash 中旧额度脚本/旧读者对新增签名字段的处理。并发刷新使用两个 PostgreSQL 连接和确定性屏障，验证同一 current secret 只有一个更新成功，另一个得到 refresh race，最终数据库和验签缓存保持有效会话。

`git diff --check` 通过；全套测试跳过。执行输出保存在本机忽略目录 `.local-releases/rc49-review/redis-mac-*.log`。

验证结束后，按创建时记录的精确容器 ID 删除了本轮两个测试容器及 PostgreSQL 匿名测试卷；过滤检查确认均已清理。没有清理其他项目资源。

## 审查发现与已处理问题

三个独立角色参与实现及交叉审查。实际发现并修复：Session 临时数据库错误被当作无效 refresh token；SQL 读取后缓存门禁失败仍放行旧权限；重试中的 Redis 观察失败忽略已有撤权边界；过期 Session 观察只检查 SQL 而绕过尚未提交的 deny fence；刷新密钥先提交后失败造成客户端无法继续使用原密钥。

## 上线前仍需完成

这些是本地功能及协议验证，**不是精确旧、新应用二进制的完整混跑验收**。现有旧读者/额度 Lua 测试不能证明完整 Cookie/JWT、SSE、异步任务和 writer handoff 的连续性。

仍需按 [Redis 方案](rc49-redis-plan.md) 在隔离环境完成：精确 active/rollback 版本混跑、candidate 写入后 abort 和无清缓存回退、真实充值/退款/限额并发、完整登录与在途工作连续性、冷缓存/旧 writer 竞争/集中签名过期的 PostgreSQL 查询和延迟预算。通过后才可进入生产 stage。

可用性边界：Redis 已启用但无法确认 Session 撤权/版本，或观察过期时，返回临时失败并保留 Cookie；不承诺故障期间持续成功。刷新事务会在缓存门禁期间持有对应 Session 行锁，应纳入并发和延迟预算。Redis 签名不保护可变金额计数本身，也不替代 PostgreSQL 账务约束。

正式发布仍需单独授权，遵循现有 schema 合同核对、Mac 手工构建、stage/canary/promote、错误窗口、回退与清理门禁。Redis 本身的验证不构成可上线结论。后续整版本地整合状态见 [rc49 实施记录](rc49-integration.md)，生产门禁仍独立。

## 整版整合后的复验

整合 rc49 其余模块后，重新使用本机隔离 PostgreSQL 18.4 与 Redis 8.6.3 验证令牌/用户/会话完整性、旧元数据修复与并发刷新，model 范围通过（5.718s）；公开刷新临时错误和相关共享边界的 service 定向范围通过（8.292s）。精确命令见 [验证记录](rc49-verification.json)，原始日志为 `redis-after-integration.log` 和 `shared-boundaries-final.log`。这仍不是两个完整应用版本的混跑验收。

整合后复验所新建的 `kkai-rc49-postgres-local` 和 `kkai-rc49-redis-local` 也已按精确 ID 删除并确认不存在；未触及其他容器或生产资源。
