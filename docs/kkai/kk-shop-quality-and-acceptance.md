# KK 商城质量门禁与验收计划

版本：D2；日期：2026-10-28；状态：商城业务已有本地实现与定向测试。本文定义完整验收基线，实际执行证据见[实施状态](./kk-shop-implementation-status.md)；计划、设计稿演示及已有充值测试不计作商城完整验收通过。

依据：[PRD](./kk-shop-prd.md)、[业务规则](./kk-shop-business-rules.md)、[API 契约](./kk-shop-api-contract.md)、[UI 规格](./kk-shop-ui-spec.md)、[复审问题](./kk-shop-review-findings.md)。开发入口见[开发总纲](./kk-shop-development.md)，实现边界见[后端](./kk-shop-backend-development.md)与[前端](./kk-shop-frontend-development.md)开发文档。规则冲突先修正文档和联调 fixture，不由测试擅自改变交易含义。

## 1. 完成标准与当前证据

首版完成要求：18 项 REQ、11 个 PAGE、24 项 AC 和 40 项 BR 均有对应实现与证据；自动和人工交付、异常收款、退款、售后、安全领取都属于首版。没有购物车、物流、多商家、API 额度支付或自动渠道退款能力，不应出现伪入口。

| 证据层级 | 当前已有 | 不能据此声称 |
| --- | --- | --- |
| 文档 | 产品、业务、设计和技术契约；本文件提供完整追踪关系 | 业务功能已完成、经营参数已确认 |
| V2 原型 | [视觉复审](./kk-shop-visual-review.md)记录 11 页桌面/390px、26 个状态展示检查、25 项原型状态断言，以及部分浏览器交互 | 26 条完整业务流程均走通；真实支付、数据库、权限或密文存储通过 |
| 原型局限 | 320px 为抽样；15 组 token 对比度最低 4.58:1；完整 C–H 浏览器链路、200% 缩放、全程键盘/读屏、剪贴板拒绝注入仍待补 | 完整 WCAG 达标、全部原型交互已复测 |
| 现有应用测试 | 充值核销、回执恢复、outbox、会话/证明、钱包前端均有定向测试 | 商城能够直接复用充值核销，或新 schema 自动兼容充值 |
| 本地应用验证 | Go/前端定向回归、本地 TLS 假网关签名回调与查单、TopUp/quota 隔离；准确命令见实施状态 | 真实 Epay 商户已验收或商城已上线 |
| React 浏览器局部验收 | 隔离 Chrome、实际 React、全 API mock；1440×1000 / 390×844 明暗目录、结账、付款恢复、秘密清理及后台表单；[脱敏证据与 14 张截图](./kk-shop-screenshots/implementation/browser-review.json) | 真实后端/支付商 E2E、真实证明提供者或全部 B 层/无障碍矩阵通过 |
| 隔离真实数据库 | PostgreSQL 18.4 canonical 18→19；MySQL 5.7 schema 19 DDL；两库独立连接业务/锁定矩阵 | MySQL 完整历史迁移已通过、所有数据库版本或生产故障转移已验收 |
| 2026-10-27 文档阶段（历史） | 当时仅核对源码、测试入口、编号与文档链接 | 用当时的文档检查替代后续应用测试 |

V2 原型的冻结 SHA256 为 `17b79c15ffe5045bcaecc1cfe88782d317466fc134f1a01c5076bf4002fee8fc`，历史证据仅适用于该版本。`/tmp/kk-shop-state-check.cjs` 等临时脚本不是永久测试资产；文件缺失或内容变化时，不能把历史 PASS 当作当前结果。正式实现需要本文件所列可重复的仓库内测试。

### 1.1 已确定开发边界与待经营配置

| 已确定开发要求 | 开发处理 |
| --- | --- |
| 主站单商家、会话登录、单商品单件、自动与人工交付 | 写入服务端校验；租户/PAT/API Key 不得绕过 |
| 商城资金与充值额度/充值返佣隔离 | 复用渠道验真能力，商城不调用 TopUp 核销器 |
| 金额为整数最小单位，API 以十进制字符串传输 | ID/version 也为字符串；时间 UTC RFC3339；精度、溢出、币种逐层校验 |
| 独立收款/交付/售后/退款/风险事实 | 主要状态与并列异常提示一致，不能相互覆盖 |
| 凭据加密、专门领取、版本窗口、操作证明、审计 | 服务端逐次授权，UI 遮罩不作为安全边界 |
| 新单关闭后继续存量责任 | 回调、核实任务、历史订单、售后、退款仍可处理 |

待确认的是首批商品与授权来源、真实价格/币种/首发渠道、渠道查单关单及退款证据能力、预留时长/待付款上限、人工 SLA/服务时段/容量、查看时长、质保/退款政策、保留和销毁期限、责任人及提醒阈值、预计规模。测试用显式虚构 fixture；这些字段缺失时允许草稿，禁止真实上架/收款。候选的 15 分钟、24 小时、7 天不是生产默认。

结构化政策 DTO、嵌套资源版本归属、管理员领取版本、人工证据规则及操作证明 scope 已从 API 契约 D1 基线进入本地实现，验收按该结构核对共用 fixture 和证明允许列表、构建及消费。首发渠道method、回调ACK及实际核验步骤在对应实现前继续补齐；真实经营值未定不阻塞领域逻辑和隔离测试。

## 2. 测试分层与确定性 fixture

| 标记 | 层次 | 核心证据 |
| --- | --- | --- |
| U | Go/TypeScript 纯规则单元测试 | 精确输入输出、边界时刻/金额、结构化政策计算 |
| DB | model 事务与真实数据库集成 | 提交后的实收/归属/版本/责任记录、唯一约束、回滚结果 |
| H | Gin HTTP/路由与网关适配测试 | 实际中间件链、HTTP 状态/稳定 code、响应字段/缓存头、渠道请求和 ACK |
| W | service/worker 故障恢复测试 | 持久任务状态、重放后同一业务结果、责任队列与审计 |
| FE | Vitest + React Testing Library | 用户可见状态、按钮/表单行为、缓存清除、请求契约 |
| B | 真实浏览器端到端与可访问性检查 | 路由、完整点击/键盘流程、截图、网络响应及受控剪贴板故障 |
| C | 选定渠道隔离联调 | 经过验证的商户/环境/原交易身份、原始证据受控保存、脱敏结果 |

测试准备统一要求：

1. 显式创建主站买家 A/B、租户买家/管理员、主站管理员、禁用用户、会话和 PAT；用户组、渠道设置、余额、返佣边界、缓存和时间均由 fixture 初始化。涉及全局 DB/settings 的测试恢复现场，不用 `t.Parallel()` 竞争共享状态。
2. 自动商品准备两份不同的无效演示凭据；最后库存测试只准备一份。人工商品准备固定字段、容量和结构化 SLA。价格示例 `{"amount_minor":"3900","currency":"CNY","exponent":2}` 仅属 fixture。
3. 时间使用注入的固定时钟；分别断言截止前、等于截止、截止后。并发用两个独立连接和可控 barrier/通道确定提交顺序；不能靠 sleep、随机循环、大量 goroutine 或重复跑“碰概率”。超时只防测试挂死，不用耗时比较证明正确。
4. 网关 stub 由明确脚本驱动：创建成功/明确未创建/创建未知、可信收款、签名错误、重复交易、异金额/币种/商户/环境、查询未知、可靠关闭。记录交易引用和调用次数，禁止测试连接真实收款环境。
5. 故障在事务提交、回执落库、任务领取、交付提交、ACK、通知发送等明确边界注入。恢复重新创建 processor/服务对象，读同一数据库，不能只重调未持久化的内存对象。
6. Go 新增或重写测试用 `require` 做准备/致命断言，`assert` 做值断言；测试行为与资金/归属不变量，不锁住私有常量、SQL 字符串或函数分层。前端用语义查询与真实用户事件，不以大 DOM 快照代替流程断言。
7. 输出证据只含虚构标识与脱敏摘要；日志/快照禁止保存测试密钥、完整凭据、DSN、支付入口。秘密泄漏测试可用可识别的虚构哨兵，在受控捕获器内部断言不存在，不把内容打印到报告。

## 3. REQ 与 PAGE 追踪矩阵

每项需求至少一个直接验收入口；“共享 AC”不免除该页面自身的错误、空载与身份边界。

