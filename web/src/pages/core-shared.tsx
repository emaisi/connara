import { Database, Plus, Search } from "lucide-react";
import { type ReactNode } from "react";
import { Link } from "react-router";
import { type DemoAuthInstance, type DemoAuthScheme, type DemoSystem } from "../demo";
import { Card, Field, cn, fieldClass } from "../ui";

export function authInstancesFor(
  system: DemoSystem | undefined,
  instances: DemoAuthInstance[],
  schemes: DemoAuthScheme[] = [],
  executableOnly = false,
  supportedFlows?: string[],
) {
  if (!system) return [];
  return instances.filter(
    (instance) =>
      instance.status === "ready" &&
      instance.systemIds.includes(system.service) &&
      system.authTemplateIds.includes(instance.templateId) &&
      (!executableOnly || isExecutableAuthTemplate(instance.templateId, schemes, supportedFlows)),
  );
}

const builtinFlows: Record<string, string> = {
  oauth2: "oauth2_code",
  oauth1: "oauth1",
  api_key: "static",
  basic: "static",
  hmac: "request_signing",
  no_auth: "none",
  "username-password-token": "password_token",
  "client-credentials": "client_credentials",
  mtls: "mtls",
  oidc: "oidc",
  jwt: "jwt",
  "jwt-direct": "jwt_direct",
  "jwt-bearer-grant": "jwt_bearer_grant",
  "aws-sigv4": "aws_sigv4",
  "oauth2-token-exchange": "token_exchange",
  "saml-token-exchange": "saml_exchange",
  kerberos: "kerberos",
};

export function isExecutableAuthTemplate(templateId: string, schemes: DemoAuthScheme[], supportedFlows?: string[]) {
  return (
    schemes.some((scheme) => scheme.id === templateId && scheme.status === "published") ||
    authMethodCatalog.some(
      (method) =>
        method.id === templateId &&
        method.executable &&
        (supportedFlows === undefined || supportedFlows.includes(builtinFlows[templateId])),
    )
  );
}

export function connectionStatusTone(status: string): "success" | "warning" | "danger" {
  if (status === "active") return "success";
  if (status === "pending") return "warning";
  return "danger";
}

export interface AuthMethod {
  id: string;
  name: string;
  executable: boolean;
  mode: string;
  summary: string;
  lifecycle: string;
  fields: string[];
}

