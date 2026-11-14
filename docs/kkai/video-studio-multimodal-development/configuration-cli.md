# 视频工作室一次性受控配置

`/kkai-video-studio-config` 随本轮精确后端镜像交付。它只处理原有 11 个 profile
及 `video_studio.multimodal_enabled`，不启动应用、任务或迁移，不创建模型，不接受
任意 option、SQL 或外部传入的 API3 快照。日常维护继续使用真实管理员 UI。

固定目标为 ID `1,3,4,5,6,7,8,9,10,11,12`，逐一绑定当前精确型号。
普通 2.0 与 `seedance-2.5` 仅配置图片、音频；五个 `_with_video_ref` 型号还配置视频，
且要求至少一个视频。图片最多 9 张，视频/音频各最多 3 个；没有跨类型合计上限。
时长、字节、格式、尺寸与帧率保持已审查的独立限制。原 `seedance-2.5` 禁用状态保留。

## 清单与顺序

每次先从精确镜像运行 `--inspect --dsn-stdin`，保存输出及镜像、源提交、执行者证据。
返回每条 profile 的 `expected_version`、`expected_sha256`、当前 specification/defaults
及整个集合的 `profiles_sha256`；指纹覆盖原始 specification/defaults、名称、描述、
排序、启用状态和时间戳。清单中的指纹必须直接取本次结果。

先将开关显式持久为 false（若已经 false 无须重复写），再准备 profiles，最后独立
开启。不能用代码默认值代替持久 false：批量配置事务同时锁定控制行，避免并发开启。

开关清单示例；下面的摘要占位符不能执行：

```json
{
  "protocol": "video-studio-config-v1",
  "operator": "tokk",
  "action": "multimodal",
  "expected_profiles_sha256": "<本次 inspect 的 profiles_sha256>",
  "switch": { "expected": "absent", "value": false }
}
```

开启前另建清单，使用配置完成后的新集合指纹，`expected:"false"`、`value:true`。
开关 action 不允许携带 profile 更新；关闭也必须绑定刚观察到的状态。开启会再次
检查全部 11 个 profile 的分类策略与保留 defaults，不能先开启再补空模型目录。

profile 清单必须完整列出 11 条；下面只展示一条的形状：

```json
{
  "protocol": "video-studio-config-v1",
  "operator": "tokk",
  "action": "profiles",
  "profiles": [
    {
      "id": 1,
      "model": "sd_2.0_fast_special_720p",
      "expected_version": 3,
      "expected_sha256": "<本次 inspect 的逐条指纹>",
      "specification": {
        "version": 4,
        "modes": ["text_to_video", "reference_to_video"],
        "parameters": [],
        "reference_policy": null
      }
    }
  ]
}
```

示例的空参数/空策略不可使用。实际 `parameters` 必须逐项保留原定义和默认值，
仅把参数作用域及创作方式中的 `image_to_video` 改为 `reference_to_video`；移除
`reference_inputs`，新增已审查的分类策略，version 恰好增加 1。工具拒绝顺便改参数。
共享字段和 defaults 不接受外部覆盖，事务内从锁定的原记录获取。

对保存后的清单计算 SHA256。预览命令无 `--apply`，使用只读事务；完整服务层更新
及已发布样片检查在 apply 事务中执行，任何失败整批回滚，成功只允许更新 11 个。
服务层自动保存 API3 snapshot，不能以自行拼接 JSON 替代。

```text
/kkai-video-studio-config --dsn-stdin --manifest /plan/profiles.json --manifest-sha256 <SHA256> --timeout 30s
/kkai-video-studio-config --dsn-stdin --manifest /plan/profiles.json --manifest-sha256 <SHA256> --timeout 30s --apply --audit-file /audit/profiles-<unique>.jsonl
```

apply 必须是主机授权的 root 操作者。每次使用新审计文件，权限固定 0600；先落盘
并同步 applying 记录，再修改数据库，提交后追加 applied 结果。提交或最终审计结果
不确定时只读核对，不能盲目重试。旧清单在版本或 hash 变化后会被拒绝。

## 凭据与精确镜像执行

由发布操作者先按正常 runbook 取得并核对实际容器 ID、镜像 ID、启动时间、应用 PID、
writer 身份与版本；完成双协议后端/writer/adapter/schema 验收，并排除进行中的 rollout。
工具只执行配置，不代替这些门禁。不得将历史记录作为当前进程证据。

使用已确认容器的 root-controlled `/proc/<host-pid>/environ` 中已有的 `SQL_DSN`。
只在主机 root Python 进程内提取该字段，通过 `subprocess.run(input=...)` 送给命令
stdin；不把 DSN 放入参数、临时文件、输出或 Docker 环境变量。不要输出整个 environ，
也不读取 session/JWT 等凭据。新容器必须不设置 `SQL_DSN` / `KKAI_MIGRATION_DSN`。
以下为受控包装器的核心形状，坐标必须来自同一次身份核对：

```python
entries = Path(f"/proc/{verified_host_pid}/environ").read_bytes().split(b"\0")
dsn_values = [entry[len(b"SQL_DSN="):] for entry in entries if entry.startswith(b"SQL_DSN=")]
if len(dsn_values) != 1 or not dsn_values[0] or b"\n" in dsn_values[0]:
    raise RuntimeError("runtime DSN unavailable")
command = [
    "docker", "run", "--rm", "--pull", "never", "-i", "--user", "0:0",
    "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
    "--network", "container:" + verified_container_id,
    "--mount", f"type=bind,source={reviewed_directory},target=/plan,readonly",
    "--mount", f"type=bind,source={audit_directory},target=/audit",
    "--entrypoint", "/kkai-video-studio-config", verified_image_id,
    "--dsn-stdin", "--manifest", "/plan/profiles.json",
    "--manifest-sha256", reviewed_manifest_sha256, "--timeout", "30s",
]
# 仅在预览和计划审核后追加 --apply / --audit-file；每次使用新的审计文件名。
completed = subprocess.run(command, input=dsn_values[0] + b"\n",
                           capture_output=True, timeout=45, check=False)
```

`verified_image_id` 必须是含该 CLI 的精确 B 镜像；代码内还会核对该构建所需的完整
运行时 schema/扩展契约。主机计划与审计目录须 root 所有且权限 0700，清单 0600。
执行后再次核对源进程身份未变化，保存脱敏结果；不要在异常中打印 stdin 或环境。

配置完成后先 inspect 比较 11 条的版本、分类能力、原状态及 API3 快照；再执行独立
开启清单。B 的 `/api/status.video_studio.multimodal_enabled` 可观察进程实际值；
A 没有该新增字段，不能声称 A 已提供此观察接口。确认运行时接收状态后才按正常
前端控制器激活新 UI。用户接手的业务 E2E 不替代配置核验和发布门禁。
