import { queryClient, invalidateResources } from "./query";
import type {
  Action,
  AdminSession,
  Connection,
  CreatedRuntimeToken,
  Integration,
  Meta,
  Provider,
  ProviderSummary,
  Run,
  RuntimeToken,
} from "./types";

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly code = "",
    readonly requestId = "",
    readonly operationId = "",
    readonly rawMessage = "",
  ) {
    super(message);
  }
}

export const api = {
  login: (email: string, password: string) =>
    request<AdminSession>("/api/auth/login", {
      method: "POST",
      body: { email, password },
      skipUnauthorizedEvent: true,
    }),
  acceptInvitation: (token: string, password: string, displayName: string) =>
    request<{ email: string }>("/api/auth/accept-invitation", {
      method: "POST",
      body: { token, password, displayName },
      skipUnauthorizedEvent: true,
    }),
  session: () => request<AdminSession>("/api/auth/session", { skipUnauthorizedEvent: true }),
  logout: () => request<void>("/api/auth/logout", { method: "POST", skipUnauthorizedEvent: true }),
  lookups: () => request<{ systems: any[]; connections: any[] }>("/api/lookups"),
  meta: () => request<Meta>("/api/meta"),
  providers: () => request<ProviderSummary[]>("/api/systems"),
  provider: (service: string) => request<Provider>(`/api/systems/${encodeURIComponent(service)}`),
  searchActions: (query: string, service = "") =>
    request<Action[]>(`/api/actions?q=${encodeURIComponent(query)}&system=${encodeURIComponent(service)}`),
  runs: (limit = 100) => request<Run[]>(`/api/operations?limit=${limit}`),
  systemGroups: () => request<any[]>("/api/system-groups"),
  saveSystemGroup: (body: unknown) => request<any>("/api/system-groups", { method: "POST", body }),
  updateSystemGroup: (id: string, body: unknown) =>
    request<any>(`/api/system-groups/${encodeURIComponent(id)}`, { method: "PATCH", body }),
  deleteSystemGroup: (id: string) =>
    request<void>(`/api/system-groups/${encodeURIComponent(id)}`, { method: "DELETE" }),
  systems: () => request<any[]>("/api/systems", { allPages: true }),
  createSystem: (body: unknown) => request<any>("/api/systems", { method: "POST", body }),
  updateSystem: (id: string, body: unknown) =>
    request<any>(`/api/systems/${encodeURIComponent(id)}`, { method: "PATCH", body }),
  setSystemAuthTemplates: (id: string, authTemplateIds: string[]) =>
    request<any>(`/api/systems/${encodeURIComponent(id)}/auth-templates`, {
      method: "PUT",
      body: { authTemplateIds },
    }),
  authTemplates: () => request<any[]>("/api/auth-templates"),
  saveAuthTemplate: (body: unknown, id = "") =>
    request<any>(id ? `/api/auth-templates/${encodeURIComponent(id)}` : "/api/auth-templates", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  setAuthTemplateStatus: (id: string, status: string) =>
    request<any>(`/api/auth-templates/${encodeURIComponent(id)}/status`, { method: "POST", body: { status } }),
  deleteAuthTemplate: (id: string) =>
    request<void>(`/api/auth-templates/${encodeURIComponent(id)}`, { method: "DELETE" }),
  authInstances: () => request<any[]>("/api/auth-instances"),
  saveAuthInstance: (body: unknown, id = "") =>
    request<any>(id ? `/api/auth-instances/${encodeURIComponent(id)}` : "/api/auth-instances", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  testAuthInstance: (id: string) =>
    request<any>(`/api/auth-instances/${encodeURIComponent(id)}/test`, { method: "POST" }),
  integrations: () => request<any[]>("/api/integrations"),
  saveIntegration: (body: unknown, id = "") =>
    request<any>(id ? `/api/integrations/${encodeURIComponent(id)}` : "/api/integrations", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  createIntegration: (body: { providerId: string; name: string }) =>
    request<Integration>("/api/integrations", { method: "POST", body }),
  connections: () => request<any[]>("/api/connections", { allPages: true }),
  saveConnection: (body: {
    integrationId: string;
    name: string;
    authType?: string;
    credentials: Record<string, unknown>;
    connectionKey?: string;
    endUserKey?: string;
    endUserName?: string;
  }) => {
    const { authType: _authType, ...values } = body;
    return request<Connection>("/api/connections", {
      method: "POST",
      body: {
        ...values,
        connectionKey: values.connectionKey ?? values.name,
        endUserKey: values.endUserKey ?? "default",
      },
    });
  },
  updateConnection: (id: string, body: unknown) =>
    request<any>(`/api/connections/${encodeURIComponent(id)}`, { method: "PATCH", body }),
  verifyConnection: (id: string) =>
    request<any>(`/api/connections/${encodeURIComponent(id)}/verify`, { method: "POST" }),
  oauthStart: (body: unknown) => request<{ authorizationUrl: string }>("/api/oauth/start", { method: "POST", body }),
  actions: () => request<any[]>("/api/actions", { allPages: true }),
  saveAction: (body: unknown, id = "") =>
    request<any>(id ? `/api/actions/${encodeURIComponent(id)}` : "/api/actions", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  testAction: (id: string, body: unknown) =>
    request<any>(`/api/actions/${encodeURIComponent(id)}/test`, { method: "POST", body }),
  syncTasks: () => request<any[]>("/api/sync-tasks"),
  saveSyncTask: (body: unknown, id = "") =>
    request<any>(id ? `/api/sync-tasks/${encodeURIComponent(id)}` : "/api/sync-tasks", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  deploySyncTask: (id: string) => request<any>(`/api/sync-tasks/${encodeURIComponent(id)}/deploy`, { method: "POST" }),
  pauseSyncTask: (id: string) => request<any>(`/api/sync-tasks/${encodeURIComponent(id)}/pause`, { method: "POST" }),
  syncRecords: (id: string, cursor = "") => fetchPage(`/api/sync-tasks/${encodeURIComponent(id)}/records`, {}, cursor),
  runSyncTask: (id: string) => request<any>(`/api/sync-tasks/${encodeURIComponent(id)}/run`, { method: "POST" }),
  deleteSyncTask: (id: string) => request<void>(`/api/sync-tasks/${encodeURIComponent(id)}`, { method: "DELETE" }),
  webhookEndpoints: () => request<any[]>("/api/webhook-endpoints"),
  saveWebhookEndpoint: (body: unknown, id = "") =>
    request<any>(id ? `/api/webhook-endpoints/${encodeURIComponent(id)}` : "/api/webhook-endpoints", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  testWebhookEndpoint: (id: string) =>
    request<any>(`/api/webhook-endpoints/${encodeURIComponent(id)}/test`, { method: "POST" }),
  webhookSources: () => request<any[]>("/api/webhook-sources"),
  saveWebhookSource: (body: unknown, id = "") =>
    request<any>(id ? `/api/webhook-sources/${encodeURIComponent(id)}` : "/api/webhook-sources", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  webhookDeliveries: () => request<any[]>("/api/webhook-deliveries"),
  webhookDelivery: (id: string) => request<any>(`/api/webhook-deliveries/${encodeURIComponent(id)}`),
  webhookDeliveryAttempts: (id: string) => request<any[]>(`/api/webhook-deliveries/${encodeURIComponent(id)}/attempts`),
  retryWebhookDelivery: (id: string) =>
    request<any>(`/api/webhook-deliveries/${encodeURIComponent(id)}/retry`, { method: "POST" }),
  runtimeTokens: () => request<RuntimeToken[]>("/api/runtime-tokens"),
  createRuntimeToken: (body: {
    name: string;
    allowedActions: string[];
    blockedActions: string[];
    allowedConnections: string[];
    allowedProxies?: string[];
  }) => request<CreatedRuntimeToken>("/api/runtime-tokens", { method: "POST", body }),
  revokeRuntimeToken: (id: string) =>
    request<void>(`/api/runtime-tokens/${encodeURIComponent(id)}`, { method: "DELETE" }),
  operations: () => request<any[]>("/api/operations"),
  operation: (id: string) => request<any>(`/api/operations/${encodeURIComponent(id)}`),
  metrics: (hours = 24) => request<any>(`/api/metrics?hours=${hours}`),
  auditLogs: () => request<any[]>("/api/audit-logs"),
  teamMembers: () => request<any[]>("/api/team/members"),
  inviteMember: (body: unknown) => request<any>("/api/team/members", { method: "POST", body }),
  updateMember: (id: string, body: unknown) =>
    request<any>(`/api/team/members/${encodeURIComponent(id)}`, { method: "PATCH", body }),
  removeMember: (id: string) => request<void>(`/api/team/members/${encodeURIComponent(id)}`, { method: "DELETE" }),
  settings: () => request<any>("/api/settings"),
  saveSettings: (body: unknown) => request<any>("/api/settings", { method: "PATCH", body }),
};

interface RequestOptions {
  method?: string;
  body?: unknown;
  skipUnauthorizedEvent?: boolean;
  page?: boolean;
  allPages?: boolean;
}

function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  if ((!options.method || options.method === "GET") && !path.startsWith("/api/auth/"))
    return queryClient.fetchQuery({ queryKey: [path], queryFn: () => fetchRequest<T>(path, options) });
  return fetchRequest<T>(path, options).then((value) => {
    if (options.method && options.method !== "GET") {
      if (path === "/api/auth/login" || path === "/api/auth/logout") queryClient.clear();
      else if (!path.startsWith("/api/auth/")) invalidateResources(path);
    }
    return value;
  });
}

async function fetchRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const response = await fetch(path, {
    method: options.method ?? "GET",
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      ...(options.body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  const text = await response.text();
  const payload = parseJSON(text);
  if (!response.ok) {
    if (response.status === 401 && !options.skipUnauthorizedEvent) {
      queryClient.clear();
      window.dispatchEvent(new Event("apihub:unauthorized"));
    }
    const fallback =
      response.status === 502
        ? "后端服务未启动或无法连接，请先启动 127.0.0.1:8080 的 APIHub 后端。"
        : `请求失败（${response.status}）`;
    const value = payload as
      | {
          code?: string;
          requestId?: string;
          error?: { code?: string; message?: string };
          meta?: { operationId?: string };
        }
      | undefined;
    const error = value?.error ?? value;
    if (options.method && options.method !== "GET" && !path.startsWith("/api/auth/")) invalidateResources(path);
    const requestId = value?.requestId ?? response.headers.get("X-Request-ID") ?? "";
    throw new ApiError(
      response.status,
      (localizedError(payload) ?? fallback) + (requestId ? `（请求 ID：${requestId}）` : ""),
      error?.code ?? (value as { errorCode?: string })?.errorCode,
      requestId,
      value?.meta?.operationId,
      (error as { message?: string })?.message,
    );
  }
  if (response.status === 204) return undefined as T;
  if (payload === undefined) throw new ApiError(response.status, "服务返回了无法解析的响应");
  if (options.allPages && Array.isArray(payload)) {
    const next = response.headers.get("X-Next-Cursor");
    if (next) {
      const target = new URL(path, window.location.origin);
      if (target.searchParams.get("cursor") === next) throw new ApiError(500, "服务返回了重复分页游标");
      target.searchParams.set("cursor", next);
      return [...payload, ...(await fetchRequest<any[]>(target.pathname + target.search, options))] as T;
    }
  }
  if (options.page) return { items: payload, nextCursor: response.headers.get("X-Next-Cursor") ?? "" } as T;
  return payload as T;
}

function parseJSON(value: string): unknown {
  if (value === "") return null;
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return undefined;
  }
}

function localizedError(payload: unknown): string | undefined {
  if (!payload || typeof payload !== "object") return undefined;
  const envelope = payload as { code?: unknown; message?: unknown; error?: { code?: unknown; message?: unknown } };
  const value = envelope.error ?? envelope;
  const language = window.localStorage?.getItem("apihub.language") === "en" ? "en" : "zh-CN";
  const code =
    typeof value.code === "string"
      ? value.code
      : "errorCode" in value && typeof value.errorCode === "string"
        ? value.errorCode
        : "";
  const messages: Record<string, [string, string]> = {
    conflict: ["配置已改变或资源正在使用，请刷新后重试", "The resource changed or is in use. Refresh and try again"],
    internal_error: [
      "服务暂时无法完成此请求，请用请求 ID 排查",
      "The service could not complete this request. Check the request ID",
    ],
    invalid_input: [
      "参数不符合要求，请检查必填字段、JSON 和输入约束",
      "Check required fields, JSON and input constraints",
    ],
    provider_request_failed: [
      "未能确认上游调用结果，请查看运行详情并核对上游",
      "The upstream outcome is unknown. Check the run and verify upstream",
    ],
    connection_verification_failed: [
      "连接验证失败，请检查凭据、验证路径和上游服务",
      "Connection verification failed. Check credentials, verification path and upstream",
    ],
    provider_error: [
      "上游返回错误，请检查连接权限和请求参数",
      "The upstream returned an error. Check permissions and input",
    ],
    integration_not_ready: ["集成尚未就绪，请先完成配置", "Configure the integration before continuing"],
    auth_instance_not_ready: ["认证实例尚未就绪，请先检查认证配置", "Check the authentication configuration first"],
    invitation_invalid: [
      "邀请已过期、已使用或已撤销，请联系管理员重新邀请",
      "This invitation expired, was used or was revoked",
    ],
    forbidden: ["当前角色没有执行此操作的权限", "Your role cannot perform this operation"],
    invalid_credentials: ["邮箱账号或密码错误", "Incorrect email or password"],
    unauthorized: ["登录会话无效或已过期", "Your session is invalid or has expired"],
    login_unavailable: ["登录服务暂时不可用，请稍后重试", "Sign-in is temporarily unavailable. Please try again later"],
    rate_limited: ["请求过于频繁，请稍后重试", "Too many requests. Please try again later"],
  };
  if (messages[code]) return messages[code][language === "en" ? 1 : 0];
  return typeof value.message === "string" ? value.message : undefined;
}

export function fetchPage(
  path: string,
  params: Record<string, string>,
  cursor = "",
): Promise<{ items: any[]; nextCursor: string }> {
  const query = new URLSearchParams({ ...params, limit: "100", ...(cursor ? { cursor } : {}) });
  return fetchRequest(`${path}?${query}`, { page: true });
}
