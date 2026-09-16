# 多命名自动分组实施方案

## 1. 目标与边界

在保留现有 `auto` 行为和配置兼容性的前提下，增加独立的命名自动分组，例如 `auto2`：

- `auto` 使用现有 `AutoGroups` 顺序。
- `auto2` 使用自己的候选顺序，例如 `["vip", "default"]`。
- 每个自动分组都复用现有的渠道选择、失败重试、模型列表、权限和计费流程。
- 一个 API Key 只选择一个自动分组；同一个请求不会同时执行两套自动策略。
- 本次不改变数据库 schema，不批量修改已有 API Key，不改变 `DefaultUseAutoGroup` 的含义。

本方案针对 `web/default` 和后端主链路，同时维护 `web/classic` 的兼容显示和提交行为。只增加可选配置项，不要求旧版本已有配置迁移。

## 2. 配置契约

继续保留现有配置：

```json
["default", "vip"]
```

该配置的 option key 仍是 `AutoGroups`，含义仍是虚拟组 `auto` 的候选顺序。新增 option key：

```json
{
  "auto2": ["vip", "default"],
  "auto3": ["default", "premium"]
}
```

新增配置名建议为 `AutoGroupProfiles`。它只保存额外的命名自动组，不把旧 `AutoGroups` 嵌入新对象，避免旧版前端保存分组设置时覆盖 `auto` 配置，也避免升级时需要重写旧 JSON。

### 2.1 名称规则

- `auto` 是保留的兼容名称，由 `AutoGroups` 管理。
- 新 profile 使用 `auto2`、`auto3`、`auto10` 等格式，规则为 `^auto[2-9][0-9]*$`。
- 不接受空名、`auto1`、重复候选、空候选数组、嵌套对象、自引用或候选中的虚拟自动组。
- profile 名不能同时作为普通 `GroupRatio` 分组名。option 校验层保存时返回明确错误，不静默覆盖普通分组。
- 候选分组可以暂时不存在于分组比例表；运行时沿用现有逻辑取用户可用组与候选列表的交集，找不到渠道时返回无可用渠道。

### 2.2 删除和失效策略

- 删除 profile 配置前，后端查询并提示仍有 API Key 使用该 profile；第一版禁止直接删除，要求先把这些 Key 改回普通组或另一个自动组。
- profile 候选为空或 JSON 损坏时，更新失败并保留上一份有效配置。启动加载也先解析、校验，再原子替换，不能像当前 `UpdateAutoGroupsByJsonString` 那样先清空后解析。
- 旧 `AutoGroups` 配置为空的行为保持不变：`auto` 报未启用；额外 profile 为空时只影响对应 profile。

## 3. 后端实现分层

### 3.1 setting 层：单一配置解析入口

改造 `setting/auto_group.go`：

- 保留 `GetAutoGroups`、`AutoGroups2JsonString` 和旧更新入口。
- 增加 `AutoGroupProfiles` 的类型、解析、序列化和并发安全读写。
- 增加通用方法：
  - `IsAutoGroup(name string) bool`：识别 `auto` 和已配置 profile。
  - `GetAutoGroupCandidates(name string) []string`：返回独立的候选副本。
  - `GetAutoGroupProfilesCopy() map[string][]string`：供管理端和状态接口只读使用。
- 所有 getter 返回拷贝，禁止调用方修改全局 slice/map。
- 使用 `common.Unmarshal` / `common.Marshal`，先解码到临时值并完成全部校验，成功后一次性替换。

### 3.2 service 层：统一用户可用候选展开

改造 `service/group.go`：

- 保留 `GetUserAutoGroup(userGroup)` 作为 `auto` 的兼容包装。
- 增加 `GetUserAutoGroupCandidates(userGroup, autoGroup string)`，内部读取对应 profile 的候选顺序，再与 `GetUserUsableGroups(userGroup)` 求交集。
- 增加 `GetUserAutoGroups(userGroup)`，只返回当前用户有权限的虚拟自动组，供下拉框和 API 返回。
- `GroupInUserUsableGroups` 继续统一处理普通组和自动组，不在调用点写名称判断。

### 3.3 认证、选组和重试

统一替换所有 `group == "auto"` 的自动策略判断：