| REQ | 交付能力 | 验收映射 | 主测试层 |
| --- | --- | --- | --- |
| REQ-01 | 搜索/分类/分页/公开可售目录 | AC-01、13、16；PAGE-01 | H、FE、B |
| REQ-02 | 商品规格、金额、交付/查看/售后条件 | AC-01、14、16；PAGE-02 | H、FE、B |
| REQ-03 | 登录单件下单与不变快照 | AC-02、22；BR-02、03、30 | DB、H、FE |
| REQ-04 | 正确付款与未知结果恢复 | AC-03、04、19、23 | DB、H、W、C |
| REQ-05 | 本人订单、状态与历史责任 | AC-06、15、22；BR-31 | H、FE、B |
| REQ-06 | 唯一自动交付与版本领取 | AC-05、06、17、24 | DB、H、W、B |
| REQ-07 | 人工资料、SLA 与交付 | AC-07；BR-15、16、27 | U、DB、FE、B |
| REQ-08 | 售后、追加记录、补发 | AC-08、17、24；BR-25、26 | DB、H、FE、B |
| REQ-09 | 真实退款及后续资金事实 | AC-09、20、21；BR-21～24、32 | DB、H、FE、C |
| REQ-10 | 商品草稿/上架/下架与历史保护 | AC-01、02、15、16 | DB、H、FE、B |
| REQ-11 | 安全库存导入、预留、隔离 | AC-05、10；BR-28 | DB、H、FE |
| REQ-12 | 人工/异常队列及风险操作 | AC-07、15、18、22 | DB、H、FE、B |
| REQ-13 | 售后审核、补发、退款处理 | AC-08、09、17、18、20、21 | DB、H、FE、C |
| REQ-14 | 晚到/重复/无法匹配收款 | AC-04、11、19、21、23 | DB、H、W、FE、C |
| REQ-15 | 状态提示与独立通知重试 | AC-12、15；BR-14、29 | W、H、FE |
| REQ-16 | 会话、所有权、秘密、审计、删除 | AC-06、18、22、24；BR-17、18、28 | DB、H、FE、B |
| REQ-17 | 响应式、主题、键盘、i18n、错误 | AC-13、14；所有 PAGE | FE、B |
| REQ-18 | 真实待办/低库存/收款概览 | AC-15；BR-08、31、35、37 | DB、H、FE |

| PAGE | 必走场景 | 额外断言/证据 |
| --- | --- | --- |
| PAGE-01 | 有商品、无商品、筛选无结果、请求失败、售罄、非主站 | 清筛选/重试可达；下架直接链接拒绝；真实价格和币种；分页稳定 |
| PAGE-02 | 自动/人工详情、访客购买、缺货/容量满、长规则 | 登录回来重新核验且不自动下单；完整规则文字可读；图片失败仍可理解商品 |
| PAGE-03 | 条款未选、变价、无渠道、创建未知、回跳、登录过期 | 不静默改价；原 key/attempt 恢复；不能以回跳或“已付款”按钮确认成功 |
| PAGE-04 | 无订单、多页、全部筛选、异常与退款并存 | 只列本人；无秘密/完整资料；状态动作指向原单 |
| PAGE-05 | 自动/人工/待补资料、暂停、到期、退款、补发、额外款 | 付款/交付事实保留；每次 copy 重新授权；退出/跳转/切账号清明文 |
| PAGE-06 | 普通申请、已有活动申请、追加、拒绝、质保外 | 活动案件唯一；历史不可编辑；内部备注不可见；申请不解锁或关闭领取 |
| PAGE-07 | 待交付、逾期、低库存、未知款/退款、无待办 | 数字与对应列表同一条件；实收与应收/退款分开；明确区间与币种 |
| PAGE-08 | 缺经营字段草稿、回填、发布失败、修改、下架 | 文本转义；人工 SLA/容量/查看时长保存；旧单快照不变；演示不能真发布 |
| PAGE-09 | 正常/重复/坏行导入、预览后并发冲突、隔离、单条查看 | 全成或全不成；脱敏行号错误；敏感 body 不进自动审计；无批量明文导出 |
| PAGE-10 | 双管理员交付、缺资料、逾期、晚到款、无映射款、风险暂停/恢复 | 冲突显示最新状态；责任/证据/公开说明可追踪；不允许手工“已付款” |
| PAGE-11 | 补发、原款/额外款退款、未知/失败/重试、外部调整/争议 | 同一责任下尝试有链；只有可信证据成功；不重复退赔；完整键盘弹层操作 |

## 4. AC 实施验收矩阵

下表中的“记录”指隔离测试数据库和脱敏 HTTP 证据。DB/H 断言先于浏览器结论；任何测试未实现或被 skip 均记为未验证。

| AC | 确定性场景与通过条件 | 层次 | 必须保留的证据 |
| --- | --- | --- | --- |
| AC-01 | 上架 A、售罄 B、下架 C；目录筛选及直链只见允许项，C 不能下单，A 详情规则齐全 | H、FE、B | 列表/详情响应、直链拒绝、桌面/小屏截图 |
| AC-02 | 同 key 并发/超时重试返回同单；未得 order_no 时只读按原 key 恢复；同 key 异请求冲突；下单后改价改规则/浏览另一商品，原单不变 | DB、H、FE | 唯一订单/预留、快照前后相等、409 契约、not_observed 不触发换 key |
| AC-03 | 回跳/伪造通知均不付款；可信匹配款才 paid；前后用户 quota、充值赠额/返佣记录不变 | DB、H、FE、C | 验真结果、付款/交付记录、额度及返佣差量为零 |
| AC-04 | 创建已发生但响应丢失；重复成功、成功后失败、到期后成功逐次注入 | DB、H、W、C | 原 attempt 持续核实、实收不丢、交付至多一次、异常可处理 |
| AC-05 | 两连接竞争最后一份；按两种提交顺序测试到期与收款竞争 | DB | 一份库存一个归属、败方无可支付订单、已售不释放；三数据库结果 |
| AC-06 | A 重领同版本；B/访客/租户/管理员买家路径/PAT/API Key 拒绝；普通查询无明文 | H、DB、FE | 完整权限表结果、缓存头、泄漏断言、领取审计 |
| AC-07 | 付款先/资料先各一次；缺资料无假截止；后续补问不重置；双管理员提交首交付 | U、DB、H、FE、B | 固定 due_at、一份交付、冲突返回、公开与内部资料隔离 |
| AC-08 | 重复售后定位同案；普通售后可查看；补发成功后新旧记录关联，旧版停显 | DB、H、FE、B | 售后追加记录、唯一新版本、无二次收费 |
| AC-09 | 原款批准先于交付/领取时拒绝；额外款退款不拒绝正常领取；未知不成功；成功凭证匹配 | DB、H、FE、C | 责任/尝试/证据关联、已退上限、原库存不回可售 |
| AC-10 | 坏行、批内重复、库内已售重复均拒绝；两个有效预览同时确认同凭据；commit 丢响应后按原 key 查批次 | DB、H、FE | 整批原子结果、脱敏行号、最终无重复指纹、恢复接口不返秘密 |
| AC-11 | 同单两笔不同真实交易分别入账，重放各一次；仅一份商品，额外款有独立责任 | DB、H、W、FE | 两条实收、一份首交付；额外退款后商品款仍 paid |
| AC-12 | 交付提交后通知失败、worker 重启再试；订单仍可领；退款通知同样处理 | W、DB、FE | 通知尝试与业务事实分离、库存/退款数量无新增 |
| AC-13 | 320/390/中间宽度/1440px，长内容和错误；全程键盘完成购买、领取和售后 | FE、B | 无整页溢出、焦点/读屏记录、200% 缩放、对比度与非颜色状态 |
| AC-14 | 全部已启用语言切换，含错误/表单/ARIA/动态状态；大金额及跨日时间 | U、FE、B | 所有新 key/占位符齐全；当前七语言；商品原文不送入 t() |
| AC-15 | 下架/停新单时保留未决创建、已付款、售后/退款；运行回调、恢复与旧单访问 | DB、H、W、FE | 新单拒绝、旧责任可推进、概览与队列数量一致 |
| AC-16 | 未填真实渠道/币种/政策或标记演示时尝试 publish/启用支付；绕 UI 直调也拒绝 | H、FE、B | 草稿可存、发布字段错误、无真实 checkout；示例持续标识 |
| AC-17 | 原交付已过期后批准补发，固定新 delivered_at 与原快照时长 | U、DB、H、FE | 新窗口可领、旧版仍拒绝、商品权益/质保未被隐式延长 |
| AC-18 | 无售后也能有理由暂停；无权/旧版恢复拒绝；有退款/争议/到期时解除暂停仍禁领 | DB、H、FE、B | 风险事实、内部/公开理由隔离、审计、动作矩阵 |
| AC-19 | 无映射、短款、异币种、异环境分别录入可信实收；仅原映射可证实时重跑校验 | DB、H、W、FE | 独立 receipt 检索、原身份未改、错误款不履约、原款退款或待核实 |
| AC-20 | 未知结果拒绝新尝试；可信失败仍有责任；无成功/在途/争议证据齐全后重试 | DB、H、FE | 同一 refund 下关联 attempts；失败/未知 UI 分离；冲突无外部动作 |
| AC-21 | 本地退款与同渠道外部退款引用重复、外部部分调整、在途、拒付争议各一例 | DB、H、FE、C | 同一调整不双计、余额不超退、原实收不删、争议冻结相关履约/退款 |
| AC-22 | 每类未结责任分别尝试软删/硬删；删除与新建订单两种先后；禁用账号尝试领取 | DB、H、W | 主站删除入口均拒绝孤立责任；禁用不越权；清责去标识化不级联交易 |
| AC-23 | 同渠道充值/商城各正常、重复、交叉投递，再在目标 schema 重做 | DB、H、W、C | 充值一次 quota/适用返佣；商城一次履约/零额度；跨业务不互认 |
| AC-24 | 当前版本截止前/等于/之后直调 view/copy，刷新、开关售后再试 | U、H、FE、B | 服务端到期拒绝、明文清除、时间不重置、历史与待办保留 |

