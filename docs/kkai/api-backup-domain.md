# 主站备用域名 api.kkai.win

2026-11-01（北京时间）按用户授权启用，供用户验证真实模型请求及长会话。

## 接入方式

- Base URL：`https://api.kkai.win/v1`，复用主站 API Key、账户、余额。
- 初次启用时复用既有 `*.kkai.win` DNS、有效通配证书和 business revision 2 Edge 入口。随后按用户授权切换到下述独立 SaaS 接入；Edge、Worker、镜像、运行环境和 schema 均未更改，没有重启服务。
- 使用已部署 `ResolveTenantByHost` 的平台域名分支，在 `tenant_domains` 新增一行：`id=5, tenant_id=0, hostname=api.kkai.win, prefix=api, status=active`。`api` 是代理申请的保留前缀；没有新建代理租户。
- 此登记依赖 `TENANT_ROUTING_ENABLED=true`，操作时 active、standby、writer 均已确认开启。以后关闭租户路由会使此备用域名返回 404；环境白名单中的主域名不受影响。后续交付须保留这条平台域名登记。
- 该入口仍经过 Cloudflare，不能将备用域名启用等同于 524 已修复，也不包含请求压缩启用。

## 验收记录

- 变更前精确域名无数据库记录；HTTPS 返回 `404 TENANT_HOST_UNKNOWN`。
- 变更后 HTTPS `/api/status`、`/api/site-config` 均为 200，后者识别 `tenant_id=0`、主站 KKAI。
- 同一个本机既有凭据请求两个域名的 `/v1/models` 均为 200，各返回 9 个模型。凭据仅经进程 stdin 传给 curl，没有写入命令参数或本文。
- 未认证 `/v1/models`、POST `/v1/responses`、`/responses` 返回正常 401；`/responses` 有兼容重写标记；有效 gzip 请求配合无效凭据同样返回 401。
- HTTP 跳转到同域 HTTPS，TLS 验证成功；随机未登记子域仍为 404。
- 操作前后从同级 infra 执行 `make newapi-status` 均为 `HEALTHY`、`snapshot stable`、`rollback ready`，版本仍是 `kkai-prod-20261031.1793418174-13d32c3f5`。
- 初次启用只更改域名数据，未运行测试套件。后续已按用户要求完成真实模型 SSE 小请求和合成 2 MiB 请求验证；长会话和用户网络效果仍由用户测试，第三方登录、Passkey、支付和完整控制台流程未验收。

## 独立 SaaS 与优选入口（2026-11-01 北京时间）

在 `kkai.win` 区域配置独立 Cloudflare for SaaS，不依赖 `kkrich.ltd` 的回源域名：

| DNS 名称 | 类型 | 目标 | 代理 |
| --- | --- | --- | --- |
| `api` | CNAME | `saas.sin.fan` | 仅 DNS |
| `api-origin` | A | `51.81.109.154` | 已代理 |
| `_acme-challenge.api` | CNAME | `api.kkai.win.5661a47678486e06.dcv.cloudflare.com` | 仅 DNS |
| `*` | A | `51.81.109.154` | 已代理，原记录保留 |

- SaaS 回退源为 `api-origin.kkai.win`，状态有效。
- 自定义主机名 `api.kkai.win` 的证书、主机名状态均有效。证书到期日显示为 2027-01-29，最低 TLS 1.2，DCV CNAME 用于自动颁发和续期。
- **自定义源服务器必须显式设为 `api-origin.kkai.win`。** 本次仅使用默认源服务器并切换优选 CNAME 时返回 Cloudflare 1000；显式指定后恢复 200。源服务器 SNI 同为 `api-origin.kkai.win`，HTTP Host 保留 `api.kkai.win`。
- 区域 SSL/TLS 保持既有“完全（严格）”。灰云仅用于优选 DNS，HTTP 流量仍经过 Cloudflare SaaS。
- `api-origin.kkai.win` 本身不登记平台站点。TLS SNI 为 origin、Host 为 API 的真实源站请求返回 200 且证书验证通过；Host 为 origin 时返回未知站点 404。
- 主域与备用共用 `saas.sin.fan` 和同一源站服务器，独立的是 SaaS 区域和回源域名，不是完整灾备。

切换后的定向验收：

- DNS CNAME、HTTPS、`/api/site-config` 的平台身份和真实 SSE 请求通过。两域各 3 次 `gpt-6.1-sol` 小请求，全部收到 `response.completed`，无失败。
- 同一 Mac、同一 HTTP 代理出口的小请求首文本中位数：主域 2.315 秒，备用 2.352 秒。`/api/status` 总耗时中位数：主域 0.353 秒，备用 1.101 秒；样本包含波动，不能据此保证全国加速或 524 消失。
- 本机不使用显式 HTTP 代理的 `/api/status` 单次检查两域均 200，主域 0.784 秒、备用 1.266 秒；不代表全国直连情况。
- 验证脚本不保存凭据和原始模型输出；凭据经 stdin 传给 curl。仅改 DNS/SaaS，无代码发布，跳过全量测试套件。

当时的自定义测速脚本位于系统临时目录，未纳入仓库；上述耗时数据需重新测量才能复现。可重复执行的基础检查：

```sh
dig +short api.kkai.win CNAME
dig +short _acme-challenge.api.kkai.win CNAME
make -C ../kkai-infra newapi-status
```

只撤回优选接入时，将 **`api.kkai.win` 这一条**改为 A → `51.81.109.154` 并开启橙云，保留平台域名登记、SaaS 回源和证书记录；不要改 `*.kkai.win` 或主域名。完全停用备用域名才使用下方 SQL。

公网基础检查命令：

```sh
curl --proxy http://127.0.0.1:7897 -sS --max-time 15 https://api.kkai.win/api/site-config
curl --proxy http://127.0.0.1:7897 -sS --max-time 15 https://api.kkai.win/v1/models
curl --proxy http://127.0.0.1:7897 -sS --max-time 15 https://unregistered-probe.kkai.win/api/status
make -C ../kkai-infra newapi-status
```

## 精确停用

需要撤回时，经 `sys1` 对生产库执行以下事务。先核对返回行数为 1；不要修改代理站记录或关闭全局租户路由。停用后该域名恢复未知站点响应，主域继续使用。

```sql
BEGIN;
SET LOCAL lock_timeout = '1s';
SET LOCAL statement_timeout = '5s';
UPDATE tenant_domains SET status='disabled', updated_at=CURRENT_TIMESTAMP
WHERE id=5 AND tenant_id=0 AND hostname='api.kkai.win'
  AND prefix='api' AND status='active'
RETURNING id, tenant_id, hostname, status;
COMMIT;
```
