# rc.49 发布记录

用户已授权整合、验证、提交、受控在线扩展及上线，约束是保持线上用户的登录、请求和账务连续性。用户接手真实登录、模型 Relay、Image/Video Studio 等业务验收，代理未验证生产业务 E2E，未创建生产测试账号、Key 或业务请求。

## 2026-11-10 首次发布与安全回退

源码 `e8742162e0746027cfb3725c9982dbdcb3fd55c7` 构建为后端 `kkai-prod-20261110.1794289144-e8742162e`，镜像 `sha256:9dfb450bf5a370b08f09663525aa6e1044252081c0abe3c7909632d3413a3547`。候选精确身份、10%/50%/100%各至少120秒、promotion、即时及五分钟延迟健康检查通过，但发布后错误审阅发现真实 Gemini 后缀回归，因此首次后端发布未通过最终验收。

当 `gemini.thinking_adapter_enabled=false`、未命中blacklist或模型映射时，旧版会剥离裸 `-medium` 后缀并传递推理强度，新版保留后缀，改变上游模型URL和基础模型映射行为。原窗口 `06:34:05Z–06:39:05Z` 的404满足这些条件。本地实际 helper→映射→Gemini URL 对比得到旧版通过、新版预期失败、修复版通过；不是仅按状态码推断。兼容修复及准确验证命令见 [Gemini验证记录](rc49-gemini-compatibility.json)。

已通过正式 `kkai-newapi-manual-deploy rollback` 恢复后端 `kkai-prod-20261109.1794239898-a552996db`；即时 `make newapi-status` 为 HEALTHY、blue、snapshot stable、rollback ready。回退过程中等待在途连接排空并由控制器安全替换writer，未手动重启容器或恢复数据库/Redis快照。旧版不具备新缓存MAC验证，回退期间记录此能力降低。

新前端 `kkai-frontend-20261110.1794289195-e8742162e` 已独立激活。入口、6个版本化JS/CSS和首页静态HTML均200且摘要匹配，缓存头符合规则。正式 `rollback-plan` 返回backend-only，当前前端兼容旧后端，回退保留前端。会员恢复原模式为apply，回退后已通过generation校验恢复并确认各进程acknowledged；正常请求结算不受后台暂停开关影响。

原窗口另一条图片502为上游200后的 `bad_response_body`。请求计数1、stream=false、无markup/svg；现有脱敏日志没有原响应结构，不能断言其根因。worker/SSE桥接未改，16个本地JSON结构比较无解析差异。为移除未证实的解析器变量，修复版仅将Image Studio响应校验恢复为旧版gjson实现，保留原大小、数组数量和图片字段校验，以及新计数功能；不将此保守措施称为已证明修复原502。429按用户要求不再调查。

## 数据库、控制器与备份

保留canonical21，独立 `rc49-online-v1` 扩展已完成并复核物理列、并发索引和回执。canonical digest为 `sha256:84687631f06a71630ad6035a28ce29c91bd24fb5cf824e72e394b7f8fc0680f0`，扩展digest为 `sha256:ff1dbaacac62dd31c931ecb62a497c0dc16be5dbba6e46538ec427739bb7db4e`。迁移角色NOLOGIN且无活动会话。没有推进ledger22，回退不撤销兼容扩展。

新profile均要求 `(21,21,21)` 与扩展回执，旧max21继续兼容。已安装控制器及应用pin为 `8fe80d1a4f2e6a6233b16b7de32aff3a60a8e9fa`，protocol为router-v3-staged。回退后的独立只读schema状态再次为ready/safe。保留root迁移产物及receipt，不重复扩展。

扩展前异地加密备份 `20261110T053622Z`，manifest SHA `f7a55b4878cb4cfdd0690676ddbfe3556479541baa2aaafd13998a0bb68ad940` 已校验。未执行恢复演练，不宣称PITR。

## 已完成的本地验证

完整命令、原始失败和定向修复结果见 [完整验证清单](rc49-full-validation.json)。Go根模块完整运行201秒，初轮13个失败包修复后定向通过；四个独立Go模块完整通过。前端typecheck、build通过，Vitest初轮3940项中4项失败修复后定向通过；全局lint163项和format23个未修改文件的既有缺口保留。不能称完整检查一次全绿。

独立PostgreSQL/Redis上的旧current、rollback、新main混跑覆盖登录/JWT/刷新/APIKey/撤权、钱包扣退款、SSE及候选退出旧版连续性；旧注销墓碑导致新500已修。DDL锁冲突、授权过期、并发索引失败修复及非superuser权限演练通过。infra policy首轮fixture失败修复后续全部146项通过；无Ansible变更，未重复make check。用户仅使用PostgreSQL，本次不增加SQLite/MySQL兼容工程。

Gemini修复仅执行相关包和直接依赖定向检查；不重跑已通过且输入未变的全套。图片恢复旧校验执行现有超限/无效结构拒绝和正常单图通过检查。

## 修复版交付与收尾

后端修复须形成新的本地production提交、唯一不可变镜像，并重新执行正式stage、完整灰度和发布后门禁。兼容format2前端原样复用，不重新构建前端。当前不把首次promotion成功等同于整次升级完成。

清理仍待最终安全结果：v10 schema snapshot保护线上backend release/image，不能手删；前端尚无退出保护满30天的到期项。root扩展产物/receipt、controller源码/bundle和当前/回退资产均须保留。已有本地/远端精确清理清单，但首次验收失败后没有提前删除重试/恢复产物。历史未上线前端d3e640059归属仍待维护核定，保留约20MB archive；此项有明确保留处置，不冒险删除。

完整操作证据保留于本checkout的 `.local-releases/rc49-integration/`；未进行代码托管网络同步。
