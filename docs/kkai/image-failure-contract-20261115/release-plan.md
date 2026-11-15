# 图片失败协议修复：执行发布计划

日期：2026-11-15。状态：发布前只读准备；命令尚未代表执行成功。专业审查见 `review-release.md`，现场证据与时间见 `release-preparation.md`。本文件由发布/SRE角色维护，真正构建、提交和生产写操作由主执行者串行完成。

## 1. 冻结范围、顺序与边界

顺序固定为 **NewAPI backend → NewAPI frontend → NewAPI 完整验收/收尾 → Sub2API image → Sub2API 完整验收/收尾**。先交付安全诊断，使 Sub2API marker 生效时已有诊断能力。两端协议兼容且各自可回退；没有跨服务原子切换。Sub2API 灰度期间禁止其他应用发布，否则非目标服务生命周期保护会检测到漂移。

NewAPI 为联合变更，数据库/schema/控制器/Edge 配置不变；使用 external 后端与已安装格式对应的前端流程。Sub2API 为 image scope，代码审查需证明文本路径未改变。不得创建分支/worktree、访问代码托管网络、在生产构建、更新控制器或执行迁移。当前目标只是代码热修复，不是官方版本升级。

所有正常 build、upload、stage、canary、promote、观察、归因、回退与受控清理已获用户授权；执行者自行审阅控制器计划和错误证据，不再向用户索取等价确认。用户不提供登录账号，不创建账号、Key 或业务数据：**用户接手业务测试，代理未验证**。范围包括真实登录、管理员日志详情交互、真实图片生成/编辑与终态退款 E2E。定向本地自动化、只读自然流量、版本/健康/契约/灰度/回滚仍需完成，已发现回归不得以此豁免。

## 2. 固定路径与本次证据目录

以下 shell 块在 Mac 的 Bash 中使用。大写 `VERIFIED_*` 值只从本次已成功输出赋值，不照抄历史版本或按文件 mtime 猜测。

```bash
newapi_app='/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/new-api-kkai'
newapi_infra='/Users/tokk/Documents/Codex/2026-08-04/s-y-s-s/kkai-infra'
sub2_app='/Users/tokk/Documents/Codex/2026-08-09/li/work/sub2api'
sub2_infra='/Users/tokk/Documents/Codex/2026-08-09/li/work/kkai-infra-warmup'
evidence_root="${newapi_app}/.local-releases/image-failure-contract-20261115"
mkdir -p "${evidence_root}/preparation" "${evidence_root}/newapi" "${evidence_root}/frontend" "${evidence_root}/sub2"
chmod 700 "${evidence_root}"
ssh_options=(-o BatchMode=yes -o ConnectTimeout=12 -o IdentitiesOnly=yes
  -o ExitOnForwardFailure=yes -o ProxyCommand=none -o ProxyJump=none
  -o KexAlgorithms=curve25519-sha256 -i ~/.ssh/ovh_sys1)
export GIT_NO_LAZY_FETCH=1
```

应用产物仍由各原生脚本输出：NewAPI `.local-releases/`、前端脚本打印的 archive/metadata、Sub2API infra `artifacts/sub2api/`。本任务证据目录保存 metadata 引用、精确校验、stdout/stderr、阶段状态和审阅，不复制密钥、DSN、原始模型请求或客户图片。小型结果纳入 `acceptance.md`，大制品不入 Git。

## 3. 构建前门禁

