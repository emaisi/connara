# 2026-09-16 修改和升级操作

## 修改范围

保留 Chi/net/http、PostgreSQL、Redis 的单机/VPS 架构。建立 Git 基线 `6b70cdf`，所有改动可用 `git diff` 审查。按用户要求，同主机 HTTPS → HTTP 重定向策略原样保留；`guarded_client.go` 没有修改。

- 请求路径在参数展开前后校验，禁止路径穿越、绝对 URL 和双重编码绕过。
- JSON 业务值使用 `json.Number`，保持大整数主键在入参、响应、审计和同步中的精度；模板仅扫描原文一次，凭据中的 `{{...}}` 不再递归展开。
- owner/admin 管理平台；developer 管理系统、认证、集成、连接、操作、同步和 Webhook；viewer 只读。开发者不能签发运行令牌、管理团队或修改平台设置。仅 owner 可以授予/调整 owner，至少保留一位 owner。
- OAuth scope 字符串数组、授权参数及额外公开配置完整保留。密钥 PATCH 按字段合并，`null` 删除指定字段；版本不匹配返回 409。已有连接的集成不能改目标地址、系统或认证实例，需要创建新集成后迁移使用方。
- 认证实例的 `publicConfig.verificationPath` 可配置只读 GET 探测路径。留空时连接可用但 `lastVerifiedAt` 为空，表示只检查了配置。填写后，只有上游返回 2xx 才记录验证时间。
- 刷新等待上限 40 秒，锁 60 秒，Token HTTP 客户端上限 35 秒。等待者读取新修订；临时网络错误/5xx 不禁用连接；`invalid_grant` 要求重新授权。旋转后的 Token 使用独立五秒上下文持久化。
- 幂等请求在发出上游调用前进入 running；尚未发送的失败请求释放 claim。运行结果和幂等响应在同一事务收尾，客户端取消不影响收尾上下文。网络错误的上游执行结果记为 unknown，重放已保存的错误响应，不再次发送。崩溃留下的 running 记录由清理器转为 unknown，不能自动释放重试。
- worker 使用随机进程身份、每次领取一个任务、15 秒续租和 attempt 校验。过期领取者不能续租或提交；同一同步任务只允许一个 queued/running 作业。scheduler 的时间推进与入队同事务；每页同步的记录、检查点同事务，最后一页的运行状态和 outbox 也同事务。
- 系统、连接、操作与运行记录支持 `q/status/limit/cursor`（操作另有 `system`，运行另有 `kind`）；响应保持数组格式，`X-Next-Cursor` 指示下一页。页面按资源和筛选缓存，并有“加载更多”。配置选择器所用的目录索引会逐页缓存完整列表，以保留跨页选择能力；操作历史列表单独查询后端。
- 同步记录使用 pgx batch 写入；运行令牌规则/授权连接并入一次查询；连接和令牌 `last_used_at` 最多每分钟更新一次。
- 前端移除未使用的演示初始数据和旧页面，核心页面拆分并按路由懒加载；保存成功后才关闭表单；单个资源错误不会清空其它已加载资源。语言切换保留最新动态文字和属性，不再把加载条数、页面标题或提示恢复成旧值。

## 构建和迁移

