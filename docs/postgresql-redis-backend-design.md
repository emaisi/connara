# APIHub PostgreSQL、Redis 与 Go 后端设计

> 状态：v1 已实现并已在 PostgreSQL 18 上完成迁移、初始化与主要管理接口验证。
>
> 对应 UI：2026-09-15 中文控制台；不包含环境切换、网络代理、函数运行时、智能体、MCP 和账单入口。

## 1. 技术结论

- HTTP：Go 1.23、`net/http`、`github.com/go-chi/chi/v5`。
- 数据库：PostgreSQL 是唯一业务事实库，使用 `pgx/v5/pgxpool`。
- 临时协调：Redis 使用 `go-redis/v9`，只承担 OAuth 一次性状态、限流和 Token 刷新锁。
- 前端：React 19、TypeScript、Vite、Tailwind CSS 4，生产资产通过 `go:embed` 随 Go 服务发布。
- 密钥：连接凭据与系统级秘密使用 AES-256-GCM；运行时 Token 和邀请 Token 只保存 SHA-256 哈希。
- 后台任务：PostgreSQL `FOR UPDATE SKIP LOCKED` 队列，不引入 Kafka。
- 可观测性：`slog` JSON 日志和 PostgreSQL 运行聚合，不强制引入 Prometheus、Grafana、Loki、Tempo 或 Elasticsearch。

本项目是全新开发，没有 SQLite 迁移、双写或旧库兼容路径。

## 2. 页面与后端资源

| 页面 | 主表 | 配套表 | 真实用途 |
| --- | --- | --- | --- |
| 概览、快速开始 | 多表聚合 | `operation_runs`、`jobs` | 展示真实资源数和运行状态 |
| 系统目录 | `systems` | `system_groups`、`system_auth_templates` | 管理公开平台和企业系统、分组及允许的认证模板 |
| 认证中心 | `auth_templates`、`auth_instances` | 无 | 模板定义字段/执行规则，实例保存具体系统端点和 OAuth App 配置 |
| 集成配置 | `integrations` | `auth_instances` | 组合系统、认证实例和 HTTPS API 基础地址 |
| 连接账号 | `connections` | `end_users` | 保存某个用户、租户或服务账号的加密凭据 |
| API 操作 | `actions` | `operation_runs`、`idempotency_records` | 定义和执行受控的相对路径 HTTP 操作 |
| 同步任务 | `sync_tasks` | `sync_task_states`、`sync_records`、`jobs` | 手动或固定周期拉取、检查点、重试和记录存储 |
| Webhook | `webhook_endpoints`、`webhook_sources` | `webhook_ingress_events`、`outbox_events`、`webhook_deliveries` | 入站验签去重和出站签名投递 |
| 运行中心 | `operation_runs` | `operation_events`、`jobs` | 统一查看 Action、Sync、Webhook 运行事实 |
| 访问控制 | `runtime_tokens` | `runtime_token_action_rules`、`runtime_token_connection_grants` | 运行 API 的操作 allow/deny 和连接白名单 |
| 审计日志 | `audit_logs` | 无 | 控制面变更证据和 CSV 导出 |
| 平台设置 | `platform_settings` | 无 | 平台名称、OAuth 公开地址、运行与审计保留天数 |
| 团队管理 | `users`、`workspace_members` | `workspace_invitations` | 成员目录、固定角色和一次性邀请令牌 |

完整可执行 DDL 见 [postgresql-schema-v1.sql](postgresql-schema-v1.sql)。DDL 不使用外键和 `CHECK`，跨表归属与枚举状态由事务和 HTTP 写接口校验。

## 3. 领域关系

```text
Workspace
├── Team / Settings / Runtime Tokens / Audit
├── System Groups
│   └── Systems
│       ├── allowed Auth Templates
│       └── Actions
├── Auth Templates
│   └── Auth Instances (一个实例只属于一个 System)
├── Integrations (System + Auth Instance + Base URL)
│   ├── Connections (End User + encrypted credentials)
│   ├── Sync Tasks
│   └── Webhook Sources
└── Operations / Outbox / Webhook Deliveries / Jobs
```