1. 完成最终代码审查和各开发文档要求的最小相关检查；只 stage/commit 本任务文件。NewAPI 保持 `production/kkrich`，Sub2API 保持本次已确认 `release/v0.2.21-kkai`。
2. 每个应用记录完整 HEAD 并执行 `git cat-file -e "${sha}^{commit}"` 与 `git archive --format=tar HEAD >/dev/null`；NewAPI 构建时必须干净树且正式 build/deploy/contract 文件齐全。保留其他用户改动，不清空工作树。
3. 使用本次 preparation 的 fresh `make -C "$newapi_infra" newapi-status` 首个快照；必须 HEALTHY、rollback ready。发生身份变化或跨越较长开发窗口时，在真实 build/stage 前更新必要只读身份与 schema证据，不能把准备记录当永久授权坐标。
4. 核对应用 pin `ee4e460d1ac77e017ee3ad3a4af97beb3b090634` / `router-v3-staged` 等于实际安装控制器；本地 pin 对象可读。只因 infra 文档 HEAD 变化不重装控制器。
5. 精确目标源码目前两个 profile 都为 canonical21 `(21,21,21)`，且要求 `rc49-online-v1` 与 `video-media-metadata-v1`。取得实时 canonical digest、两项完整扩展回执/物理校验，再明确选择 `release_schema_contract`。没有证据或不匹配时停止，不默认 bridge、不迁移。目标提交若改契约，必须重做此比较。
6. 核对实际 selected frontend format、current/previous、backend API contract、已安装 controller/Edge 能力。format2兼容后端阶段不构建/切换前端；保留原指针/校验和。若实际是 format1，停止本文件 format2 分支，依 runbook20 落实精确配对，不现场伪装格式迁移。
7. Sub2API 实际 self-test 已确认安装 e7f898f7831a50ed9675885fd92cbdf71e306847，`operator-reviewed-v1` 与 `reviewed-image-cleanup`。在其正式发布开始时重新核对同一 installation identity 和稳定 state，保持 source wrapper 匹配；`status` 可能 rebaseline/save，在实际发布阶段执行。

2026-11-15只读准备已通过，详见 `release-preparation.md`：实际frontend为format2/api_contract4，pair与delivery验证通过，当前rollback-plan为backend-only；live canonical21和两扩展齐全，安装pin匹配。因实际tenant routing已启用且安装门禁要求feature，本次明确设置 `release_schema_contract=feature`。最终源码及正式stage仍须绑定新制品；准备快照不替代后续实时状态。

## 4. NewAPI 后端

构建前记录真实 UTC 基线；时间变量保留在同一 shell/证据中：

```bash
baseline_end_epoch="$(date -u +%s)"
baseline_start_epoch="$((baseline_end_epoch - 300))"
baseline_start="$(date -u -r "${baseline_start_epoch}" '+%Y-%m-%dT%H:%M:%SZ')"
baseline_end="$(date -u -r "${baseline_end_epoch}" '+%Y-%m-%dT%H:%M:%SZ')"
cd "${newapi_app}"
test "${release_schema_contract}" = bridge || test "${release_schema_contract}" = feature
scripts/kkai/build-manual-release.sh --schema-contract "${release_schema_contract}" --frontend-mode external
```

默认 2 CPU/3g 资源档，不顺带做提速实验。只为精确提交构建一次，使用 stdout `MANUAL_RELEASE_METADATA` 的精确路径：

```bash
release_metadata="${VERIFIED_MANUAL_RELEASE_METADATA}"
expected_version="$(jq -er .version "${release_metadata}")"
test "$(jq -er .schema_contract "${release_metadata}")" = "${release_schema_contract}"
scripts/kkai/deploy-manual-release.sh --stage "${release_metadata}"
```

stage 的已安装 image-validation 必须执行并绑定精确 archive/image 的 binary contract 与实时数据库；若安装证据不能证明该能力，先在 Mac对同一不可变 image ID 执行 `docker run --rm --pull never --network none --entrypoint /kkai-migrate "$verified_image_id" --describe-contract --dialect postgres --json`，比对第3节证据。不得为查询契约重建。

stage 要求 preflight ready、pin/protocol/schema一致、staged输出版本一致。非零结果先保留输出，并仅调用一次 candidate-status 判断已发生的状态，不盲重试 stage/promote/build。

