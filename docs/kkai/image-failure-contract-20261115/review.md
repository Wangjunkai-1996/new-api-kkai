# 方案审查与处置总表

初次冻结：2026-11-15，plan.md v1.0。实现审查收口：2026-11-16，plan.md v1.1；实际上线与验证状态见 acceptance.md，方案批准不代替生产验收。

| 专业角色 | 审查记录 | 发现 | v1.0 处置 |
| --- | --- | --- | --- |
| 协议架构 | review-protocol.md | P-01～P-06 | 首次读取证明、严格原始字段、unknown 跨尝试/批次粘滞、部分结果优先、专用语义优先、安全 wire message、明确 passthrough 状态规则均纳入第3节。事故路由决定 OAuth 是否需要本次扩展，不允许收敛后仍声称事故已修复。 |
| 计费与安全 | review-billing-security.md | R1～R3 | 替换直接公开原文的初始补丁；限定 admin_info、精确 key 先删、UTF-8 限长、结构化回退；保持原错误与资金行为；显式 legacy/membership 验证并核对 skip。 |
| 发布与 SRE | review-release.md | RP-1～RP-5 | 串行 NewAPI 后端/前端/收尾再 Sub2 image；live schema 与格式能力门禁；免登录如实记缺口；自然样本每阶段上限30分钟；错误逐组归因、精确身份回退及受控清理。 |

原始审查的条件保留为实现验收项。开发文档先落地，再由主代理检查与冻结契约一致后启动对应代码开发；跨模块代码完成后再次独立审查。没有新增迁移、控制器升级、分支、worktree 或代码托管网络同步。

## 开发文档复核记录

- 前端 `frontend-development.md`：与现有 DetailsDialog、admin_info 投影和复制 Hook 一致，字段缺失兼容；不改变图片任务状态，批准按文档实施。
- NewAPI 后端 `newapi-backend-development.md`：已审阅并批准；使用 DurableImageExecution 限定审计分支，结构化回退、权限投影和明确计费模式验证符合 R1–R3。实施中发现首次读取日志出口须同步收敛，仅限 async 图片。
- Sub2API `docs/kkai/image-failure-development-20261115.md`：已审阅并批准；事故 native/apikey 有当时日志依据，首次读取证明和跨尝试否决均保留，全部图片最终出口与共享文本零值行为纳入测试。OAuth 本次不新增 marker。
- 发布 `release-plan.md`：由 SRE 根据实际安装能力落地精确执行命令，执行前完成证据核对。

## 实现审查收口（v1.1）

NewAPI 后端与前端独立代码审查均批准，分别见 `code-review-backend.md`、`code-review-frontend.md`。Sub2API 的 S-01～S-10 已逐项修正并由独立角色复核批准，详见 `/Users/tokk/Documents/Codex/2026-08-09/li/work/sub2api/docs/kkai/image-failure-code-review-20261115.md`。

v1.1 将已批准的实际范围和安全边界统一写回主方案：仅 native JSON 新增证明；合法 UTF-8 与无重复键；按消费端大小写/空白语义抑制无证明的保留码；SSE 先规范化再统计输出；专用 policy 分类只读实际字符串；安全诊断与终态 proof 分离；两端各自的脱敏顺序及原始 provider 字段保留范围按实际代码描述。未扩大到 OAuth 终态判定、数据库、重试或计费算法。

最终 handler 完整 unit 和图片近域回归通过；Sub2 service 完整包的一项未修改内存阈值测试失败，隔离运行通过，但未证明全包瞬态失败原因。该验证缺口已进入开发和审查记录，不能描述为完整包通过。发布以受影响代码回归、静态因果核查、精确制品及后续生产门禁共同验收。
