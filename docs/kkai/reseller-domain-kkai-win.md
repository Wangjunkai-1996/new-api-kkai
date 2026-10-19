# kkai.win 代理品牌站域名接入记录

记录日期：2026-10-19（Asia/Shanghai）。应用代码仍在独立 worktree；本记录不代表代理功能已经部署。

## 本次已完成的 Cloudflare 配置

通过已登录的 Cloudflare 控制台确认 `kkai.win` 是账户内新注册域名，注册状态为 Active，初始 DNS 记录数为 0。

| 项目 | 本次结果 |
| --- | --- |
| SSL/TLS 回源模式 | Full → Full (strict)，保存后 Overview 已确认 |
| 最低 TLS 版本 | TLS 1.0 → TLS 1.2，界面已确认 |
| TLS 1.3 | 原有开启状态保留 |
| Cloudflare Universal SSL | 已有 Active 证书，覆盖 `kkai.win` 与 `*.kkai.win` |
| DNS-01 API Token | `kkai-tenant-lego-kkai-win` 已创建并显示 Active；服务器验证成功 |
| A/AAAA/CNAME | 已新增 `*.kkai.win` proxied A 至 `51.81.109.154`，TTL Auto；根域未配置业务记录 |

Cloudflare 边缘证书只证明客户端到 Cloudflare 的证书覆盖，不证明服务器已有该域名证书。Full (strict) 要求服务器证书同时通过受信验证和域名匹配。

## 待接入方案

- `kkai.win` 为代理站根域；先不把根域设为 KKAI 主站，也不直接开放注册。
- 第一家测试站使用 `pilot.kkai.win`，仅在应用中登记、启用后提供业务。
- 为 `*.kkai.win` 准备受控通配符 DNS 和独立 Edge 入口。DNS 通配符不能代替 `tenant_domains` 精确登记。
- 新域名入口不使用现有固定域名的 gzip Worker，也不直接复制主站的 SaaS CNAME。
- 原 `kkrich.ltd` DNS 只读核对：`api` 当前是 DNS-only CNAME 至 `saas.sin.fan`，`api-origin` 是 proxied A 至 `51.81.109.154`。这仅是 DNS 记录证据；不能据此断言当前应用版本、活动槽位或源站路由。
- 已按用户确认使用 `/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra`，执行过一次 `make newapi-status` 并只读核对现场 Edge 配置；当时状态为 `HEALTHY`、rollback `ready`。这是本次检查记录，不代替后续操作前的状态门禁。
- 源站入口候选为 `sys1`，IPv4 `51.81.109.154`；先准备只拒绝业务的独立入口，不能将新 Host 代理到未部署租户隔离功能的旧应用。

应用目标配置：

```dotenv
TENANT_ROOT_DOMAIN=kkai.win
TENANT_PLATFORM_HOSTS=api.kkrich.ltd
TENANT_ROUTING_ENABLED=false
```

平台 Host 列表需保留上线时确认的全部平台域名，上例仅说明语义；禁止填入 `*.kkai.win` 或任何代理域名，避免误映射到主站 tenant=0。功能开关只能在 v12 显式迁移及验收完成后开启。

## 服务器入口交付要求

1. 只增加新域名入口，保留原平台 Host 的路由。HTTP 到 HTTPS 使用 308。
2. 通过 DNS-01 配置自动续期的通配符源站证书；若根域也提供页面，证书 SAN 同时包括根域。
3. 保留请求原始 Host，关闭流式响应缓冲，提供现有 SPA 静态资源及 API 路由。
4. 未登记、停用或保留的子域名不得回退到主站；验证 404/421 行为后再公开 DNS。
5. 若采用 Cloudflare 橙云，使用 Full (strict)，登录、API、品牌和价格响应不得被公共缓存；真实验证 SSE、首包等待及长请求行为。
6. 用新域名验证 TLS/SNI、HTTP 308、Cookie/Origin 隔离和双租户完整链路，再开放其他代理。

## 实施记录与范围

用户已明确允许创建限权 Token 并安全配置。Token 仅含 `Zone/DNS/Edit`、`Zone/Zone/Read`，资源仅 `kkai.win`，源 IP 仅 `51.81.109.154/32` 和 `2604:2dc0:100:1f9a::/128`。已保存到现有 SOPS 的 `tenant_cloudflare_api_token`，解密回读验证其值正确且原有键值未变；一次性明文临时文件已删除。服务器验证返回 active，现已安装到独立路径 `/srv/kkai/secrets/acme/tenant-domains/kkai-win/cloudflare_api_token`，root-only、0400，由 systemd credentials 提供给新域名证书实例。