```bash
candidate_status="$(ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-manual-deploy candidate-status)"
rg -qx 'KKAI_CANDIDATE_STATUS=ready' <<< "${candidate_status}"
rg -Fxq "KKAI_CANDIDATE_VERSION=${expected_version}" <<< "${candidate_status}"
candidate_target="$(sed -n 's/^KKAI_CANDIDATE_TUNNEL_TARGET=//p' <<< "${candidate_status}")"
target_slot="$(sed -n 's/^KKAI_CANDIDATE_SLOT=//p' <<< "${candidate_status}")"
test -n "${candidate_target}"
test "${target_slot}" = blue || test "${target_slot}" = green
candidate_tunnel_socket="/tmp/kkai-newapi-image-failure-$$.sock"
test ! -e "${candidate_tunnel_socket}"
ssh "${ssh_options[@]}" -M -S "${candidate_tunnel_socket}" -fN -L "127.0.0.1:4322:${candidate_target}" sys1
curl --retry 3 --retry-connrefused --retry-delay 1 --fail --silent --show-error http://127.0.0.1:4322/api/status |
  jq -e --arg version "${expected_version}" '.success == true and .data.version == $version'
ssh "${ssh_options[@]}" -S "${candidate_tunnel_socket}" -O exit sys1
unset candidate_tunnel_socket
```

记录业务测试交接。candidate 为 serving且无后台作业；其健康不能证明 async worker，worker依赖本地回归和promote后writer自然证据。不调用图片生成接口制造数据。

```bash
rollout_start="$(ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-rollout start --target-slot "${target_slot}" --observation-seconds 120)"
rollout_id="$(sed -n 's/^KKAI_ROLLOUT_ROLLOUT_ID=//p' <<< "${rollout_start}")"
test -n "${rollout_id}"
```

等真实 observation_ends_at，并在每个窗口记录目标版本、rollout、业务交接、已取得的按槽证据和错误归因。三次窗口每次至少120秒；不使用一次长阻塞 sleep。按阶段顺序执行：

```bash
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-rollout status
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-rollout apply --percent 50 --observation-seconds 120
# 等50%窗口结束并复核
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-rollout status
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-rollout apply --percent 100 --observation-seconds 120
# 等100%窗口结束并复核
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-rollout status
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-rollout handoff-ready --target-slot "${target_slot}" --target-version "${expected_version}" --rollout-id "${rollout_id}"
source "${newapi_app}/scripts/kkai/manual-deployment-contract.env"
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-manual-deploy promote --expected-infra-sha "${KKAI_INFRA_SHA}" --deployment-protocol "${KKAI_DEPLOYMENT_PROTOCOL}"
```

promote内部finalize，不重复执行。writer排空可能最长24分钟，保持精确事务证据并等其结果；中断后先用 `release-resume-status.sh --metadata "$release_metadata" --slot "$target_slot" --rollout-id "$rollout_id"` 只读核对。

成功promote后记录 candidate_start UTC；立即 `make -C "$newapi_infra" newapi-status`，五分钟后再执行；都须精确版本、HEALTHY、rollback ready。保存promote输出证明实际router/writer/leader拓扑，status本身不证明这些。candidate窗口300秒结束后再等60秒：

```bash
BASELINE_START="${baseline_start}" BASELINE_END="${baseline_end}" \
CANDIDATE_START="${candidate_start}" CANDIDATE_END="${candidate_end}" \
make -C "${newapi_infra}" newapi-error-window
```

只用已批准聚合，执行者完成逐组因果审查；exit0/review_required不是通过。已独立证明不相关的错误不增加等待；无法排除变更回归时保持或官方回退。真实样本不足/HTTP延迟聚合不可用记 unavailable，不记零错误。

## 5. NewAPI format2 前端

后端事务完整完成且接口向前兼容后，在同一干净源码提交构建独立产物（这不是重复后端构建）：

```bash
cd "${newapi_app}"
scripts/kkai/frontend-build-release.sh --theme default
```

记录脚本输出的精确metadata/archive、release ID、SHA-256和字节数，按私有sys1路由传到本次唯一目录，双端验哈希。不得在文档中预填尚未生成的文件名。保存当时选中前端ID和 `/srv/kkai/newapi-current/release-manifest.yml` 实际不可变目标。

```bash
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-frontend-manual-deploy prepare --metadata "${VERIFIED_REMOTE_FRONTEND_METADATA}" --backend-manifest "${VERIFIED_CURRENT_BACKEND_MANIFEST}"
ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-frontend-manual-deploy activate --release-id "${VERIFIED_FRONTEND_RELEASE_ID}" --expected-current-release "${VERIFIED_PREVIOUS_FRONTEND_ID}" --backend-manifest "${VERIFIED_CURRENT_BACKEND_MANIFEST}"
```

