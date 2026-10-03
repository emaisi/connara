# Connara 原生认证扩展开发方案

> 状态：A 至 E 批次已在本地实现；真实服务商凭据与部署验收待执行。基于 2026-10-02 工作区代码编写。
>
> 范围：Connara 作为客户端连接上游 API 时使用的认证。Connara 管理台的企业单点登录是独立需求，不包含在本文的 OIDC 流程内。
>
> 目标：补齐目前常见且有明确标准或服务商契约的认证能力；每项只有通过配置、连接和真实请求三层验收后，才在页面显示“可直接使用”。

## 实施记录（2026-10-02）

- 后端新增 `mtls`、`aws_sigv4`、`jwt_direct`、`jwt_bearer_grant`、`oidc`、`token_exchange` 流程；OAuth 令牌请求支持 `client_secret_post`、`client_secret_basic`、`private_key_jwt`、`tls_client_auth`。签名在最终 HTTP 请求组装后执行，mTLS 按请求派生受控客户端。
- OIDC 授权请求绑定一次性 nonce；回调使用固定 issuer 的发现文档和 JWKS 验证 ID Token 后保存连接。Token Exchange 仅处理已经持有的 subject token，不生成或验证 SAML 断言。
- 数据库 v5 迁移为既有工作区增加明确的新内置模板；含糊的旧 `saml-token-exchange` 模板继续不可执行。`/api/meta` 公布可执行流程，管理台据此显示能力；复杂流程在认证中心配置，在接入向导中复用。
- 已增加 JWT、OAuth 客户端认证、SigV4、mTLS 握手及证书隔离、令牌交换、OIDC 正反例与最终请求签名测试。真实 AWS、企业 IdP、证书颁发机构和目标 API 的端到端验收仍须使用各自测试环境完成，不能由本地测试推断通过。
- 认证中心、系统设置和接入向导不再提供 OAuth 1.0a、通用 HMAC、旧 JWT、SAML 交换、Kerberos/NTLM 五个未完成的内置模板作为新配置选项；既有模板与实例记录保留用于兼容和迁移，不删除数据库数据。AWS SigV4、两种明确的 JWT 方式及标准 Token Exchange 继续显示。

### 配置速查

| 模板 | 认证实例中的非秘密配置 | 连接凭据或实例密文 |
| --- | --- | --- |
| mTLS | 可选 `caCertificate` | 连接的 `certificate`、`private_key` |
| AWS SigV4 | `region`、`service` | 连接的 `access_key_id`、`secret_access_key`，可选 `session_token`、`expires_at` |
| JWT 直接签发 | `issuer`、`audience`，可选 `subject`、`keyId` | 连接的 `private_key`，只接受 RSA 2048 位以上或 P-256 私钥 |
| JWT Bearer grant | `tokenUrl`、`issuer`，可选 `audience`、`scope`、`subject`、`keyId` | 连接的 `private_key` |
| 上游 OIDC | HTTPS `issuer`、发现文档匹配的 `authorizationUrl` 与 `tokenUrl` | 实例的 `clientId` 与所选客户端认证方式对应的密钥或证书 |
| Token Exchange | `tokenUrl`、`subjectTokenType`，可选 `requestedTokenType`、`audience`、`resource`、`scope` | 连接的 `subject_token`；如端点要求客户端认证，另配置客户端 ID 与对应密文 |

OAuth 客户端认证由 `clientAuthMethod` 选择；空值沿用 `client_secret_post`。`private_key_jwt` 使用实例密文 `oauth_private_key`，`tls_client_auth` 或 `tlsForToken` 使用 `certificate` 和 `tls_private_key`。`tlsForApi` 可给其他流程的 API 请求附加客户端证书。实例 `/test` 只报告配置检查；连接验证配置了 `verificationPath` 并获得上游 2xx 后才标记为已验证。令牌端点未返回有效期时，JWT Bearer grant 与 Token Exchange 会在下一次请求前重新交换。

## 1. 开发前实现与关键约束