infra 本地已准备：

- 独立 ACME 实例 `kkai-tenant-kkai-win-acme`，证书包含 `kkai.win`、`*.kkai.win`；账户、Token、证书路径独立，使用现有只读 TLS 父挂载。
- 固定拒绝入口工具 `scripts/production/kkai_tenant_domain_edge.py`：HTTP 308、HTTPS 404、no-store，不包含业务 upstream。
- 现场配置只读预览：仅增加两个 vhost，原配置字节保持。预览存放于本机受限目录 `/Users/tokk/.codex/tmp/kkai-domain-20261019/edge-preview.diff`，不是可直接执行的生产 plan。
- 3 项 ACME 隔离测试、11 项 Edge 事务测试及本机固定镜像隔离运行验证通过；已有 ACME 域名策略和相关 playbook syntax 检查通过。新增门禁已接入 Makefile。
- 用户授权后已执行完整 `make check`：`make syntax`、`make policy` 全部通过；`site.yml --check --diff` 在既有 `roles/storage/tasks/main.yml:125` 的预留空间断言失败，整体退出 2。首次检查时 VG 未分配空间约 51.31 GiB，旧门槛为大于 98 GiB；PostgreSQL 文件系统内部余量不能替代 VG 空间。用户选择先解决存储门禁，未接受跳过例外。

- 存储配置已对齐实际布局：PostgreSQL 176 GiB；appdata 16 GiB 严禁扩容，移出自动管理，仅校验现有 UUID、容量和挂载；64 GiB 备份卷也只读验证。新预留预算为 32 GiB（16+8+8），前置计算按 PE 取整的全部拟增长，分配后复核。18 项定向测试通过，真实容量报告验证计划新增 0、旧 appdata 64 GiB 计划被拒绝。重新执行的完整 `make check` 中 syntax、policy、storage 范围通过；整体仍因原 SSH 首次锁定的 WireGuard 地址断言退出 2，未修改 SSH/防火墙保护。基础平台源码也禁止覆盖已接管的独立 Edge；继续专项安装前等待限定门禁例外或另行适配完整检查。没有调整磁盘或删除快照。

用户随后批准限定门禁例外：按完整 syntax/policy、已通过的线上 storage 检查及专用 ACME 检查继续本次域名安装；不修改 SSH、防火墙、磁盘，实际 Edge apply 仍须审阅真实 plan 后另行确认。全量 `site.yml` 未通过，不将专项结果冒充全量通过。

控制器已从干净精确提交安装并自测通过。首轮证书签发因源站本地 DNS stub `127.0.0.53` 返回旧 TXT 集失败；Cloudflare 权威 NS 与公共 DNS 已能看到本轮挑战。未删除共存的 Cloudflare 管理 TXT，失败挑战由 Lego 清理。修复仅对新域名实例固定使用 `1.1.1.1:53`、`1.0.0.1:53`，`CLOUDFLARE_PROPAGATION_TIMEOUT=600`，service 总预算 25 分钟，保留完整传播校验；主站脚本/service 渲染字节及系统 DNS 不变。

修复提交 `9b3aaf7c10c88d761cb72c3e05f977325e0a1817` 已精确安装，应用发布契约 pin 同步；安装自测及 `make newapi-status` 通过，状态 `HEALTHY`、rollback `ready`。本次没有发布应用。修复后的 3 项 ACME 定向测试、完整 `make syntax`、`make policy` 和专用 `--check --diff` 通过；现场差异仅租户 renew 参数与 service 超时。

重试安装成功：专用 playbook `ok=38 changed=4 failed=0`。源站证书覆盖且仅覆盖 `kkai.win`、`*.kkai.win`，Let's Encrypt 信任链有效，期限为 2026-10-19 至 2027-01-17（UTC）；公私钥匹配，文件 root:10005、0440，独立 timer enabled/active。主站 TLS current 及证书/私钥摘要前后完全一致。证据存放于本机受限目录的 `tenant-certificate-verification.log`。

真实 Edge plan 已生成并通过工具内隔离 nginx preflight：

