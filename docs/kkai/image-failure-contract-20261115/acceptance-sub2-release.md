# Sub2API 图片失败协议发布验收

日期：2026-11-16（Asia/Shanghai）。结论：用户明确豁免自然图片样本后的第二次发布已完成，10%→50%→100%→complete、立即/延迟/清理后运行检查、错误审阅与回滚门禁通过。正式删除集清理完成，两份来源不明的历史镜像按受控保留规则登记待维护处置；不声称所有历史产物清空。首轮发布及回退保留在 `acceptance.md`，真实图片业务 E2E 未由代理验证。

## 授权、范围与制品

用户明确要求“**不用管样本 直接上线啊**”，本次不等待正的自然图片样本，不发起登录、账号/Key 创建或付费图片探针。只免除本次图片样本/业务成功验收等待，保留身份、迁移、健康、固定灰度顺序、每档至少 120 秒、60 秒日志保护、错误审阅、CAS、回滚与立即/延迟验收。业务 E2E 未由代理验证。

| 项目 | 精确坐标 |
| --- | --- |
| 应用源码 | `855bdb83bc422e4385938846b2b9a64864559a84`，`release/v0.2.21-kkai` |
| 原不可变镜像 | `kkai/sub2api:v0.2.21-kkai-image-errors-20261116-855bdb83bc` |
| image ID | `sha256:80d76634e087ffa7e9742bc1275dd2933b9c40a3bd90123687d75daad46962e1` |
| archive | 48,000,512 bytes；SHA256 `aa52d379a256b469a0df386fbb0a281ccc2a93ea2493e2f38497b6043f80b6c6` |
| request SHA256 | `f610cff78eead9cc1a8407e844f8bd6955077d96aab7e13dd3c0cd734bd19a8c` |
| migration contract SHA256 | `c93c498c3efe7e9f4180b8ac49360f55693621a88eff31c0f6eabd211c34103f` |
| 发布范围 | image，仅替换 inactive image 槽，image/account-test 同步切流 |
| 新控制器源码 | `b8b622e564a50d765f78cd745327573d41cd8e60` |
| install manifest SHA256 | `072c9df6efe82e33f7292046926fed5e95d577e280feca44d500ae832ba72e82` |
| previous 控制器 | `e7f898f7831a50ed9675885fd92cbdf71e306847` |

原应用制品经过重用核验，没有重新构建。NewAPI 已完成的后端、前端和控制器 pin 不因本次重试改变。没有数据库迁移、源码托管网络操作、新分支或 worktree。

## 必要的独立控制器变更

原安装版本的正式 CLI 和 Mac wrapper 强制要求正样本错误窗口，省略文件或直接调用内部 Python 都不是有效发布入口。经独立开发、审查与验证，仅在既有 v1 人工接受路径增加 `waive_image_samples: true`：必须为 image scope、真实非负整数样本且至少一侧为零、两侧错误列表均为空，并绑定当前 release、source/destination、entered_at、router SHA 和最终整文件 SHA。默认 evaluator、text/both、时间、runtime、迁移和 CAS 门禁不变。

控制器通过精确 clean commit 的离线 bundle 安装，双端 SHA256 `aca3f1d2751de2d0b2e76a3c49451cedc4fa15a63cf928c0988203bb2ddfd72f`，2,173,069 bytes。已安装 self-test 返回 `explicit-image-sample-waiver`，原 R1 observation contract 保留，previous 可读。

安装前快照误将 Docker Mounts 无序数组直接计算哈希，造成配置漂移误报；本地 shell 又未在该断言失败后终止，后续已批准 installer 仍执行。此执行顺序缺陷保留在审计记录中。随后对配置投影和 Mounts 顺序进行只读复核，没有发现真实配置变化；全部 43 个容器身份/生命周期、项目服务、router inode/内容/有效路由、apps-current 与四槽直连健康保持。快照已改为排序后的 canonical hash，安装 helper 在同一 Python 进程以 `check=True` 和断言保护后续写入；没有重装。

控制器安装后从已确认 NewAPI infra 执行 `make newapi-status`：目标 `kkai-prod-20261115.1794758551-f3334a7da`、green、HEALTHY、stable、rollback ready。该结果是安装时快照。

## 新发布与分档结果