export const authMethodCatalog: AuthMethod[] = [
  {
    id: "oauth2",
    name: "OAuth 2.0 授权码",
    executable: true,
    mode: "跳转授权",
    summary: "适合 Google、GitHub、Slack 等由最终用户授权的公网平台。",
    lifecycle: "State + PKCE → 回调换取令牌 → 到期前自动刷新",
    fields: ["客户端 ID", "客户端密钥", "授权地址", "令牌地址", "权限范围", "回调地址"],
  },
  {
    id: "oauth1",
    name: "OAuth 1.0a",
    executable: false,
    mode: "签名跳转",
    summary: "兼容仍使用临时令牌和请求签名的旧版公网平台。",
    lifecycle: "请求令牌 → 用户授权 → 访问令牌 → 每次请求签名",
    fields: ["Consumer Key", "Consumer Secret", "请求令牌地址", "授权地址", "访问令牌地址"],
  },
  {
    id: "api_key",
    name: "API 密钥 / Bearer 令牌",
    executable: true,
    mode: "静态凭据",
    summary: "把密钥注入请求头、查询参数或 Cookie，并支持自定义前缀。",
    lifecycle: "录入并校验 → 加密保存 → 请求时按规则注入",
    fields: ["凭据字段", "注入位置", "参数名称", "值前缀", "验证请求"],
  },
  {
    id: "basic",
    name: "Basic 认证",
    executable: true,
    mode: "静态凭据",
    summary: "适合支持 HTTP Basic 的传统系统和内部管理接口。",
    lifecycle: "录入用户名和密码 → 加密保存 → 请求时编码注入",
    fields: ["用户名", "密码", "验证请求"],
  },
  {
    id: "hmac",
    name: "服务商 HMAC（旧模板）",
    executable: false,
    mode: "请求签名",
    summary: "各服务商的 HMAC 规范不同，需按目标系统编写签名适配。AWS SigV4 请选独立模板。",
    lifecycle: "读取密钥 → 规范化请求 → 计算签名 → 注入签名头",
    fields: ["Access Key", "Secret Key", "算法", "区域", "服务名", "签名头"],
  },
  {
    id: "aws-sigv4",
    name: "AWS SigV4",
    executable: true,
    mode: "逐次签名",
    summary: "使用区域、服务名和 AWS 凭据对每次最终请求签名。",
    lifecycle: "组装请求 → 计算正文哈希 → SigV4 签名 → 发出请求",
    fields: ["区域", "服务名", "Access Key ID", "Secret Access Key", "Session Token"],
  },
  {
    id: "no_auth",
    name: "无需认证",
    executable: true,
    mode: "公开接口",
    summary: "只为确实公开且无需凭据的接口建立虚拟连接。",
    lifecycle: "创建虚拟连接 → 直接调用，不保存任何凭据",
    fields: ["验证请求"],
  },
  {
    id: "username-password-token",
    name: "用户名密码换 Token",
    executable: true,
    mode: "两步登录",
    summary: "先用用户名和密码调用登录接口获取 Token，再把 Token 注入后续请求的 Header。",
    lifecycle: "提交用户名密码 → 提取 Token → 加密缓存 → Header 注入 → Token 到期后重新登录",
    fields: ["登录地址", "请求格式", "用户名", "密码", "Token 路径", "Header 名称", "Header 值模板"],
  },
  {
    id: "client-credentials",
    name: "OAuth 2.0 客户端凭证",
    executable: true,
    mode: "服务到服务",
    summary: "适合微服务、ERP 和数据中台之间的机器身份认证。",
    lifecycle: "客户端凭据换取令牌 → 缓存 → 到期前自动续期",
    fields: ["客户端 ID", "客户端密钥", "令牌地址", "Scope / Audience", "客户端认证方式"],
  },
  {
    id: "mtls",
    name: "mTLS 双向证书",
    executable: true,
    mode: "双向 TLS",
    summary: "使用CA 签发的客户端证书和私钥建立双向 TLS。",
    lifecycle: "检查证书与私钥 → TLS 握手 → 证书轮换",
    fields: ["客户端证书", "客户端私钥", "CA 证书链", "服务器名称", "到期提醒"],
  },
  {
    id: "oidc",
    name: "上游 OIDC 员工授权",
    executable: true,
    mode: "员工授权",
    summary: "复用身份提供商，让员工委托访问内部或 SaaS 资源。",
    lifecycle: "发现端点 → OIDC 授权 → 校验 nonce/ID Token → 刷新",
    fields: ["Issuer", "客户端 ID", "客户端密钥", "权限范围", "回调地址"],
  },
  {
    id: "jwt",
    name: "JWT 服务账号（旧模板）",
    executable: false,
    mode: "服务账号",
    summary: "旧模板未定义直接签发或令牌交换的具体契约；请选对应的新模板。",
    lifecycle: "生成短期断言 → 签名 → 直接调用或交换令牌",
    fields: ["Issuer", "Subject", "Audience", "私钥", "算法", "令牌地址"],
  },
  {
    id: "jwt-direct",
    name: "JWT 服务账号 · 直接签发",
    executable: true,
    mode: "服务账号",
    summary: "用私钥签发短期 JWT，直接作为 API Bearer 凭据。",
    lifecycle: "读取私钥 → 签发短期 JWT → 调用 API",
    fields: ["Issuer", "Subject", "Audience", "私钥"],
  },
  {
    id: "jwt-bearer-grant",
    name: "JWT 服务账号 · 换取令牌",
    executable: true,
    mode: "服务到服务",
    summary: "按 JWT Bearer grant 换取访问令牌并自动重取。",
    lifecycle: "签发断言 → 换取令牌 → 缓存 → 到期重取",
    fields: ["Issuer", "Audience", "私钥", "Token 地址"],
  },
  {
    id: "oauth2-token-exchange",
    name: "OAuth 2.0 Token Exchange",
    executable: true,
    mode: "凭据交换",
    summary: "把账号中的 Subject Token 换成目标 API 的访问令牌。",
    lifecycle: "读取 Subject Token → 标准交换 → 缓存 → 到期重取",
    fields: ["Subject Token", "Token 类型", "Audience", "Token 地址"],
  },
  {
    id: "saml-token-exchange",
    name: "SAML / Token Exchange",
    executable: false,
    mode: "凭据交换",
    summary: "通过认证网关把 SAML 断言或内部票据交换成 API 令牌。",
    lifecycle: "接收身份断言 → 受控交换 → 获取短期令牌 → 自动续期",
    fields: ["交换地址", "断言字段", "客户端认证", "令牌路径", "过期时间路径"],
  },
  {
    id: "kerberos",
    name: "Kerberos / NTLM 网关",
    executable: false,
    mode: "受控网关",
    summary: "由部署在内部网络内的网关完成传统域认证，平台只使用短期结果。",
    lifecycle: "连接内部网关 → 域认证 → 获取短期会话 → 调用目标系统",
    fields: ["网关地址", "域", "服务主体", "凭据引用", "会话验证地址"],
  },
];

