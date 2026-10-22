# 订阅会员真实联调步骤

范围：当前隔离工作区的会员与 Image Async 整合。仅使用专用测试账户、测试数据库、
测试商品和受限测试渠道。本机真实数据库、本地模拟上游的图片 worker 及本地 SDK 签名
支付回调已验证；实际应用进程的初始化、加密登录、目录发布和权限隔离也已通过。
支付商沙箱与真实模型上游尚未执行。实际结果以
[验证记录](subscription-membership-validation.md) 为准。

2026-10-22 联调规则更新：后台发布后，有效新老会员后续新请求均采用当前档位最新折扣；
取消购买版本及新增组首发系数锁定。已预扣请求按完整原快照结算，订单实付金额、额度和
有效期不变。下方既有执行结果为历史记录，不能替代此规则的定向回归。

## 1. 本机真实数据库

本轮两个临时容器已清理。复跑时创建任务专属容器；以下命令只使用本机已有镜像，端口
仅绑定回环地址，数据库位于临时内存目录。不要复用其他任务的容器或已有应用数据库。
fixture 自动创建、清理自己的随机 database/
schema，使用正式迁移与真实行锁；环境变量缺失会跳过对应子项，`SKIP` 不算通过。

```sh
cd /Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai
docker --context desktop-linux run --detach --pull=never --platform linux/amd64 --name kkai-membership-mysql-test --label codex.task=subscription-membership --publish 127.0.0.1::3306 --tmpfs /var/lib/mysql --env MYSQL_ALLOW_EMPTY_PASSWORD=yes --env MYSQL_ROOT_HOST=% mysql:5.7
docker --context desktop-linux run --detach --pull=never --platform linux/amd64 --name kkai-membership-postgres-test --label codex.task=subscription-membership --publish 127.0.0.1::5432 --tmpfs /var/lib/postgresql/data --env POSTGRES_HOST_AUTH_METHOD=trust postgres:9.6
docker --context desktop-linux exec kkai-membership-mysql-test mysqladmin ping --silent
docker --context desktop-linux exec kkai-membership-postgres-test pg_isready -U postgres
```

两条 readiness 命令成功后再执行测试；启动未完成时稍后重试 readiness，不把连接失败当作账务失败。

```sh
KKAI_MEMBER_MYSQL_PORT=$(docker --context desktop-linux port kkai-membership-mysql-test 3306/tcp | cut -d: -f2)
KKAI_MEMBER_POSTGRES_PORT=$(docker --context desktop-linux port kkai-membership-postgres-test 5432/tcp | cut -d: -f2)
export KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN="root@tcp(127.0.0.1:${KKAI_MEMBER_MYSQL_PORT})/?parseTime=true"
export KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN="postgres://postgres@127.0.0.1:${KKAI_MEMBER_POSTGRES_PORT}/postgres?sslmode=disable"
KKAI_MEMBERSHIP_ENABLED=true go test ./pkg/kkaimigrate -run '^TestMembershipRealDatabaseTransactions$' -count=1 -timeout=120s -v
go test ./pkg/kkaimigrate -run '^TestImageAsyncRealDatabaseContract$' -count=1 -timeout=120s -v
docker --context desktop-linux rm -f kkai-membership-mysql-test kkai-membership-postgres-test
```

期望两库子项均 `PASS`：9→10→11 迁移保留旧历史；并发付款只发一份权益，并发预留
不超支；超额封顶、回滚、原账户返还及钱包回退正确；图片受理失败无残留扣款，旧 fence
不能改账。这些测试使用本地付款事实与用量，不会访问支付商或模型上游。

## 2. 独立 HTTP 测试实例

本轮已在全新隔离 SQLite 上通过实际应用进程执行 27 项检查。一次性复现脚本保留于：

```sh
python3 /tmp/kkai-membership-http-b61s7ki7/run_http_integration.py
```

它创建独立运行目录、随机测试凭据和端口，先用实际应用初始化基础表，再执行默认
schema 9、显式 10、显式 11 迁移。测试完成后停进程并删除数据库和临时公钥；不会创建
支付订单。该脚本是本轮临时验证产物，系统清理 `/tmp` 后不保证保留。需要常驻沙箱时按下述步骤配置。

使用已经完成应用初始化、至少为 schema 9 的专用测试库；不要把上一节自动清理的
fixture schema 当作常驻应用库。由执行者在本机安全注入 `KKAI_SANDBOX_SQL_DSN`、
`SESSION_SECRET` 和 `CRYPTO_SECRET`，不写入仓库或聊天；不继承生产 Redis、渠道或支付配置。