新 release 为 `sub2api-v0.2.21-kkai-image-errors-20261116-855bdb83bc-text-green-image-green-20261116T033048Z`，新 plan SHA256 为 `26a41f812965ceccc37a18024f0e41af1db0d42048b428400ded0f678b341518`。正常 plan-stage 复用原 archive/request，候选为 `sub2api-image-green`，容器 `5ad8bdbb8c369f5c85f36adcae56b5a87956b425a79ef45d7ec5d396e3dde202`；stage、迁移兼容和 runtime 门禁通过。

正式 plan-stage PTY 输出的 migration 检查 exit0（0.085073 秒）、候选健康检查 exit0（0.095423 秒）已由 `stage-receipt-index.json` 索引，并与 `timings.jsonl`、`staged-status.json` 绑定；未重复运行迁移或用后续健康替代该记录。

旧 active/回退为 `sub2api-image-blue`，容器 `9988d2f23b7b1f91b905a11b7dff0e0f3de6990e3e7e19b1595f1bb6eeb4bb6a`，镜像 `sha256:3dd3f4763c4a603f1ce07fb46672a97cca69691104e3d2e059870516e3957114`。发布 image scope；text 保持原 green 版本。

| 阶段 | 实际观察窗（UTC） | 旧槽 / 新槽完成样本 | 写入保护（旧 / 新） | 结果 |
| --- | --- | --- | --- | --- |
| 10% | 03:40:19—04:10:19，1800 秒 | 5 / 0 | 926 / 931 秒 | 两侧 errors 为空；显式样本豁免晋级。 |
| 50% | 04:53:02—04:55:02，120 秒 | 0 / 0 | 99 / 102 秒 | 两侧 errors 为空；显式样本豁免晋级。 |
| 100% | 04:57:14—04:59:14，120 秒 | 0 / 0 | 73 / 77 秒 | 两侧 errors 为空；显式样本豁免完成。 |

10% 采集编排仍沿用了原最长 1800 秒窗口，且操作整理进一步延迟了推进；不能写成只观察 120 或 300 秒。已修正后续编排为 `end=entered_at+120`、当前 epoch 至少 `end+60` 后采集，不再等待自然样本。原 1800 秒窗口、完整输入和采集日志保留，未选取干净子窗口替代。沟通中的个别手写 UTC/时长错误已纠正，表内全部从 JSON epoch 转换。

三个窗口均无记录到失败 attempt、进程 ERROR、尾部错误或未完成请求下界；日志配置前后稳定。旧槽 5 个 HTTP 200 的图片业务终态仍为 unknown；所有 `business_terminal_successes_verified` 都是 0。不能据此证明零业务失败或图片成功。

各级最终文件 SHA256：

- 10→50：`b322d227acbca0f47e44389c3f2f4c05e54d33f07f7413179caa2b1e23750493`。
- 50→100：`4518ea8f559d6223cf4e1db8463febfe374107ef008e401d764fe9f9a0ad5dee`。
- 100→complete：`0e062b033524e311cf0434637fb28712a70922ae894045594cc3530f11b83f10`。

三次正式 CLI 晋级均精确接受上述整文件 SHA，保留 `passed:false / classification:operator_accepted`，没有伪装普通 evaluator 自动通过。complete 于 epoch `1794805259`（2026-11-16 05:00:59 UTC）完成；即时 status 为 `complete/runtime_gate=passed`，身份、已审 plan、controller 和有效路由一致。最终 router SHA256 为 `0eade27169af6604c286750a0b25b81ac8e9111f60be6872dde9531deac2c555`，inode `919629` 保持，`apps-current` 指向本次 release。

## 延迟验收与清理

延迟快照 epoch `1794805668`（05:07:48 UTC），距 complete 409 秒；正式 status 仍为 `complete/runtime_gate=passed`。43 个容器和配置投影、router、state、apps-current 与即时快照相等，四槽直连健康、restart=0、OOM=false；image/account-test 都指向新 green，旧 image-blue 保持原身份可回退，text 角色版本未改变。

完成后 05:00:59—05:05:59 UTC 的 300 秒窗口，旧/新槽样本仍为 0/0，错误列表为空，写入保护为 86/90 秒；没有记录到 attempt、进程 ERROR、尾部错误或 pending，下界为零不表示完整图片业务已验证。错误审阅通过且保留原始脱敏结果，未通过账号或付费模型请求补样本。