// Legacy built-ins remain available for naming existing instances, but cannot be selected for new setup.
export const availableAuthMethods = authMethodCatalog.filter((method) => method.executable);

export function authTemplateName(templateId: string, schemes: DemoAuthScheme[]) {
  return (
    authMethodCatalog.find((method) => method.id === templateId)?.name ??
    schemes.find((scheme) => scheme.id === templateId)?.name ??
    templateId
  );
}

export function authInstanceName(instanceId: string, instances: DemoAuthInstance[]) {
  return instances.find((instance) => instance.id === instanceId)?.name ?? instanceId;
}

export interface CustomCredentialField {
  required?: boolean;
  type?: string;
  name: string;
  label: string;
  secret: boolean;
}

export interface CustomInjectionRule {
  target: string;
  name: string;
  template: string;
}

export interface ActionCatalogItem {
  inputSchema?: Record<string, unknown>;
  integrationId?: string;
  backendId?: string;
  id: string;
  systemKey: string;
  provider: string;
  integration: string;
  description: string;
  scopes: string[];
  mode: "local";
  method: string;
  path: string;
  source: "built-in" | "custom";
  input: string;
}

export interface SyncTask {
  syncConfig?: Record<string, unknown>;
  id?: string;
  name: string;
  integration: string;
  actionId?: string;
  connectionId?: string;
  schedule: string;
  checkpoint: string;
  input: string;
  status: "deployed" | "draft" | "paused" | "disabled";
  lastRun: string;
}

export function QuickLink({
  to,
  icon: Icon,
  title,
  text,
}: {
  to: string;
  icon: typeof Plus;
  title: string;
  text: string;
}) {
  return (
    <Link to={to}>
      <Card className="group flex h-full items-start gap-3 p-4 transition hover:-translate-y-0.5 hover:border-blue-300 hover:shadow-md">
        <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-[var(--muted)] text-blue-600">
          <Icon className="size-5" />
        </span>
        <div>
          <p className="text-sm font-bold group-hover:text-blue-600">{title}</p>
          <p className="mt-1 text-xs leading-5 text-[var(--muted-text)]">{text}</p>
        </div>
      </Card>
    </Link>
  );
}

export function isRedirectAuth(auth: string) {
  return auth === "oauth2" || auth === "oidc";
}