- plan ID：`f80ec20a308546088b3430373281bdcc`。
- plan SHA-256：`99a41d4e64e26aabb4b9a03d48386c21082961c857f056df72433a01761c9c31`。
- 远端目录：`/srv/kkai/frontend/tenant-domains/kkai-win/plans/f80ec20a308546088b3430373281bdcc`。
- 本机完整差异：`/Users/tokk/.codex/tmp/kkai-domain-20261019/edge-real-config.diff`。
- 仅增加根域及合法一级子域两个拒绝 vhost：HTTP 308、HTTPS 404、no-store；无业务 upstream。用户明确批准该真实差异的 inode 保留热加载例外，现已 apply 成功。

首次 apply 因 Docker 返回挂载列表顺序变化而在写入前阻断，配置/挂载内容均未变化。修复按 Destination 排序后比较完整 mount 字典，保留权限、来源和增删检查；新增重排 apply/rollback 与权限变化拒绝测试，13 项定向测试及完整 `make policy` 通过。精确 controller `c2b0502dc21cf14b798014c04f102142f926b474` 已安装、自测并同步应用 pin，沿用同一已批准 plan，未改其 SHA 或实际差异。

源站实际监听地址验证根域、`pilot.kkai.win`、未登记一级子域均返回 HTTP 308、HTTPS 404、no-store，TLS 有效；未知域和多级子域 TLS 拒绝。loopback 80 探测超时，未修改网络；Mac 直连源站探测部分成功后超时，后续公网验收使用实际系统代理和服务器公网路径。

Cloudflare 通配符 proxied A 已创建并读回确认，公共递归 DNS 返回 Cloudflare 地址；服务器通过正常公网 DNS、Mac 通过现有系统 HTTP 代理分别验证两个一级子域的 HTTP 308、HTTPS 404、no-store、可信 TLS、Cloudflare 响应及无跨站 Cookie。真实 Edge status 为 `reject-only`、`TENANT_BUSINESS_ENABLED=false`。原配置快照和事务记录保留，可按同一 plan 执行文档化 rollback。

原有 Worker 未提交改动已恢复，SHA-256 与操作前备份一致；未混入本次 infra 提交。

新域名拒绝入口及 DNS 已完成；尚未执行数据库迁移、代理应用发布或开放代理业务。当前 404 是预期的预接入状态，不会显示 KKAI 主站或开放客户注册。部署要求干净且精确提交的 infra checkout；现有无关 Worker 改动已恢复，未混入本次提交。

现场 nginx.conf 绑定源位于已部署 release。infra 的不可变 release 规则禁止直接修改；新工具的 acknowledgment 参数本身不构成授权。本次已完成强制专项验证、精确安装、实际证书和真实 plan SHA/diff 审阅，并取得专属受控增量例外后 apply；没有用全量模板覆盖其他服务。本次授权仅针对该真实差异，不是以后修改 release 的通用授权。

infra 具体操作边界见 `docs/runbooks/24-tenant-domain-kkai-win.md`。完成拒绝入口验收后再添加 proxied A；新域名可解析仍不代表代理业务已开放。

## 本次验证命令与结果

在确认的 infra checkout 执行：

```sh
/opt/homebrew/Cellar/ansible/14.1.0/libexec/bin/python roles/acme/tests/test_tenant_namespace.py -v
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/test_tenant_domain_edge.py -v
make syntax
make policy
ansible-playbook playbooks/tenant-domain-acme.yml --check --diff -e ansible_host=sys1
ansible-playbook playbooks/tenant-domain-acme.yml -e ansible_host=sys1
make newapi-status
git diff --check
```

以上最终结果通过，ACME 3 项、Edge 13 项。完整 `make check` 已按授权执行，syntax/policy/storage 通过，但整体被旧 SSH 首次锁定断言阻断；用户批准限定专项例外，没有把全量结果改称通过。应用全仓测试、构建、schema 迁移和业务端到端未运行，本次没有发布代理功能。

临时验收脚本保留在受限证据目录。公网 Mac 验证的精确命令：

```sh
KKAI_VERIFY_HTTP_PROXY=http://127.0.0.1:7897 python3 /Users/tokk/.codex/tmp/kkai-domain-20261019/verify-tenant-ingress.py
```

同一脚本通过 sys1 正常公网 DNS 复核；证书验证脚本通过 sys1 执行，检查信任链、SAN、有效期、公私钥和 systemd 状态。Edge `status` 使用上文精确计划路径及 SHA，结果 `TENANT_EDGE_READY=reject-only`。未在生产执行一次实际回滚切换；恢复快照、inode/hash 校验和定向回滚测试均保留。

后续应用/数据库门禁及现有验证结果见 [实现记录](./reseller-multitenant-implementation.md)。