## 5. BR 交易回归矩阵

下表是测试输入与观察点，不要求为每行创建独立测试文件。相关行可组成带名称的确定性 table test，但每个业务不变量必须实际断言。

| BR | 固定前提与动作 | 通过断言 | 层次/证据 |
| --- | --- | --- | --- |
| BR-01 | 1 件可售，A/B 同时购买 | 成功预留最多 1；另一方缺货；库存唯一归属 | DB；归属/订单计数 |
| BR-02 | 同用户同 key 同请求重放，再换商品沿用 key | 同请求同单，异内容冲突，无额外预留 | DB、H；响应/行数 |
| BR-03 | 注入 price/currency/quantity/quota，金额负数/越界/非法字符串 | 未知字段拒绝；服务端 snapshot 决定金额，数量固定 1 | U、H；400/409与无副作用 |
| BR-04 | 网关收到 create 后丢响应 | 本地原 attempt unknown，查询原引用，create 不换号 | H、W；stub 调用与持久尝试 |
| BR-05 | 原尝试经可信查询确认未创建/不可支付，订单仍有效 | 可新 attempt；旧记录保留；过期订单不能再建 | DB、H；尝试链/状态 |
| BR-06 | 无实收；回跳成功/错误签名/错误商户通知 | 无 paid/交付；不可信通知不写可信实收 | H；响应/ACK及数据库 |
| BR-07 | 同交易 success 两次再 failed | 一个收款、一个首交付、paid 不倒退 | DB、W；业务记录 |
| BR-08 | 相同订单、两个不同交易引用 | 两笔实收，一笔有效商品款，额外退款待办 | DB、FE；事实与并列提示 |
| BR-09 | 可信短款/币种错误收款 | 保留实收，不分配交付，差错责任可检索 | DB、H；金额/差异记录 |
| BR-10 | 收款提交后、履约前崩溃 | 新 worker 从数据库恢复，一次履约或明确异常 | W、DB；重启前后快照 |
| BR-11 | 已释放预留，迟到付款；分别留有/耗尽库存 | 原金额核对；可用则重分配，不可用则已付异常和退款入口 | DB、W；两分支归属 |
| BR-12 | 到期释放与确认付款交错 | 仅一个一致归属；先售出不能被释放 | DB；受控并发双顺序 |
| BR-13 | unknown 在本地到期后释放，再收到成功 | 查询/回调责任未删，按迟到款处理 | W、DB；原 attempt/待办 |
| BR-14 | 已交付任务重放、通知重试、买家重领 | 首交付/库存不增长，返回同版本 | W、H；计数/版本 |
| BR-15 | 已付人工订单缺字段，补齐后再请求补充 | 无假 due_at；齐全时起算，后续请求不静默重置 | U、H、FE；绝对时间/公开缺项 |
| BR-16 | 两管理员同版本不同内容提交 | 一个首交付；冲突者得到最新脱敏状态，原内容不覆盖 | DB、H；双请求结果 |
| BR-17 | B/访客/跨租户/管理员买家路径/PAT/API Key 请求 A 的 secret | 所有入口拒绝，无对象存在性/秘密泄漏 | H；权限矩阵/日志捕获 |
| BR-18 | A 合法 view/copy | 独立 no-store；普通 DTO、日志、通知、存储无秘密；copy 不绕权限 | H、FE、B；响应/缓存检查 |
| BR-19 | 退款批准与旧任务交付两种提交顺序 | 退款先则不交付；交付先则退款保留已交付事实，库存不回售 | DB；原子事务结果 |
| BR-20 | 普通售后申请，再显式 risk-hold | 申请不禁领；暂停后 view/copy/补发一致拒绝 | H、FE；各入口返回 |
| BR-21 | 已操作退款未知，重新打开/重试 | 续同责任/原尝试，不能新退或显示 succeeded | DB、H、FE；尝试数 |
| BR-22 | 证据错误商户/交易/金额/币种/结果 | 不允许成功，冲突证据追加，责任不消失 | DB、H；证据关系与错误 |
| BR-23 | 额外款退款完成 | 有效商品款和正常交付不变，仍可领取 | DB、H、FE；事实/响应 |
| BR-24 | 商品款全退后领取/过期释放 | 拒绝领取，旧库存隔离，历史不删 | DB、H；状态/行关联 |
| BR-25 | 活动售后批准补发 | 新版本关联原单/案件，旧版停显，无新收费 | DB、H；版本链 |
| BR-26 | 补发重放/并发，或退款批准后旧请求到达 | 不再分配，已有结果或明确冲突 | DB、H；库存与版本数量 |
| BR-27 | 有质保快照；用户不查看、晚查看、补发 | 依原约定算保障，点击不延后、补发不暗中缩短 | U、FE；固定时间表 |
| BR-28 | 已售秘密重导入、密文被改/缺 key/字段错误 | 拒绝/隔离；不返回半份秘密/密文；异常可处理 | DB、H、W；泄漏/异常证据 |
| BR-29 | 停新单，有旧 attempt/交付/退款 | 新建拒绝；旧回调/任务/订单/售后/退款继续 | H、W；关闭前后行为 |
| BR-30 | 下单后改价/条款/下架；再购 | 旧 snapshot 不变，新购校验新条件；风险另记 | DB、H、FE；快照比较 |
| BR-31 | 原款已退款，额外款未结 | refunded 主状态和额外款待办共存，不写全部结清 | FE、H；组合 DTO/截图 |
| BR-32 | 批准退款后结果未知，尝试 cancel | 拒绝；只有可靠未发生且无在途/争议才有理由撤销 | DB、H；资金核实证据 |
| BR-33 | 原版到期后补发 | 新 expiry = 新 delivered_at + 原快照时长；旧版拒绝；权益/质保独立 | U、DB、H；两个版本边界 |
| BR-34 | 无售后的风险单；暂停/恢复及 proof 重放 | 有理由/公开说明/审计；恢复不覆盖付款、退款、争议、到期限制 | H、DB、FE；动作与证明 |
| BR-35 | 可信 receipt 无单/身份异常；分配责任、核实原映射 | 可独立定位；只允许可信原归属，全量重验；不改实收或人工成功 | H、W、DB；差错时间线 |
| BR-36 | 可信 failed；分别加入未知、在途、成功或争议事实再重试 | 有冲突均拒绝；安全重试同责任新尝试，旧事实保留 | DB、H、FE；责任/尝试链 |
| BR-37 | 同引用外部退款、本地退款、在途、拒付/部分调整 | 同资金不双计；未明争议冻结；证据清楚后重验；拒付不冒充退款 | DB、H、C；资金对账表 |
| BR-38 | 主站未结各类责任；软删/硬删/禁用与并发建单 | 不遗留孤立责任；禁用认证仍拒绝，管理员承接；交易引用不级联 | DB、H、W；删除双顺序/引用 |
| BR-39 | 同渠道充值/商城正常、重放、交叉投递 | 充值仅一次额度/返佣；商城仅履约；跨业务拒绝核销 | H、DB、W、C；差量与路由 |
| BR-40 | expiry 等于 now 后 view/copy，刷新/开关售后 | 不返回秘密/重置期限；历史待办保留；补发仅走 BR-33 | U、H、FE；截止边界/响应 |

