# 多模态视频工作室：测试与设计验收计划

状态：开发验收方案；本地实现已执行定向应用测试，实际命令、结果与剩余范围见 [实现与验收记录](implementation-qa.md)。真实媒体全链路和付费生成尚未验收。本文中的“必须通过”是交付门槛，不表示矩阵全部完成。

## 1. 验收依据与职责

需求依据是[产品方案](../video-studio-multimodal-design-20261114.md)，完整页面依据是 [Web 设计稿](../video-studio-multimodal-web/index.html)、[样式](../video-studio-multimodal-web/design.css)与[交互说明](../video-studio-multimodal-web/README.md)。实现契约以本目录的 [backend.md](backend.md) 为唯一来源；组件、状态与响应式实施以 [frontend.md](frontend.md) 为准。旧局部表单示意、旧“文生/图生”模式布局和现有标量请求不能成为新版本的验收基准。

验收分四层，任何一层不能替代下一层：

| 层次 | 证明什么 | 必须保留的证据 | 不能据此声称 |
| --- | --- | --- | --- |
| 完整 Web 视觉与交互 | 页面结构、比例、主题、状态反馈及操作路径 | 同环境截图、几何记录、交互录像或浏览器断言 | 上传、播放、报价、生成已经实现 |
| 前后端确定性测试 | 新契约、状态转换、校验、权限、幂等及账务不变量 | 精确命令、提交号、测试输出、断言覆盖表 | 供应商实际接受全部素材 |
| 本地/测试环境真实媒体链路 | 文件内容探测、上传恢复、签名播放、归档 | 固定测试文件摘要、真实探测结果、网络及状态证据 | 真实供应商计费与生成质量已验证 |
| 已授权的供应商验收 | 精确型号与适配器版本接受约定的组合、顺序和费用 | 脱敏请求投影、受理回执、任务结果、费用对账 | 未测型号、未测组合或更高限额同样受支持 |

产品负责人确认范围与未知能力；设计/前端确认完整页面与可访问性；后端/接入负责人确认请求、媒体、路由和费用；QA 维护用例与证据。角色是责任分工，不代表已指定真实人员。真实付费调用和生产操作须处于明确授权范围，文档交付不授权这些动作。

## 2. 需求—场景—证据矩阵

下表“补充”是前序计划分类；当前哪些用例已经实现和通过须查看实现记录，不以该标记推断当前状态。“既有基础”表示仓库已有相关测试，不能理解为它已覆盖新多模态需求。错误状态与错误码直接按 [backend.md](backend.md) 的错误表断言，避免在两份文档中维护不同约定。