```sh
: "${KKAI_SANDBOX_SQL_DSN:?set the isolated test database DSN}"
: "${SESSION_SECRET:?set a temporary test session secret}"
: "${CRYPTO_SECRET:?set a temporary test encryption secret}"
export SESSION_SECRET CRYPTO_SECRET
printf '%s\n' "$KKAI_SANDBOX_SQL_DSN" | go run ./cmd/kkai-migrate --dsn-stdin --target 10 --timeout 120s
printf '%s\n' "$KKAI_SANDBOX_SQL_DSN" | go run ./cmd/kkai-migrate --dsn-stdin --target 11 --timeout 120s
printf '%s\n' "$KKAI_SANDBOX_SQL_DSN" | go run ./cmd/kkai-migrate --dsn-stdin --observe --current --json --timeout 30s
SQL_DSN="$KKAI_SANDBOX_SQL_DSN" LOG_SQL_DSN= REDIS_CONN_STRING= FRONTEND_MODE=external KKAI_NODE_ROLE=serving DISABLE_BACKGROUND_TASKS=true KKAI_MEMBERSHIP_ENABLED=true go run -tags external_frontend . --port 3001 --log-dir /tmp/kkai-membership-sandbox-logs
```

迁移观察应到 11，启动应通过会员表校验。此命令不构建生产镜像；测试机只开放本地访问，
HTTP 服务自身监听所有接口。另一终端启动现有 default 开发前端：

```sh
cd /Users/tokk/.codex/worktrees/subscription-membership/new-api-kkai/web/default
VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001 bun run dev
```

完成测试账户真实登录；管理员发布黄金/钻石/王者的测试目录，普通用户只能看到和操作自己的订单。
售价、额度与折扣可用测试值，不把它们当作正式售卖配置。

## 3. 易支付复用与付款验证

用户已确认现有易支付在线支付正常，本轮优先验证会员对这条通道的复用。会员使用现有
`PayAddress`、`EpayId`、`EpayKey` 和 `PayMethods`，无需新增商品 ID 或周期扣款产品。
首发套餐价格为 CNY ¥600/¥2000/¥5000（内部 `price_minor` 为 60000/200000/500000），
额度显示值为 600/2000/5000，按现有额度显示配置换算成内部 quota；默认 `QuotaPerUnit=500000`
时为 300000000/1000000000/2500000000。套餐价格以整数分保存，创建订单时锁定；易支付
`money` 使用该订单金额，精确到两位小数，不会套用钱包充值优惠。套餐售价调整只影响新订单，
继续付款仍使用原订单金额。单次请求预扣仍受后端 `int32` 上限约束，套餐总额度不因此截断。

签名表单由现有 SDK 在本地生成，浏览器通过 POST 提交到现有 `submit.php`；会员回调
为 `/api/membership/epay/notify`。回调核对签名、商户、订单、支付方式及精确金额后，
在同一事务内记录收款和开通权益。返回会员页面只刷新服务端状态，不自行授予额度。

这些应用差异可以用隔离实例及符合协议的签名回调验证，不要求另行开通易支付沙箱。
本轮已用实际后端进程完成 84 项，包含冻结金额、三档升级、正钱包余额保留、重复回调、
错误签名/商户/金额/方式、过期收款、异常款按订单号处理及本地失败重试。临时复现命令：

```sh
python3 /tmp/kkai-membership-epay-h58crurp/run_epay_http_integration.py
```

需要验收一次真实收款时，再使用可从网关访问的会员测试实例和商户允许的测试交易；
当前未部署的本地会员接口不能直接使用线上旧地址接收回调。密钥不写入仓库或聊天。

其他支付商及外部环境的配置参考：

| 支付商 | 测试配置 | 回调路径 |
| --- | --- | --- |
| Stripe | 测试 API key、测试端点的 webhook secret；使用单次 `payment` 动态金额 | `/api/stripe/webhook` |
| Creem | 测试 API key、webhook secret、`CreemTestMode=true`、金额币种匹配的一次性测试商品 | `/api/creem/webhook` |
| Pancake | 测试 merchant/private key、测试 Store/Product ID，确认商品属于测试环境 | `/api/waffo-pancake/webhook/test` |
| Epay | 商户提供的测试网关、商户 ID/key、测试支付方式；项目没有统一沙箱开关 | `/api/membership/epay/notify` |

