# NewAPI 图片失败诊断发布验收

日期：2026-11-15 UTC（北京时间2026-11-16）。结论：后端/前端发布及适用安全门禁完成；业务测试由用户接手，代理未验证。执行角色：发布/SRE。所有产物来自一次冻结的本地源码；本文件和后续文档提交不改变运行代码来源，不触发重建。

## 精确坐标

| 项目 | 值 |
|---|---|
| 应用源码 | `f3334a7da0915dcd34e9979e7ccc1fc7ffc72bcf` / `production/kkrich` |
| 后端版本 | `kkai-prod-20261115.1794758551-f3334a7da` |
| 后端 image ID | `sha256:6aa3919a6fd578e2060a50d89348bdc39088d8b1cd840ce14e1c744280b4825c` |
| 后端 archive | 117432320 bytes；SHA256 `4d44b05d8ebbbeed6e2d4f1474174b1d888d93caffdf853e089c49fe6faedd6a` |
| 前端 | `kkai-frontend-20261115.1794759894-f3334a7da`；format2；API contract4 |
| 前端 archive | 20692745 bytes；SHA256 `126da1cc3d353e91b6cc2b21629ead392eb1e7ef31aec257227e66922899eeab` |
| 前端 outer metadata SHA256 | `0c2baab14033e0353d96674506669f07c9b6d944bcd296739c3a1b03d6a2df90` |
| 前端 manifest SHA256 | `aec552208d7e6895b4aeb0b3717dccb63d63b82a0da3a148b1522ebb9b9d00e8` |
| 安装控制器 | `ee4e460d1ac77e017ee3ad3a4af97beb3b090634` / `router-v3-staged` |
| 灰度 | `e2ddf61ad285457f87edf9fefa0867c2`；blue → green |
| 回退后端 | `kkai-prod-20261114.1794644275-8a7bdca8f`；image `sha256:f9768bdee81af93736c5f1f4ce603e87a2355ba8401eea1072c9f51e072d7eaa` |
| 回退 manifest | `/srv/kkai/newapi-releases/kkai-prod-20261115.1794758551-f3334a7da.rollback-pair/release-manifest.yml` |
| 上一前端 | `kkai-frontend-20261114.1794654138-55a55f56e` |
| 前后端回退顺序 | 控制器 `rollback-plan` 返回 `backend-only`；当前前端兼容当前及回退后端 |

## 构建、数据库契约与 stage

Mac 通过正式脚本唯一构建后端 feature/external/Linux AMD64；前端通过正式脚本和固定 Bun1.4.4 唯一构建 format2。依赖安装使用 frozen lockfile，源码与锁文件无变更。没有在服务器构建，没有迁移、控制器重装或代码托管网络操作。

live schema baseline 已验证 canonical21 `(21,21,21)`、canonical digest `sha256:84687631f06a71630ad6035a28ce29c91bd24fb5cf824e72e394b7f8fc0680f0`，并验证 `rc49-online-v1`、`video-media-metadata-v1` 完整回执。feature 由真实 tenant routing 及目标契约选择，不依赖历史 profile 名称。

**精确 image 契约证据**：准备阶段核对实际安装文件 SHA256 与上述 controller pin 的 Git 对象一致，见 `preparation/deployment-identities.json`、`newapi-controller-self-test.txt`。该安装版本的 image-validation 在 archive owner/hash校验、load后，用实际 image ID（非可变 tag）运行 `docker run --rm --pull never --network none --entrypoint /kkai-migrate IMAGE_ID --describe-contract --dialect postgres --json`，继而 `require_newapi_schema_contract` 与 `newapi_tenant_online_preflight.py --image-id IMAGE_ID` 核对 live DB/扩展/rollback/writer，成功才替换idle。正式stage输出 `KKAI_TENANT_ONLINE_PREFLIGHT=ready` 后才进入候选任务并最终 `staged`，源代码入口为安装pin的 `scripts/production/kkai-newapi-manual-deploy` image-validation段；完整成功证据在 `newapi/stage-after-pause.stdout.log` 和 `.local-releases/kkai-prod-20261115.1794758551-f3334a7da.stage-cf6zi73x.json`。没有声称另存了未实际输出的 describe-contract JSON。