## 6. 必须独立验证的高风险边界

### 6.1 支付与充值隔离

从同一渠道验签入口经过真实业务路由测试，不能只直接调用商城核销函数。准备一笔充值、一笔商城款以及无法匹配的可信款，分别执行正常、重复、乱序和交叉入口投递；渠道前缀不能独立授权业务归属。断言：

- 充值按现有规则一次额度入账、一次适用返佣/outbox；商城 quota/赠额/返佣差量为零。
- 同交易在不同 URL、不同业务前缀、错误环境或错误商户下不会互认；同一可信资金事实不变成两笔收款。
- 保存实收失败不得成功 ACK；实收已保存而履约失败可按渠道协议 ACK 并从持久任务恢复。ACK 格式、可靠查询及关单证据由首发渠道实证确定。
- 明确未创建与创建未知分开；外部请求不持有数据库事务锁；本地超时不能消除渠道资金责任。付款错误只有 `service.ShopPaymentNotCommittedError` 证明创建事务内业务拒绝未提交，才附 `data.request_outcome=not_committed`。分别覆盖事务内拒绝与 attempt 提交后同名 409；裸 400/409/404、提交不明、创建后失败均不能解锁新请求。前端此前已有 unknown 时，后来即使收到标记也保留原 key/body。
- 目标商城 schema 上同时验证原充值。当前 [`CheckWalletPaymentSchema`](../../pkg/kkaimigrate/schema_wallet_payment.go)显式兼容 canonical 18..19，保留回执与物理 schema 核验；不得改成接受任意更高版本。PostgreSQL 18→19 已验证旧钱包数据与门禁，MySQL schema 18 引导错误仍单独阻断完整链路。

### 6.2 数据库与并发

| 数据库 | 必测行为 | 证据要求 |
| --- | --- | --- |
| SQLite | 文件库、多个连接下最后库存/幂等/删除竞争；不发送 FOR UPDATE；busy/提交失败不吞掉责任 | 明确连接数与 fixture；可恢复冲突须有稳定错误或有界重试，不允许两个成功归属 |
| MySQL ≥ 5.7.8 | 真事务与行锁、唯一索引、Unicode/长引用、死锁恢复、导入整批；兼容最低语法能力 | 实际版本与 SQL mode；不能用 MySQL 8 独有约束/锁语法代替；拒绝只凭 DryRun 宣称兼容 |
| PostgreSQL ≥ 9.6 | 真事务/唯一冲突、锁顺序、退款/交付竞争、删除/下单竞争、migration 失败恢复 | 实际版本、受控并发结果；只在新版本跑过时明确最低版本未验 |

共用行锁用 `lockForUpdate(tx)`；MySQL/PostgreSQL 锁定同一稳定订单/用户记录，SQLite 靠其受支持事务与条件写入/唯一约束保证不变量。统一锁顺序写入后端文档，并针对下单/取消/支付/交付/退款/删除交叉路径做双顺序测试。最终一致不是“两个都失败就通过”：可恢复冲突经过已定义重试后，应有唯一结果或明确可处理责任。

关键 DB 不变量：收款身份唯一；首交付唯一；库存一个有效归属；同售后一次有效补发；一个活动售后；同责任不能并发创建两个有效退款尝试；同渠道调整不双计；金额总计不超过对应实收；旧版本不能覆盖新版本。数据库约束和事务都应保护它们，不以进程内锁、按钮禁用或 Redis 锁作为唯一保证。

使用临时 SQLite 文件及独立 `kkai_test_*` MySQL/PostgreSQL 数据库/schema。现有部分 fixture 会 DropTable，不能把任意 DSN 当作安全输入。环境未配置而 skip 只记“未运行”；不得将它写成“三库通过”。新商城矩阵应先校验隔离目标，生产、共享开发库均不得用于此类测试。

### 6.3 故障恢复

| 故障注入点 | 恢复后必须成立 |
| --- | --- |
| 下单已提交、HTTP 响应丢失 | 同 key 返回原单/原预留，不新建；买家可找到该订单 |
| 支付尝试落库后、外部调用前 | 恢复使用原持久引用；不能抛弃尝试换交易号 |
| 外部创建成功、本地入口尚未保存 | 保留 unknown，原引用查单/核实；不再支付一遍 |
| 可信实收落库后、交付任务未完成 | 回执可独立恢复；实收记录和待办仍在 |
| 交付/库存事务中任一写失败 | 两者共同回滚；没有售出却无交付或有交付却仍可售 |
| 交付提交后、通知或 worker ACK 前 | 重放仍同版本；通知单独重试 |
| worker 租约丢失、旧 worker 继续执行 | 提交前检查最新事实/有效租约或 fencing；旧 worker 不抢写新结果 |
| 人工退款操作后页面丢失 | 原责任/尝试继续待核实；不能新建重复退款 |
| 密钥缺失/密文认证失败 | 暂停秘密输出，保留已付事实与可处理异常，不重新收费/随意换货 |
| 服务重启、通知关闭、新单入口关闭 | 核实和存量责任恢复，不依赖用户一直打开浏览器 |

重试耗尽后的死信/异常不能只写日志；后台有责任人、最后进展和可重试/核实入口。手动重放也须经过最新退款/风险/版本约束和操作审计。

### 6.4 权限、秘密与账户删除

HTTP 测试必须挂真实 `UserAuth/AdminAuth`、会话来源/Origin、主站限制和操作证明链，直接调用 handler 的测试不能独立满足验收。

| 身份/请求 | 目录 | 本人订单/领取 | 商城管理/秘密 |
| --- | --- | --- | --- |
| 主站访客 | 仅上架商品 | 需登录；无他人对象信息 | 拒绝 |
| 主站买家 A | 允许 | 仅 A 的订单；领取再校验最新资格 | 拒绝 |
| 买家 B | 允许 | A 的资源与不存在资源同样 404 | 拒绝 |
| 租户用户/租户管理员 | 首版主站边界拒绝 | 拒绝跨站 | 拒绝 |
| PAT/API Key，包括管理员拥有的 token | 不升级身份 | 私有接口拒绝 | 拒绝 |
| 主站管理员会话 | 允许 | 不通过买家接口越权领取 | 普通管理授权；敏感动作精确证明 |
| 已禁用/已注销/被撤销会话 | 依公开目录规则 | 认证拒绝，页面清秘密 | 拒绝，存量任务不因账号禁用丢失 |
| Cookie + 错误 Origin/伪造 Host/跨站请求 | 不能改变可信站点身份 | 写入拒绝；回调走独立验签链 | 拒绝 |

必须覆盖以下接点：