把 `ServerAddress` / `CustomCallbackAddress` 指向隔离测试地址；使用供应商控制台或已有
安全转发方式接收真实沙箱回调。只配置实际要验证的支付商，不需要为了本地测试接入全部商户。
配置 Stripe 时，可先验证无签名请求被拒绝：

```sh
curl --silent --show-error --include --request POST http://127.0.0.1:3001/api/stripe/webhook --header 'Content-Type: application/json' --data '{}'
```

期望非 2xx 且没有权益变化；这只是本地拒绝检查，真实验签成功必须由支付商沙箱发回调。

| 操作 | 期望结果 |
| --- | --- |
| 在 `/membership` 选择黄金并完成沙箱付款 | `POST /api/membership/orders` 创建单次 checkout；跳转返回本身不发权益，验签成功且金额/币种/订单匹配后订单成功、额度只发一次 |
| 从支付商测试控制台重发同一事件 | 返回已处理结果，账户/额度/收款记录不重复 |
| 缺签名、错误签名或篡改正文 | 回调被拒绝，不发权益、不改余额；使用有效签名才能检查后续业务校验 |
| 测试环境模拟金额/币种/支付方式不符、额外或迟到收款 | 保存收款事实并进入异常处理；不误发权益，不把已收款事实丢弃 |
| 在测试网络中断 checkout 响应后继续支付 | 复用已保存结果；外部结果未知时保持待核实，不再次创建可能收费的 checkout |
| 先消费部分黄金额度，再升级钻石、王者 | 同一会员账户、余额通用，等级和有效期按订单更新；旧预留仍按原来源和冻结价结算 |
| 有效老会员、新会员及待付订单并存时发布更高或更低折扣 | 各会员后续新请求均按当前档最新发布值；待付订单金额、额度和有效期不变，付款后也不锁旧折扣 |
| 新增组从未发布到首次发布，再次调价 | 所有有效新老会员的新请求同步采用最新值，不停留在首次发布系数 |
| 请求已预扣后发布折扣，再结算及发起下一笔 | 在途请求按完整原快照结算和返还，下一笔采用最新折扣；历史账本不改 |
| 用另一个普通测试账户查询订单，或访问管理核账接口 | 拒绝越权，管理接口要求 Root 身份 |

异常订单在会员管理的订单列表处理；核对 `/api/membership/self`、订单、收款记录和
资金账本。人工操作记录幂等操作号与原因；重复提交同一操作不得二次发放或扣款。
不得为了制造异常而修改生产订单、供应商真实账单或生产支付配置。

## 4. 真实上游：受限渠道与测试额度

在测试实例登记一个允许真实调用的模型/渠道，设置覆盖该渠道的累计用量硬上限及依据，
使用有限输出和小额测试额度。每个场景记录请求/任务 ID、冻结报价、预留来源、应计、
实扣、剩余额度及供应商结果；不记录 API key、回调签名或完整敏感正文。

1. 普通请求与流式请求分别验证成功、预扣不足、会员不足整笔回退钱包、已发送后断线。
   未知结果保留预留，不重发；确认零费用才释放原来源。
2. Realtime 使用手动响应、单响应及有限输出，连续两轮验证独立授权及跨轮钱包回退；
   缺失终态用量进入核账，不能用客户端断线代替零费用证明。
3. MJ / 原生 Task 分别验证受理、可信成功/失败轮询、缺上游 ID、超时及重复结果；
   未知结果不自动退款，人工结案后晚到结果不能重开账务。
4. Image Studio 在该隔离库启用 `worker_enabled` / `async_submission_enabled`，并以
   `KKAI_NODE_ROLE=leader DISABLE_BACKGROUND_TASKS=false` 运行专用测试进程；验证编辑图片、
   部分交付、恢复与重复投递。只对安全交付图片收费，未交付成本由平台承担。
5. 对已预扣的普通、Realtime 响应轮及异步任务分别跨折扣发布完成，核对旧快照不变；
   后续新请求或新响应轮采用最新档位折扣，不能在结算时重新读取目录改价。

共享核账入口为 `/api/membership/admin/billing-reviews`。图片会员组合已通过本地模拟
上游/R2 服务验证；外部沙箱与模型场景执行后，再把实际命令、事件 ID 和结果补进验证记录。
不以本机数据库通过替代真实支付或上游联调，也不运行全仓测试来替代这些场景。