| ID / 需求来源 | 核心场景与可观察结果 | 最小证据与测试归属 |
| --- | --- | --- |
| QA-01 / PRD 4 | 模型→参考素材→提示词→输出→费用的统一表单；无顶层文生/图生切换；桌面完整导航、编辑器、结果、近期任务同时成立 | 完整页面截图、DOM 顺序；补充 `video-create-page.test.tsx`、`video-composer.test.tsx` |
| QA-02 / PRD 3、7.1 | 每个精确型号只展示已验收能力；同名模型候选渠道不支持音频时禁止错误路由，不静默漏字段 | 能力矩阵与版本、分发断言；补充模型规格与 controller 测试 |
| QA-03 / PRD 4.1 | 无参考时仅允许支持文字生成的型号；有参考时为 `reference_to_video`；视频必填型号不能空提交；高级帧控制显式选择且互斥 | 前后端同输入表；补充 domain/submission 测试，不保留旧别名兼容断言 |
| QA-04 / PRD 3、4.2 | 单图、多图、单视频、图+视频、图+音频、视频+音频、三类组合按型号矩阵接受或明确拒绝；单音频、删除最后一份视觉素材都阻断 | 组合表测试、实际 adapter 投影；上游实测与 mock 分列 |
| QA-05 / PRD 3、6 | 数量、真实字节、尺寸、比例、时长、帧率以及分类累计值的上下界正确；未知跨类型上限不能当成 15 | §4 边界表、探测证据、前后端断言；补充 media/submission 测试 |
| QA-06 / PRD 4.3、7.3 | 混合添加；逐项上传/校验/ready；部分失败保留成功项；未 ready 项使本次完整请求阻断 | 上传状态、草稿内容、禁止 quote/POST 的断言；既有 upload 测试加 uploader 组件测试 |
| QA-07 / PRD 4.4 | 替换进行中旧素材可见且禁止报价/提交；新素材 ready 才原子替换；失败保留旧素材；移除仅解绑 | 两个 asset ID 的时序记录、引用与存储断言；补充 uploader/submission 测试 |
| QA-08 / PRD 5 | 不兼容素材保留为待处理并阻断；只有显式“本次不使用”才排除；切回模型不自动恢复；无静默模型/Token/分组/价格变化 | 交互断言、实际请求与费用摘要；补充 composer 测试 |
| QA-09 / PRD 4.4、7.2 | 重复 asset 拒绝；组内前移/后移改变真实 position；自然语言提示词不被重写；键盘可操作 | 请求快照、焦点和键盘断言；补充 domain/composer 测试 |
| QA-10 / PRD 4.3、6 | 图片可放大、视频可播、音频可听；切换媒体暂停前一项；不自动播放；关闭预览停止并释放；失效签名可恢复 | 实际有 src 的媒体、播放事件与网络证据；既有 player 测试扩展至参考音频 |
| QA-11 / PRD 3.2、6 | 未验证的“参考音频+关闭视频声音”阻断，文案提示开启声音或移除音频；不自行修改开关 | UI 与服务端组合校验；补充参数/组合测试 |
| QA-12 / PRD 5、7.2 | 有效素材/角色/组内顺序/型号/能力版本/Token/分组/参数/提示词变化使报价失效；仅签名续期或折叠未使用区不改变语义 | canonical request、hash 与 quote 断言；既有 submission integrity 测试扩展 |
| QA-13 / PRD 6、7.2 | 提交前持久保存幂等键与 POST 快照；网络未知、刷新或重入先查原提交；同键同语义恢复原任务；同键不同语义冲突 | 传输次数、任务数、账务笔数、恢复状态；既有 submission 测试加 §6 恢复用例 |
| QA-14 / PRD 7.4 | 报价、实际型号、预扣、结算/退款一致；异常数量、时长、元数据和倍率不能形成负收费；失败路径不重复退款 | 精确账务断言、脱敏账本差额；既有 relay failure/quota 基础及新边界测试 |
| QA-15 / PRD 7.5 | 新样片完整恢复真实类型、role、position 与版本；缺一个引用就阻断；覆盖当前新草稿前确认；不截断、不猜类型 | 样片响应/草稿快照；补充 hydration/sample 测试 |
| QA-16 / PRD 7.5–7.6 | 旧草稿/旧样片/旧标量请求不读取、重放或自动转换；旧请求在预扣和任务创建之前拒绝 | 负例接口测试、账务和任务零变化；不新增旧协议成功路径 |
| QA-17 / PRD 7.5、8 | 已受理旧任务继续查询、归档、结算/退款；已有作品、任务、素材关联、账目保留；关闭入口只阻止新建 | 切换前后数据库快照、在途任务终态；既有 reconcile/archive 基础扩展 |
| QA-18 / PRD 6、9 | 卡片/素材区/生成按钮分层反馈；错误可恢复；长文件名、长错误、200% 缩放与键盘路径可用 | 五宽度双主题关键状态、读屏/键盘记录，见 §3 |
| QA-19 / PRD 7.3、12 | 越权、伪 MIME、危险媒体、URL/重定向等不能绕过安全边界；新分析字段不含提示词、音频内容、签名 URL | §5 安全用例、日志字段审查；既有 asset/archive 测试扩展 |
| QA-20 / PRD 6、7.4 | 排队、生成、归档、可播放分别来自真实状态；未知受理不编造 task；未归档完成不显示可播放；引用与结果抓取完整 | 请求/状态/媒体一致记录；既有 task media 与 player 测试 |

## 3. 完整 Web 视觉验收

### 3.1 基准与固定环境

现有基准包含[桌面深色](../video-studio-multimodal-web/desktop-dark.png)、[桌面浅色](../video-studio-multimodal-web/desktop-light.png)、[窄屏深色](../video-studio-multimodal-web/mobile-dark.png)。它们只覆盖已导出的状态；不足五宽度、双主题的全部基准应从同一版完整 Web 原型补齐，由设计确认后版本化保存，不得用实现截图覆盖期望基准。

每次对比同时记录源提交、浏览器完整版本、OS、字体实际加载结果、viewport、DPR、zoom、主题、语言、时区和 fixture 版本。首轮使用固定 Chromium 环境、DPR 1、zoom 100%、简体中文、`Asia/Shanghai`；应用与原型必须在相同环境中截图。等待字体、图片和布局稳定，冻结 fixture 时间与任务数据，关闭动画/闪烁光标。截图使用固定本地素材与相同裁切位置，不能用不同画面造成无意义差异。