- [`WalletPaymentMutationGuard`](../../middleware/wallet_payment.go)允许 access token 分支，商城不得直接复用。`UserAuth/AdminAuth` 后仍须验证 dashboard session 与主站。
- [`SecureVerificationRequired`](../../middleware/secure_verification.go)是 channel key 作用域。shop scope 必须完整贯通 requirements、策略、context 规范化、签发、消费和事务内再校验；只添加前端 scope 字符串不能工作。测试错误 scope/对象/版本/摘要/key、证明过期、消费重放、身份变更与并发消费；已提交幂等重放仅返回脱敏旧结果，不能重放秘密响应。
- [`AdminAuth` 自动审计](../../middleware/audit.go)现状不持久化请求正文，只临时缓冲至多 64 KiB 响应用于判断 success，落库为路由等元数据；本文件没有认定现存明文泄漏。新增业务审计须显式白名单，导入、人工交付、补发、买家资料与证明正文禁止采集，不靠 password 正则猜字段；成功、校验错误、权限错误、panic/恢复路径均断言哨兵不进入持久日志。管理员 reveal 在包装响应前采用仅状态元数据模式；`ContextKeyAuditLogged` 只跳过 finish，不能取消既有内存缓冲，不得为此关闭其他管理员审计。
- 不仅检查 DB 密文：请求/响应日志、自动审计、错误上报、任务 payload、通知、普通 API DTO、query/mutation cache、Zustand、local/sessionStorage、浏览器历史与 URL 都不能含秘密。审计必须仍有动作、操作者、对象、原因、结果和时间。
- 加密测试验证 key ID、认证标签及对象/AAD 绑定；不同 unit/order 互换密文不能解密，密文损坏/旧 key 缺失不能返明文或半份内容。指纹用带密钥方案，不能通过普通哈希泄露低熵卡密；重复检查覆盖已售、隔离与历史库存。
- view 和 copy 每次服务端授权；页面遇暂停、退款、到期、退出、切用户、离开路由时清当前明文。已送达浏览器的信息无法收回，不把 UI 清除描述为追回凭据。
- 管理员库存查看也按状态测试：available 先原子变为 quarantined 并审计后才返回，reserved 拒绝独立查看，sold 沿关联订单当前版本/退款/风险/到期资格判断。库存查看与下单竞争两种提交顺序都不能留下“已揭示仍可售”或绕过买家预留的结果。证明绑定库存 version；订单管理领取绑定订单 version 与 delivery_id。
- [`checkTenantUserDeletionTx`](../../model/tenant_consumption.go)的主站路径已接入商城未结责任检查；验收必须覆盖 [`User.delete/HardDelete`](../../model/user.go)、本人删除和后台删除入口；分别构造付款未知、待交付、活动售后、争议和退款责任。与下单共用用户状态/锁顺序，跑“删除先”和“下单先”；不能只在商城页禁按钮。
- 允许去标识化时交易/收款/交付/售后/审计引用仍可追踪；保留/销毁任务不得清除未结责任。已销毁的秘密不能通过备份索引或历史领取入口恢复。

### 6.5 退款、外部调整与人工核实

为每笔原收款生成对账表：原实收、已确认返还、在途、争议、剩余责任分别列示，金额始终带原币种。同渠道退款引用在本地尝试与外部调整重复出现时只计算一次；不同引用不能因金额相同而错误合并。

固定测试样例至少包含：正常全退；额外款全退且商品仍可领；商品款已退+额外款未结；失败可安全重试；未知拒绝重试；证据身份/金额不符；本地已执行但发现外部已退；外部在途；未决拒付；部分外部调整。后者只登记和核实剩余责任，不通过接口自动开通商品部分退款或自动补退。

退款审批、登记尝试、登记渠道操作、确认结论为不同动作；不能测试成一个“完成退款”按钮。手工证据要验证来源、操作者、原交易、商户/环境、渠道调整引用、金额币种和结果，保持追加与受控访问；用户截图/勾选不构成自动成功。因退款失败而恢复领取、因退额外款而暂停正常交付，均为阻断缺陷。

`POST /evidence` 的浏览器 source 只能是 merchant_console，不得伪装 provider_query/provider_webhook；observed_status 只是材料陈述，创建材料不能改变退款/实收成功。字段长度、原收款版本、来源/身份、金额及强验证逐项测试，无法回到渠道核实则保持 unknown。退款 attempts/record/verify 统一用退款责任 version；收款 adjustments/review 统一用收款投影 version。用两个不同子对象同时提交相同根版本，确认不能绕过同一资金责任的并发检查。

### 6.6 前端与浏览器

- `/api/shop`、`/api/admin/shop` DTO 用共用脱敏 fixture：字符串 ID/version、Money、UTC 时间、null、202、4xx、未知枚举和 version 冲突。超出 JS 安全整数的 amount_minor 不得损失精度；负数、前导零、指数、分数字符串、越界值被正确拒绝。UI 不以 message 文本分支，不将协议错误默认为成功。
- 人工 SLA 按结构化政策测试 elapsed/business、周末跨窗口、IANA 时区及 DST 跳过/重复小时；固定输入断言 UTC 绝对 due_at。若预先约定资料暂停，暂停区间仅结算一次，原截止与调整事件保留。view/warranty/entitlement 分别计算，不能通过补发查看窗口延长质保或商品权益。
- 未获得订单号或导入批次时，使用原 actor/Host/session 绑定的恢复标记，分别查询 `/order-requests/:key` 和 `/inventory/import-requests/:key?product_id=...`。recorded 返回对象引用，not_observed 不证明失败；GET 无副作用且无原请求正文。更换用户、目标商品或 scope 不泄露他人的结果；恢复标记 TTL 到期也不得自动换 key。重新输入同 key 异内容应冲突，不能为恢复持久化 buyer_input 或库存明文。
- 前端只能按照服务端 `allowed_actions` 提供操作；所有写接口仍再校验。测试“页面已打开后后台批准退款/下架/暂停”等陈旧视图，不能因本地缓存成功展示禁用动作。
- UI 复用现有主题 semantic tokens 和组件；浅/深/系统主题、当前主题预设、长中文/日文/法文/俄文、图片加载失败和减少动效均检查。纸艺插图不承载价格、按钮或交易条件；保留 new-api / QuantumNous 标识。
- 实际启用语言见 [`config.ts`](../../web/default/src/i18n/config.ts)：en、zhCN、zhTW、fr、ru、ja、vi，对应 en/zh/zh-TW/fr/ru/ja/vi 文件。新增错误码、Toast、placeholder、验证文案、ARIA label、动态状态均翻译，插值名称一致；界面语言经 `toIntlLocale` 转换后才用于 Intl。商品/买家内容按原文转义，不能当 translation key。
- 当前 `i18n:check` 扫全 src 且只验证 en/zh，不等于新增商城七语言齐全；需商城局部 key/插值检查加人工语义抽查。不要为测试自动覆盖用户正在修改的 locale 文件。
- 320/390/768/1440px，无整页水平溢出；管理表格允许自身滚动。200% 缩放时核心表单/金额/按钮可达；Tab 顺序、可见焦点、Dialog 焦点约束/Esc/关闭后返回、错误关联、读屏状态公告逐一验。正文对比度至少 4.5:1，不能只依赖颜色。
- 真浏览器验证完整购买→付款核实→交付→领取/复制→售后；人工缺资料→补齐→双端交付；退款未知/失败/重试、额外款、暂停/到期、补发、导入/草稿。mock gateway 必须保持回跳不等于付款；异常用受控服务器 fixture，不能把原型审核工具移入产品。
- clipboard promise resolve/reject 各一次；reject 不能显示复制成功，合法当前内容可手动选择。网络断开、响应丢失、登录过期、多标签页、后退/刷新不新建重复订单/退款；秘密始终不持久化。

付款专用路径已按服务端 `not_committed` 契约验证。付款外已修正授权查询数据库错误传播，以及订单提交后响应读取失败误报普通 404 的路径。确定性注入提交后停用用户，HTTP 返回 `409 SHOP_REQUEST_UNRESOLVED`；普通读取继续隐藏资源，恢复权限后同 key 重放只返回原订单、只占用一次人工名额。前端 hook 验证首次未决错误不会调用恢复标记清理回调，也不会改变 key。该结果不扩展为所有写操作的穷尽验证。

## 7. 代码评审门禁

| 门禁 | 审核问题与通过标准 |
| --- | --- |
| 业务边界 | Router→Controller→Service→Model 清晰；API/worker/回调进入同一事务规则；没有通用商城框架、任意表单系统或“以后用”抽象 |
| 支付 | 没有复用 TopUp 额度/返佣核销；可信实收先持久化；回跳不核销；原交易未知不能换号；渠道边界显式验证 |
| 金额 | 只用整数最小单位与币种规则；解析/累加/差额有范围检查；不浮点计钱，不以饱和隐藏错误，不混币求和 |
| 数据库 | 三库合法、约束真实存在、锁顺序一致、独立连接并发通过；无外部 I/O 持锁；无 raw dialect 假设或 legacy FOR UPDATE 写法 |
| 幂等与事务 | key 绑定 actor/tenant/action/object/规范内容；事务含必要事实与 outbox；重放不能重发/重退；旧版本不能覆盖；冲突不会被泛化为成功 |
| 输入/输出 | `common` JSON 包装；严格写字段/字节上限/固定 schema；显式 DTO 白名单；分页稳定有限；所有权和 Origin 在服务端 |
| 机密与认证 | 精确 shop scope、一次性证明完整链；日志 body 白名单；专门 reveal；无查询缓存明文； key 缺失安全失败并有可处理异常 |
| 退款/售后 | 原款/额外款/外部调整分开；未决争议不退赔；失败不同于未知；补发原子新版本；已售不回可售 |
| 生命周期 | 账户软/硬删与建单竞争受保护；商品下架/停新单不丢旧责任；清理不会删除待核实/退款依据 |
| 前端 | 使用现有 API/组件/主题/i18n；状态无多份相互矛盾的副本；秘密只在组件短期内存；错误和无障碍是完整流程的一部分 |
| 可维护性 | 每个新增 helper 有复用或稳定业务含义；单次机械函数不随意提取；不新增非必要依赖；查询无逐行 N+1；不无限读取凭据/时间线 |
| 测试与声明 | 明确哪些是 existing/proposed、哪些真实跑过/skip；证据对应当前源码；不以覆盖率数字、DryRun、原型截图替代资金/并发验收 |
| 发布/schema | 契约与精确目标源码一致；新增 schema 不随启动自动迁移；旧充值与回退运行版本兼容证据齐全，不发明迁移版本 |