正式 `cleanup-plan/cleanup-apply` 使用 `before_epoch=1794719269` 和精确 plan SHA256 `e2fe46e6c456ced8c8e0184cee74243f98e07a7fe23c6faf29a3e9a436e51028`，返回 `cleanup_complete`。删除两份无引用旧镜像：

| 标签 | 镜像 ID | 镜像记录字节数 |
| --- | --- | ---: |
| `kkai/sub2api:0.2.18-kkai-20261027-r1-caff1f96c8` | `sha256:745597c8a09f31f36ab85b2ef8a41288b56017ca99e9e292f0bab76a1550807c` | 142363944 |
| `kkai/sub2api:v0.2.18-kkai-remove-managed-recovery-20261108-162bc0518b` | `sha256:77cc1793b7a5a68ccd877838504dfa5f60c16fd42deef655c1551c4131e1b200` | 142408980 |

当前新镜像 `80d766…`、旧 active/回退 `3dd3f4…`、text 另一槽恢复镜像 `b1c8a2…` 均因 runtime/recovery 引用保留。另有两份历史镜像被控制器判为 `unknown_or_conflicting_provenance`，明确记为待维护处置，不能归为已清理或活跃回滚资产：`kkai/sub2api:0.2.15-f6acbe2a9c`（`sha256:59d34dcffadfe43b5ad1b02a63e06bc7597b3fd9f613d13645e02487b2d4912f`）、`kkai/sub2api:0.2.6-kkai-62c843b121`（`sha256:a6e064e4a0d012a54811a364eb9f0d3f902adafda45d110cc2d92c11b43a113e`）。负责人为后续基础设施维护操作者，需先补齐来源证据，再用受控清理入口处理；本次不绕过控制器强删。该残留不改变已完成的上线结果。

本机盘点对应应用/infra 的归档、标签和 9 个容器引用后，删除同两份已确认旧镜像标签及相邻 request 验证的 tar，共 93,839,872 bytes；tar SHA256 分别为 `b9efcf7d215ceb49f2bb4ffbf9563498f7d1a42bb5cdcbf3d447ca17ee8ad551`、`f82336011b643e3c79f28e27197a70c519032b8b3468af3f996070c8a26467ea`。本机 OCI descriptor ID 与生产 config ID 分别记录，没有混为一谈。三份 active/rollback/四槽恢复归档仍在，request、manifest 和历史证据均保留。

本次远端 controller bundle 在 SHA 核对后删除，精确临时父目录移除；正式应用上传目录盘点为 0。最后 `status-final.json` 与延迟 status 完全相同，仍 `complete/runtime_gate=passed`、有效路由和新 apps-current 一致，回退旧槽健康。执行角色为发布/SRE，独立只读审查角色核对最终状态、阶段 SHA、删除/保留集和归档身份后通过。精确删除路径和字节校验在 `local-cleanup-result.json`、`upload-cleanup.json`；待维护项在 `cleanup-deferred.json`，总结果在 `final-release-result.json`，小型证据校验清单在 `SHA256SUMS.json`。

两端收尾后，从确认的 NewAPI infra 再执行一次 `make newapi-status`，退出 0：`kkai-prod-20261115.1794758551-f3334a7da`、green、HEALTHY、snapshot stable、rollback ready。证据为 `newapi-status-final.txt`（SHA256 `001b84d81f56b52e15369c0916fcbd8167542c8bbcdb123e1a292d2d212a3107`）。最终 160 项证据 SHA 自校验通过；没有为该检查重复发布。

## 正式命令结果

生产操作均来自确认的 Sub2API infra 正式 `scripts/production/kkai-sub2api-release` wrapper。绑定本节所述精确 release/plan/文件，已执行的命令包括：

