# Image Studio R2 直传验收记录

时间：2026-10-18 00:45–01:04（北京时间）。本次只创建自有随机 `image-studio/acceptance/` 临时对象，未调用图片生成接口、写用户业务行或修改 Cloudflare 配置。

## 结论与边界

真实 R2 与 Chrome 155 均验证通过：UploadPart 签名中的 `Content-Length` 会被服务端执行，浏览器可通过 Blob 自动提供该值，并读取响应 ETag。已有凭据支持 Complete、GET、ListMultipartUploads、Abort 和删除；清理全部完成。

签名由应用锁定的 `aws-sdk-go-v2/service/s3 v1.97.3` 生成，使用与新图片 signer 相同的 `UploadPartInput.ContentLength` 参数；没有从尚未发布的新上传 API 获取签名。真实浏览器 Origin 为现有 CORS 允许的 `http://127.0.0.1:4322`。生产 Origin `https://api.kkrich.ltd` 的 OPTIONS/PUT 使用真实 HTTP 验证，不能把它表述为生产网页端到端验收。

本次包括 68 字节合成 PNG 的浏览器边界验收，以及 16,780,612 字节合法合成 PNG 的四分片恢复与合并验收。没有覆盖配置允许的最大尺寸、真实弱网性能或新 API 的完整业务流程；后者仍需要本地契约测试和候选版本验收。

## 实测结果

| 检查 | 结果 |
| --- | --- |
| 签名 `X-Amz-SignedHeaders` 包含 `content-length` | 是 |
| 签名 68 字节、实际 67 字节 | R2 HTTP 403 |
| 签名 68 字节、实际 69 字节 | R2 HTTP 403 |
| 签名 68 字节、实际 68 字节 | R2 HTTP 200，ETag 存在 |
| 生产 Origin 的 OPTIONS | HTTP 204；允许 `GET, HEAD, PUT`、`content-type` |
| 生产 Origin 的 PUT | `Access-Control-Allow-Origin: https://api.kkrich.ltd`；暴露 `ETag` |
| Chrome 页面少/多 1 字节 PUT | 均被拒绝；错误响应不暴露 CORS，页面读不到 403 数字 |
| Chrome 页面精确长度 PUT | HTTP 200，JavaScript 可读取 ETag |
| Chrome 是否手动设置禁止的 Content-Length | 否，由 Blob 自动产生 |
| ListParts | 一个 68 字节分片，ETag 可用于 Complete |
| Complete → GET → SHA-256 | 成功，68 字节与合成源一致 |
| ListMultipartUploads → 按精确 key Abort | 成功；原会话随后 `ListParts=NoSuchUpload` |
| 清理后 HEAD | `NotFound` |
| 清理后精确 key 的 MPU 数量 | 0 |

当前存储凭据的 `GetBucketCors` 返回 `AccessDenied`，因此没有读取或更改桶策略。上表的 CORS 结论来自实际预检和上传响应。

R2 的 ListMultipartUploads 返回的 UploadId 与 CreateMultipartUpload 返回值可以不同。用列表返回 ID 直接 Abort 成功，原 ID 随后返回 NoSuchUpload。清理实现应使用列表 ID，不能额外要求两种 ID 字符串相等；当前实现符合这一点。

## 大图分片恢复验收

本机用标准库生成 2048 × 2048 RGBA PNG，逐行使用独立随机字节、合法 PNG chunk/CRC 和 zlib 数据流；文件为 **16,780,612 字节**。服务端完整 `png.Decode` 成功。源 SHA-256：`7820b8791abe98f092417163e1b16fdcb89ef1d9a473af6da85be7d6cf4a0352`。

固定生产分片尺寸 **5,242,880 字节（5 MiB）**。首片上传后丢弃本地完成清单，通过 ListParts 恢复首片 ETag，只上传缺少的第 2–4 片，最终使用再次 ListParts 返回的清单 Complete。首片实际发送次数为 1；这证明续传协议路径，不是弱网性能测试。

```text
large_png_decode=ok width=2048 height=2048 bytes=16780612
large_part_size=5242880 part_count=4
large_upload_part=1 bytes=5242880 http_status=200 etag_present=true
large_resume_list=ok
large_resume_recovered_parts=1 large_resume_recovered_etag=true
large_upload_part=2 bytes=5242880 http_status=200 etag_present=true
large_upload_part=3 bytes=5242880 http_status=200 etag_present=true
large_upload_part=4 bytes=1051972 http_status=200 etag_present=true
large_final_list=ok
large_first_part_upload_count=1 large_list_total_bytes=16780612
large_complete=ok
large_get=ok
large_hash_match=true downloaded_bytes=16780612
large_cleanup_abort=ok
large_cleanup_delete=ok
large_cleanup_head=NotFound
large_cleanup_list=ok
large_cleanup_sessions=0
```