必须保持 `System != Integration != Connection`：

- System 是系统目录项，例如 GitHub 或企业 ERP。
- Integration 是该系统在当前工作区的一套可调用配置，可以有不同区域或基础地址。
- Connection 是某个 Integration 下的一份最终用户或服务账号凭据。

前端没有环境切换。所有业务查询由服务端固定到 `APIHUB_WORKSPACE_ID`，客户端不能通过 Header 改写 workspace。

## 4. 认证三层模型

### 认证模板

只定义连接需要收集的字段、Token 请求格式和一个或多个 Header/Query/Cookie 注入规则，不保存实际系统地址和秘密。自定义模板支持：

- `static`：静态凭据直接注入；
- `password_token`：JSON 请求换取 Token；
- `client_credentials`：标准表单请求，自动加入 `grant_type=client_credentials`。

### 认证实例

属于一个具体 System，保存 Token/刷新地址、Token 与有效期 JSON 路径、OAuth 授权地址、Scope、客户端 ID/密钥等系统级配置。OAuth 密钥加密保存，编辑时留空会保留旧密文。

### 连接账号

属于一个 Integration 和 End User，保存用户名密码、API Key、refresh token 等账号级凭据。新建后状态为 `pending`，只有真实解析/换取凭据成功后才进入 `active`；失败进入 `error`。

当前可直接执行的流程是 `none`、`static`、`password_token`、`client_credentials`、`oauth2_code` 和受控 `gateway` Token。OAuth1、mTLS、OIDC、JWT 签名、SAML 和请求签名只保留配置模型，运行时失败关闭，必须增加经过审核的 Go 扩展或接入企业认证网关。

## 5. 关键执行链

### Action

```text
Runtime Token -> Redis 限流 -> 操作与连接策略
-> Action -> ready Integration -> active Connection
-> 解密/刷新认证 -> JSON Schema 输入校验
-> SSRF Guarded HTTP Client -> Operation + Event + 幂等结果
```

运行时 Token 没有 allow 规则时默认拒绝全部操作；deny 始终优先。列表、详情和执行接口使用同一策略。

### 用户名密码换 Token

```text
POST /api/connections/{id}/verify
-> 解密 username/password
-> 按模板发起 JSON 或 form Token 请求
-> 按实例 tokenPath / expiryPath 提取结果
-> revision CAS 重新加密写入 Connection
-> 标记 active 或 error
```

### OAuth 2.0

```text
POST /api/oauth/start
-> Redis 保存 10 分钟 state + PKCE verifier
-> 浏览器跳转提供商
-> GET /oauth/callback/{systemKey}
-> 原子消费 state -> code 换 Token -> 加密创建 Connection
```

平台设置中的公开基础地址立即参与后续 OAuth 回调地址计算；部署环境变量只作为初始化和回退值。

### Sync

```text
scheduler 扫描 deployed 任务 -> PostgreSQL job + queued operation
-> worker lease -> 调用同一个 Action executor
-> upsert records -> 成功后提交 checkpoint -> outbox
```

v1 只支持手动、每 15 分钟、每 30 分钟、每小时和每天，不宣称支持任意 cron。

### Webhook

```text
入站：sourceKey -> 2 MiB JSON 限制 -> HMAC-SHA256 -> event ID 去重 -> operation + outbox
出站：outbox -> 订阅匹配 -> delivery job -> HMAC-SHA256 -> 主地址 -> 重试时备用地址 -> delivered/dead
```

跨主机 HTTP 重定向会被拒绝，避免把自定义认证 Header 或 Cookie 带到其他域名。

## 6. PostgreSQL 与 Redis 边界

PostgreSQL 保存全部不可丢失事实：系统、认证配置、凭据密文、连接、运行、同步检查点、records、jobs、Webhook outbox/delivery、访问策略、团队、设置和审计。