每个基准保存 viewport 截图、full-page 截图和关键元素 `getBoundingClientRect()` 记录。截图不能只裁出素材区；必须覆盖平台头部、侧栏、页标题、工作室导航、编辑器、完整结果区、近期任务、页面底部。浏览器原生媒体控件因平台差异单独记录，在固定环境比较其可用区域和功能。

### 3.2 尺寸、主题与状态矩阵

| 主基准 viewport（CSS px） | 用途 | 默认态 |
| --- | --- | --- |
| 320 × 900 | 最窄屏、两列素材及长错误 | 深色、浅色 |
| 390 × 844 | 主要移动端、三列素材与纵向流程 | 深色、浅色 |
| 1280 × 900 | 普通桌面双栏、整页密度 | 深色、浅色 |
| 1440 × 1000 | 主设计基准、双栏比例与结果层级 | 深色、浅色 |
| 1920 × 1080 | 宽屏最大内容宽与留白 | 深色、浅色 |

默认态采用同一组图片+视频+音频、固定输出参数、可播放结果和近期任务。关键状态在 1440×1000 与 390×844 下覆盖双主题：空素材、上传中、校验中、文件失败、音频缺视觉、总时长超限、不兼容待处理、显式排除、替换中/失败、报价中/过期、提交受理不明、排队/生成/归档/成功/失败、素材预览、模型选择及新样片覆盖确认。320 宽额外检查最长错误、添加/预览弹层、展开音频控件、最多素材与生成按钮可达。

768×1024、1024×900 作为结构补充；对设计断点 360、600、940、1200、1600 各取临界前后 1px 检查一次默认态的列数、侧栏、溢出和控件可达，不把全部状态与全部断点做笛卡尔积。真实粗指针设备/仿真另检查 390 宽的触控命中与输入字号；不要把桌面缩窄当成完整移动设备验证。

### 3.3 必须对齐的几何与 token

以完整 Web 样式的最终级联值为准。下表列主锚点，用于定位偏差，不能只用“看起来接近”验收：

| 项目 | 原型基准 |
| --- | --- |
| 桌面头部/侧栏 | 高 56px / 宽 184px；≤1200px 侧栏 64px；≤600px 侧栏隐藏、头部 52px |
| 桌面主体 | padding `24px 28px 20px`，max-width 1690px |
| 桌面工作区 | 左栏 410px，栏间 24px，右栏自适应；≥1600px 左栏 450px；≤1200px 左栏 370px、间距 20px |
| ≤940px | 单列工作区；保持一个编辑器实例与同一份草稿，不靠两套 DOM 分别隐藏实现 |
| 面板/卡片 | 面板边框 1px、圆角 12px；素材卡圆角 7px、间距 8px；默认缩略图高 76px |
| 编辑器 | 默认 padding 20px；≥1600px 为 24px；≤1200px 为 17px；≤940px 为 24px；≤600px 为 18px |
| 参考与输出 | 默认素材三列；≤940px 四列（三个素材时三列）；≤600px 三列；≤360px 两列；输出窄屏按原型重排 |
| 提示词/结果 | textarea 默认最小高 119px、≥1600px 134px；结果画面 16:9；不能把结果压成卡片缩略图 |
| 文字层级 | 主标题 24px（移动端 23px）；主体 13px；字段 12px；最终覆盖后的辅助文字主要 11px；按最终级联核验 |

| token | 深色 | 浅色 |
| --- | --- | --- |
| page | `#111315` | `#f5f6fa` |
| shell | `#191b1e` | `#ffffff` |
| panel | `#1c1e21` | `#ffffff` |
| raised | `#25282c` | `#f2f4f8` |
| text / muted | `#f1f1ee` / `#a3a7ae` | `#242a35` / `#677080` |
| border | `#34373c` | `#e0e4ec` |
| accent / accent text | `#b1bcff` / `#191d2c` | `#4c63da` / `#ffffff` |
| soft | `#303442` | `#edf0ff` |
| success / warning / danger | `#97d6bb` / `#efc98b` / `#f5a7ac` | `#35755b` / `#8b5a0b` / `#b33445` |

实现应映射到现有主题 token；如现有全局 token 无法表达设计，在工作室作用域处理，避免为还原一页改全站主题。任何有意偏离需写明原型位置、原因、新基准与设计批准记录。

### 3.4 容差、遮罩与人工判定

以下是**建议的团队验收阈值**，不是供应商事实或当前实现的通过结果。首轮固定环境后由设计/前端确认，之后不能为通过测试临时放宽。

