# NewAPI 后端独立代码审查

日期：2026-11-15。审查角色：主协调者，非本次Go实现作者；使用 code-review/ponytail。基线55a55f56e，范围为本次worker、relay审计、图片诊断投影及相关回归测试。

## 结论

代码审查通过，无剩余阻断问题。最终定向测试已记录于newapi-backend-development.md第6节并通过；此审查不代替生产验收。无数据库、计费算法、状态机或新依赖改动。

## 审查结果

- 初始直接公开上游原因的补丁已由安全投影替换。worker传原错误仅供原有分类/退款，DurableImageExecution限定共享日志分支；公开及系统日志为固定摘要，诊断仅在admin_info.image_upstream_error。
- 首次无效JSON响应的独立系统日志出口已按现有execution fence限定屏蔽正文，普通relay语义保持。worker完成日志使用固定分类，没有拼接上游自由文本。
- 脱敏以活动key精确及JSON转义回显为先，覆盖Bearer/Basic/常见凭据、URL/IP和控制字符；64KiB/非法编码/不安全结构仅输出结构化字段，最后2048字节UTF-8限长。type/code限制128安全标识字符，未将任意对象转字符串。
- 审查发现的凭据外形tag（sk-…）和冒号凭据tag（api_key:…、password:…）已通过复用既有模式拒绝并补测试。控制字符清理后重新检查可能拼回的敏感模式，不能通过截断遗留半个秘密。
- 诊断构造无I/O，不修改原错误或触发policy副作用。日志写入失败维持现有非阻塞行为；worker成功结果/完整accounting优先、旧fence拒绝、单次退款和重复事件不重发路径未修改。
- 权限验证覆盖admin/root保留、self/token/tenant剥离；前端独立审查另记code-review-frontend.md。测试正常中文原因只在管理员字段，generation/outbox和系统输出无原文。
- service表测在现有真实结算边界增加provider_error的成功/partial/staging/count/accounting/fence场景，未复制新的资金算法；legacy与membership需分别核对实际执行，不能以skip冒充通过。

## 边界

本次是后台图片诊断修复，不把任意供应商自由文本宣称为适合公开。通用relay和OAuth生产者不作全仓日志重构；NewAPI对明确失败marker的既有消费合同保持，原始JSON严格完整性由Sub2API来源层承担。历史原始事故JSON未恢复，不能以测试fixture声称恢复过该内容。

定向测试由实现角色记录，审查复用通过且输入未变化的结果，不重复全仓套件。依用户要求不登录生产账号；上线仍需正式身份、契约、灰度、观察、回退与清理证据。