export function credentialFields(
  auth: string,
  scheme?: DemoAuthScheme,
): (CustomCredentialField & { required: boolean })[] {
  if (scheme) return scheme.credentialFields.map((field) => ({ ...field, required: field.required !== false }));
  if (isRedirectAuth(auth) || auth === "no_auth") return [];
  if (auth === "basic" || auth === "username-password-token")
    return [
      { name: "username", label: "用户名", secret: false, required: true },
      { name: "password", label: "密码", secret: true, required: true },
    ];
  if (auth === "client-credentials")
    return [
      { name: "client_id", label: "客户端 ID（实例已设置时可留空）", secret: false, required: false },
      { name: "client_secret", label: "客户端密钥（实例已设置时可留空）", secret: true, required: false },
    ];
  if (auth === "jwt" || auth === "jwt-direct" || auth === "jwt-bearer-grant")
    return [{ name: "private_key", label: "服务账号私钥", secret: true, required: true }];
  if (auth === "mtls")
    return [
      { name: "certificate", label: "客户端证书 PEM", secret: true, required: true },
      { name: "private_key", label: "客户端私钥 PEM", secret: true, required: true },
    ];
  if (auth === "aws-sigv4")
    return [
      { name: "access_key_id", label: "Access Key ID", secret: false, required: true },
      { name: "secret_access_key", label: "Secret Access Key", secret: true, required: true },
      { name: "session_token", label: "Session Token", secret: true, required: false },
      { name: "expires_at", label: "凭据到期时间 RFC3339", secret: false, required: false },
    ];
  if (auth === "oauth2-token-exchange" || auth === "saml-token-exchange")
    return [{ name: "subject_token", label: "Subject Token", secret: true, required: true }];
  return [{ name: auth === "custom" ? "token" : "apiKey", label: "访问凭据", secret: true, required: true }];
}
export function ConnectionCredentialFields({
  auth,
  scheme,
  values = {},
  onChange,
  enforceRequired = true,
}: {
  enforceRequired?: boolean;
  auth: string;
  authName?: string;
  scheme?: DemoAuthScheme;
  values?: Record<string, string>;
  onChange: (name: string, value: string) => void;
}) {
  if (isRedirectAuth(auth)) return <p className="text-sm">点击保存并授权后，将打开上游授权页面。</p>;
  const fields = credentialFields(auth, scheme);
  return (
    <>
      {fields.length === 0 && <p className="text-sm">无需填写账号凭据。</p>}
      {fields.map((field) => (
        <Field key={field.name} label={field.label + (field.required ? " *" : "")}>
          {field.type === "boolean" ? (
            <select
              aria-label={field.label}
              className={fieldClass}
              required={field.required && enforceRequired}
              value={values[field.name] ?? ""}
              onChange={(event) => onChange(field.name, event.target.value)}
            >
              <option value="">请选择</option>
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
          ) : (
            <input
              className={fieldClass}
              type={field.secret ? "password" : field.type === "number" ? "number" : "text"}
              autoComplete={field.secret ? "new-password" : "off"}
              required={field.required && enforceRequired}
              value={values[field.name] ?? ""}
              onChange={(event) => onChange(field.name, event.target.value)}
            />
          )}
        </Field>
      ))}
    </>
  );
}

export function SearchBox({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
}) {
  return (
    <div className="relative w-full max-w-xl">
      <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--muted-text)]" />
      <input
        className={`${fieldClass} pl-10`}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
      />
    </div>
  );
}