首次stage被会员恢复未暂停门禁安全拒绝，未上传、candidate=none；随后官方控制暂停/排空并复用原archive，成功stage。原模式apply，原generation `8a5ab96b93b242ae9bccd1d826d9d92a`；暂停generation `aab30b56ffe74cea9294f582f58a4bd3`。后端promote和healthy确认、candidate=none、rollout=inactive后，以fresh generation CAS恢复apply；新generation `91df9139059c4151913107b028897242`，green/writer/blue全部acknowledged、synchronized、无inflight。

候选隧道带 `Host: api.kkrich.ltd` 的 `/api/status` 精确返回目标版本，随后关闭本任务socket。首次未设置Host的404符合既有TenantRouting未知Host保护；正式controller探针同样指定health_host。已记录操作示例需携带Host，不将其作为代码回归。

## 灰度和后观察

| 档位 | UTC窗口（每档120秒） | 目标完成HTTP / 5xx | 原active完成HTTP / 5xx |
|---|---|---:|---:|
| 10% | 16:12:57—16:14:57 | 31 / 0 | 455 / 11 |
| 50% | 16:15:34—16:17:34 | 254 / 0 | 181 / 12 |
| 100% | 16:18:56—16:20:56 | 387 / 10 | 0 / 不可计算 |

三档官方JSON均healthy、drift=[]，router/target/image/version/rollout精确匹配。额外HTTP读取器输出固定路由类、计数和延迟；未返回原始路径/消息。100%新槽错误为chat3、other7；旧槽前两档已有chat/other错误。HTTP完成/200不证明业务终态；本次窗口没有可归因image成功样本。零样本记insufficient_samples，未伪装0%错误率；混合路由延迟未当作等价性能基准。

`handoff-ready`通过后只执行一次官方promote，返回 `promoted`，内部已finalized；没有再次finalize。控制器验证router、两槽、writer/唯一leader及对称回退拓扑。立即status和五分钟延迟status均目标版本、green、HEALTHY、stable、rollback ready；最终清理后status再次一致。

基线错误窗15:57:31—16:02:31，候选窗16:24:04—16:29:04，均300秒；16:30:04之后取批准聚合，满足60秒写入保护。基线799事件/12错误；候选802事件/15错误，全部原始汇总保留。候选404一次、502十四次；原白名单标为chat2/unknown13。一次同窗只读固定枚举复核证明unknown13均Gemini动态模型路由502；其余chat404/502各一次，15条均无图片诊断标记。新代码只在ImageExecutionFence/DurableImageExecution图片分支修改诊断，chat/Gemini转发逻辑未改。结合源码边界和原槽证据，将其归为本次变更范围外的故障；不声称已证明具体上游根因，也不把日志事件率当用户失败率。没有重新选择干净窗口或隐藏错误。

## 前端、runtime与公网

上传双端archive/metadata SHA一致；prepare与activate分别成功，CAS绑定实际旧current和当前不可变backend manifest。前端最初上传目录权限与后续shell glob检查问题均发生在prepare前，无指针变化，修正操作后复用原artifact；未重建。当前/previous精确匹配表中坐标。

公网 `/`、`/console/log` HTML与本地不可变index一致，200且no-store；`/assets/kikai-home.html` 200/no-store且SHA256 `ab442b86decb6c129ac25c5b2ce8824ee37498ac6f6f15ff34de390751a7cfc8`一致。所有6个入口JS/CSS版本化URL均200、immutable和SHA匹配。三个较大JS首次Mac直连未压缩传输超时，仅对失败资源使用压缩及已配置Mac代理重查并通过，保留初次失败。`/console-runtime.json` 200/no-store，包含API contract4及全部required capabilities。安装pair gate验证matched且真实delivery有效；rollback-plan为backend-only。

## 清理、保留与限制

已盘点确认的app/infra本地产物、镜像标签和容器引用，以及sys1 upload/backend release/image/frontend树与retention。sys1有185个后端release目录、102个NewAPI镜像标签，因已安装控制器确认v10 schema snapshot仍retained而整体受保护；没有强删或以重放promote清warning。前端122个树，current/previous保护2个，其余120个自退出保护集计时尚未满30天，expire候选0；activate未报清理warning，不执行跳过保留期prune。