| 位置 | 当前行为 | 对扩展的影响 |
| --- | --- | --- |
| `internal/store/bootstrap.go` | 内置模板包含 13 种展示项；工作区的 `Bootstrap` 完成一次后直接返回 | 新模板必须考虑既有工作区的数据迁移，修改初始化数组本身不足以升级已安装环境 |
| `internal/authn/service.go` | `SupportsFlow` 仅允许 `none`、`static`、`password_token`、`client_credentials`、`oauth2_code`、`gateway`；`ResolveConnection` 产出固定 Header、Query、Cookie | JWT、OIDC、mTLS、请求签名和标准 Token Exchange 尚无执行逻辑；`gateway` 仅可注入已有令牌 |
| `internal/executor/executor.go` | 最终方法、URL、查询参数、正文和认证字段在 `Action` 中组装 | SigV4/HMAC 必须在组装完成之后对**最终请求**签名，不能预先算固定 Header |
| `internal/executor/guarded_client.go` | 共用的受控 HTTP 客户端限制目标地址和跨主机跳转 | 证书和签名扩展必须沿用出站限制，且不能让连接 A 的证书进入连接 B 的请求 |
| `internal/httpapi/oauth.go` | OAuth2 授权码使用 state、PKCE、回调换令牌 | OIDC 可复用主流程，但须增加 nonce、发现文档及 ID Token 验证 |
| `internal/httpapi/admin.go` | ready 状态按 `SupportsFlow` 判断；认证实例 `/test` 多数情况下只做结构校验 | 新流程应有明确的“配置有效”和“上游已验证”两种结果，避免把结构校验表述为认证成功 |
| `web/src/pages/core-shared.tsx` | 认证目录用静态 `executable` 布尔值显示能力 | 页面能力必须与后端一致，不能只改一个布尔值；目前 Kerberos/NTLM 卡片与后端 `gateway` 令牌注入的粒度不同 |

开发前可直接执行的六类方式为：无认证、API Key/Bearer、HTTP Basic、用户名密码换 Token、OAuth2 客户端凭证、OAuth2 授权码。`gateway` 仅覆盖受控网关令牌注入，不代表 Connara 会进行 Kerberos/NTLM 协商。自定义模板仍只允许 `static`、`password_token`、`client_credentials` 三种执行流程。对不支持的流程继续失败关闭。

## 2. 能力范围与交付边界

“HMAC / AWS SigV4”“JWT 服务账号”“SAML / Token Exchange”各包含不同协议。交付项按真实执行契约拆分，避免某个子项完成后整张卡片错误地变成可用。

| 优先级 | 能力 | 本方案的完成定义 | 原目录映射 |
| --- | --- | --- | --- |
| P0 | OAuth2 客户端认证补齐 | 令牌端点可按实例选择 `client_secret_basic`、`client_secret_post`、`private_key_jwt`；同一请求只使用一种方式；授权码交换、刷新和客户端凭证共用配置 | 增强现有 OAuth2，不新增误导性的独立卡片 |
| P0 | JWT 服务账号 | 分别支持短期自签 JWT 直接作为 Bearer、RFC 7523 JWT Bearer grant 换访问令牌；`private_key_jwt` 属于 OAuth2 客户端认证，不与前两项混用 | 拆分“JWT 服务账号” |
| P0 | mTLS | 按连接或实例绑定客户端证书与私钥；上游 API 请求与 OAuth 令牌端点可分别启用；可验证证书、密钥、用途和有效期 | “mTLS 双向证书” |
| P0 | AWS SigV4 | 使用明确的区域、服务名和 AWS 凭据对最终 HTTP 请求逐次签名；支持临时凭据的 session token，过期后不能继续使用 | 从“HMAC / AWS SigV4”拆出 |
| P1 | 上游 OIDC 员工授权 | 在 OAuth2 授权码基础上发现并固定 issuer，生成与校验 nonce，验证 ID Token 签名及必要声明，再保存 API 访问令牌 | “OIDC / 企业 SSO”；仅针对连接上游 API |
| P1 | OAuth2 Token Exchange | 按 RFC 8693 用连接中受控的 subject token 换取访问令牌，支持 audience/resource、scope、过期与重取 | 从“SAML / Token Exchange”拆出 |
| 按目标服务 | 服务商 HMAC、OAuth 1.0a、SAML 断言、Kerberos/NTLM | 先取得目标服务的协议文档和测试环境，再做对应 Go 适配与合同测试；“支持 HMAC”不能代表任意 HMAC 规则都能运行 | 旧卡片从新建入口隐藏；具体变体完成后再单独展示 |

DPoP 和 HTTP Message Signatures 是可考虑的后续能力，不因已有卡片而自动纳入本轮。它们需要服务端明确支持以及专门的密钥、重放和签名语义；不能按协议名称推断每个目标系统都接受。

## 3. 统一认证执行契约

### 3.1 执行阶段

以现有 `authn.Service` 和 `executor.Executor` 为边界，增加受控的认证计划，而不是让模板运行脚本。计划至少包含：