1. 结构硬门槛：区块顺序、断点列数、主操作、费用、错误、状态和导航全部正确；无横向滚动、遮挡、截断关键信息或不可达操作。主锚点的 x/y/宽高与基准误差各不超过 2 CSS px；边框与圆角误差不超过 1 CSS px。动态错误导致合理增长时以该状态独立基准比较。
2. 字体硬门槛：字体族/字重/字号与设计映射一致，同环境关键标题、按钮、费用及错误的换行行数一致。内容相同却因为窄栏或错误字重整体换行，不属于抗锯齿误差。
3. 像素辅助门槛：按头部、侧栏、编辑器、结果、近期任务分别出 diff；固定环境下可先采用每通道差值 ≤12/255 忽略抗锯齿、小于该区域 1% 的其余差异进入人工复核。连续色块、边界错位、图标缺失或文字遗漏一律失败，不因全页差异百分比低而豁免。主题色应先检查计算样式的 token 精确值。
4. 允许遮罩：冻结失败且不影响功能的时间戳、随机任务 ID、输入光标、真实播放画面的动态像素。每个遮罩记录原因与精确矩形，优先冻结数据/视频时间。
5. 禁止遮罩：整张卡片、整个结果区域、素材类型与数量、限额、费用、模型、有效/待处理/未使用状态、错误、按钮、进度状态、布局边界。原生播放器的内容像素可隔离，其边界、可见控件和点击路径不能消失。

可访问性优先于照搬原型的小字号/小命中区，但必须显式评审并形成新基准：主要操作和媒体控制在粗指针下具备至少 44×44 CSS px 有效命中区，透明扩展不能互相覆盖或误触删除；重要错误不能以原型中的 8–9px 装饰字号交付。普通文本对比度至少 4.5:1，大字及非文本控件至少 3:1；状态不只靠颜色表达。

键盘必须能添加、预览、替换、移除、前移/后移、排除/恢复、修改参数、查询提交。预览入口在 focus 时可见；弹层有名称、焦点约束、Escape 关闭并返回触发点；删除后焦点回到相邻卡片或添加入口。读屏仅在阶段变化时播报，不逐百分比打断。200% 缩放后内容与操作仍可达。

语言覆盖按运行时 [languages.ts](../../../web/default/src/i18n/languages.ts)，当前实际有 `zhCN/en/fr/ru/ja/vi/zhTW` 七种，包含 `zh-TW.json`，不能因早期 PRD 写六种而漏掉繁体。逐一检查本次新 key、变量、复数/时长、错误文案和无裸 key；用最长译文检查 390/1440 宽布局。动态数字/时间使用正确 locale 映射，不将 `zhCN` 直接交给 `Intl`。

## 4. 真实媒体与边界测试

先冻结每个精确型号的能力版本、格式、编码、每类限额、跨类型 `max_total_count` 和 MB→字节定义，再生成确定性 fixture。下面的 9/3/3、2–15 秒等来自当前产品采用的保守候选；未完成能力确认前不得用它们反向宣布上线支持。2.5 的 30/10/10 与 9/3/3 文档冲突需要接入证据解决。

| 边界 | 必测输入 | 判定 |
| --- | --- | --- |
| 每类数量 | 0、1、上限−1、上限、上限+1；一次混合批量选入越界 | 按完整选择反馈，不能静默截断；音频组合额外校验 |
| 跨类型数量 | 已确认总上限前/等/后；每类各未超但总数超限 | `max_total_count` 独立校验；不得推断 9+3+3 必然有效 |
| 严格大小 | 有效 `max_size_bytes` 的 −1、恰等、+1；另测供应商严格门槛，及声明/真实长度不一致 | 有效 max−1/max 通过大小检查、max+1 拒绝；供应商 `<50 MB` 的 50,000,000 字节恰等拒绝，两种边界不能混淆。按真实长度及较严限制复核 |
| 图片尺寸/比例 | 300、301、5999、6000px；比例恰 0.4、略大、略小于 2.5、恰 2.5；旋转元数据 | 严格开区间；按媒体合同明确的有效尺寸计算；不能靠文件名或浏览器预览放行 |
| 单项时间 | 1.99、2、15、15.01 秒；缺失、非有限、负数、异常巨大探测值 | 边界保留精度；不先显示舍入再判定；异常值拒绝进入费用计算 |
| 分类累计时间 | 视频 7.5+7.5、7.5+7.51；音频同样一组；图+视频+音频组合 | 视频与音频各自累计，不合并成一个 15 秒，也不当作输出时长 |
| 帧率 | 略低 24、24、60、略高 60；有理数值、缺失值、可变帧率 | 按已验收探测口径判定；不能字符串/整数截断；无法证明有效则拒绝 |
| 容器/编码 | 有效 JPEG/PNG/WebP、MP4/MOV、MP3/WAV；扩展名伪装、错误 MIME、损坏/截断、无目标轨 | 文件真实内容与声明匹配；首版未验收格式拒绝，不能自动转码掩盖差异 |

