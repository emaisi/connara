# API、系统、集成与账号开发完成度核查

核查日期：2026-10-03。对照文档：`api-system-integration-ui-development-plan.md`。

结论：**本期开发与本地验收已完成。下列 8 项核查问题均已修复，完整隔离集成测试无排除项通过。** 用户 Windows 上游与真实账号未在本次验收中调用。下文保留首次核查的复现记录，最终结果见文末。

## 已确认完成的主要部分

系统级 API 创建、系统内唯一标识、独立保存、API 详情和测试页面、同系统执行目标选择、无认证账号省略、预览与执行共用 URL/body 构建、响应业务条件与结构检查、集成地址编辑、目标版本验证失效、账号启停、历史目标快照以及导航简化均有对应实现。工作流已使用 API 与步骤超时中较短的限制。

首次核查发现以下边界缺陷；现已补齐并复验。

## 首次核查发现的缺陷（均已修复）

### A01 高：Schema 与参数映射缺少一致性校验，默认值可能绕过约束

位置：`internal/httpapi/admin.go:878`、`internal/httpapi/runtime.go:138`、`internal/httpapi/action_preview.go:73`、`internal/executor/request_config.go:293`。

保存时分别校验请求配置和 JSON Schema，没有检查两者的字段、类型、必填和默认值是否一致。执行先用原始输入校验 Schema，构建器随后才添加默认值，添加后不再进行范围约束校验。

本次临时 Go 程序复现：query 参数 `page` 类型 integer，默认值 0；输入 Schema 的 `page.minimum` 为 1，字段非必填。配置和 Schema 都被接受，空输入通过 Schema，实际生成 `https://example.test/items?page=0`。

相反，Schema 将有默认值的字段设为 required 时，空输入会在应用默认值之前被拒绝。页面自动生成的 array Schema 还固定使用 string items，与构建器允许标量数组的语义不同。

完成标准：保存时校验映射与 Schema 契约；预览、执行、工作流、同步均先生成同一份有效输入，再校验含默认值的输入；增加默认值越界、必填默认值和数组类型的回归覆盖。

### A02 高：固定 API Key 写法未完全被禁止

位置：`internal/executor/request_config.go:67`。

`secretName` 识别 `api-key` 和 `api_key`，但不识别 `ApiKey`。临时 Go 程序使用 `{name:"ApiKey",value:"dummy-audit-value"}` 固定请求头，`ValidateConfig` 返回成功。

因此用户仍能把这类认证值存入 API 定义的普通 JSON 配置，违反文档要求认证秘密只从账号和实例注入。预览脱敏函数采用另一套名称判断，不能替代保存入口的禁止规则。

完成标准：共享已有敏感字段名称判断，覆盖大小写和分隔符写法；拒绝相应固定头和参数。测试使用假值，不能在测试或报告中记录真实凭据。

### A03 中：大小写不同的同名 header 参数会静默覆盖

位置：`internal/executor/request_config.go:116`、`:140`。

参数名称唯一检查区分大小写，但 HTTP header 不区分大小写。本次程序同时声明 `X-Id` 和 `x-id`，配置被接受；输入值分别为 first、second，实际最终 header 为 second。

完成标准：header 参数名称按 HTTP 语义检查重复，固定头和动态头使用统一冲突规则，保存时明确报错。

### A04 中：示例和默认值的表单错误状态与实际输入不一致

位置：`web/src/pages/api-definition-editor.tsx:310`、`:338`。

示例和默认值使用 `defaultValue` 与 onBlur 更新，所有行共享一个 valueError；任意其他字段解析成功都会清除该错误。按数组下标作为 key，删除行也可能使未受控输入继续显示上一行的值。

8081 浏览器复现：integer 参数的默认值填 `not-a-number`，离开输入框后显示错误；再将示例改为 1 并离开，错误提示消失，但默认值框仍是 `not-a-number`。未解析的输入不会更新参数对象，存在显示内容与待提交旧值不一致的问题。

证据：`/tmp/connara-completion-audit/screenshots/invalid-default.png`、`error-cleared.png`。本次没有保存这个表单。

完成标准：维护各行各字段的原始输入与错误，使用稳定行 ID；任一字段错误必须阻止保存，直到修正或删除相应字段。删除行后剩余字段不串值。

### A05 中：移除路径占位符缺少确认和参数清理

位置：`web/src/pages/api-definition-editor.tsx:108`。

changePath 只自动增加参数，不处理删除。浏览器输入 `/items/{id}` 后改成 `/items`，未出现确认，旧必填 path 参数 id 仍保留。后端会拒绝无占位符的映射，用户需要自己找到并删除旧参数。

证据：`/tmp/connara-completion-audit/screenshots/orphan-path.png`。

完成标准：移除占位符时明确确认其对应参数如何处理；不能残留无效映射，也不能静默丢弃用户输入。

### A06 中：同步调用没有应用 API 的超时配置

位置：`internal/background/service.go:255`、`:262`。

同步复用了请求构建与响应检查，但直接用 job context 调用 Executor.Action，没有调用 executor.Budget，也没有建立每次 API 请求的超时 context。共享 Executor 本身不读取 executionConfig.timeoutMs；API 和工作流依靠各自调用方施加预算。

完成标准：同步的每次上游调用也遵守 API 配置的超时，与现有任务预算取更严格限制，并验证不会因超时自动重复不确定请求。

### A07 中：版本冲突后未刷新预览和候选

位置：`web/src/pages/actions.tsx:224`。