export function Tabs({
  value,
  onChange,
  items,
  labels,
}: {
  value: string;
  onChange: (value: string) => void;
  items: string[];
  labels?: Record<string, string>;
}) {
  return (
    <div
      role="tablist"
      aria-label="页面视图"
      className="inline-flex max-w-full gap-1 overflow-x-auto rounded-xl bg-[var(--muted)] p-1"
    >
      {items.map((item) => (
        <button
          key={item}
          role="tab"
          aria-label={item}
          aria-selected={value === item}
          tabIndex={value === item ? 0 : -1}
          onKeyDown={(event) => {
            const index = items.indexOf(item);
            const next =
              event.key === "ArrowRight"
                ? (index + 1) % items.length
                : event.key === "ArrowLeft"
                  ? (index + items.length - 1) % items.length
                  : event.key === "Home"
                    ? 0
                    : event.key === "End"
                      ? items.length - 1
                      : -1;
            if (next >= 0) {
              event.preventDefault();
              onChange(items[next]);
              (event.currentTarget.parentElement?.children[next] as HTMLElement)?.focus();
            }
          }}
          onClick={() => onChange(item)}
          className={cn(
            "whitespace-nowrap rounded-lg px-3 py-2 text-xs font-semibold transition",
            value === item
              ? "bg-[var(--surface)] text-[var(--text)] shadow-sm"
              : "text-[var(--muted-text)] hover:text-[var(--text)]",
          )}
        >
          {labels?.[item] ?? item}
        </button>
      ))}
    </div>
  );
}

export function Logo({ letters, small = false }: { letters: string; small?: boolean }) {
  return (
    <span
      translate="no"
      className={cn(
        "grid place-items-center rounded-xl bg-gradient-to-br from-slate-100 to-blue-50 font-black text-blue-700 dark:from-slate-800 dark:to-blue-950 dark:text-blue-300",
        small ? "size-9 text-xs" : "size-11 text-sm",
      )}
    >
      {letters}
    </span>
  );
}

export function MiniStat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="rounded-xl bg-[var(--muted)] p-3">
      <p className="text-xs text-[var(--muted-text)]">{label}</p>
      <p className="mt-1 font-bold">{value}</p>
    </div>
  );
}

export function KeyValues({ items }: { items: Array<[string, ReactNode]> }) {
  return (
    <div className="divide-y divide-[var(--border)] rounded-xl border border-[var(--border)]">
      {items.map(([label, value]) => (
        <div key={label} className="grid grid-cols-[8rem_1fr] gap-3 p-3 text-sm">
          <span className="text-[var(--muted-text)]">{label}</span>
          <span className="break-all font-medium">{value}</span>
        </div>
      ))}
    </div>
  );
}

export function ListRow({ title, meta, action }: { title: string; meta: string; action: ReactNode }) {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-[var(--border)] p-3">
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold">{title}</p>
        <p className="mt-1 text-xs text-[var(--muted-text)]">{meta}</p>
      </div>
      {action}
    </div>
  );
}

export function StatusDot({ status }: { status: string }) {
  return (
    <span
      className={cn(
        "size-2.5 shrink-0 rounded-full",
        status === "success"
          ? "bg-emerald-500"
          : status === "failed"
            ? "bg-red-500"
            : status === "unknown" || status === "cancelled"
              ? "bg-slate-400"
              : "bg-blue-500 animate-pulse",
      )}
    />
  );
}

export function scheduleLabel(item: { scheduleType?: string; cronExpression?: string }): string {
  if (item.scheduleType === "manual") return "仅手动运行";
  return item.cronExpression || "每 30 分钟";
}

export function FeatureCard({ icon: Icon, title, text }: { icon: typeof Database; title: string; text: string }) {
  return (
    <Card className="flex gap-3 p-4">
      <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-[var(--muted)] text-blue-600">
        <Icon className="size-5" />
      </span>
      <div>
        <p className="text-sm font-bold">{title}</p>
        <p className="mt-1 text-xs leading-5 text-[var(--muted-text)]">{text}</p>
      </div>
    </Card>
  );
}

export function Th({ children }: { children?: ReactNode }) {
  return <th className="whitespace-nowrap px-4 py-3 font-semibold">{children}</th>;
}
export function Td({ children }: { children: ReactNode }) {
  return <td className="px-4 py-3.5">{children}</td>;
}