真实文件集至少包含上述受支持格式的有效样例、一个带音轨的视频、一个纯音频、最短/最长边界、损坏文件和内容伪装文件。保存 fixture 生成方法、SHA-256、字节数、预期流类型/codec/时长/尺寸/帧率；限制样例体积，不提交用户私密素材。大量字节边界可在可信边界测试用存储长度夹具覆盖，但**至少一次真实上传→探测→ready→签名播放**必须使用实际 MP3/WAV 与视频文件。

纯音频应走合法音频探测分支，不要求视频流、不生成视频海报或视频预览；长时间媒体处理受到时限、资源与磁盘约束。已有 `video_media_processor_test.go` 使用 runner fixture 验证解析与调用参数，其中模拟 ffprobe JSON 不等于真实解码成功。真实文件测试记录实际 ffmpeg/ffprobe 版本与退出结果；如果环境缺少工具，记录阻塞，不能用 fake runner 报告真实链路通过。

设计稿的图片静帧、空 `src` 的 video/audio 控件和装饰波形只能验收布局；不能作为试听、播放器续签、上传、声音生成或真实波形的证据。产品第一版不要求自绘波形。

## 5. 上传恢复、安全与资产生命周期

上传用例覆盖单次 PUT 与服务端指定的分片路径，不能客户端自行降级。断网/刷新恢复要复用有效 reservation；过期 reservation 明确重建上传，不复用不可访问凭据；分片签名每次重试更新，完成请求按真实 part/ETag 校验。重复 complete 回调、对象已完成而数据库写入失败、晚到回调不能制造重复资产或破坏状态。

取消分清三个行为：关闭预览只停止播放；移除草稿项仅解除本次绑定；取消上传才调用上传 abort。abort 失败保留可重试证据，不能假装取消成功后遗留对象；取消与 PUT/complete 竞争、离页 teardown、账号切换、旧回调晚到均覆盖。未完成上传允许重新尝试上传；校验较慢只查询，不再创建上传。已受理生成的“取消”不得凭空增加上游能力，按真实任务协议处理。

安全回归按真实入口测试：

- 他人、已删除、未 ready、隐藏未发布样片及缺少所需探测证据的 asset 无法签名、引用、报价或提交；客户端改 MIME、role、position 不越过授权与类型校验。
- 引用计数/关联保护 task 与 sample 已使用素材；草稿移除不会删除任务仍需读取的对象；孤儿 GC 不删除近期、处理中、已引用对象。
- 图片解码炸弹、异常视频流元数据、超大时长/分辨率、命令超时、符号链接、本地文件协议与非交互命令行为保持既有防护；音频分支不能绕过这些限制。
- 外部抓取 URL、重定向、DNS 混合私网地址、凭据跨源/失败重放、响应 MIME、流式大小上限、deadline、磁盘不足遵循现有 fetch 边界；只对明确配置的供应商源允许已有受控私网例外。
- 签名 URL 续期不改变请求语义；提交时仍可用不等于上游稍后抓取一定成功，按实际延迟验证 URL 生命周期。上游抓取失败必须有可理解的任务失败与账务处理，不偷偷漏掉该素材。
- 新日志/分析事件仅保留必要类型、数量、时长档位、错误码、契约版本和关联 ID；检查新字段不写入提示词、音频内容、完整签名 URL 或密钥。测试报告也须脱敏。

## 6. 请求一致性、幂等与账务

按 [backend.md](backend.md) 使用现有 `token_id` 与模型字符串，并加入能力版本和 `asset_id/role/position`。`position` 在各 role 内从 0 连续编号；固定角色顺序规范化。测试必须区分：物理 JSON 数组重排但 role/position 不变，语义相同；同一 role 内 position 对调，语义不同。不要再按 asset ID 排序导致真实创作顺序丢失。

报价签名、请求 hash 与幂等指纹需要覆盖同一份有效创作请求。显式 false/0 在合同允许时保持存在，缺省值与零值不得被 `omitempty` 误合并；非法输出时长、数量与计费倍率在预扣之前拒绝。测试 quote 过期、伪造/延长过期时间、跨用户/Token/分组复制 quote、能力版本变化及真实参考不可用。

重点时序：

