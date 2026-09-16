# Nango 与 OpenConnector 合并分析

## 结论

两个系统可以合并，但合并点是领域职责，不是照搬全部菜单：

- OpenConnector 提供 System/Action Catalog、JSON Schema 和 Agent 友好的操作发现语义。
- Nango 提供 Integration、Connection、OAuth、Sync、Webhook 和运行治理语义。
- 新项目以 Go + Chi 为 HTTP 内核，以 PostgreSQL 为唯一事实库，以 Redis 承担短时协调。

项目不尝试完整兼容两个上游产品，也不保留环境切换、任意 Proxy、用户代码函数、智能体、MCP、计费或 Marketplace。首版围绕“配置系统后可安全调用 API、同步数据和收发 Webhook”闭环。

## 合并后的核心模型

```text
System Catalog
├── System Group
├── allowed Auth Templates
└── Actions + JSON Schema

Workspace
├── Auth Instances (System 级端点和应用配置)
├── Integrations (System + Auth Instance + Base URL)
│   └── Connections (End User + encrypted credentials)
├── Sync Tasks / Records / Checkpoints
├── Webhook Sources / Endpoints / Deliveries
├── Runtime Tokens / Policy
└── Operations / Audit / Settings / Team
```

最重要的边界是：

- System 是“接什么系统”。
- Auth Template 是“认证规则长什么样”。
- Auth Instance 是“这家企业、这个系统具体使用哪个认证端点和 App 配置”。
- Integration 是“该系统从哪个 API 基础地址调用，并使用哪个认证实例”。
- Connection 是“哪个用户、租户或服务账号的真实凭据”。
- Action 是“允许调用哪个相对路径、方法和输入结构”。

## UI 流程

```text
1. 系统目录：选择内置系统，或添加企业系统并分组
2. 认证中心：选择/创建模板，再为具体系统创建认证实例
3. 集成配置：组合系统、认证实例和 HTTPS Base URL
4. 连接账号：录入账号凭据，后端验证成功才标记 active
5. API 操作：配置相对路径和 JSON Schema，使用连接做真实验证
6. 可选：把操作配置成同步任务，或配置入站/出站 Webhook
7. 运行治理：查看运行、指标、访问令牌和审计证据
```

网络代理不是必经步骤，也没有单独菜单。普通公网和企业 API 直接由受控 HTTP executor 调用；只有目标位于私网时，运维才需要配置精确 CIDR 白名单或部署侧网络连通。

## 能力取舍

| 领域 | v1 选择 | 未纳入原因 |
| --- | --- | --- |
| HTTP 框架 | `net/http + chi/v5` | 保留标准 Context、OAuth callback、Webhook 和流式边界 |
| 数据库 | PostgreSQL + pgx | 事务、JSONB、SKIP LOCKED、可查询运行事实 |
| 缓存/协调 | Redis | 只做 OAuth state、限流、刷新锁，不承担业务事实 |
| 后台任务 | PostgreSQL jobs/outbox | 当前规模无需 Kafka |
| 指标 | PostgreSQL 聚合 | 当前无需 Prometheus/Grafana 全家桶 |
| 自定义认证 | 声明式字段、Token 请求、Header/Query/Cookie 注入 | 不允许在控制台执行任意脚本 |
| 高级认证 | Go 扩展或企业网关 | mTLS、SigV4、OAuth1、SAML 等需要专门审核与测试 |
| 同步 | 固定周期 + checkpoint + record upsert | 任意 cron 和用户函数运行时后置 |
| Proxy/MCP/智能体 | 不提供 | 不是当前页面主流程，避免扩大攻击面和运维成本 |

## 已实现边界

- PostgreSQL Schema v1、嵌入式迁移和幂等初始化。
- 系统分组、系统、认证模板/实例、集成、连接和操作管理 API。
- AES-GCM 凭据、OAuth2 Authorization Code + PKCE、用户名密码换 Token、客户端凭证、Basic、静态多规则注入。
- Action 执行、输入 Schema、运行 Token allow/deny、连接白名单、幂等记录和 SSRF 防护。
- 同步 scheduler/worker、租约重领、records/checkpoint、失败重试。
- 入站 HMAC/去重、PostgreSQL outbox、出站签名、备用地址和人工重投。
- 运行中心、PostgreSQL 指标聚合、审计、数据保留、平台设置和团队目录。
- 中文 React 管理台全部改为真实 `/api` 数据；未实现的能力不会显示成可用按钮。

## 明确未完成的产品能力

- 邀请接受流程和更细粒度的角色强制执行；当前控制台已使用账号密码和服务端会话。
- OAuth1、mTLS、OIDC/SSO、JWT 服务账号签名、SAML/Token Exchange、HMAC/SigV4 原生执行器。
- OpenAPI 文件生成、SDK、CLI、MCP 和任意 Provider Proxy。
- 真实第三方/企业系统的生产级 provider contract 验收。

这些能力的配置模型可以保留，但 UI 必须标为“需 Go 扩展或企业网关”，运行时必须失败关闭。

## 完成与上线标准

代码编译、单元测试和本地页面可点击只证明工程闭环。生产上线还需要：

- PostgreSQL 最小权限账号、TLS、备份恢复和故障演练；
- Redis 网络连通、认证、TLS/白名单和故障行为验证；
- 每个真实目标系统的登录、刷新、401、限流、分页、Webhook 重放和超时验收；
- 多进程 worker/scheduler 租约竞争和容量测试；
- 密钥轮换、日志脱敏和私网 CIDR 最小授权复核。