- `middleware/auth.go`：通过 `service.IsAutoGroup` 或 `setting.IsAutoGroup` 判断虚拟组；权限仍由 `GetUserUsableGroups` 决定。
- `middleware/distributor.go`：渠道 affinity 命中时使用当前 profile 的候选列表；写入 `ContextKeyAutoGroup` 的仍然是真实候选组名。
- `service/channel_select.go`：根据 `param.TokenGroup` 取得对应 profile 候选，复用现有跨组重试状态机。日志中记录虚拟 profile 和最终真实组，避免把 `auto2` 当成计费组。
- `cross_group_retry` 继续控制当前自动 profile 是否在重试时跨候选组切换；初次候选不可用时仍按现有规则寻找下一个候选。
- `controller/relay.go` 的普通 relay 和 task/异步路径都继续调用同一个选择入口，不新增第二套重试实现。

### 3.4 模型列表和 token API

以下入口必须使用统一的 profile 展开 helper：

- `controller/token.go` 的 token 模型列表和 token group 校验。
- `controller/model.go` 的 OpenAI 模型列表和 owner group 计算。
- `controller/group.go` 的用户分组选项：返回用户可用的普通组，以及用户有权限的 `auto`/`auto2` 等虚拟组。
- 自动组 option 增加 `is_auto: true`；前端据此识别虚拟组，旧前端仍可回退到 `value === "auto"`，禁止通过 `startsWith("auto")` 猜测。
- `controller/pricing.go`：`auto_groups` 字段保持兼容表示 `auto` 的真实候选；新增可选字段 `auto_group_chains` 返回可见 profile 到真实候选的映射，避免前端猜名称。
- `controller/misc.go` 的状态信息增加 profile 配置是否存在所需的只读元数据，但不把内部未授权候选泄露给普通用户。

### 3.5 计费和上下文不变量

自动 profile 本身不能参与倍率查询。每次成功选中候选后必须：

1. 将真实候选写入 `ContextKeyAutoGroup`。
2. 将 relay 的 `UsingGroup` 更新为真实候选组。
3. 让 `service/quota.go`、`relay/helper/price.go` 和日志使用真实候选组计算倍率。
4. 保留 `TokenGroup` 为用户选择的虚拟 profile，便于审计和回显。

这样 `auto2` 不会触发 `GetGroupRatio("auto2")`，也不会产生错误计费或空倍率。该不变量需要在成功选择、候选失败、重试切换和无候选四种路径分别测试。

### 3.6 状态、指标和聚合

- `controller/perf_metrics.go` 的活动组集合改为从配置注册表生成。
- `controller/kkai_group_status.go` 将当前用户可见的每个自动 profile 及其候选传入 service。
- `service/kkai_group_status.go` 和 `service/kkai_group_status_merge.go` 抽取通用的“虚拟组聚合真实候选”函数；`auto` 和 `auto2` 只传不同的 profile 名。
- 每个 profile 的状态只聚合其候选中用户可见的真实组，不能泄露被权限过滤的组，也不能重复累加同一候选。
- 对外状态中的 `group` 是 profile 名，内部指标和计费仍使用真实组事件。

## 4. 配置持久化和管理端

### 4.1 后端 option 持久化

修改 `model/option.go`：

- 初始化 `common.OptionMap["AutoGroupProfiles"]`。
- 在启动同步、单项更新和批量更新路径中注册该 key。
- 更新时调用 setting 层校验；失败不写入数据库、不更新内存 OptionMap。
- `validateOptionValue` 对 JSON 类型、名称格式、候选唯一性和普通分组冲突做同样校验。
- 旧版本没有该 key 时使用 `{}`，无需数据库迁移。

### 4.2 `web/default`

修改系统设置的 group form、section registry、类型和 `use-update-option`：

- 在「分组定价」中新增“自动分组策略”编辑区。
- 提供 profile 名、候选分组顺序、添加/上移/下移/删除和保存校验。
- `auto` 继续显示现有 `AutoGroups` 编辑器；`auto2` 等额外策略编辑 `AutoGroupProfiles`。
- 不让管理员直接编辑一大段嵌套 JSON 作为唯一入口；保留 JSON 模式用于高级用户，但两种模式使用同一序列化函数。
- 保存前提示 profile 删除会影响哪些 token；失败时展示后端错误。
- API Key 表单从 `/user-groups` 的返回值渲染所有虚拟组，不再只判断 `value === 'auto'`。
- `cross_group_retry` 的显示条件改为该分组是任意自动 profile。
- API Key 列表、分组 badge、模型/价格页不要通过 `auto` 字符串猜测；使用后端返回的 `is_auto_group` 或 profile 元数据。
- 所有新增文案进入 en/zh/fr/ja/ru/vi 六套 locale；不引入新依赖。