1. POST 前持久保存意图、幂等键和准确请求快照；持久化失败则不发送。传输前再次检查账号身份；401 不自动刷新后重播付费 POST。
2. POST 超时、网络断开、成功响应缺合法 receipt 或跨账号晚到，都保留“受理不明”，不能清空证据或生成新键重发。
3. 已落地的只读 `GET /api/video-studio/submissions/by-key` 按 `Idempotency-Key` 与 session owner 恢复 `accepted/pending/not_found`。三种状态都需测试，跨用户不可查询。`not_found` 只代表当前未找到，不能证明绝未受理，更不能触发自动新键 POST。
4. 已绑定任务优先恢复，不因报价现已过期而阻断查询；同键同语义只返回原任务，同键不同语义明确冲突；修改内容后又改回原值仍不能绕开未确认提交保护。
5. 非终态或 ambiguous 的已绑定视频提交，在现有 24 小时幂等清理点仍保留保护，直到满足 backend 规定的终态/保留条件。使用可控时钟或显式过期记录测试，禁止真实等待 24 小时。
6. 分发失败、上游拒绝、归档失败重试、重复完成通知、重复结算/退款均断言任务数与账务笔数。mock 请求次数之外，还要断言同一意图预扣最多一次、结算/退款至多一次且结果正确。

新增/重写 Go 测试使用 `require` 做环境与致命前提，`assert` 做结果断言。账务 fixture 明确用户余额、Token/分组、价格与计费模式、数据库状态；不依赖全局测试残留。溢出/NaN/无穷值/超界输入必须拒绝或按既有安全方法饱和且保留审计，不产生负收费。只有本次改动触及 quota 公共实现时才扩大到其直接依赖回归，不为心理安慰运行整仓测试。

真实供应商验收至少记录：精确本站型号、权限/分组、能力版本、应用插件与外置适配器版本、单一上游协议路径、三类完整输入及各自顺序、签名抓取时间、报价/预扣/最终账务、任务终态及归档结果。当前外置 `seedance-special-adapter` 的源码/版本尚待定位，缺失时不能将应用插件 mock 成功标记为贯通。

## 7. 新流程切换与在途数据

不为旧标量协议、旧模式别名、旧草稿、旧样片建立成功回放或迁移测试。负例应证明旧请求在任何预扣与任务创建前被拒绝，并给出按新格式重新配置的路径；不得静默转换、删减引用或重发。

另建一组存量安全 fixture：切换前已受理且正在排队/处理/归档的任务、已完成作品、失败待退款任务、被 task/sample 引用的媒体和现存账目。切换新建入口或关闭新增能力后，原任务仍按现有 task 记录继续查询上游、归档、完成结算/退款；作品可查看，关联和账目不被删除。验收既不要求旧输入可再次生成，也不允许以“不兼容旧流程”为由破坏任务后处理。

新格式样片的 `reference_assets` 按合同携带 role、position 与嵌套真实 asset 信息；恢复前校验全部引用。失效样片应阻断并修复，不取前 N 个做部分成功；切换时不批量删除旧草稿/样片。账号切换和新样片异步加载晚到不能覆盖用户已编辑的新草稿。

## 8. 仓库现有测试与明确缺口

下列路径均为现有文件；这里列的是已读到的保护方向，不代表本轮执行通过。

