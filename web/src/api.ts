import { queryClient, invalidateResources } from "./query";
import type {
  Action,
  AdminSession,
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
    readonly details?: unknown,
    readonly outcome?: string,
    readonly meta?: Record<string, unknown>,
  ) {
    super(message);
  }
}

export interface AdminConnection {
  id: string;
  connectionKey: string;
  name: string;
  integrationId: string;
  integrationKey: string;
  authInstanceId: string;
  endUserKey: string;
  status: string;
  revision: number;
  lastVerifiedAt?: string;
}

export interface AdminAuthInstance {
  id: string;
  version: number;
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
  runs: (limit = 100, actionId = "") =>
    request<Run[]>(`/api/operations?limit=${limit}&action=${encodeURIComponent(actionId)}`),
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
  setSystemAuthTemplates: (id: string, authTemplateIds: string[], defaultAuthTemplateId: string) =>
    request<any>(`/api/systems/${encodeURIComponent(id)}/auth-templates`, {
      method: "PUT",
      body: { authTemplateIds, defaultAuthTemplateId },
    }),
  authTemplates: () => request<any[]>("/api/auth-templates"),
  saveAuthTemplate: (body: unknown, id = "") =>
    request<any>(id ? `/api/auth-templates/${encodeURIComponent(id)}` : "/api/auth-templates", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  setAuthTemplateStatus: (id: string, status: string, version?: number) =>
    request<any>(`/api/auth-templates/${encodeURIComponent(id)}/status`, { method: "POST", body: { status, version } }),
  deleteAuthTemplate: (id: string) =>
    request<void>(`/api/auth-templates/${encodeURIComponent(id)}`, { method: "DELETE" }),
  authInstances: () => request<any[]>("/api/auth-instances"),
  saveAuthInstance: (body: unknown, id = "") =>
    request<AdminAuthInstance>(id ? `/api/auth-instances/${encodeURIComponent(id)}` : "/api/auth-instances", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  checkAuthInstance: (body: unknown, id = "") =>
    request<any>(id ? `/api/auth-instances/${encodeURIComponent(id)}/check` : "/api/auth-instances/check", {
      method: "POST",
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
  connections: () => request<AdminConnection[]>("/api/connections", { allPages: true }),
  saveConnection: (body: {
    integrationId: string;
    name: string;
    credentials: Record<string, unknown>;
    connectionKey: string;
    endUserKey: string;
    endUserName?: string;
  }) => {
    return request<AdminConnection>("/api/connections", { method: "POST", body });
  },
  updateConnection: (id: string, body: unknown) =>
    request<AdminConnection>(`/api/connections/${encodeURIComponent(id)}`, { method: "PATCH", body }),
  setConnectionEnabled: (id: string, revision: number, enabled: boolean) =>
    request<any>(`/api/connections/${encodeURIComponent(id)}/enabled`, { method: "POST", body: { revision, enabled } }),
  verifyConnection: (id: string, body?: unknown) =>
    request<{ verified: boolean; connection: AdminConnection }>(`/api/connections/${encodeURIComponent(id)}/verify`, {
      method: "POST",
      body,
    }),
  oauthStart: (body: unknown) => request<{ authorizationUrl: string }>("/api/oauth/start", { method: "POST", body }),
  actions: (fresh = false) => request<any[]>("/api/actions", { allPages: true, fresh }),
  saveAction: (body: unknown, id = "") =>
    request<any>(id ? `/api/actions/${encodeURIComponent(id)}` : "/api/actions", {
      method: id ? "PATCH" : "POST",
      body,
    }),
  previewAction: (id: string, body: unknown) =>
    request<any>(`/api/actions/${encodeURIComponent(id)}/preview`, { method: "POST", body }),
  actionExecutionOptions: (id: string, fresh = false) =>
    request<any[]>(`/api/actions/${encodeURIComponent(id)}/execution-options`, { fresh }),
  action: (id: string) => request<any>(`/api/actions/${encodeURIComponent(id)}`),
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
  workflows: () => request<any[]>("/api/workflows"),
  workflow: (id: string) => request<any>(`/api/workflows/${encodeURIComponent(id)}`, { fresh: true }),
  workflowCapabilities: () => request<any>("/api/workflows/capabilities", { fresh: true }),
  previewWorkflowStep: (body: unknown) => request<any>("/api/workflows/preview-step", { method: "POST", body }),
  previewWorkflowSchedule: (body: unknown) => request<any>("/api/workflows/preview-schedule", { method: "POST", body }),
  saveWorkflowLayout: (id: string, body: unknown) =>
    request<any>(`/api/workflows/${encodeURIComponent(id)}/layout`, { method: "PATCH", body }),
  workflowRunView: (id: string) => request<any>(`/api/workflow-runs/${encodeURIComponent(id)}/view`),
  saveWorkflow: (body: unknown, id = "", version = 0) =>
    request<any>(id ? `/api/workflows/${encodeURIComponent(id)}` : "/api/workflows", {
      method: id ? "PATCH" : "POST",
      body: { ...(body as Record<string, unknown>), version },
    }),
  deployWorkflow: (id: string, expectedVersion?: number) =>
    request<any>(`/api/workflows/${encodeURIComponent(id)}/deploy`, { method: "POST", body: { expectedVersion } }),
  pauseWorkflow: (id: string, expectedVersion?: number) =>
    request<any>(`/api/workflows/${encodeURIComponent(id)}/pause`, { method: "POST", body: { expectedVersion } }),
  runWorkflow: (id: string, input?: Record<string, unknown>, expectedVersion?: number) =>
    request<any>(`/api/workflows/${encodeURIComponent(id)}/run`, {
      method: "POST",
      body: { ...(input ? { input } : {}), expectedVersion },
    }),
  workflowRunResult: (id: string) => request<any>(`/api/workflow-runs/${encodeURIComponent(id)}/result`),
  deleteWorkflow: (id: string) => request<void>(`/api/workflows/${encodeURIComponent(id)}`, { method: "DELETE" }),
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
  operation: (id: string) => request<any>(`/api/operations/${encodeURIComponent(id)}`, { fresh: true }),
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
  fresh?: boolean;
  method?: string;
  body?: unknown;
  skipUnauthorizedEvent?: boolean;
  page?: boolean;
  allPages?: boolean;
}

function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  if ((!options.method || options.method === "GET") && !path.startsWith("/api/auth/"))
    return queryClient.fetchQuery({
      queryKey: [path],
      queryFn: () => fetchRequest<T>(path, options),
      ...(options.fresh ? { staleTime: 0, retry: false } : {}),
    });
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
          meta?: { operationId?: string; outcome?: string };
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
      (payload as { details?: unknown })?.details,
      value?.meta?.outcome,
      value?.meta,
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
    workflow_preview_busy: [
      "样本预览正在使用，请保留当前输入后稍后重试",
      "A preview is active; keep your inputs and try again later",
    ],
    code_capacity_busy: [
      "代码执行繁忙，正式运行优先；当前配置和样本已保留",
      "Code capacity is busy; formal runs have priority. Your draft and sample are retained",
    ],
    code_runtime_unavailable: [
      "代码执行环境不可用，请联系管理员检查",
      "The restricted code environment is unavailable",
    ],
    workflow_capability_disabled: ["平台暂未开放此工作流能力", "This workflow capability is disabled"],
    code_compile_error: [
      "JavaScript 编译失败，请查看源码诊断位置",
      "JavaScript compilation failed; check the source diagnostics",
    ],
    code_entrypoint_invalid: ["请声明同步 function main(input)", "Declare synchronous function main(input)"],
    code_timeout: ["代码执行超过两秒，已终止本次进程", "Code exceeded two seconds; this process was terminated"],
    code_output_invalid: [
      "返回值不符合 JSON、大小或字段声明要求",
      "The return value violates JSON, size or declared fields",
    ],
    code_worker_failed: ["受限代码进程异常退出，本次步骤失败", "The restricted code process exited unexpectedly"],

    api_key_exists: ["此系统中已存在相同 API 标识，请更换标识", "This system already has an API with this key"],
    version_required: ["请先刷新请求预览，再执行调用", "Refresh the request preview before executing"],
    configuration_changed: [
      "配置已变化，请刷新请求预览并确认地址",
      "Configuration changed; refresh the preview and confirm the URL",
    ],
    account_verification_required: [
      "账号尚未在当前地址完成验证，请先前往账号管理验证",
      "Verify the account on the current target URL first",
    ],
    account_disabled: ["账号已停用，请先启用", "Enable this account first"],
    response_check_failed: [
      "上游响应检查失败，请查看业务条件或响应结构",
      "Upstream response checks failed; check business conditions and response structure",
    ],

    connection_name_exists: [
      "该集成中已存在同名账号，请更换账号名称，或打开已有账号重试验证",
      "An account with this name already exists in this integration. Choose another name or retry verification on the existing account",
    ],
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
      "上游请求失败，请查看实际地址和运行详情",
      "Upstream request failed. Check the target URL and run details",
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
  const outcome = (payload as { meta?: { outcome?: string } }).meta?.outcome;
  const localized =
    code === "provider_request_failed" && outcome === "unknown"
      ? language === "en"
        ? "The upstream outcome is unknown. Check the run before retrying"
        : "上游调用结果未知，请核对运行详情与上游后再决定是否重试"
      : messages[code]?.[language === "en" ? 1 : 0];
  if (localized) {
    const detail = typeof value.message === "string" && value.message !== localized ? value.message : "";
    return detail ? `${localized}（${detail}）` : localized;
  }
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
