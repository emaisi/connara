import { ArrowRight, Check, Database, Plus, Search } from "lucide-react";
import { type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";
import { type DemoAuthInstance, type DemoAuthScheme, type DemoSystem } from "../demo";
import { Card, Field, cn, fieldClass } from "../ui";

export function authInstancesFor(
  system: DemoSystem | undefined,
  instances: DemoAuthInstance[],
  schemes: DemoAuthScheme[] = [],
  executableOnly = false,
) {
  if (!system) return [];
  return instances.filter(
    (instance) =>
      instance.status === "ready" &&
      instance.systemIds.includes(system.service) &&
      system.authTemplateIds.includes(instance.templateId) &&
      (!executableOnly || isExecutableAuthTemplate(instance.templateId, schemes)),
  );
}

export function isExecutableAuthTemplate(templateId: string, schemes: DemoAuthScheme[]) {
  return (
    schemes.some((scheme) => scheme.id === templateId && scheme.status === "published") ||
    authMethodCatalog.some((method) => method.id === templateId && method.executable)
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
  group: "内置方式" | "企业认证";
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
    group: "内置方式",
    executable: true,
    mode: "跳转授权",
    summary: "适合 Google、GitHub、Slack 等由最终用户授权的公网平台。",
    lifecycle: "State + PKCE → 回调换取令牌 → 到期前自动刷新",
    fields: ["客户端 ID", "客户端密钥", "授权地址", "令牌地址", "权限范围", "回调地址"],
  },
  {
    id: "oauth1",
    name: "OAuth 1.0a",
    group: "内置方式",
    executable: false,
    mode: "签名跳转",
    summary: "兼容仍使用临时令牌和请求签名的旧版公网平台。",
    lifecycle: "请求令牌 → 用户授权 → 访问令牌 → 每次请求签名",
    fields: ["Consumer Key", "Consumer Secret", "请求令牌地址", "授权地址", "访问令牌地址"],
  },
  {
    id: "api_key",
    name: "API 密钥 / Bearer 令牌",
    group: "内置方式",
    executable: true,
    mode: "静态凭据",
    summary: "把密钥注入请求头、查询参数或 Cookie，并支持自定义前缀。",
    lifecycle: "录入并校验 → 加密保存 → 请求时按规则注入",
    fields: ["凭据字段", "注入位置", "参数名称", "值前缀", "验证请求"],
  },
  {
    id: "basic",
    name: "Basic 认证",
    group: "内置方式",
    executable: true,
    mode: "静态凭据",
    summary: "适合支持 HTTP Basic 的传统系统和内部管理接口。",
    lifecycle: "录入用户名和密码 → 加密保存 → 请求时编码注入",
    fields: ["用户名", "密码", "验证请求"],
  },
  {
    id: "hmac",
    name: "HMAC / AWS SigV4",
    group: "内置方式",
    executable: false,
    mode: "请求签名",
    summary: "运行时根据请求方法、路径、时间和正文计算签名。",
    lifecycle: "读取密钥 → 规范化请求 → 计算签名 → 注入签名头",
    fields: ["Access Key", "Secret Key", "算法", "区域", "服务名", "签名头"],
  },
  {
    id: "no_auth",
    name: "无需认证",
    group: "内置方式",
    executable: true,
    mode: "公开接口",
    summary: "只为确实公开且无需凭据的接口建立虚拟连接。",
    lifecycle: "创建虚拟连接 → 直接调用，不保存任何凭据",
    fields: ["验证请求"],
  },
  {
    id: "username-password-token",
    name: "用户名密码换 Token",
    group: "企业认证",
    executable: true,
    mode: "两步登录",
    summary: "先用用户名和密码调用登录接口获取 Token，再把 Token 注入后续请求的 Header。",
    lifecycle: "提交用户名密码 → 提取 Token → 加密缓存 → Header 注入 → Token 到期后重新登录",
    fields: ["登录地址", "请求格式", "用户名", "密码", "Token 路径", "Header 名称", "Header 值模板"],
  },
  {
    id: "client-credentials",
    name: "OAuth 2.0 客户端凭证",
    group: "企业认证",
    executable: true,
    mode: "服务到服务",
    summary: "适合微服务、ERP 和数据中台之间的机器身份认证。",
    lifecycle: "客户端凭据换取令牌 → 缓存 → 到期前自动续期",
    fields: ["客户端 ID", "客户端密钥", "令牌地址", "Scope / Audience", "客户端认证方式"],
  },
  {
    id: "mtls",
    name: "mTLS 双向证书",
    group: "企业认证",
    executable: false,
    mode: "双向 TLS",
    summary: "使用企业 CA 签发的客户端证书和私钥建立双向 TLS。",
    lifecycle: "校验证书链 → 安全装载私钥 → TLS 握手 → 证书轮换",
    fields: ["客户端证书", "客户端私钥", "CA 证书链", "服务器名称", "到期提醒"],
  },
  {
    id: "oidc",
    name: "OIDC / 企业 SSO",
    group: "企业认证",
    executable: false,
    mode: "员工授权",
    summary: "复用企业身份提供商，让员工委托访问内部或 SaaS 资源。",
    lifecycle: "发现端点 → OIDC 授权 → 校验 nonce/ID Token → 刷新",
    fields: ["Issuer", "客户端 ID", "客户端密钥", "权限范围", "回调地址"],
  },
  {
    id: "jwt",
    name: "JWT 服务账号",
    group: "企业认证",
    executable: false,
    mode: "服务账号",
    summary: "用私钥签发短期 JWT，或用断言交换访问令牌。",
    lifecycle: "生成短期断言 → 签名 → 直接调用或交换令牌",
    fields: ["Issuer", "Subject", "Audience", "私钥", "算法", "令牌地址"],
  },
  {
    id: "saml-token-exchange",
    name: "SAML / Token Exchange",
    group: "企业认证",
    executable: false,
    mode: "凭据交换",
    summary: "通过企业网关把 SAML 断言或内部票据交换成 API 令牌。",
    lifecycle: "接收企业断言 → 受控交换 → 获取短期令牌 → 自动续期",
    fields: ["交换地址", "断言字段", "客户端认证", "令牌路径", "过期时间路径"],
  },
  {
    id: "kerberos",
    name: "Kerberos / NTLM 网关",
    group: "企业认证",
    executable: false,
    mode: "受控网关",
    summary: "由部署在企业网络内的网关完成传统域认证，平台只使用短期结果。",
    lifecycle: "连接内部网关 → 域认证 → 获取短期会话 → 调用目标系统",
    fields: ["网关地址", "域", "服务主体", "凭据引用", "会话验证地址"],
  },
];

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

export function BuildFlow({ current }: { current: "systems" | "auth" | "integrations" | "connections" | "actions" }) {
  const [params] = useSearchParams();
  const context = new URLSearchParams();
  for (const key of ["system", "integration", "connection", "returnTo"])
    if (params.get(key)) context.set(key, params.get(key)!);
  const steps = [
    ["systems", "系统", "/providers"],
    ["auth", "认证", "/auth"],
    ["integrations", "集成", "/integrations"],
    ["connections", "连接", "/connections"],
    ["actions", "验证", "/actions"],
  ] as const;
  return (
    <Card aria-label="集成构建流程" role="navigation" className="grid grid-cols-5 gap-1 p-1.5 sm:p-2">
      {steps.map(([id, label, to], index) => (
        <Link
          key={id}
          to={to + (context.size ? `?${context}` : "")}
          aria-current={current === id ? "step" : undefined}
          className={cn(
            "flex min-w-0 items-center justify-center gap-1 rounded-xl px-1.5 py-2 text-xs font-semibold transition hover:bg-[var(--muted)] sm:justify-start sm:gap-2 sm:px-3 sm:py-2.5 sm:text-sm",
            current === id && "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300",
          )}
        >
          <span
            className={cn(
              "grid size-5 shrink-0 place-items-center rounded-full bg-[var(--muted)] text-[10px] sm:size-6 sm:text-xs",
              current === id && "bg-blue-600 text-white",
            )}
          >
            {index + 1}
          </span>
          {label}
          {index < steps.length - 1 && <ArrowRight className="ml-auto hidden size-3.5 opacity-40 lg:block" />}
        </Link>
      ))}
    </Card>
  );
}

export function isRedirectAuth(auth: string) {
  return (
    (auth.includes("OAuth") && !auth.includes("客户端凭证")) ||
    auth.includes("OIDC") ||
    auth.includes("SSO") ||
    auth.includes("GitHub App")
  );
}

export function credentialFields(auth: string, scheme?: DemoAuthScheme) {
  if (scheme) return scheme.credentialFields.map((field) => ({ ...field, required: true }));
  if (isRedirectAuth(auth) || auth.includes("无需认证")) return [];
  if (auth.includes("Basic") || auth.includes("用户名密码"))
    return [
      { name: "username", label: "用户名", secret: false, required: true },
      { name: "password", label: "密码", secret: true, required: true },
    ];
  if (auth.includes("客户端凭证"))
    return [
      { name: "client_id", label: "客户端 ID", secret: false, required: true },
      { name: "client_secret", label: "客户端密钥", secret: true, required: true },
    ];
  if (auth.includes("JWT")) return [{ name: "private_key", label: "服务账号私钥", secret: true, required: true }];
  return [
    { name: auth.includes("自定义") ? "token" : "apiKey", label: auth || "访问凭据", secret: true, required: true },
  ];
}
export function ConnectionCredentialFields({
  auth,
  scheme,
  values = {},
  onChange,
}: {
  auth: string;
  scheme?: DemoAuthScheme;
  values?: Record<string, string>;
  onChange: (name: string, value: string) => void;
}) {
  if (isRedirectAuth(auth)) return <p className="text-sm">下一步将打开授权页面，无需在此填写平台密码。</p>;
  const fields = credentialFields(auth, scheme);
  return (
    <>
      {fields.length === 0 && <p className="text-sm">此集成无需填写凭据。</p>}
      {fields.map((field) => (
        <Field key={field.name} label={field.label + (field.required ? " *" : "")}>
          <input
            className={fieldClass}
            type={field.secret ? "password" : "text"}
            autoComplete={field.secret ? "new-password" : "off"}
            required={field.required}
            value={values[field.name] ?? ""}
            onChange={(event) => onChange(field.name, event.target.value)}
          />
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
}: {
  value: string;
  onChange: (value: string) => void;
  items: string[];
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
          {item}
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

export function StepBar({ step, labels }: { step: number; labels: string[] }) {
  return (
    <div className="flex items-center">
      {labels.map((label, index) => (
        <div key={label} className="flex flex-1 items-center last:flex-none">
          <div className="grid justify-items-center gap-1">
            <span
              className={cn(
                "grid size-7 place-items-center rounded-full text-xs font-bold",
                step >= index + 1 ? "bg-blue-600 text-white" : "bg-[var(--muted)] text-[var(--muted-text)]",
              )}
            >
              {step > index + 1 ? <Check className="size-3.5" /> : index + 1}
            </span>
            <span className="whitespace-nowrap text-[10px] text-[var(--muted-text)]">{label}</span>
          </div>
          {index < labels.length - 1 && (
            <span className={cn("mx-2 mb-4 h-0.5 flex-1", step > index + 1 ? "bg-blue-500" : "bg-[var(--border)]")} />
          )}
        </div>
      ))}
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