| 现有基础 | 可复用保护 | 本需求必须补齐或替换 |
| --- | --- | --- |
| [video-composer.test.tsx](../../../web/default/src/features/video-studio/components/video-composer.test.tsx)、[video-create-page.test.tsx](../../../web/default/src/features/video-studio/video-create-page.test.tsx) | 持久化后 POST、未知提交、账号变化、过期报价、编辑器实例及草稿保护 | 统一表单、三类素材、显式排除、替换保护、新状态与新样片 |
| [video-domain.test.ts](../../../web/default/src/features/video-studio/video-domain.test.ts)、[video-submission-protocol.test.ts](../../../web/default/src/features/video-studio/video-submission-protocol.test.ts)、[api-submission.test.ts](../../../web/default/src/features/video-studio/api-submission.test.ts)、[use-video-submission.test.tsx](../../../web/default/src/features/video-studio/hooks/use-video-submission.test.tsx) | quote/receipt/identity、不重复传输、权限与 UTF-8 | 新规范化、role/position、版本、恢复 GET；旧标量成功断言按新合同替换 |
| [video-upload-protocol.test.ts](../../../web/default/src/features/video-studio/video-upload-protocol.test.ts)、[video-upload-lifecycle.test.ts](../../../web/default/src/features/video-studio/video-upload-lifecycle.test.ts) | reservation、分片/ETag、abort 失败保留、签名刷新、生命周期 | 音频 purpose、混合上传、替换竞争与组件级错误；拟新增 `components/video-asset-uploader.test.tsx`，当前不存在 |
| [video-generation-player.test.tsx](../../../web/default/src/features/video-studio/components/video-generation-player.test.tsx)、[video-reference-hydration.test.ts](../../../web/default/src/features/video-studio/video-reference-hydration.test.ts) | 续签不跳时间、撤销 src、媒体互斥、owner 隔离、引用恢复 | 参考音频真实播放、缺引用阻断、可访问播放弹层 |
| [video_studio_submission_test.go](../../../service/video_studio_submission_test.go)、[video_studio_relay_test.go](../../../controller/video_studio_relay_test.go) | quote 过期、Token/分组、请求一致性、分发前守卫、已有幂等重放 | 多模态合同、完整顺序、旧请求提前拒绝、恢复查询及非终态 key 保留 |
| [video_media_processor_test.go](../../../service/video_media_processor_test.go)、[video_asset_pipeline_test.go](../../../service/video_asset_pipeline_test.go) | 媒体探测解析、图片真实性、期限、安全输入、pipeline 状态 | 纯音频与真实文件矩阵；fake runner 不能代替实际解码证据 |
| [video_assets_test.go](../../../service/video_assets_test.go)、[video_multipart_upload_test.go](../../../service/video_multipart_upload_test.go)、[video_upload_lifecycle_test.go](../../../service/video_upload_lifecycle_test.go) | ownership、引用保护、分片完成恢复、取消竞争、GC | 音频上传/授权、混合草稿解绑、替换与跨用户回调 |
| [video_archive_fetch_test.go](../../../service/video_archive_fetch_test.go)、[task_video_media_test.go](../../../service/task_video_media_test.go)、[video_task_archive_once_test.go](../../../service/video_task_archive_once_test.go)、[video_task_reconcile_test.go](../../../service/video_task_reconcile_test.go) | 抓取安全、ready 与权限、归档幂等、继续扫描 | 新入口切换后在途任务继续，未知/归档状态不伪造 |
| [sora_responses_test.go](../../../plugins/sora_responses_test.go)、[video_studio_sora_failure_test.go](../../../controller/video_studio_sora_failure_test.go)、[quota_math_test.go](../../../common/quota_math_test.go) | 既有 Seedance 投影、上游拒绝退款、费用转换安全 | 新数组/顺序/音频投影及外置 adapter 合同；必要时补输入边界 |

前端新增测试用现有 Vitest + React Testing Library，按用户可观察行为与请求契约断言，不锁死私有 helper、CSS class 或无业务含义的调用顺序。视觉/真实媒体浏览器 runner、fixture 与基准维护流程尚需实现，不能把原型 README 中本机 `/tmp/check-*.cjs` 当作仓库内可复现的应用测试。

## 9. 最小验证命令与执行边界

本节保留候选验证入口；实际已执行的命令和结果见 [实现与验收记录](implementation-qa.md)，不能据此清单宣称全部已跑或全部未跑。每次只选与改动直接相关的测试文件/命名用例；先确认环境与时间预算。任何预计超过五分钟或耗时无法估计的命令，应先给出精确命令、理由、预期耗时并取得批准；不会因本文列出命令就自动获得全量执行许可。

前端命令在 `web/default/` 下执行，下面按改动拆组，不要求每次全部运行：

```sh
# 创作状态、规范化及提交
bun run test src/features/video-studio/components/video-composer.test.tsx src/features/video-studio/video-create-page.test.tsx src/features/video-studio/video-domain.test.ts src/features/video-studio/video-submission-protocol.test.ts src/features/video-studio/hooks/use-video-submission.test.tsx src/features/video-studio/api-submission.test.ts

# 上传/恢复、媒体与新样片
bun run test src/features/video-studio/video-upload-protocol.test.ts src/features/video-studio/video-upload-lifecycle.test.ts src/features/video-studio/video-reference-hydration.test.ts src/features/video-studio/components/video-generation-player.test.tsx

# 仅示例：将显式文件列表替换为本次实际修改的 TS/TSX 文件
bunx --no-install oxlint -c .oxlintrc.json src/features/video-studio/video-domain.ts src/features/video-studio/components/video-composer.tsx
bunx --no-install oxfmt --check src/features/video-studio/video-domain.ts src/features/video-studio/components/video-composer.tsx
```

拟新增 uploader 测试只有实际创建后才加入对应命令。i18n 的新增 key/占位符应检查七个运行时语言的对应子集；若编写定向校验脚本，交付其确切路径与命令，不能把尚未存在的脚本记录为已执行。