重复收款漏记/重复履约/超额退赔/越权或秘密泄漏为阻断缺陷；权限、未知结果恢复、数据库原子性与原充值回归不能以“下版再修”通过。其他首版功能若与 PRD 不一致也须修复或显式更新范围，不能由测试自行降级。

## 8. 分阶段定向验证命令

本节区分现有共享回归入口与已实现的商城定向入口；只按改动范围选择，实际执行组合及结果见实施状态，不把整段命令都视为已运行。Go 命令在应用根目录，Bun 命令在 `web/default/`；`[no tests to run]` 或真实库缺环境导致的 SKIP 均不是该项验收通过。

用户规定：L0 文档只做 diff/链接/编号检查；L1 模块内检查；L2 影响模块及直接依赖；L3 schema/认证/并发等按相关专门门禁。任何全套命令、预计超过五分钟或耗时未知的命令，执行前必须列出精确命令、为何定向检查不足、预计耗时并取得明确许可。下列 `-timeout` 仅限制测试运行，不保证冷编译、依赖下载或环境启动总耗时；首次耗时未知时同样遵守该规则。

### 8.1 现有可复用回归入口（不是商城测试）

| 变更触及 | 现有命令 | 证明范围 |
| --- | --- | --- |
| 充值核销/共享业务路由 | `go test ./model -run '^(TestFinalizeTopUpConcurrentReplaySerializesOnTheOrder\|TestFinalizeTopUpCommitsOrderQuotaAndOutboxAtomically\|TestFinalizeTopUpReplayDoesNotDoubleCreditOrDuplicateOutbox\|TestFinalizeTopUpRollsBackWhenOutboxInsertFails)$' -count=1 -timeout=2m` | 原充值额度、outbox、幂等事务；不能替代 BR-39 商城交叉测试 |
| 可信实收恢复 | `go test ./model -run '^(TestWalletVerifiedLatePaymentCreditsOnceAndRetainsSecondPayment\|TestWalletPaidReceiptSurvivesAccountingFailureAndCanRetry\|TestWalletLocalDeadlineNeverDiscardsKnownPaymentResponsibility)$' -count=1 -timeout=2m` | 已有钱包迟到/第二笔/实收责任 |
| 共享回执恢复服务 | `go test ./service -run '^TestWalletReceiptRecoveryAfterCrashBeforeCredit$' -count=1 -timeout=2m` | 已有回执崩溃后恢复 |
| 若选用/修改 Stripe 适配器 | `go test ./service -run '^(TestWalletStripeGatewayKeepsOriginalCheckoutAndIdempotency\|TestWalletStripeQueryRejectsDifferentOriginalPaymentIdentity\|TestWalletStripeClosureRequiresAuthenticatedRecheckAndPaymentWins)$' -count=1 -timeout=2m` | Stripe 原入口/身份/关闭语义；不能证明其他渠道 |
| 若选用/修改 Epay 回调 | `go test ./controller -run '^(TestWalletEpayUnknownOrderCallbackPersistsOnlyAuthenticatedPaidFact\|TestWalletEpayPaidCallbackPersistsBeforeAckWithoutMerchantHTTP)$' -count=1 -timeout=2m` | Epay 可信回执与 ACK；仍需商城路由测试 |
| outbox 共享执行 | `go test ./service -run '^(TestKKAIOutboxProcessorDeliversClaimedEvent\|TestKKAIOutboxProcessorDoesNotDoubleClaimLiveLock\|TestKKAIOutboxProcessorReclaimsStaleLock\|TestKKAIOutboxDeadEventRedriveIsAuditedAndIdempotent)$' -count=1 -timeout=2m` | 共享领取/恢复/手动重放能力 |
| 会话/证明扩展 | `go test ./service -run '^(TestSecurityProofBindsIdentityAndOperation\|TestSecurityProofBindsAccessTokenSession)$' -count=1 -timeout=2m` | 旧证明绑定不回归；shop 会话拒绝 token 另测 |
| 主站删除保护接入 | `go test ./model -run '^TestTenantAccountDeletionWaitsForFinancialSettlement$' -count=1 -timeout=2m` 及 `go test ./controller -run '^TestAccountDeletionReportsPendingSettlement$' -count=1 -timeout=2m` | 旧租户结算删除保护保持；不能证明主站商城 |
| schema 门禁变化 | `go test ./service -run '^(TestWalletPaymentSchemaGateRequiresReceiptAndRechecksDatabaseDrift\|TestWalletPaymentSchemaCanceledCallerCannotPolluteReadiness)$' -count=1 -timeout=2m` | 原 readiness/取消处理；目标商城 schema 兼容另测 |
| 钱包 UI/API 共用部分 | `bun run test src/features/wallet/payment-flow-api.test.ts src/features/wallet/lib/payment-flow.test.ts src/features/wallet/hooks/use-wallet-payment-flow.test.tsx` | 旧钱包恢复与前端动作不回归 |
| 前端验证流程扩展 | `bun run test src/features/auth/secure-verification/__tests__/verification-api.test.ts src/features/auth/secure-verification/__tests__/verification-flow.test.tsx` | 现有安全验证 UI 基础；新增 shop 操作另测 |
| 共享主题/弹层变更 | `bun run test src/context/__tests__/theme-preferences.test.tsx src/components/ui/__tests__/dialog-layout.test.tsx` | 既有主题/弹层行为，不代替商城浏览器检查 |

仅修改商城内部组件时无需运行所有钱包测试；只有对应共享边界被改变或首发交叉验收需要时才选取。通过后输入未变不重复跑。

### 8.2 已实现的定向验证入口

