# 发布前只读准备记录

日期：2026-11-15。现场观察区间：15:43:58–15:49:49 UTC（北京时间23:43:58–23:49:49）。执行角色：发布/SRE。以下是准备时刻事实，不是本次修复已经发布；后续以精确最终提交和正式stage输出再次绑定目标制品。

## 已通过的准备门禁

| 门禁 | 现场结果 | 证据 |
| --- | --- | --- |
| NewAPI首次fresh状态 | HEALTHY、service active、health ok、snapshot stable、rollback ready | `preparation/newapi-status.txt` |
| 当前应用 | blue，`kkai-prod-20261114.1794644275-8a7bdca8f`，current路径 `/srv/kkai/newapi-releases/kkai-prod-20261114.1794644275-8a7bdca8f` | 同上；不从该命令推断writer/leader/router |
| 已安装NewAPI控制器 | pin `ee4e460d1ac77e017ee3ad3a4af97beb3b090634`；installation-only self-test成功 | `newapi-controller-self-test.txt`、`deployment-identities.json` |
| 控制器源码一致性 | 远端manual deploy、frontend deploy、frontend pair gate、tenant preflight文件SHA-256逐项等于本地精确pin对象 | `deployment-identities.json`；本地使用 `git show PIN:path` 计算并assert相等 |
| Schema | canonical21与两个必需扩展完整，通过官方只读baseline及实际runtime检查 | `newapi-schema-baseline.json` |
| 前端 | format2，api_contract4，当前 `kkai-frontend-20261114.1794654138-55a55f56e` | `deployment-identities.json` |
| 实际前后端/Edge | 已安装pair gate无publish参数运行，`FRONTEND_PAIR_RESULT=matched`；执行当前产物校验、运行时契约和真实delivery probe | `frontend-pair-gate.txt` |
| 当前回退组合 | 当前前端兼容当前rollback backend，`order=backend-only` | `frontend-rollback-plan.json` |
| 目标源码契约 | HEAD `55a55f56e7ae969b090fa475dafec1dece27cc46` 的migrator包、CLI与node_role源码相对当前active源码 `8a7bdca8f6435d1c47bbb26f927ba33baef5e753` 没有变化 | `source-contract-comparison.json` |

原始安全证据目录：

`/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai/.local-releases/image-failure-contract-20261115/preparation/`

本次已确认Sub2API安装身份和v1/清理能力见 `review-release.md`；未在只读阶段运行可能rebaseline/save的Sub2API status。正式发布阶段再执行匹配wrapper status和安装身份核对。

## 数据库及实际runtime契约

官方命令 `sudo -n /usr/local/sbin/kkai-newapi-schema-migrate video-media-metadata baseline` 成功。该命令通过standby身份只读observe当前数据库，检查实际active/rollback/writer契约、rc49状态和现有media runtime的check，不执行迁移或启用migrator：

- runtime min/max/target：`21/21/21`，migration kind `none`，schema management `external`。
- canonical digest：`sha256:84687631f06a71630ad6035a28ce29c91bd24fb5cf824e72e394b7f8fc0680f0`。
- `rc49-online-v1`：`sha256:ff1dbaacac62dd31c931ecb62a497c0dc16be5dbba6e46538ec427739bb7db4e`，ready/safe。
- `video-media-metadata-v1`：`sha256:316d22268ec9be33b406b1e7ff4f582cf1f51a3530544ab778e2dc8e7251a67f`，observe及runtime check通过。
- 三运行时均 `tenant_aware=true`、`tenant_routing_enabled=true`、`separate_log_database=false`，契约一致；本记录不列出敏感运行环境。
- active/writer使用版本 `kkai-prod-20261114.1794644275-8a7bdca8f`、image `sha256:f9768bdee81af93736c5f1f4ce603e87a2355ba8401eea1072c9f51e072d7eaa`。
- rollback使用版本 `kkai-prod-20261114.1794643601-0702ae1c5`、image `sha256:9202d990cbef3c4c256d3344cc9cf247e3b5d4fde2fd8c83b5c1229c5ed128d0`。

**本次选用 `release_schema_contract=feature`。** 两个源码profile的schema数字相同，但实际tenant routing已启用，已安装控制器的tenant门禁明确要求external feature；因此选择由源码、实时数据库和安装门禁共同决定，不是沿用名称默认值。最终提交若改变上述契约相关代码或现场状态漂移，需重新比较。

安装文件已与pin逐字节匹配，确认stage会对exact image执行 `--describe-contract --dialect postgres --json` 并运行 `newapi_tenant_online_preflight.py` 的实时数据库与兼容runtime检查，且在替换idle前完成。正式stage成功时可复用其archive/image绑定验证，不需要在Mac重复load/run，也不需要新建镜像来查询契约。

## 前端与回退坐标

selected frontend metadata SHA-256：`dfe760b6765ac245f4ed5dde615945a0859ea9b9d7c25648348721631a9196ff`。current路径 `/srv/kkai/frontend/releases/kkai-frontend-20261114.1794654138-55a55f56e`；previous路径 `/srv/kkai/frontend/releases/kkai-frontend-20261114.1794643867-45c3846a4`。

actual format2与delivery probe已通过，因此采用 `release-plan.md` format2正常路径：backend canary/promote期间不改变当前前端；完成backend事务和后端观察后再独立prepare/activate新前端，不重复Edge首次接管。当前前端api_contract为4；最终前端仍须兼容字段缺失，保留独立rollback能力。

当前backend rollback manifest为 `/srv/kkai/newapi-releases/kkai-prod-20261114.1794644275-8a7bdca8f.rollback-pair/release-manifest.yml`。本次rollback-plan报告 `backend-only`，只证明当前组合；新前端activate后必须重新检查新组合，不能复用旧结论。

## 可复用只读命令

使用 `release-plan.md` 的 `ssh_options` 和已确认路径。需要刷新相关身份时分别执行，不把全部准备步骤无条件反复运行：

```bash
make -C "${newapi_infra}" newapi-status
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-manual-deploy self-test --installation-only
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-schema-migrate video-media-metadata baseline
ssh "${ssh_options[@]}" sys1 sudo -n /usr/bin/python3 /usr/local/libexec/kkai_frontend_pair_gate.py --current /srv/kkai/frontend/current --releases-dir /srv/kkai/frontend/releases --backend-manifest /srv/kkai/newapi-current/release-manifest.yml
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-frontend-manual-deploy rollback-plan --backend-manifest /srv/kkai/newapi-rollback/release-manifest.yml
```

pair gate未传 `--publish-runtime` 或 `--record-deployment`。没有stage/build/promote/commit、schema apply、账号登录、业务请求或生产配置改动。控制器的只读检查可创建短生命周期检查容器/锁，不改变业务表、路由或正在服务的容器。

## 尚待正式执行的证据

最终应用commit、干净构建门禁、新artifact/contract、两端stage计划与候选、三个canary窗口、promote与writer交接、新前端激活、即时/延迟状态、错误归因及受控清理均尚未执行。登录与真实账号业务E2E按用户要求交接，代理未验证。Sub2每阶段自然样本最多30分钟，无真实正样本则按计划停止/回退。

本步骤仅检查文档/源码契约、安装文件SHA及上述只读命令；全部已执行命令成功。没有执行应用测试、前端测试或全仓套件，不将其他角色的测试结果算作本步骤验证。