1. **取得凭据**：查连接和认证实例，解密对应密文，检查状态、模板版本和配置完整性；需要令牌时在刷新锁下交换或刷新。
2. **组装请求**：按已注册的 Action 生成最终方法、目标 URL、查询参数和正文，应用静态 Header/Query/Cookie 注入。
3. **绑定传输**：若流程需要 mTLS，从受控客户端的传输配置派生该连接的证书配置；保留 DNS/IP 限制、超时和重定向限制。
4. **签名请求**：在全部请求字段确定后执行 SigV4 或其他已注册的 Go 签名器；签名器只接收当前连接的凭据和当前请求，不接受控制台上传的代码。
5. **发送与记录**：请求经受控客户端发出；运行日志、错误和审计不能含私钥、令牌、断言、证书正文或签名明文。

`executor.RequestAuth` 可以扩展为“静态字段 + 请求签名器 + 传输配置”的内部类型。签名器应是 Go 代码中注册的有限实现，且签名失败时不得发送请求。对于签名请求，重定向需在重新签名有明确实现前拒绝；不能把原签名或认证字段原样带到重定向目标。

同一执行契约要覆盖运行 API、Action 测试、连接验证、同步任务及工作流步骤。目前这些入口分别调用执行器；只改一个入口会造成同一连接在别处不能使用。签名和证书必须随每次调用解析，不能把可变密钥或证书放进全局共享 `http.Client`。

### 3.2 配置与密文归属

| 位置 | 存放内容 | 原则 |
| --- | --- | --- |
| `auth_templates` | 流程类型、字段结构、固定注入规则 | 内置模板的执行能力由后端代码声明；自定义模板仍不能提供任意 Go/JS 代码 |
| `auth_instances.public_config` | issuer、audience、scope、区域、服务名、签名变体、令牌 URL 等非秘密配置 | 保存时严格校验组合、HTTPS 地址和允许值；未识别的安全关键字段不得悄悄忽略 |
| `auth_instances.secret_blob` | 同一认证应用共用的客户端私钥、密钥或证书引用 | 沿用 AES-GCM、密钥版本和轮换流程；编辑时留空保留旧值的语义需明确 |
| `connections.credential_blob` | 账号级私钥、证书、access key、subject token、刷新令牌等 | 按连接隔离、加密保存；不通过列表 API 返回明文 |

私钥和证书实际属于实例还是账号，按目标服务的所有权确定，不能复制两份。初版可使用现有 JSON 配置与密文字段，但实现前要定义每个内置流程的有版本配置结构；如需查询、约束或轮换字段，再增量加列和迁移。旧实例不得因默认值改变而自动获得新的认证行为。

### 3.3 能力状态与升级

- 用同一后端能力表驱动 `SupportsFlow`、ready 状态校验、`/api/auth-instances/{id}/test` 和管理台显示。能力表要区分 `jwt_direct`、`jwt_bearer_grant`、`aws_sigv4` 等具体变体，不能只记录“JWT=true”。
- `/api/meta` 可扩展只读的能力摘要，前端据此显示“可配置”“需要连接验证”“可运行”；旧前端面对未知流程时继续显示不可运行。
- 已有模板 key 和草稿实例保持兼容。对于含糊的旧 `jwt`、`hmac`、`saml-token-exchange` key，新增明确的新 key；旧实例由管理员显式迁移，不能在升级时静默更换语义。
- `Bootstrap` 已由 `bootstrap_state` 限制为首次执行。新增内置模板或兼容关系时，要通过下一可用版本的数据库迁移或幂等的专用升级步骤处理既有工作区；迁移编号需和当前未合并迁移、工作流计划协调。
- 认证实例 `/test` 的配置检查结果继续叫“配置有效”。只有用真实连接凭据完成上游验证请求，才显示“上游已验证”；没有验证路径时不得声称已通过上游认证。

## 4. 各协议的实现任务

### 4.1 OAuth2 客户端认证与 JWT

1. 抽取令牌请求的客户端认证策略，使授权码交换、刷新、客户端凭证与 Token Exchange 可复用。补 `client_secret_basic`；保留已有 `client_secret_post` 配置的兼容行为；支持 `private_key_jwt`，并拒绝多种方式同时发送。
2. 私钥签名只允许明确算法与受控声明。初版以目标服务文档选定的 RS256/ES256 为范围；验证 PEM 密钥类型、`iss`/`sub`/`aud`、`iat`/`exp`、`kid`，使用短有效期。不可接受来自用户配置的任意 `alg` 或无限期令牌。
3. JWT 直接调用与 JWT Bearer grant 使用两个独立流程。grant 产生的访问令牌按现有加密、过期、刷新锁和连接修订保存；直接调用模式则按有效期重新签发，不把过期 JWT 长期当静态密钥保存。
4. 补齐现有 OAuth 授权参数白名单，禁止自定义参数覆盖 `response_type`、state、PKCE challenge、redirect URI 等协议关键值。