- `scripts/production/kkai-sub2api-release plan-stage --request "$REQUEST" --archive "$ARCHIVE" --plan-output "$EVIDENCE/reviewed-plan.json"`：经执行者提交本次 plan SHA 后，staged_0；`REQUEST`、`ARCHIVE` 为表中原不可变制品，`EVIDENCE` 为本文件证据根。
- `scripts/production/kkai-sub2api-release promote --release "$RELEASE_ID" --to 10`：canary_10；`RELEASE_ID` 为本文件的新 release。
- `scripts/production/kkai-sub2api-release promote --release "$RELEASE_ID" --to 50 --confirm-gates-passed --error-window "$EVIDENCE/error-window-10.json" --accept-error-window-sha256 b322d227acbca0f47e44389c3f2f4c05e54d33f07f7413179caa2b1e23750493`：canary_50，operator_accepted。
- `scripts/production/kkai-sub2api-release promote --release "$RELEASE_ID" --to 100 --confirm-gates-passed --error-window "$EVIDENCE/error-window-50.json" --accept-error-window-sha256 4518ea8f559d6223cf4e1db8463febfe374107ef008e401d764fe9f9a0ad5dee`：canary_100，operator_accepted。
- `scripts/production/kkai-sub2api-release promote --release "$RELEASE_ID" --to complete --confirm-gates-passed --error-window "$EVIDENCE/error-window-100.json" --accept-error-window-sha256 0e062b033524e311cf0434637fb28712a70922ae894045594cc3530f11b83f10`：complete，operator_accepted。
- `scripts/production/kkai-sub2api-release status`：立即、延迟、清理后三次 complete/runtime_gate passed。
- `scripts/production/kkai-sub2api-release cleanup-plan --release "$RELEASE_ID" --before-epoch 1794719269` 及 `scripts/production/kkai-sub2api-release cleanup-apply --release "$RELEASE_ID" --before-epoch 1794719269 --expect-plan-sha256 e2fe46e6c456ced8c8e0184cee74243f98e07a7fe23c6faf29a3e9a436e51028`：执行者审阅精确删除集后 cleanup_complete。

## 验证命令与真实结果

以下在 `/Users/tokk/Documents/Codex/2026-08-09/li/work/kkai-infra-warmup` 执行：

| 精确命令 | 结果 |
| --- | --- |
| `PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=scripts/tests python3 -m unittest scripts.tests.test_sub2api_release_error_window_operator_v1 scripts.tests.test_sub2api_release_operator_acceptance scripts.tests.test_sub2api_release_controller` | 36 项 PASS，0.151s。 |
| `PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=scripts/tests python3 -m unittest scripts.tests.test_sub2api_release_controller_runtime_gates scripts.tests.test_sub2api_release_acceptance_wrapper scripts.tests.test_sub2api_controller_install` | 13 项 PASS，1.469s。 |
| `make policy` | 60 组、381 项 unittest 全部通过，随后 source-size 因新增测试文件 303 行超过 250 失败；该完整命令不是退出 0。 |
| `PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=scripts/tests python3 -m unittest scripts.tests.test_sub2api_release_image_sample_waiver scripts.tests.test_sub2api_release_operator_acceptance scripts.tests.test_source_size_policy` | 将新增集成用例拆到注册的独立文件后，21 项 PASS，0.737s；运行时代码未改变。 |
| `python3 scripts/check_source_size.py` | PASS，335 files、23 baselines、250-line policy。 |
| `git diff --check`、`git diff --cached --check` | PASS，精确提交后 clean。 |

未在文件拆分后重复全量 policy；没有 Ansible 行为修改，未执行 `make check`。应用侧定向测试与完整 service 单元范围的一项内存阈值失败详见 `acceptance.md`，不能将后者写成全包通过。应用全仓套件和真实登录/图片生成/退款 E2E 未执行。

## 证据索引

本次 ignored 根目录：`.local-releases/image-failure-contract-20261115/sub2-waiver-20261116/`。用户授权、操作计划、控制器安装和快照、原制品重用、精确新 plan、阶段窗口、即时/延迟状态、清理和独立审查均在此保存。`independent-release-review.md` 由独立只读角色维护；原 `sub2/` 下回退尝试不改写。

冻结采集器 SHA256 `cbf29027278b578d8969ab0f825087cd173508114eb731dff3541864fe9519ff`，原 13 个离线 fixture 通过。采集基于真实完成时间、1 小时 carry-in、晚到日志、重试/内嵌失败与尾部错误；不把 HTTP 200 写成图片成功，不伪造样本或可计算错误率。