需要 Go 1.27.1（[官方发行页面](https://go.dev/dl/)）、Node/npm、PostgreSQL 15+ 和 Redis。仓库不改变现有 `.env` 或密码文件。

```bash
npm --prefix web ci
./scripts/build.sh
# APIHUB_ENV_FILE 可指向包含迁移账号的独立文件。
./scripts/init.sh
./scripts/start.sh
```

`apihub-init` 优先读取 `APIHUB_MIGRATION_DATABASE_URL`，没有时使用 `APIHUB_DATABASE_URL`。只有该命令执行 DDL 和首次数据初始化。服务进程只检查 Schema v2，不再要求管理员初始密码哈希，也不会在重启时覆盖手工编辑的目录、角色或配置。老数据库以已有 platform_settings 记录确认已初始化；新数据库完成首次 bootstrap 后写标记。

运行账号需要业务表 SELECT/INSERT/UPDATE/DELETE、schema_migrations 的 SELECT 和 schema USAGE；不需要 CREATE/ALTER/DROP。DDL 账号由运维单独保存。本次只在隔离本机实例上运行迁移，未对现有远程库执行升级。

停止进程时先停止领取，最长等待 45 秒，随后取消剩余工作并等 worker 收尾后再关数据库。外部 Webhook 仍是至少一次投递，接收者必须按已签名的事件 ID 去重。

## Webhook v1 信封签名（协议变化）

出站和入站 HMAC 使用相同格式；旧的“仅签正文”格式不再接受。接收端验证原始正文，不能先 JSON 解析再重编码。签名内容是以下 UTF-8 前缀紧接原始 body 字节：

```text
v1\n{Unix秒时间戳}\n{事件ID}\n{事件类型}\n{原始正文}
```

Header 为 `X-APIHub-Timestamp`、`X-APIHub-Event-ID`、`X-APIHub-Event`，签名 Header 为 `X-APIHub-Signature: v1=<HMAC-SHA256小写十六进制>`。时间容差五分钟；同一 source/event ID 在数据库中去重。每次重试使用新的时间戳和相同事件 ID。

Python 接收/发送端生成示例：

```python
import hashlib, hmac, time
stamp = str(int(time.time()))
event_id = "unique-provider-event-id"
event_type = "sync.completed"
body = b'{"records":1}'
prefix = f"v1\n{stamp}\n{event_id}\n{event_type}\n".encode()
signature = "v1=" + hmac.new(secret, prefix + body, hashlib.sha256).hexdigest()
```

## 分页同步

任务支持 `syncConfig`：

```json
{
  "recordsPath": "data.items",
  "idPath": "id",
  "cursorPath": "meta.nextCursor",
  "cursorParam": "after",
  "pageSizeParam": "limit",
  "pageSize": 100,
  "maxPages": 100
}
```

记录路径留空兼容顶层数组或 `data`。主键默认 `id`，必须是非空字符串或 JSON 数字；缺失或同页重复直接失败，不使用数组下标。游标路径/请求参数必须一起配置；空游标表示结束，重复游标报错。每页持久化 operation ID、游标、条数、页数和任务版本；重试跳过已提交的页。运行中修改配置会拒绝旧结果，需发起新运行。单次响应上限 4 MiB，maxPages 1–10000、pageSize 不超过 1000。

## 主密钥轮换

密文兼容旧版（没有 keyVersion 视为 1）。生产轮换需要先在所有读写进程准备新旧密钥，再切换写入版本。示例只展示变量名，不包含密钥：

```bash
# 在受控环境文件中设置：
# APIHUB_ENCRYPTION_KEY_VERSION=2
# APIHUB_ENCRYPTION_KEY=<new Base64 key>
# APIHUB_PREVIOUS_ENCRYPTION_KEYS={"1":"<old Base64 key>"}
# 以下路径替换为已准备好的受控环境文件；轮换命令不会自动加载 .env。
set -a
source /secure/path/apihub-rotation.env
set +a
./bin/apihub-rotate-keys
```

命令按每批 100 条处理 connections、auth_instances、webhook_endpoints、webhook_sources 的全部密文（包括软删除数据），保持 AAD 和连接修订不变，CAS 防止覆盖同时刷新/编辑的凭据，可中断重跑。只有所有实例停止用旧版本写入、轮换命令返回零条且完成备份恢复验证后，才移除旧密钥。尚未启用的 platform_secrets/MFA 存储不由该命令处理；启用这些功能时必须定义 AAD 和对应轮换支持。

## 保留和恢复

已投递/死亡的 Webhook、投递尝试、已发布 outbox、终态 jobs 保留 30 天。过期会话、邀请和已完成幂等记录分批删除；运行和审计沿用平台保留天数。每类删除每轮最多 1000 条，正在运行或排队的工作不清理。unknown 幂等记录不自动删除，先依据 operation ID 和提供商日志确认结果，再由运维决定是否解除；未实现自动“猜测后重试”。

本地检查入口：`scripts/check.sh`、`scripts/test-integration.sh`，可用 `APIHUB_TEST_PG_BIN` 指定 PostgreSQL 可执行目录。集成脚本新建临时目录和监听端口，退出时停止测试实例并清理，不读取业务 `.env`。真实提供商、生产容量及备份恢复仍须在部署环境验收。

CI 配置位于 `.github/workflows/check.yml`，准备好远程 Git 仓库后会在 push/PR 中运行前端、构建、隔离集成测试、vet 和扫描；本轮未连接远程仓库，也没有声称云端 CI 已运行。