prepare不切流，activate按当前CAS坐标切换。前端UI字段缺失必须安全降级，以支持旧backend/回退组合。验证入口、版本化JS/CSS、no-store HTML、实际资源哈希、当前/previous、console runtime交集及rollback-plan；存在 `default/assets/kikai-home.html` 时比对公网200/no-store和当前文件SHA。管理员登录交互由用户接手，代理不声称验证。失败只选择仍兼容的已安装前端回退，禁止手改指针。完成前端及第8节NewAPI收尾后，才开始Sub2API。

## 6. Sub2API image

先用匹配wrapper核对安装与当前稳定状态，再构建：

```bash
"${sub2_infra}/scripts/production/kkai-sub2api-release" status
"${sub2_infra}/scripts/production/kkai-sub2api-release" build --app-dir "${sub2_app}" --version "${VERIFIED_UNIQUE_VERSION_BASE}" --scope image --reason 'Preserve definitive image failure evidence and stable error code'
```

wrapper固定Mac `kkai-mirror-builder`、linux/amd64、Docker代理 `http://host.docker.internal:7897`，使用精确git archive并加入10位commit后缀。只用输出的archive/request；检查源码/版本/平台/镜像ID和migration contract，不改生产schema、不用deploy/build_image.sh。

```bash
"${sub2_infra}/scripts/production/kkai-sub2api-release" plan-stage --request "${VERIFIED_SUB2_REQUEST}" --archive "${VERIFIED_SUB2_ARCHIVE}" --plan-output "${evidence_root}/sub2/reviewed-plan.json"
```

工具展示完整plan后，执行者核对scope、目标服务/回退/保护集、router inode/hash、apps-current、archive/source/image、固定v1及安装身份；通过原PTY stdin提交exact plan_sha256（默认300秒）。无需用户确认。成功结果必须staged_0；记录release ID。计划漂移则停止并重新只读plan审查，复用原制品；不重复成功stage。

候选直连/health、迁移契约、有效图像路由、restart=0/OOM=false及保护服务生命周期必须通过；完整图片业务E2E交接用户。

```bash
"${sub2_infra}/scripts/production/kkai-sub2api-release" promote --release "${sub2_release_id}" --to 10
```

每阶段从真实entered-at开始，每120秒评估已取得自然流量，最长30分钟；三个观察阶段都至少120秒。禁止发送模型请求凑样本。`baseline.samples`、`candidate.samples`需真实正数；各群失败完整保留，HTTP200内嵌失败不得改标为成功。窗口不足时继续当前阶段，延长原窗口而非丢弃原失败。30分钟仍没有足够真实样本或因果不明，使用exact release官方rollback并报告限制，不伪造证据晋级。

每次审阅最终完整v1文件后执行对应下一步：

```bash
"${sub2_infra}/scripts/production/kkai-sub2api-release" promote --release "${sub2_release_id}" --to 50 --confirm-gates-passed --error-window "${evidence_root}/sub2/error-window-10.json"
"${sub2_infra}/scripts/production/kkai-sub2api-release" promote --release "${sub2_release_id}" --to 100 --confirm-gates-passed --error-window "${evidence_root}/sub2/error-window-50.json"
"${sub2_infra}/scripts/production/kkai-sub2api-release" promote --release "${sub2_release_id}" --to complete --confirm-gates-passed --error-window "${evidence_root}/sub2/error-window-100.json"
```

这些命令之间分别保留完整120秒至30分钟观察，绝不能连续执行。v1正常error event仅五字段 `count/status/origin/reason/upstream_status`。正常允许candidate外部 overload/timeout 且两个状态在408/502/503/504。可独立证明不相关的既有/客户端/外部错误按 runbook 的 `operator_acceptance` 绑定本阶段release、source/destination、entered-at、router hash、reason，并额外传 `--accept-error-window-sha256` 的最终文件SHA；保留正常evaluator失败和operator_accepted，不能伪装自动通过。v2/advance不用。