Go 命令在仓库根目录执行；`-run` 选择行为测试，仍会编译被测包及其依赖，因此初次环境未知时先估时。新增测试落地后按其确切名称补入，不用 `go test ./...` 替代：

```sh
# 请求与报价合同
go test ./service -run 'Test(NormalizeVideoStudioSubmission|VideoStudioIdempotencyFingerprint|VideoStudioQuote|VideoStudioRequestHash|ValidateVideoModelSpec|ValidateVideoParameters)' -count=1

# 媒体与上传，仅涉及这些模块时运行
go test ./service -run 'Test(FFmpegVideoMediaProcessor|ParseVideoProbeOutput|VideoAssetPipeline|MultipartVideoUpload|SingleVideoUpload|ExpiredSingleVideoUpload)' -count=1

# 受理前守卫、分发与上游失败账务
go test ./controller -run 'Test(PrepareVideoStudioTaskRequest|VideoStudioDistribution|VideoStudioSoraHTTPRejectionRefundsWalletAndFailsGeneration)' -count=1

# 应用插件投影
go test ./plugins -run 'Test(SoraSpecialSeedanceProjection|SoraResponsesProtocol)' -count=1
```

具体 `VideoStudio...` controller 场景按实际改动添加到 `-run`，不能误以为上述前缀已运行该文件所有用例。修复失败后只重跑失败或最近相关范围。测试正常通过且无新变化，不反复重跑。

当前 `package.json` 的 `typecheck` 写为 `bun check -b`，但本机 CLI 帮助探测返回 `Script not found "check"`，当前 `.bin` 未找到 tsc/tsgo；它不是已可用的 checker。实施前确认受支持工具链，不临时下载最新版；具备工具后按 frontend.md 建立包括实际依赖的定向配置，不裁剪关联错误。没有可行定向方式时记录缺口，为必要的全 app typecheck 按规则另行申请；授权扩大范围不能代替修复工具缺口。`bun run lint`、裸 `bun run test`、`bun run typecheck`、`bun run build`/`build:check` 是全项目范围，不作为本需求 L0–L2 的默认命令。`format:check` 的保护头脚本遍历项目，不能给它附加文件名便称为定向检查；采用上面的显式文件 `oxfmt --check`。`i18n:test` 测试的是检查脚本本身，不证明新翻译完整；`i18n:check` 当前也没有文件子集参数。需要全项目检查时按项目规则另行说明理由、精确命令与耗时，等待授权。

本次纯文档只检查本文件链接和空白，不运行应用测试、全量套件、构建、生产状态查询或付费 API。后续执行记录必须明确实际命令/退出状态、全量是否跳过及剩余缺口，不能仅写“测试通过”。

## 10. 阻断交付的问题与证据单

出现以下任一项不得宣称完成：参考素材被静默丢弃或改序、自动换型号/Token/分组/费用、未 ready 或越权素材可提交、失效报价仍可创建任务、受理不明自动重发、重复收费/退款或负收费、旧请求在拒绝前产生账务/任务、在途任务被新入口切换中断、无真实视频/音频播放、把假进度当真实状态、关键页面结构失真或小屏主操作不可达。对应能力未完成真实 adapter 验收时，保持未开放并列明缺口。

每个交付版本维护如下证据单；未测项使用“未执行/阻塞/不适用+原因”，不用空白代替：

| 字段 | 要求 |
| --- | --- |
| 版本与责任 | 应用提交、设计基准版本、能力版本、adapter 版本、验证人/角色与日期 |
| 范围 | 精确型号、组合、角色顺序、权限/分组、数据库方言及环境；未覆盖范围 |
| 自动化 | 精确命令、工作目录、退出状态、失败断言与复跑结果；明确全量是否跳过 |
| 视觉 | 五宽度双主题默认态、关键状态、viewport/full-page、几何 JSON、区域 diff、遮罩清单、批准偏差 |
| 媒体 | fixture hash、真实探测工具版本与结果、播放/续签/互斥、上传取消/恢复证据 |
| 接入与账务 | 脱敏投影、受理回执、任务状态链、quote/预扣/结算或退款、对象归档结果 |
| 数据完整性 | 旧请求无副作用、新草稿/样片合同、在途任务完成、作品/引用/账目保留证据 |
| 安全与可访问性 | ownership/危险文件/日志审查、键盘与焦点、对比度、200% 缩放、七语言结果 |
| 剩余项 | 未确认供应商事实、未测型号、实际执行阻塞、负责人及解除条件 |

只有设计、行为、真实媒体、接入/费用四层证据在声明范围内闭合，才能使用“该型号的多模态视频工作室已完成”这一结论；文档、原型或单包测试完成应分别报告。