验收样例：GitHub App 短期 JWT、Google 服务账号 JWT grant、支持 `private_key_jwt` 的 OAuth2 令牌端点各使用隔离的测试凭据。实现前以服务商当前文档确定各自额外声明和请求字段，不能把标准 JWT 签发等同于已通过所有服务商验收。

### 4.2 mTLS

1. 校验客户端证书链与私钥匹配、有效期、密钥用途；CA、服务器名称及目标主机配置必须受限，不提供关闭服务端证书验证的选项。
2. 从当前受控 HTTP transport 派生按连接隔离的 TLS transport；缓存如有必要，键至少包含工作区、连接、凭据修订和证书指纹，轮换后关闭旧空闲连接。
3. 对 API 请求和 OAuth 令牌请求分别声明是否使用证书；支持二者都需要证书的场景。若要支持证书绑定访问令牌，还需按 RFC 8705 检查其专用配置，不把普通 mTLS 误标为令牌绑定。

验收样例：要求客户端证书的测试服务可成功握手；无证书、错证书、过期证书及连接间证书串用均失败；原有内网地址限制仍生效。

### 4.3 AWS SigV4 与服务商 HMAC

1. SigV4 使用 AWS SDK for Go v2 的官方签名器或等价且经官方测试向量验证的实现。配置显式区域、服务名、access key、secret key、可选 session token 和凭据到期时间。
2. 签名覆盖最终 URL、查询参数、请求头及正文哈希；每次请求重新生成时间戳和签名。时间漂移或凭据过期返回可识别错误，不发送旧签名。
3. “通用 HMAC”不开放任意表达式或代码执行。取得目标服务的规范化规则后，为该服务增加受控签名 profile、字段校验和固定测试向量；单个 profile 验收成功才显示相应能力。

验收样例：AWS 官方签名向量、本地验签服务、GET/POST 与含查询参数和正文请求；修改任意已签字段后验签失败；同步任务多页每页生成不同签名。

### 4.4 上游 OIDC

1. 明确模式为“员工授权连接上游 API”，复用授权码 + PKCE；在授权请求加入 `openid` scope 和随机 nonce，并把 nonce 绑定到一次性 OAuth state。
2. 按固定 issuer 读取 discovery 和 JWKS；限制出站目标并缓存公钥。回调校验 ID Token 的签名、算法、issuer、audience、有效期、`azp`（适用时）与 nonce。失败时不得创建连接。
3. ID Token 用于确认用户身份，调用 API 仍用 access token；刷新后继续遵守目标服务的令牌语义。若只需要访问 API 且服务商没有要求 ID Token，应使用现有 OAuth2 授权码方式。

验收样例：有效授权、错误 issuer/audience/nonce、过期 ID Token、公钥轮换和重放回调；分别验证身份与 API 调用，不以令牌端点返回 200 作为最终成功。

### 4.5 OAuth2 Token Exchange

1. 按 RFC 8693 构造 `grant_type`、`subject_token`、`subject_token_type`，并按实例配置 audience/resource、scope 和所请求的令牌类型；subject token 来自该连接的密文凭据或已定义的受控来源，不自动使用 Connara 运行时令牌。
2. 复用 4.1 的客户端认证方式；解析令牌、过期时间和错误，利用现有刷新锁避免并发交换。对不能续取的断言明确提示重新授权。
3. SAML 断言的生成、签名和验证不属于 RFC 8693 本身。有具体企业 IdP/STS 契约时，另加 SAML profile 和相应证书、受众、时效及防重放验证。

验收样例：标准令牌交换成功、错误 audience/subject token、过期断言、并发刷新只执行一次交换；带 SAML 的测试只有对应 profile 落地后才纳入。

### 4.6 按目标服务补充的协议

- **OAuth 1.0a**：需要请求令牌、用户授权、访问令牌和每次 API 请求签名；按 RFC 5849 与目标服务实际参数测试。未指定仍使用它的目标服务时，保留“需适配”。
- **Kerberos/NTLM**：需要确定域、SPN、委托方式、连接作用域和服务环境；仅持有 `gateway_token` 并注入 `Negotiate` Header 不等于实现协议协商。真实 AD/内网测试环境具备后再写 Go 适配。
- **SAML**：先确定是 Connara 管理台登录、上游员工授权，还是 SAML 断言换 API 令牌；三者入口与信任边界不同，不能合并成一个流程。

