# 视频工作室 API 3 / API 4 相邻版本兼容

本文件记录本地代码的兼容边界及发布顺序，不证明生产安装、schema receipt、外置适配器、付费供应商或回滚状态已通过验收。

## 协议边界

- 旧页面不发 `X-KKAI-Console-API`，或显式发 `3`，使用冻结的 API 3 facade。基线为本地提交 `b17930150`：旧模型 `reference_inputs`、样例 ID/URL 数组与隐式复用、`image_to_video`、报价 canonical JSON、幂等 fingerprint 保持原语义。
- 新页面的 Studio 用户/管理 JSON 请求发 `X-KKAI-Console-API: 4`，使用新能力策略、有序 `reference_assets` 与严格 JSON 解码。签名媒体 URL 和对象存储上传不增加此请求头。
- 未知版本返回 400；所有版本响应添加 `Vary: X-KKAI-Console-API`。
- 首次通过 API 4 更新旧能力模型时，旧 specification 和 defaults 被保存在现有 specification JSON 内的 `api3_snapshot`。API 3 读/改该快照，API 4 读/改外层 specification；共享名称、启用状态等仍遵守原管理语义。不增加数据库列。
- 旧样例保留原 ID 和引用快照，新样例使用 object envelope；两种公开样例列表按格式隔离。API 4 更新旧样例返回 `409 video_sample_requires_copy`，要求另建，避免原地转换旧样例 ID 或清理其引用。API 3 新建/编辑样例记录旧 specification 快照的版本。
- API 3 允许原先已 ready 且没有新媒体元数据的素材，视频上传沿用原服务器上限，不接收音频 purpose。API 4 仍执行严格媒体元数据和模型限额校验。
- 普通 `/v1/videos` 的已知旧图片/视频标量字段与 API 3 Studio 均投影成 `reference_image` / `reference_video`，不改成数组。只有没有新 `reference_assets` envelope 的旧请求可走此路径。新旧引用混用、未知参数及伪造 `studio_api_contract` 均拒绝。
- API 4 引用集合继续投影为复数数组及首尾帧字段；必须以精确外置适配器版本的契约验证为启用前提，不能用 JS 插件单测代替。

## 持久开关与发布顺序

后端结构能力声明为 API contracts `[3,4]`，并声明 `video_studio_multimodal`；能力声明不代表功能开关已开启。

持久选项为 `video_studio.multimodal_enabled`，通过既有 `PUT /api/option/` 更新。首轮源码默认值显式为 `false`。关闭时，API 4 的创建任务、报价、发起上传和管理配置写入返回 `503 video_multimodal_unavailable`；旧 API 3、查询、已有任务及用户上传收尾仍可继续。

1. 明确持久化 `false`，保持新 API 4 前端未激活；按正常发布门禁交付双协议后端 A 和新媒体 writer。
2. 验证 A 的旧页面路径和旧上游字段，准备第二个真实源码版本 B（默认值 `false → true`，不是空提交或同提交重建）。持久选项仍为 `false`，使 B 发布过程保持关闭。
3. 确认 active、writer、rollback 后端全部具备双协议、媒体与新适配器契约，取得必要 schema/controller/adapter 证据后，再持久化 `true`、配置新模型能力并激活 API 4 前端。
4. A 作为回滚版本读取同一持久 `true`，可以继续处理已打开的新页面；不能回滚到仅 API 3 的前任后端或旧 writer。

后台 worker 不依赖此开关停止处理已受理的任务。开关控制新入口，不替代安装、schema、适配器、canary 和回滚验收。

## 本地验证

