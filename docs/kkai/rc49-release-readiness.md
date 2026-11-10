# rc.49 发布准备与剩余门禁

用户已授权继续验证、提交、受控数据库扩展与上线，约束是保持线上用户的登录、请求和账务连续性。授权不是绕过失败门禁。本记录持续更新，未标记完成的生产步骤不能从本地测试推断。

## 本轮已确认

- `make newapi-status`：HEALTHY，当前 `kkai-prod-20261109.1794239898-a552996db`，blue，快照稳定、rollback ready。
- 已安装 controller `29e9a8e927ea35df939e912baa381e663591f4a2` 的 installation-only self-test 通过；独立 schema21 状态为 safe，canonical digest `sha256:84687631f06a71630ad6035a28ce29c91bd24fb5cf824e72e394b7f8fc0680f0`。
- 三个运行进程的契约均为 `(14,21,14)`；active/writer 为 `a552996db`、rollback 为 `fc46400f3`，日志与主数据库共库。该结论来自独立 schema 观察，未把普通 status 当作 schema/topology 证明。
- 有界只读表观察：PostgreSQL18.4，rc49三列和审计索引均不存在；审计表估计776761行、521216000字节，quota_data估计814163行、213417984字节。计数是统计估计，不是客户数据查询。证据在 `.local-releases/rc49-integration/production-schema-observation.json`。
- 上述是本轮取证快照，未来真正切流前仍按手册要求刷新必要证据。

## 最终数据库方案

保留 canonical21，使用独立 `rc49-online-v1` 回执验证三列与并发审计索引。新二进制契约为 `(21,21,21)` + `required_expansions`；旧 max21 仍能读取原数据和使用默认值写入。应用启动不做 DDL。

专用协议 `rc49-expansion-v1` 绑定精确镜像、控制器、数据库和三进程身份；主表 NOWAIT、短超时，索引 `CONCURRENTLY`，失败后的无效索引只能独立确认修复。canonical22 不属于本次交付。生产流程见同级 infra 的 `docs/runbooks/33-newapi-rc49-online-expansion.md`。

完整镜像仅构建一次：通过 `--prepare-online --planned-deployment-protocol rc49-expansion-v1` 形成不可变准备产物，先供独立迁移工具使用；扩展成功后 `--stage-rc49` 重用原始镜像/metadata并再次核验。普通 `--stage` 仍拒绝准备产物，不篡改JSON、不重建相同提交。

## 验证状态

- 后端根模块完整运行已执行，201秒；初轮13个失败包分别排查，测试配置污染与真实回归分开修复。完成的包、准确命令和最终待项由 `.local-releases/rc49-integration/verification-repairs.json` 记录。不得将初轮全套记为一次性通过。
- relaykit、tokenkit、third_party/tokenizer、build/kkai-image四个独立Go模块通过各自完整测试。
- 前端完整typecheck通过；完整Vitest初轮3940项中4项失败，均修复后定向通过；完整build通过。全局lint发现163个未修改文件错误，format剩23个未修改文件，保留为既有检查缺口，未据此扩大清理。后续cc12h解析修复也完成直接测试、类型和构建验证。
- 浏览器使用明确的本地API fixture验证令牌余额开关、旧后端能力隐藏、移动端CSV及真实下载脱敏；这是页面验收，不是生产或真实后端端到端。
- 新迁移真实PostgreSQL演练通过：canonical账本逐行不变、旧字段写入、NOWAIT锁冲突原子退出、事务失去授权回滚、并发索引超时与受控修复。旧迁移positive用例适用旧artifact，当前版本会明确跳过，并用负例确保不能误授权旧协议。
- 手工构建脚本、部署脚本和准备产物测试通过；基础设施 `make policy` 首轮在安装CLI的新增命令计数夹具失败，修复后从失败步骤继续执行剩余全部检查通过。运行时参数改变后已单独复跑对应测试通过。原始日志保留失败和续跑证据。

## 本地门禁完成与控制器安装

精确当前版 `a552996db2adb65d9e2074dbfc2e97962d0ef9bd`、回退版 `fc46400f3fed2647fafadd974ea1fc8d005d6015` 和新代码实际main程序在独立PostgreSQL/Redis上完成HTTP混跑。最终新源码内容摘要为 `2922f2a7ab30dd393a77c71bf675805aada75c15b20f0f30f987c7d41679b185`。旧注销→新500已修复，定向模型/服务测试、18项会话复验和候选退出后40项旧版连续性检查通过。小规模缓存测量通过，不是生产容量证明；范围和未覆盖的durable任务/会员writer交接均见 [完整验证清单](rc49-full-validation.json)。

基础设施已本地提交并安装 `8fe80d1a4f2e6a6233b16b7de32aff3a60a8e9fa`，self-test/new CLI检查通过，应用pin已同步；未执行代码托管网络操作。安装后 `make newapi-status` 仍为原版本、HEALTHY、rollback ready；独立schema21状态及三进程契约复核成功。生产前端已实读确认format2、API3。

生产新增表默认权限已只读确认；独立本机非superuser迁移者/应用/只读角色验证通过。已安装的核心DR备份任务具有负载门禁；异地target只读校验21份加密快照有效，尚须记录本次扩展前的新备份准确身份。该链路不等于PITR。

## 仍须完成

1. 最终本地应用提交、正式镜像与机器契约。
2. 新备份校验、专用在线扩展及旧版回退证据。
3. 真实生产候选业务验收、各档灰度、promote、即时和延迟健康/错误复核、前端发布与清理。

截至本记录，已安装控制器并启动受控专库备份；没有生产DDL、应用镜像上传或业务切流。生产业务仍由原版本处理。正式候选业务验收需要专用测试凭据，已向用户询问文件路径；独立准备继续进行。