| 范围 | 实际命令 | 覆盖与限制 |
| --- | --- | --- |
| 模型、账户责任 | `go test ./model -run '^TestShop\|^TestTenantAccountDeletionWaitsForFinancialSettlement$' -count=1 -timeout=90s` | 支付身份、幂等、资金/交付/查看围栏、库存陈旧候选争抢、关键引用、删除；隔离 SQLite，非 MySQL/PG 锁调度证明 |
| 服务、会话、HTTP、路由 | `go test ./service ./middleware ./controller ./router -run '^TestShop' -count=1 -timeout=90s` | 加密/AAD、模板、证明绑定、请求/回调边界、归属、响应脱敏、API 投影；非真实商户联调 |
| 私密资料真实会话 HTTP | `go test ./controller -run '^TestShopBuyerInputHTTPRequiresLiveOwnedSession$' -count=1 -timeout=90s` | 真实 JWT/UserSession/认证/主站/session 中间件和 AES-GCM 资料、归属/撤销/Origin/缓存头/审计；Host fixture，未挂完整 router/schema/rate-limit；PUT 只覆盖 Origin 与非法体，不代表有效写入全链路 |
| 严格 JSON | `go test ./common -run '^TestUnmarshalStrict' -count=1 -timeout=90s` | 重复键、未知键、尾随及异常 JSON |
| schema 19 | `go test ./pkg/kkaimigrate -run '^TestShop' -count=1 -timeout=90s` | 无 live DSN 时为 SQLite 实际 DDL/回执、模型与三方言静态验证；真实库子项按环境显式 SKIP，不可把 skip 记为 PASS |
| PostgreSQL schema 18→19 | `go test ./pkg/kkaimigrate -run '^TestShopRealDatabaseSchemaContract/postgres$' -count=1 -timeout=90s -v` | 已在 PostgreSQL 18.4 隔离 schema 通过；canonical 回执、16 表形状/索引、重放、旧钱包门禁与数据保留 |
| MySQL schema 19 DDL | `go test ./pkg/kkaimigrate -run '^TestShopMySQLSchemaDDLContract$' -count=1 -timeout=90s -v` | 已在 MySQL 5.7 隔离库通过；完整历史迁移链仍被 v18 cursor 1064 阻断 |
| 真实库业务并发 | `go test ./model -run '^TestShopLiveDatabaseContracts$' -count=1 -timeout=120s -v` | MySQL 5.7/PostgreSQL 18.4 独立连接矩阵通过；使用模型 fixture，不冒充 canonical 迁移或多进程故障转移 |
| 授权查询错误传播 | `go test ./model -run '^(TestShopOrderReadPreservesDatabaseErrorsAndHidesInactiveUsers\|TestShopCommittedReplayRechecksActorAndTenant\|TestShopCreateOrderSnapshotsTermsAndReservesLastImportedUnit)$' -count=1 -timeout=60s` | PASS，0.328s；公开订单读取保留 context.Canceled，缺失/禁用用户仍 SHOP_NOT_FOUND；回归重放鉴权/下单。提交后故障另由下项保护 |
| 提交后响应未决 | `go test ./controller -run '^(TestShopCommittedOrderSurvivesAuthorizationChangeBeforeResponse\|TestShopOrderReadOwnershipAndNormalResponseSecrecy\|TestShopBuyerInputHTTPRequiresLiveOwnedSession)$' -count=1 -timeout=60s` | PASS，0.213s；提交后停用用户，返回未决而非普通拒绝、普通 GET 隐藏资源；恢复后原 key 仅一单/一次人工名额 |
| 文件 keyring | `go test ./service -run '^(TestShopMountedKeyringsOverrideEnvironmentAndFailClosed\|TestShopSecretBindingAndKeyRotation\|TestShopKeyringRejectsAmbiguousOrSharedSecrets)$' -count=1 -timeout=60s` | PASS，0.049s；文件优先、有界读取、异常不回退、独立密钥、历史 key 与 AAD |
| 付款错误与受控网关 | `go test ./service ./controller -run '^(TestShopPaymentRejectionDistinguishesCommittedAttempt\|TestShopSignedCallbackAndQueryNeverCreditRecharge\|TestShopPaymentErrorMarksOnlyProvenUncommittedRequest)$' -count=1 -timeout=90s` | 未提交标记、创建后失败保持原请求、本地 TLS 回调/查单及 TopUp/quota 隔离；非真实商户 |
| CLI 和 runtime 声明 | `go test ./pkg/kkaimigrate ./cmd/kkai-migrate -run '^(TestContractForDialectRequiresTenantSchema\|TestDescribeContractJSONRequiresTenantSchema\|TestShopOrdinaryTargetCannotMutate)$' -count=1 -timeout=90s` | schema 上限及 compatible prefix、普通 CLI 禁止直接安装商城 |
| 共享任务注册 | `go test . -run '^TestApplicationBackgroundJobsDeclareLeaderWriteBoundary$' -count=1 -timeout=90s` | 保留现有视频任务，商城后台写入处于 leader 边界 |
| 共享 outbox | `go test ./service -run '^(TestKKAIOutboxRuntimeJob\|TestKKAIOutboxRuntimeWorker)' -count=1 -timeout=90s` | 既有队列隔离、重试、取消及批次预算 |
| 前端 | `bunx vitest run src/features/shop/lib.test.ts src/features/shop/recovery.test.ts src/features/shop/hooks/use-shop.test.tsx`（在 `web/default`） | Money、恢复标记、不确定写入、秘密结果身份隔离及清理；页面后续回归见实施状态 |
| 付款页最终契约 | `bunx vitest run src/features/shop/pages/payment.test.tsx src/features/shop/hooks/use-shop.test.tsx`（在 `web/default`） | 当时 14 项通过；支付按 not_committed 与此前 unknown 判断请求释放，裸 4xx 不解锁；当时累计 33 个不同用例，后续新增见实施状态 |
| 付款未知后标记拒绝 | `bunx vitest run src/features/shop/pages/payment.test.tsx`（在 `web/default`） | 为两个既有用例的后续 409 补显式 not_committed；8 项通过，证明已有 unknown 后仍锁定并保留原 key/body；未新增用例 |
| 原请求恢复 | `bunx vitest run src/features/shop/hooks/use-shop.test.tsx`（在 `web/default`） | 7 项通过；新增首次 SHOP_REQUEST_UNRESOLVED 时保留原 key 和恢复标记，随后普通拒绝也不能清理 |

此表提供可运行的入口；已经执行的具体组合、结果和未执行项见[实施状态](./kk-shop-implementation-status.md)，不重复运行未变化且已通过的范围。之前 D1 的拟定测试名已用真实名称替换，没有为匹配名字增加空测试。

本轮已在本机临时 MySQL 5.7/PostgreSQL 18.4 上执行上述明确范围；临时容器、卷、namespace 及凭据均已清理。MySQL 完整 canonical 18→19 在既有 v18 `wallet_topup_runs.cursor` 未引用处出现 1064，保留冻结 SQL/digest，不以 schema19-only 的 PASS 掩盖。最小复现为设置专用本机 DSN 后执行 `KKAI_SHOP_MYSQL_BOOTSTRAP_TEST=1 go test ./pkg/kkaimigrate -run '^TestShopRealDatabaseSchemaContract/mysql$' -count=1 -timeout=90s -v`；未设置显式开关时该历史复现子项明确 SKIP。生产迁移控制器采用、其他数据库版本及真实多进程故障转移未验；`-race` 未执行。

### 8.3 lint、类型与浏览器检查

- 按实际改动文件运行 `gofmt -l <实际 Go 文件...>`；只要列表非空即需格式化。不能把未创建的通配路径当作已检查。
- 前端 lint 使用已有二进制按文件/目录定向，例如实现后运行 `bunx --no-install oxlint -c .oxlintrc.json src/features/shop src/features/shop-admin`，加入本次实际路由/共享文件，不顺带修其他功能。
- 当前 `bun run typecheck` 为 `tsgo -b`，没有已核实的商城局部脚本。改 TS/TSX 必须完成类型检查；先使用已验证能覆盖受影响依赖闭包的定向配置。若只能项目级 typecheck，先说明范围和耗时并按用户规则取得许可，未执行时明确报告类型检查缺口，不能用 Vitest 代替。
- 现有 `format:check` 包装会遍历项目并临时处理保护头部，不是传文件名即可缩小的检查。局部格式检查应核实 formatter 的准确文件参数并保留版权头；无可靠局部入口时报告缺口/申请范围，不能借此跑全仓格式修复。
- 真浏览器用当前已接入的执行设施或明确的人工脚本，保存页面/场景/视口/主题/源码版本及结果。仓库尚未有已核实的商城 E2E 命令，本文件不虚构 `test:e2e` 或要求为此新增整个框架。
- 不默认运行 `go test ./...`、不带过滤的 `bun run test`、`bun run lint`、`bun run typecheck`、`bun run build`。明确发布时执行仓库/发布手册的强制门禁，不能用日常定向策略省略发布要求。

2026-10-28 第一轮实际 React 浏览器检查通过：从 `web/default/` 执行 `KKAI_EXTERNAL_FRONTEND_BUILD=1 bunx rsbuild dev --host 127.0.0.1 --port 5178 --strict-port --no-env`，以隔离无头 Chrome 和确定性内存 API fixture 检查目录四种视口/主题组合、条款拦截、付款标记拒绝及原请求重试、交付遮罩/清除、商品金额和清空字段、人工交付验证对话框、库存预览/提交/清理。最终 0 页面错误、0 未知 API 路径，证据见[实施状态](./kk-shop-implementation-status.md)与[浏览器 JSON](./kk-shop-screenshots/implementation/browser-review.json)。该轮人工验证提供者和全部应用 API 均为 mock；开发编译不等于生产构建，局部通过不等于完整 B 层通过。

该轮最终 `bunx tsgo --project tsconfig.shop.tmp.json --noEmit` 定向闭包检查通过；临时配置及 shim 已移除，精确副本保留位置见实施状态。商城文件范围的 `git diff --check` 通过；该轮隔离浏览器和开发服务已关闭。后续真实后端联调单独记录，不改写第一轮 mock 的证据边界。

### 8.4 真实 HTTP 与 root-proof CLI 联调