被动证据需明确目标容器身份、图片端点、真实UTC范围及脱敏来源。自然usage/image_count可作计数佐证但不能独立证明有效图片；无独立输出验证则仍记录业务E2E未由代理验证。100%时baseline使用本次已保存的明确历史旧槽窗口并标明范围，不伪造同窗旧槽流量。

complete后立即及五分钟后执行wrapper status，要求 `complete`、`runtime_gate=passed`、精确image/commit、有效image/account-test路由、保护对象无漂移、旧active可回退、router inode稳定、apps-current/manifest一致。检查本次图片终态marker/unknown边界相关错误与panic、DB/Redis、持续5xx。保留所有阶段样本不足记录，不只保留最终干净摘要。

## 7. 停止、恢复与回退

- NewAPI灰度回退：`ssh "${ssh_options[@]}" sys1 sudo -n /usr/local/sbin/kkai-newapi-rollout rollback`；候选废弃用 `kkai-newapi-manual-deploy abort`；完成后端切换后的回退用官方 `rollback`，先执行前端 `rollback-plan --backend-manifest "$VERIFIED_ROLLBACK_MANIFEST"` 决定是否需先切前端。
- Sub2API：`"${sub2_infra}/scripts/production/kkai-sub2api-release" rollback --release "${sub2_release_id}"`，要求rolled_back及随后稳定拓扑；manual_recovery_required不执行通用自动回退，保留现场按专用恢复入口处理。
- 身份/contract不符、已确认候选特有回归、未解释可能相关故障、无样本到上限都停止正常推进。SSH中断不能说明操作未发生；先查原事务、锁、state和实际identity，不能重发写命令猜结果。
- 不改历史任务状态、额度或退款，不重放任务7734，不恢复旧数据库覆盖新数据。

## 8. 清理和完成标准

NewAPI后端/前端完成后分别盘点本机与sys1本次及历史制品。按exact metadata、image ID、容器引用和控制器retention确定保留集：backend current/rollback至少24小时及所有schema pin；frontend current/previous/rollback-plan目标和30天资源支持窗口。controller成功promote/activate的warning单独记录，不重放动作清warning。前端到期项仅在已安装能力证明后用 `prune --dry-run` 审阅并同参数 `--confirm-plan-sha256` 应用。不能手删线上release/image/static树。关闭本任务SSH socket，核对上传archive已由正式wrapper处理；保留小型证据、校验过的FFmpeg缓存及仍需回退archive。

Sub2API完成即时/延迟验证后，使用已确认安装的受控清理：

```bash
cleanup_before_epoch="$(( $(date -u +%s) - 86400 ))"
"${sub2_infra}/scripts/production/kkai-sub2api-release" cleanup-plan --release "${sub2_release_id}" --before-epoch "${cleanup_before_epoch}" > "${evidence_root}/sub2/cleanup-plan.json"
# 执行者核对全部删除集、四槽/其他容器引用、current/previous/state和回退资产后提取exact plan_sha256
"${sub2_infra}/scripts/production/kkai-sub2api-release" cleanup-apply --release "${sub2_release_id}" --before-epoch "${cleanup_before_epoch}" --expect-plan-sha256 "${VERIFIED_CLEANUP_PLAN_SHA256}" > "${evidence_root}/sub2/cleanup-result.json"
"${sub2_infra}/scripts/production/kkai-sub2api-release" status
```

保留active/rollback、router备份、state/audit、controller恢复版本。全局手册“无历史清理入口”为历史描述，本次只读self-test已证明reviewed-image-cleanup实际安装；以真实能力和该精确控制器接口为准，不现场升级。清理失败/身份未知/保留期未到分别记录未完成或受保护及负责人，禁止raw Docker prune/rmi和宽范围rm。

最终 `acceptance.md` 必须列明：两个精确源码SHA、独立制品和校验、安装身份/计划SHA、三个窗口与逐组决定、立即/延迟状态、有效路由和回退坐标、登录E2E交接、实际自然样本/缺口、精确验证命令及结果、全套是否跳过、清理删除集/保留集/未完成项。只有全部适用安全门禁通过，才写上线完成；清理结果单独报告。
