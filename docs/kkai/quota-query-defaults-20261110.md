# 额度查询默认开启：2026-11-10

用户明确要求所有现有 Key 开启“允许查询用户额度”，以后创建的 Key 也默认开启。

## 原因和处理

rc49 的新权限列对存量行默认关闭，导致 `/api/usage/token/` 不再返回账户余额字段。CC Switch 将缺失字段按零处理。只读实测为 HTTP 200、Key 有效、账户余额字段缺失，确认了这一兼容遗漏。

通过 sys1 对生产 PostgreSQL 进行独立数据维护，按每批500条、100ms锁等待上限和3秒语句上限更新唯一字段 `allow_user_quota_query`，跳过被实时业务锁住的行。21,284条现有记录中21,273条由关闭改为开启，剩余关闭数为0；不改变令牌状态、实际余额、消费计数或分组。操作前保存仅含ID和原布尔值的root-only恢复记录，批次结果落盘；没有导出Key。缓存沿用60秒签名元数据过期，没有清空Redis或为回填重启服务。随后CC Switch的原查询已返回真实账户余额，并通过原脚本的有效性条件。

普通创建API、前端表单、注册默认Key已默认开启。本次补齐图片和视频工作室自动创建Key的两个入口，源码提交为 `8a796af030e9b5194dd7dc3b0d7b63aacec00a47`。用户主动明确关闭仍会被保留；这里的规则是默认开启，而非永久禁止关闭。

数据库物理 DEFAULT FALSE 属于已经安装的冻结schema契约，未修改。所有支持的创建入口显式提供业务默认值true；没有通过篡改迁移checksum、加触发器或修改旧迁移来改变既有契约。

## 验证

以下定向命令通过（0.072s），覆盖工作室Key真实持久化与幂等创建：

```sh
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off .local-releases/rc49-integration/toolchain/go/bin/go test -mod=readonly -p 2 ./service -run '^Test(EnsureImageStudioTokenIsIdempotentAndManaged|VideoStudioTokenWithoutModelHintReplacesObsoleteRestrictedKey|EnsureVideoStudioTokenIsIdempotentAndUsesEnabledModelWhitelist)$' -count=1 -timeout=60s
```

`git diff --check`通过。本轮未重跑全套；没有创建生产测试账号/Key或发起模型业务请求。账户余额查询为只读真实验证；新Key创建业务E2E仍由用户接手，代理未验证。

## 发布与收尾

后端 `kkai-prod-20261110.1794299358-8a796af03` 已正式上线。10%/50%/100%灰度每档至少120秒，精确候选、即时和五分钟延迟健康、固定错误窗口复核均通过；最终 `make newapi-status` 为HEALTHY、blue、snapshot stable、rollback ready。回退版本为 `kkai-prod-20261110.1794294463-21cb44e38`，正式前端计划为backend-only；前端current/previous及Redis/router身份保持不变。

100%窗口中3条503经独立查询确认是对应模型/分组零abilities、零可用渠道，与权限默认值变更无关。最终窗口为 `08:53:01Z–08:58:01Z`，在60秒写入保护后取得聚合：1716个事件、11条错误。按精确源码差异确认，这次只改两个创建入口的布尔初值，转发、参数校验、渠道选择、计费和缓存算法未变；该权限只用于余额披露及既有缓存签名。保留原窗口，完成范围归因，不宣称逐条诊断了上游根因。用户要求停止扩展无关报错排查；一次额外只读归因查询因SSH连接关闭未返回结果，之后正常恢复控制命令成功。

会员后台已恢复apply且各进程acknowledged；候选socket已关闭，唯一新建本地镜像archive校验后删除，小型metadata与证据保留。远端上传临时文件已由控制器删除。线上release/image受v10 snapshot保护，前端资源仍在30天保留期；历史d3e640059归档按原处置继续保留。清理后身份、指针、镜像、保留资源及健康复核通过，没有清理失败。完整摘要及证据摘要值见 [机器记录](quota-query-defaults-20261110.json)。

完整本地操作证据位于 `.local-releases/ccswitch-quota-20261110/`，数据库变更前ID快照及批次记录位于 `/srv/kkai/newapi-data-maintenance/quota-query-20261110T082616Z/`。备份SHA256为 `ffd3a238e06c8b764cc26d9b2b260892377478b474b4a23b07eb62c04b2ae5aa`。这些证据不包含API Key；回退数据权限时需核对之后的用户修改，不能回滚余额或数据库快照。