执行命令：

```sh
ssh -o BatchMode=yes -o ConnectTimeout=12 -o IdentitiesOnly=yes \
  -o ProxyCommand=none -o ProxyJump=none \
  -o KexAlgorithms=curve25519-sha256 -i ~/.ssh/ovh_sys1 sys1 \
  'sudo -n python3 /tmp/kkai-image-r2-acceptance-20261018-run.py large'
```

## 执行方式

临时验收工具在 Mac 上构建，仅上传到 sys1 的 `/tmp` 运行。服务端辅助进程读取现有 writer 配置，并把只读 credential 文件挂载映射到宿主路径；凭据内容未传回本机或输出。容器根文件系统是只读的，未向运行容器写入文件。

关键命令（临时源文件保留在本机 `/tmp/kkai-image-r2-acceptance-20261018/`，远端工具已删除）：

```sh
# 在确认的基础设施仓库读取当前状态：HEALTHY。
make newapi-status

# 在应用仓库，使用现有 go.mod/go.sum 构建独立协议探针。
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -o /tmp/kkai-image-r2-acceptance-20261018/probe \
  /tmp/kkai-image-r2-acceptance-20261018/main.go \
  /tmp/kkai-image-r2-acceptance-20261018/browser.go \
  /tmp/kkai-image-r2-acceptance-20261018/large.go

ssh -o BatchMode=yes -o ConnectTimeout=12 -o IdentitiesOnly=yes \
  -o ProxyCommand=none -o ProxyJump=none \
  -o KexAlgorithms=curve25519-sha256 -i ~/.ssh/ovh_sys1 sys1 \
  'sudo -n python3 /tmp/kkai-image-r2-acceptance-20261018-run.py'

# 生成十分钟、单个临时对象的分片签名；stdout 仅进入 mode 0600 文件。
umask 077
ssh -o BatchMode=yes -o ConnectTimeout=12 -o IdentitiesOnly=yes \
  -o ProxyCommand=none -o ProxyJump=none \
  -o KexAlgorithms=curve25519-sha256 -i ~/.ssh/ovh_sys1 sys1 \
  'sudo -n python3 /tmp/kkai-image-r2-acceptance-20261018-run.py browser-prepare' \
  > /tmp/kkai-image-r2-acceptance-20261018/signed.json

# 仅监听 loopback 的验收页，使用 CUA 正常点击按钮触发网页自己的 fetch。
python3 /tmp/kkai-image-r2-acceptance-20261018/server.py

# 浏览器上传之后，完成、校验并清理临时对象/会话。
ssh -o BatchMode=yes -o ConnectTimeout=12 -o IdentitiesOnly=yes \
  -o ProxyCommand=none -o ProxyJump=none \
  -o KexAlgorithms=curve25519-sha256 -i ~/.ssh/ovh_sys1 sys1 \
  'sudo -n python3 /tmp/kkai-image-r2-acceptance-20261018-run.py browser-finish'
```

`+1` 字节的原始 HTTP 对照由服务端 Python 从同一受限临时签名文件加载 URL，`urllib.request.Request(..., data=payload+b'\0', method='PUT')` 实际请求 R2；只输出 `oversize_1byte_http_status=403`，不输出 URL。

浏览器流程仅使用 CUA 导航、按钮点击、DOM 快照与截图；未通过只读 evaluate 执行网络请求。最终 DOM 结果：

```text
Origin：http://127.0.0.1:4322
少 1 字节请求：被拒绝（错误响应未通过 CORS）
多 1 字节请求：被拒绝（错误响应未通过 CORS）
精确长度请求：HTTP 200
浏览器可读取 ETag：true
Content-Length：由浏览器 Blob 自动生成，脚本未设置
结论：PASS
```

最后一轮服务端结果：

```text
browser_list_parts=ok
browser_complete=ok
browser_get=ok
browser_hash_match=true bytes=68
browser_cleanup_abort=ok
browser_cleanup_delete=ok
browser_cleanup_head=NotFound
browser_cleanup_list=ok
browser_cleanup_sessions=0
```

所有临时签名文件均已删除，验收页面已关闭，本地 HTTP 服务已停止，远端辅助程序与状态文件已清理。本次没有运行全量测试套件。