## 5. 开发批次与代码清单

| 批次 | 交付 | 主要改动位置 | 完成门槛 |
| --- | --- | --- | --- |
| A：执行基础 | 内部能力声明、最终请求签名点、连接级 TLS 配置、统一的调用入口 | `internal/authn`、`internal/executor`、`internal/httpapi/runtime.go`、`internal/background`、`internal/workflow` | 现有六类认证行为不变；未支持流程继续失败关闭；同步和工作流与 Action 得到相同认证结果 |
| B：令牌与 JWT | OAuth2 客户端认证选择、JWT 直接调用、JWT Bearer grant | `internal/authn`、`internal/httpapi/oauth.go`、认证实例校验 | 标准测试向量、换令牌/刷新/轮换和失败路径通过 |
| C：传输与签名 | mTLS、AWS SigV4 | 受控客户端、执行器、凭据解析 | 证书隔离、签名向量、最终请求和内网限制测试通过 |
| D：员工授权与交换 | 上游 OIDC、RFC 8693 Token Exchange | OAuth state/回调、JWKS 缓存、令牌生命周期 | 错误 issuer/nonce/audience、过期与重放均拒绝 |
| E：产品化 | 新模板与既有工作区迁移、表单、能力提示、文档 | `internal/store`、`internal/httpapi/admin.go`、`web/src/pages`、`web/src/api.ts`、README | 页面和后端能力一致；旧实例无静默语义变化；新流程可从页面完成配置和验证 |
| F：服务商适配 | OAuth1、特定 HMAC、SAML、Kerberos/NTLM | 按具体系统增加 Go profile | 每个目标服务有真实契约、测试凭据、正反例和单独可用状态 |

每个批次都应能独立回归，不在批次 A 直接打开任何新的模板。新增依赖须在实现该协议时选择并锁定版本；mTLS、证书解析、基本 HMAC 与部分 JWT 签名可使用 Go 标准库，SigV4 可优先评估 AWS SDK for Go v2。安装包只是实现的依赖，不是启用认证能力的步骤。

## 6. 验收、升级与发布

**代码验收**：`gofmt`、`git diff --check`、`./scripts/check.sh`；以本地 TLS/令牌/验签测试服务验证每个流程的正常路径和拒绝路径。测试必须覆盖 Action 调用、连接验证、同步任务及工作流；若某入口尚未接线，后端能力与页面状态应继续标为未完成。加密字段的保存、读取、修订、轮换及日志脱敏同样要通过测试。

**数据库验收**：在空库和已有工作区两种情形运行升级；内置模板和兼容关系幂等写入，不覆盖自定义模板、现有凭据或草稿配置；迁移前后检查旧连接可执行。当前工作区已有尚未合并的迁移与工作流计划，实际编号以合并顺序为准。

**产品验收**：页面应能创建目标系统、认证实例、集成和连接，明确显示“配置有效”或“上游已验证”；能用真实凭据调用目标 API。AWS、Google、GitHub、企业 IdP 等的真实服务验收需要各自的测试账号、证书或密钥，不能由本地模拟测试代替。

**发布顺序**：先部署兼容旧数据的后端与迁移，确认旧连接正常，再开放前端新能力；新增模板按流程逐项开启。回滚时保留新密文与模板数据，旧版本遇到未知 flow 仍失败关闭；若证书、签名或令牌字段结构已变更，先评估旧版本是否能读取旧连接，再执行应用回滚。

## 7. 依据

- [OAuth 2.0 客户端认证，RFC 6749](https://www.rfc-editor.org/rfc/rfc6749.html)
- [OAuth 2.0 安全最佳实践，RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html)
- [JWT Bearer grant 与客户端认证，RFC 7523](https://www.rfc-editor.org/rfc/rfc7523.html)
- [OAuth 2.0 mTLS，RFC 8705](https://www.rfc-editor.org/rfc/rfc8705.html)
- [OAuth 2.0 Token Exchange，RFC 8693](https://www.rfc-editor.org/rfc/rfc8693.html)
- [OpenID Connect Core](https://openid.net/specs/openid-connect-core-1_0.html) 与 [Discovery](https://openid.net/specs/openid-connect-discovery-1_0.html)
- [AWS SigV4 签名流程](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_sigv-create-signed-request.html) 与 [Go v2 签名器](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/aws/signer/v4)
- [OAuth 1.0，RFC 5849](https://www.rfc-editor.org/rfc/rfc5849.html)