本次后端远端上传archive已由控制器移除；候选隧道已关闭。前端远端唯一上传目录中的精确tar和metadata在哈希及已保存receipt语义匹配后删除，目录确认消失；receipt经控制器JSON重新序列化，字节SHA不同但字段语义一致。已删本次本地前端tar（20692745 bytes），保留小型metadata、manifest、完整不可变静态树和证据。本次后端tar保留至少24小时及schema恢复保护；历史backend/archive和旧前端恢复archive保留。FFmpeg/工具链缓存、历史演练镜像、用户工作树备份等不是本次临时产物，未清除。历史未选择的本地前端 `kkai-frontend-20261019.1792424915-d3e640059` archive归属已匹配metadata，但没有本次恢复用途确认，保留待维护清理；负责人为后续基础设施维护操作者，不能宣称所有历史本地文件已清空。

最终运行green/writer均新image，blue旧image；router/Redis/risk-guard身份保持；相关容器restart=0、OOM=false。没有未完成发布事务。schema保护和30天窗口属于受保护保留，不是删除失败；没有为清理而解除保护。

## 精确验证命令及证据索引

证据根：`.local-releases/image-failure-contract-20261115/`（ignored，敏感权限受控）。本记录保留摘要，不把大制品/原始日志纳入Git。

- `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest scripts.tests.test_newapi_slot_http_window`：13通过，0.007s；没有重复已通过的应用测试。
- `BUILD_HTTP_PROXY=http://host.docker.internal:7897 BUILD_HTTPS_PROXY=http://host.docker.internal:7897 scripts/kkai/build-manual-release.sh --schema-contract feature --frontend-mode external`：唯一构建成功。`newapi/build.*.log`、backend metadata/计时JSON。
- `scripts/kkai/deploy-manual-release.sh --stage .local-releases/kkai-prod-20261115.1794758551-f3334a7da.json`：首次preflight拒绝后经官方pause/drain成功；`newapi/stage-after-pause.*.log`。
- 官方 `candidate-status`、Host正确的候选`curl /api/status`、`kkai-newapi-rollout start/apply/status --json/handoff-ready`、`kkai-newapi-manual-deploy promote --expected-infra-sha ee4e460d1ac77e017ee3ad3a4af97beb3b090634 --deployment-protocol router-v3-staged`：均完成；`newapi/rollout-*.json`、`rollout-review.md`、`promote.*.log`。
- `make newapi-status`：立即/延迟/清理后通过；`newapi/post-promotion-immediate-status.txt`、`post-promotion-delayed-status.txt`、`final-status.txt`。
- 按保存的BASELINE/CANDIDATE四个UTC变量执行 `make newapi-error-window`：采集成功，逐组审阅通过；`newapi/post-promotion-error-window.txt`、`error-window-focused.sql/txt`、`post-promotion-review.md`。
- `KKAI_FRONTEND_BUN_BIN=.../tools/bun-1.4.4/package/bin/bun scripts/kkai/frontend-build-release.sh --theme default`（独立ignored输出目录）：唯一成功；`frontend/frontend-build.*.log`、`frontend/artifacts/*.frontend-build-*.json`。
- 官方 `kkai-frontend-manual-deploy prepare/activate`、`kkai_frontend_pair_gate.py`、`rollback-plan`：通过；`frontend/prepare.*.log`、`activate.*.log`、`pair-gate-after.txt`、`rollback-plan-after.json`。
- `python3 .../frontend/verify-public.py`：初次7项通过/3项传输超时；仅失败项 `verify-public-retry.py`：3项通过；合并10项通过，`public-verification-final.json`。
- 清理证据：`newapi/remote-inventory.json`、`local-archive-inventory.json`、`local-image-inventory.json`；`frontend/upload-cleanup.json`、`local-archive-cleanup.json`。会员恢复完整generation/进程证据在`newapi/membership-recovery-*.json`。

全仓测试未运行；本次执行的是此前已通过的相关自动化加正式发布构建/上线门禁，不以本记录替代开发审查文档中的测试明细。剩余验证缺口：真实登录、管理员诊断交互、图片生成/编辑、明确失败终态与退款E2E。**用户接手业务测试，代理未验证**。没有账号/Key/业务数据或付费探针验收。