PostgreSQL 18.4 + 实际 Gin 路由、密码登录/session、操作 proof、leader/outbox 已通过 33 个断言、31 个不同 HTTP 检查，包括自动/人工交付、库存导入/防重复、退款审批/登记/核实，以及 TopUp 为零、API quota 不变。仅外部 Epay 是本机 TLS 模拟器；没有真实扣款或退款。复跑入口、受限目录、环境变量与清理见[本地验收说明](./kk-shop-local-acceptance.md)，精确执行结果见[HTTP JSON](./kk-shop-screenshots/implementation/real-http-review.json)。生产 HTTPS、2FA/Passkey/OAuth 不在该结果范围。

独立 Linux ARM64 CLI + PostgreSQL root-proof 演练 8 项通过，包括有效只读挂载 apply、过期/权限/可写挂载/旧回退版本拒绝、中途 DDL 失败全事务回滚、只读身份重放。见[脱敏迁移证据](../../../kkai-infra/docs/evidence/2026-10-28-shop-root-proof-rehearsal.json)。经用户批准，2026-10-28 在同级 `kkai-infra/` 执行完整 `make check`，退出码 0、296.548s；25 个 Ansible playbook 语法检查、完整 policy 与 ShellCheck 通过，见[本地检查证据](../../../kkai-infra/docs/evidence/2026-10-28-shop-infra-check.json)。合成进程证明和本地检查不能替代生产三进程证据；正式 AMD64 镜像、安装控制器、A/B 回退版本、单独迁移授权和生产验收仍是门禁。

实际 React → Go → PostgreSQL 浏览器验收另外覆盖 15 个场景，16 张截图及 130 个商城响应见[真实浏览器 manifest](./kk-shop-screenshots/real-integration/manifest.json)。包含自动/人工交付、真实密码 proof、库存导入、改价不影响旧订单、售后两端解决、移动深色、秘密清理及最后两项视觉修正。页面 JavaScript 异常为零；启动阶段及未启动邀请服务的 504/401 已单独记录，不能写成全部网络零错误。

浏览器发现同一收据的 review 与后续 matched 各写一次公开收款事件。修复后仅 matched/extra 发布；模型/签名回调查单回归验证 review 重试零事件、matched 重试一事件、两笔实际交易两事件但一次履约。随后以新 PG 库、新进程和新订单运行 HTTP 驱动的 `--payment-events-only`，6 项通过：review 不公布已收款、实际 worker 确认后仅一笔收据/一条收款事件/一次交付，STOP 正常退出。精确命令见[实施状态](./kk-shop-implementation-status.md)；历史截图保留原事件，不充当修复后的浏览器重跑证据。时间线文案回归新增 6 项，前端累计 40 个不同用例分批通过；完整套件跳过。

## 9. 进入发布准备的条件

商城增加后端、支付/权限边界和持久表，首次交付属于 backend/combined + schema 评估，不能当成纯前端页面上线。本地实现已分配 schema 19，但本文不授权迁移或发布。

开发验收必须先提供：

1. REQ/PAGE/AC/BR 追踪表，P0/P1 无未关闭缺陷；所有未跑/skip 范围明确。原充值交叉回归和主站删除保护有实际证据。
2. 已选渠道的能力表与隔离联调结果；经营参数真实配置，演示商品不可收款；人工异常/退款/争议有责任人和处理入口。
3. 精确源码的 schema、canonical digest/扩展回执需求、迁移方案、旧充值门禁及前后版本兼容证明；不能只证明新应用能读新表。
4. 清理与 key 保留策略：过期停止揭示和密文销毁分开；未结责任、退款证据、交易引用可保留；恢复备份不会丢加密 key；不能把“退货”实现为已售凭据自动回库。
5. 停新单演练与回退责任承接：旧运行版本不识别商城任务时，不允许只退应用就遗留已收款订单。说明由哪个仍兼容的处理器继续核实/交付/退款，或在开启新单前完成兼容改造；未知任务保持可见，不被清理。

生产执行时重新完整阅读 [NewAPI 发布入口](/Users/tokk/Documents/Codex/runbooks/newapi-upgrade-and-deployment.md)，按当时确认的应用/同级 infra checkout 及已安装能力选路径。当前文档/原型不证明生产版本、槽位或 format 2 已安装。

- 只有实际发布任务才取得当前 infra `make newapi-status`；schema 兼容另以新鲜只读数据库证据和精确镜像 `/kkai-migrate --describe-contract --dialect postgres --json` 核实。schema 变化需单独明确授权及对应手册，不由开机 AutoMigrate 或默认 profile 解决。
- 本地 Mac 从干净 `production/kkrich` 精确提交构建唯一不可变产物；应用 contract pin 与实际安装控制器一致。stage、候选精确版本验收、10%/50%/100% 每档至少 120 秒、promote 分开。候选使用生产 writer 身份，不能在其中运行本文件会建删测试数据的 DB fixture。
- format 2 只有安装验收后才能采用显式 API 兼容和独立前端激活；format 1 保留精确配对。旧标签页、相邻前后端版本和回退版本对商城端点应安全失败或按已确认契约工作。
- 完成须有 promotion 身份/拓扑证据、立即与五分钟延迟状态、有效路由、批准的等长脱敏 model-relay 错误窗口复核、回滚就绪及清理结果。错误复核由执行者完成，不增加重复用户批准。仅 `promoted` 不等于发布完成。
- 若用户明确接手最小真实业务 E2E，按发布入口记录“用户接手，代理未验证”及范围；不能写业务已通过，也不能省略其他强制门禁。

## 10. 验收记录模板

每个开发里程碑只填实际执行项，不把计划命令预先标绿。

```text
源码/提交或工作树摘要：
改动风险与模块：
覆盖 REQ / PAGE / AC / BR：
测试层、精确命令、工作目录：
结果：PASS / FAIL / SKIPPED / NOT RUN
数据库类型与真实版本、隔离 fixture 标识（不写 DSN）：
网关环境及使用的受控证据引用（不写密钥/支付入口）：
浏览器、视口、主题、语言与流程：
资金/库存/版本/审计关键断言：
证据文件或脱敏报告：
失败、修复与仅重跑的相关范围：
剩余验证缺口及责任人：
全套检查：跳过 / 经明确授权执行；精确命令与原因：
发布状态：本地开发验收 / 待发布 / 依正式发布证据填写：
```

当前已有商城应用定向测试、受控 TLS 网关、真实 PostgreSQL HTTP 核心交易与 root-proof CLI 演练证据；浏览器 mock 与真实后端场景分别记录在[实施状态](./kk-shop-implementation-status.md)。真实支付由用户接手、代理未验证；MySQL 完整历史迁移阻塞、未覆盖的浏览器矩阵及生产门禁仍须逐项记录，不能宣称全部验收完成。以下 D1 记录仅描述当日历史阶段。

### 10.1 D1 文档检查记录（2026-10-27 历史）

2026-10-27 执行 `python3 /tmp/kk-shop-quality-doc-check.py`：18 REQ / 11 PAGE / 24 AC / 40 BR 的 93 条独立表格行齐全且不重复，17 个本地链接有效，Markdown 表格列数与空白检查通过；现有命令中的 23 个 Go 测试名、7 个前端测试文件均已核实存在。拟新增命令未计为现有测试。

脚本同时执行 `git diff --no-index --check /dev/null docs/kkai/kk-shop-quality-and-acceptance.md`，无空白诊断；新增未跟踪文件的差异退出码 1 已按 no-index 语义处理，不把它误报为检查失败。该临时脚本仅用于本轮文档静态检查，不是商城功能测试资产。

全套测试跳过；应用测试、lint、typecheck、构建、真实支付、三库运行及生产操作全部未执行。剩余缺口为本文件列出的实现后验收，不存在“文档已通过所以交易已通过”的推论。

### 10.2 D1 集成文档检查（2026-10-27 历史）

最终集成检查命令为 `python3 /tmp/kk-shop-development-doc-check.py`，组合现有文档链接/编号检查、上述QA静态检查、五份开发文档的Markdown表格和JSON示例解析，并核对V2原型SHA256保持冻结值。脚本只读应用源码与交付物，临时脚本本身不属于商城生产功能。实际结果由集成主审运行后记录；本段不代替任何应用测试。

2026-10-27 集成主审执行结果：PASS，退出码0。全部14份商城文档/原型的218个本地链接有效；93条需求/页面/验收追踪齐全；五份D1开发规格的47个Markdown表格及JSON示例有效；23个既有Go测试名和7个前端测试文件存在；V2原型SHA256与冻结值一致。只增加本段结果记录，应用测试和全套检查仍未执行。