409 分支仅清空预览并提示“重新打开此 API”，没有重新获取定义、集成和账号版本。预览 effect 的依赖未因此改变，页面无法直接恢复新的请求预览，必须退出再进入。

完成标准：版本冲突后保留输入，重新获取目标与定义版本并生成新预览；明确要求用户重新确认目标，不自动重新执行。

### A08 低：校验失败的字段定位与响应结构原因不完整

位置：`internal/executor/request_config.go:509`、`web/src/pages/api-definition-editor.tsx:486`。

Schema 响应错误被统一替换为“response structure does not match the configured schema”，不包含失败字段路径或预期类型；保存错误主要显示页面级文本，没有字段错误映射与焦点定位。

完成标准：从已有校验器提取不含敏感值的失败路径和约束类型，返回结构化字段错误，界面可定位并聚焦。隐藏真实响应值，不必丢弃安全的路径信息。

## 首次核查验证结果（历史记录）

| 检查 | 本次结果 |
|---|---|
| `./scripts/check.sh` | 退出 0，Go 检查、前端格式/lint、11 个文件 50 个测试和生产构建通过 |
| 无排除项的隔离 PostgreSQL/Redis 集成测试 | 失败，退出 1；`TestIdempotencyCleanupPreservesUnknownAndActiveJobs` 报 unsafe replay；其他列出的包通过 |
| 额外临时 Go 复现 | 默认值越界、header 大小写覆盖、ApiKey 固定头均复现；临时程序已删除 |
| 8081 浏览器 | 路径参数残留、默认值错误被其他字段清除已复现；表单已放弃，未写业务数据 |
| 当前服务 | 根目录 `bin/apihub` 运行，health 返回 200 |
| 用户真实 Windows/WSL 18001 上游 | 本次未验证 |

全量集成测试中的幂等清理失败在上轮交付前已存在并被排除，本次明确重新运行了该用例。它不能被用“其他测试通过”替代，也不能据此声称全部集成测试已通过。需要单独修复和验证 unknown/active 请求不会重新获得可重放的声明。

日志：`/tmp/connara-completion-audit-check.log`、`/tmp/connara-completion-audit-integration.log`；这些 /tmp 证据是当前机器上的临时产物。

## 首次核查的修复顺序

优先补齐 A01、A02，再处理 A03—A07，最后完善 A08 的错误定位。针对已复现的分支补上回归测试，完成无排除项集成检查，再重新构建并验证 8081。真实 Windows 上游验收独立记录。

API 分组为原设计的可选能力，未实现不计入本次核心缺陷；OpenAPI 导入、自动重试契约、流式文件与批量测试仍在原文档范围之外，不为它们扩大本次任务。


## 最终修复与复验结果（2026-10-03）

| 项目 | 修复结果与覆盖 |
|---|---|
| A01 | 保存校验 Schema 与映射契约；所有调用入口先应用默认值再校验。保留自定义 Schema，标量数组语义统一。`request_contract_test.go`、`system_api_test.go` 覆盖越界默认值、必填默认值与旧版本拒绝。 |
| A02 | 复用敏感名称规则并覆盖 ApiKey 等写法，拒绝认证固定头和参数。 |
| A03 | header 名称按大小写不敏感语义检查重复与冲突。 |
| A04 | 每行稳定 ID；示例和默认值使用受控原始输入及独立错误；错误阻止保存并定位字段。 |
| A05 | 删除占位符要求确认；取消恢复路径及参数草稿，确认清除对应参数。 |
| A06 | 同步调用应用 API 超时预算；不确定请求结束为 unknown，并停止自动重试。`sync_test.go` 覆盖超时后仅发出一次请求。 |
| A07 | 409 后刷新定义与候选、保留输入并重建预览；确认新目标前禁止执行，不自动重发。 |
| A08 | 返回结构化字段错误；Schema 错误包含安全字段路径和约束，不包含响应值；界面支持焦点定位。 |

此外修复幂等清理：只清理过期已完成记录和无关联运行的过期孤立声明，保留 unknown 请求的阻止重放记录；重复清理仍不会释放 unknown 声明。`TestIdempotencyCleanupPreservesUnknownAndActiveJobs` 已通过，不再排除。

- 常规检查、前端 11 个文件共 60 个测试、类型检查和生产构建通过。
- 隔离 PostgreSQL/Redis 的 `go test -race -count=1 ./...` 全量通过，没有 `-skip`；覆盖数据库、HTTP、同步、工作流与幂等分支。
- 8081 已重新构建并重启，health 200。浏览器实际验证版本冲突刷新时没有上游请求、输入保留、确认后才发送；受控上游返回 HTTP 200 但 id 类型不匹配时整体失败，显示 `$["id"]` 与预期 integer。
- 浏览器验证非法默认值不会被其他字段清除、保存被阻止并聚焦错误字段；路径参数移除确认及取消恢复保留草稿。移动端 390/320 px 无横向溢出，未记录 JS 错误。
- 仅本次创建的测试 API、集成、认证实例及关联运行/审计数据已按精确 ID 清理；临时表单已放弃。未修改用户账号凭据与 Windows 上游地址。

日志：`/tmp/connara-completion-final-check.log`、`/tmp/connara-completion-full-integration.log`；浏览器证据：`/tmp/connara-completion-verified/screenshots/`。临时文件仅作为本机验收证据。真实 Windows/WSL 18001 服务及真实账号认证仍需在其可访问时单独验收，不等同于本期代码未完成。