Redis 只使用以下 Key：

| Key | TTL | 用途 | 故障行为 |
| --- | ---: | --- | --- |
| `apihub:oauth:state:{hash}` | 10 分钟 | OAuth state 与 PKCE | 新 OAuth 授权返回 503 |
| `apihub:auth:refresh-lock:{connection}` | 30 秒 | Token 刷新 single-flight | 需要刷新的调用失败关闭 |
| `apihub:rate:{scope}` | 1 至 2 分钟 | 管理/运行 API 滑动窗口 | 管理控制面降级；运行 API 返回 503 |

Redis 不可用时 Go 服务仍启动，管理页面和 PostgreSQL 查询可用，但 `/health/ready` 返回 503。凭据、任务和 Webhook 不会只写 Redis。

## 7. 进程与代码结构

```text
cmd/
  apihub/                 # API、worker、scheduler 进程入口
  apihub-init/            # 显式迁移和幂等初始化
internal/
  config/                 # 环境变量及私网 CIDR 白名单
  httpapi/                # Chi 路由、管理 API、运行 API、OAuth、Webhook
  authn/                  # 密文解析、Token 交换/刷新和注入规则
  executor/               # Action runner 与 SSRF guarded HTTP client
  store/                  # pgx 查询、事务、迁移、queue/outbox
  background/             # worker、scheduler、到期数据清理
  rediscache/             # OAuth state、限流、刷新锁
  policy/                 # 运行 Token 操作/连接策略
  secret/                 # AES-GCM
  webui/                  # 嵌入式前端
web/                      # React 管理台源码
```

`APIHUB_ROLE=all|api|worker|scheduler` 控制同一二进制的进程角色。单机用 `all`，扩容时用相同镜像拆分角色。

## 8. API 边界

- `/api/auth/login`：账号密码登录并创建 PostgreSQL 会话。
- `/api/*`：管理控制面，使用 HttpOnly 会话 Cookie。
- `/v1/providers`、`/v1/actions`、`POST /v1/actions/{actionKey}`：运行面，运行时 Bearer Token。
- `/oauth/callback/{systemKey}`：OAuth 提供商公开回调。
- `/webhooks/inbound/{sourceKey}`：企业/公开系统事件入口。
- `/health/live` 与 `/health/ready`：存活和 PostgreSQL/Redis 就绪检查。
- `/v1/proxy/*`：固定返回 `410 Gone`，任意 Provider Proxy 不属于当前产品边界。

## 9. 安全与部署

- Integration、Token、Webhook 地址必须为 HTTPS；本地回调只允许 localhost/loopback HTTP。
- 默认阻断私网、环回、链路本地、保留和组播地址；企业内网只通过 `APIHUB_ALLOWED_PRIVATE_CIDRS` 精确放行。
- 动作只能保存安全相对路径，不能覆盖目标主机。
- 自定义认证不执行脚本，只允许最多 16 条受控注入规则；阻止 Host、Content-Length 等危险 Header。
- 控制面使用账号会话，运行面使用独立 Token；日志不记录 Cookie、Authorization、密码或凭据正文。
- 管理限流与审计默认使用 TCP 对端 IP，不直接信任可由客户端伪造的转发头；反向代理部署应另行配置可信代理边界。
- scheduler 每小时清理按平台设置到期的运行、审计、幂等和入站 Webhook 数据。

生产前仍必须完成独立最小权限数据库账号、PostgreSQL TLS、Redis 网络白名单/TLS、备份恢复演练、真实提供商 OAuth/Token/Webhook 验收和容量测试。聊天中出现过的数据库与 Redis 密码应在正式上线前轮换。

## 10. 初始化

`cmd/apihub-init` 执行嵌入式 Schema v1 并幂等创建默认 workspace、owner、平台设置、13 个内置认证模板和目录系统/操作。不存在 SQLite 导入命令。