### 4.3 `web/classic`

保留旧管理端的 `AutoGroups` 编辑能力，并新增 `AutoGroupProfiles` 的最小 JSON 编辑与校验提示。token 编辑和列表中的自动组判断改为读取后端返回的自动组元数据；若旧后端没有该字段，仍按 `auto` 兼容。

## 5. 测试策略

按风险执行定向测试，不跑无关全量测试。

### 5.1 Go 单元测试

- `setting/auto_group_test.go`：旧数组兼容、profile 解析、格式校验、重复候选、空候选、原子更新、返回拷贝。
- `service/group_test.go`：用户分组权限、特殊可用规则、候选交集和顺序。
- `service/channel_select_pool_test.go`：`auto` 与 `auto2` 独立候选、初次不可用、跨组重试开关、状态推进和无候选错误。
- `controller/token_test.go` / `controller/model_list_test.go`：profile token 的鉴权、模型并集和 model limit。
- `middleware` 相关测试：普通组、`auto`、`auto2`、未授权 profile、已删除 profile。
- 计费回归测试：`auto2` 成功选到真实组后，倍率、预扣、结算和日志中的 `UsingGroup` 使用真实组。
- `service/kkai_group_status_test.go`：多个 profile 的聚合互不串组、不重复计数、隐藏候选不泄露。

### 5.2 前端定向检查

在 `web/default`：

```bash
bun run typecheck
bun run lint -- src/features/keys src/features/system-settings src/components/group-badge.tsx
bun run test -- src/features/system-settings/models/group-ratio-visual-editor.test.ts
bun run i18n:check
```

实际脚本若不支持路径参数，则使用对应脚本的全项目版本，并记录原因；不在 L2 变更中默认运行全仓库构建。

在 `web/classic` 只运行受影响的 lint/build 检查，具体命令以该目录 package.json 为准。

### 5.3 手工验收矩阵

1. 旧配置只有 `AutoGroups`：升级后 `auto` 行为、计费和现有 token 不变。
2. 配置 `auto2=[vip,default]`，用户可用 `auto2`：API Key 可选，优先走 vip，失败后按开关切 default。
3. 用户无 `auto2` 权限：下拉框不显示，手工写 token.group 也返回 403。
4. `auto2` 候选包含用户无权使用的组：该组被跳过，不越权访问。
5. 删除正在使用的 profile：管理端拒绝删除并列出影响 token。
6. `auto2` 最终选到 vip：模型列表、计费倍率、消费日志和状态聚合都反映 vip；TokenGroup 仍记录 auto2。
7. `auto` 和 `auto2` 同时请求：候选状态、重试索引和日志不互相污染。
8. 旧 classic 管理端保存普通分组配置：不会覆盖 `AutoGroupProfiles`。

## 6. 分阶段执行顺序

1. 先提交 setting parser、option 持久化和纯 helper 测试。
2. 接入鉴权、渠道选择、affinity、模型列表、token API 和计费上下文。
3. 接入状态/指标聚合和后端响应字段。
4. 接入 default 前端，再补 classic 兼容。
5. 执行定向 Go、TypeScript、lint、i18n 检查，审查 diff 和配置兼容性。
6. 在本地使用 `auto2` 完成最小 relay、失败重试、计费和模型列表验收后，才考虑提交或发布。

本功能属于后端与前端联合变更；若以后发布到 KKAI 生产，按 NewAPI runbook 选择 backend/combined scope，使用当前 `production/kkrich` checkout 和手工发布流程。方案本身不授权构建、stage、promote 或修改生产配置。

## 7. 完成标准

- 没有业务代码继续用 `group == "auto"` 判断自动策略，除非它明确处理模型参数中的普通字符串 `auto`。
- `auto` 旧配置和旧 token 行为通过回归测试。
- `auto2` 能独立选择、重试、计费、列模型、显示状态，并严格遵守用户权限。
- 配置更新失败保持上一份有效配置，删除在用 profile 有明确阻断。
- default/classic 前端均不会丢失另一套自动策略配置。
- 定向测试和前端检查通过，未执行的全量检查在提交说明中明确记录。