```sh
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./service ./controller ./plugins ./setting/video_studio_setting -run '^(TestVideoStudio|TestNormalizeVideoStudio|TestValidateVideoModel|TestValidateVideoParameters|TestVideoModel|Test.*VideoSample|TestCreateVideoAsset|TestVideoAssetPipelineInspect|TestPrepareVideoStudio|TestSubmitVideoStudio|TestQuoteVideoStudio|TestSora|TestLoadR2)' -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./service ./controller -run '^(TestVideoStudioAPI3HTTP|TestListVideoModel|TestResolveVideoModel|TestCreateVideoModelProfile|TestUpdateVideoModelProfile|TestAdminVideoModel|TestVideoMultimodal)' -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./common ./router -run '^TestConsoleContract$' -count=1 -timeout=120s
GOTOOLCHAIN=local go test -modfile=/tmp/new-api-kkai-go-test.mod ./service ./controller -run '^(TestVideoStudioAPI3SamplesRetainSnapshotVersionAndRequireAPI4Copy|TestVideoStudioMultimodalErrorsHaveStableHTTPContract|Test.*VideoSample)' -count=1 -timeout=120s
```

上述定向验证覆盖旧请求签名、跨 profile 升级幂等重放、旧样例复用、双向 profile 编辑、旧上传限制、版本/开关 HTTP 边界、新协议拒绝旧字段、Seedance 标量/数组精确投影和路由编译。全仓测试未运行，仓库 Go 依赖文件未改动。

## 现有模型的最小配置升级

当前管理页的 `getVideoModelPreset()` 仅为未配置候选型号初始化表单；编辑已存在模型使用服务端 specification 和 defaults，不自动套预设。策略字段目前为只读展示，没有“升级旧模型”按钮。发布时应逐型号准备可审查的管理 API 更新 JSON，避免重建模型或覆盖自定义设置。

1. 读取 `GET /api/admin/video-studio/model-profiles/:id`，显式发 `X-KKAI-Console-API: 4`，保存完整原响应；另以 header `3` 保存旧协议响应。
2. 更新 body 保留 `model`、`display_name`、`description`、`provider_label`、`enabled`、`sort_order`、`default_parameters` 的原值。`default_parameters` 中 `false`、`0` 与空值不能用 truthy 合并或预设值替换。specification.parameters 保留 control/default/options/min/max/step/request_key；仅将各参数的 mode 范围中的 `image_to_video` 对应为 `reference_to_video`。
3. specification.version 增加 1，specification.modes 作同一 mode 对应，移除 `reference_inputs`，追加经过精确型号和 adapter 验证的 `reference_policy`。不要擅自放宽原 duration 最大值或改写 2.0 的 seconds、2.5 的 duration 映射。`_with_video_ref` 型号的 policy 必须 `requires_video: true`，不能赋予纯文本生成能力。
4. 已有 `generate_audio: false` 保留不变；用户可显式打开后使用参考音频。如果现有 parameter key 中缺少 `generate_audio`，应在单独审查的配置差异中补充 switch，否则 `audio_requires_generate_audio` 会阻断音频引用。不要把所有型号强制默认为有声。
5. 必须确认 policy 的非空 `max_total_count`，不要仅根据各类 count 相加猜总数。前端候选预设故意将它设为 null 并默认禁用。缺少证据时不得把原模型整体禁用来“准备”新策略：`enabled` 是双协议共享字段，会中断旧用户。
6. A/B/adapter/writer/rollback 完成验收后，保持新前端未激活，先持久化 `video_studio.multimodal_enabled=true`，再以 header `4` `PUT /api/admin/video-studio/model-profiles/:id`。关闭开关时管理 POST/PUT 也被阻止。
7. PUT 会在服务端事务内冻结旧 specification/defaults；客户端不提供 `api3_snapshot`。成功后分别 GET header `3` 和 `4`，验证 API 3 保持旧模式/版本/映射/defaults，API 4 显示新策略/版本，名称和原启用状态未意外变化。并发管理编辑没有乐观锁，因此执行前要重新 GET 比较原快照，有差异则重做审查 JSON。
8. 新建 API 4 样例，不原地转换旧样例。完成 API 4 模型和样例验证后再激活新前端。

后端对自定义参数继续进行正常验证。转换失败应保留原模型并检查具体差异，不能以整个 preset 替换来绕过验证。
