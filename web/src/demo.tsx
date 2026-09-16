import { DemoContext } from "./demo-context";
import { PermissionContext } from "./permissions";
import { queryClient } from "./query";
import { useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, ApiError } from "./api";
import type { AdminSession } from "./types";

export interface DemoIntegration {
  id: string;
  name: string;
  provider: string;
  systemId?: string;
  displayName: string;
  baseUrl: string;
  authInstanceId: string;
  status: "ready" | "draft" | "disabled";
  settings?: Record<string, unknown>;
  updatedAt: string;
}

export interface DemoSystem {
  id?: string;
  service: string;
  name: string;
  source: "builtin" | "imported" | "custom";
  groupId?: string;
  letter: string;
  category: string;
  actions: number;
  executable: number;
  connections: number;
  description: string;
  authTemplateIds: string[];
}

export interface DemoAuthScheme {
  id: string;
  name: string;
  flow: string;
  status: "draft" | "published" | "disabled";
  credentialFields: { name: string; label: string; secret: boolean }[];
  injectionRules: { target: string; name: string; template: string }[];
  tokenEndpoint: string;
  refreshEndpoint: string;
  tokenPath: string;
  expiryPath: string;
  updatedAt: string;
}

export interface DemoAuthInstance {
  id: string;
  instanceKey?: string;
  name: string;
  templateId: string;
  systemIds: string[];
  status: "ready" | "draft" | "disabled";
  tokenEndpoint: string;
  refreshEndpoint: string;
  tokenPath: string;
  expiryPath: string;
  headerName: string;
  headerValueTemplate: string;
  authorizationUrl?: string;
  scopes?: string[];
  publicConfig?: Record<string, unknown>;
  version?: number;
  oauthClientId?: string;
  oauthClientSecret?: string;
  updatedAt: string;
}

export interface DemoConnection {
  id: string;
  name: string;
  integration: string;
  provider: string;
  endUser: string;
  authInstanceId: string;
  status: "active" | "pending" | "error" | "expired";
  lastVerified: string;
  lastUsed: string;
  records: number;
  tags: string[];
}

export interface DemoToken {
  id: string;
  name: string;
  actions: string[];
  blockedActions?: string[];
  connections: string[];
  status: "active" | "revoked";
  lastUsed: string;
}

export interface DemoOperation {
  id: string;
  kind: "Action" | "Sync" | "Webhook" | "Auth";
  name: string;
  integration: string;
  connection: string;
  status: "queued" | "success" | "failed" | "running" | "cancelled" | "unknown";
  duration: string;
  time: string;
  messages: string[];
}

export interface DemoContextValue {
  platformName: string;
  integrations: DemoIntegration[];
  connections: DemoConnection[];
  tokens: DemoToken[];
  operations: DemoOperation[];
  customSystems: DemoSystem[];
  systemGroups: string[];
  authSchemes: DemoAuthScheme[];
  authInstances: DemoAuthInstance[];
  notice: string;
  noticeError: boolean;
  dismissNotice: () => void;
  loading: boolean;
  authenticated: boolean;
  user: AdminSession | null;
  backendError: string;
  login: (email: string, password: string) => Promise<boolean>;
  logout: () => void;
  reload: () => Promise<void>;
  addIntegration: (
    provider: string,
    name: string,
    authInstanceId: string,
    displayName?: string,
    baseUrl?: string,
  ) => Promise<boolean>;
  updateIntegrationAuthInstance: (id: string, authInstanceId: string) => void;
  addSystem: (name: string, service: string, group: string, authTemplateId: string) => Promise<boolean>;
  addSystemGroup: (name: string) => void;
  saveAuthScheme: (scheme: DemoAuthScheme) => Promise<boolean>;
  copyAuthScheme: (id: string) => void;
  toggleAuthScheme: (id: string) => void;
  deleteAuthScheme: (id: string) => void;
  saveAuthInstance: (instance: DemoAuthInstance) => Promise<boolean>;
  toggleAuthInstance: (id: string) => void;
  addConnection: (integration: string, endUser: string, credentials?: Record<string, unknown>) => Promise<boolean>;
  startOAuth: (integration: string, endUser: string) => Promise<boolean>;
  addToken: (name: string, actions?: string[], connections?: string[]) => void;
  revokeToken: (id: string) => void;
  run: (kind: DemoOperation["kind"], name: string, integration?: string) => void;
  notify: (message: string | Error) => void;
  reset: () => void;
}

export function DemoProvider({ children }: { children: ReactNode }) {
  const authEpoch = useRef(0);
  const [platformName, setPlatformName] = useState("APIHub");
  const [integrations, setIntegrations] = useState<DemoIntegration[]>([]);
  const [connections, setConnections] = useState<DemoConnection[]>([]);
  const [tokens, setTokens] = useState<DemoToken[]>([]);
  const [operations, setOperations] = useState<DemoOperation[]>([]);
  const [customSystems, setCustomSystems] = useState<DemoSystem[]>([]);
  const [systemGroups, setSystemGroups] = useState<string[]>([]);
  const [authSchemes, setAuthSchemes] = useState<DemoAuthScheme[]>([]);
  const [authInstances, setAuthInstances] = useState<DemoAuthInstance[]>([]);
  const [groupIDs, setGroupIDs] = useState<Record<string, string>>({});
  const [systemIDs, setSystemIDs] = useState<Record<string, string>>({});
  const [templateIDs, setTemplateIDs] = useState<Record<string, string>>({});
  const [noticeError, setNoticeError] = useState(false);
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(true);
  const [authenticated, setAuthenticated] = useState(false);
  const [user, setUser] = useState<AdminSession | null>(null);
  const [backendError, setBackendError] = useState("");

  function notify(message: string | Error) {
    setNoticeError(
      message instanceof Error ||
        /失败|错误|无效|不能|没有|请选择|必须|过期|无法|请先|尚未|请填写/i.test(String(message)),
    );
    setNotice(message instanceof Error ? message.message : message);
  }

  const reload = useCallback(async () => {
    const epoch = authEpoch.current;
    setLoading(true);
    setBackendError("");
    try {
      const failures: string[] = [];
      async function resource<T>(path: string, promise: Promise<T>, fallback: T): Promise<T> {
        try {
          return await promise;
        } catch (error) {
          if (error instanceof ApiError && error.status === 401) throw error;
          failures.push(error instanceof Error ? error.message : path);
          return queryClient.getQueryData<T>([path]) ?? fallback;
        }
      }
      const [meta, groups, systems, templates, instances, integrationRows, connectionRows, tokenRows, operationRows] =
        await Promise.all([
          resource("/api/meta", api.meta(), { name: "APIHub" } as Awaited<ReturnType<typeof api.meta>>),
          resource("/api/system-groups", api.systemGroups(), []),
          resource(
            "/api/systems",
            api.lookups().then((items) => items.systems),
            [],
          ),
          resource("/api/auth-templates", api.authTemplates(), []),
          resource("/api/auth-instances", api.authInstances(), []),
          resource("/api/integrations", api.integrations(), []),
          resource(
            "/api/connections",
            api.lookups().then((items) => items.connections),
            [],
          ),
          resource("/api/runtime-tokens", api.runtimeTokens(), []),
          resource("/api/operations", api.operations(), []),
        ]);
      if (epoch !== authEpoch.current) return;
      if (failures.length) setBackendError(failures.join("；"));
      setPlatformName(meta.name || "APIHub");
      setAuthenticated(true);
      setGroupIDs(Object.fromEntries(groups.map((item) => [item.name, item.id])));
      setSystemGroups(groups.map((item) => item.name));
      setSystemIDs(Object.fromEntries(systems.map((item) => [item.systemKey, item.id])));
      setTemplateIDs(Object.fromEntries(templates.map((item) => [item.templateKey, item.id])));
      setCustomSystems(
        systems.map((item) => ({
          id: item.id,
          service: item.systemKey,
          name: item.name,
          source: item.source,
          groupId: item.groupId,
          letter: item.name.slice(0, 2).toUpperCase(),
          category: item.groupName || "未分组",
          actions: item.actionCount,
          executable: item.executableCount,
          connections: item.connectionCount,
          description: item.description,
          authTemplateIds: item.authTemplateIds.map(
            (id: string) => templates.find((template) => template.id === id)?.templateKey ?? id,
          ),
        })),
      );
      setAuthSchemes(
        templates
          .filter((item) => item.source === "custom")
          .map((item) => {
            const schema = objectValue(item.credentialSchema);
            const rules = arrayValue(item.injectionRules);
            const token = objectValue(item.tokenRequest);
            return {
              id: item.templateKey,
              name: item.name,
              flow: flowLabel(item.flowType),
              status: item.status,
              credentialFields: arrayValue(schema.fields).map((field) => ({
                name: String(field.name ?? ""),
                label: String(field.label ?? field.name ?? ""),
                secret: Boolean(field.secret),
              })),
              injectionRules: rules.map((rule) => ({
                target: rule.target === "query" ? "查询参数" : rule.target === "cookie" ? "Cookie" : "请求头",
                name: String(rule.name ?? ""),
                template: String(rule.template ?? ""),
              })),
              tokenEndpoint: "",
              refreshEndpoint: "",
              tokenPath: String(token.tokenPath ?? ""),
              expiryPath: String(token.expiryPath ?? ""),
              updatedAt: formatRelative(item.updatedAt),
            };
          }),
      );
      setAuthInstances(
        instances.map((item) => ({
          id: item.id,
          instanceKey: item.instanceKey,
          name: item.name,
          templateId: item.authTemplateKey,
          systemIds: [item.systemKey],
          status: item.status,
          tokenEndpoint: item.tokenUrl ?? "",
          refreshEndpoint: item.refreshUrl ?? "",
          tokenPath: item.tokenPath ?? "",
          expiryPath: item.expiryPath ?? "",
          headerName: item.headerName ?? "",
          headerValueTemplate: item.headerValueTemplate ?? "",
          authorizationUrl: objectValue(item.publicConfig).authorizationUrl as string | undefined,
          scopes: stringArray(objectValue(item.publicConfig).scopes),
          publicConfig: objectValue(item.publicConfig),
          version: item.version,
          updatedAt: formatRelative(item.updatedAt),
        })),
      );
      setIntegrations(
        integrationRows.map((item) => ({
          id: item.id,
          name: item.integrationKey,
          provider: item.systemKey,
          systemId: item.systemId,
          displayName: item.systemName,
          baseUrl: item.baseUrl,
          authInstanceId: item.authInstanceId,
          status: item.status,
          settings: objectValue(item.settings),
          updatedAt: formatRelative(item.updatedAt),
        })),
      );
      setConnections(
        connectionRows.map((item) => ({
          id: item.id,
          name: item.connectionKey,
          integration: item.integrationKey,
          provider: item.systemKey,
          endUser: item.endUserKey,
          authInstanceId: item.authInstanceId,
          status: ["active", "pending", "error", "expired"].includes(item.status) ? item.status : "error",
          lastVerified: formatRelative(item.lastVerifiedAt),
          lastUsed: formatRelative(item.lastUsedAt),
          records: 0,
          tags: item.tags ?? [],
        })),
      );
      setTokens(
        tokenRows.map((item) => ({
          id: item.id,
          name: item.name,
          actions: item.allowedActions ?? [],
          blockedActions: item.blockedActions ?? [],
          connections: item.allowedConnections ?? [],
          status: item.status === "revoked" ? ("revoked" as const) : ("active" as const),
          lastUsed: formatRelative(item.lastUsedAt),
        })),
      );
      const integrationNames = new Map(integrationRows.map((item) => [item.id, item.integrationKey]));
      const connectionNames = new Map(connectionRows.map((item) => [item.id, item.connectionKey]));
      setOperations(
        operationRows.map((item) => ({
          id: item.id,
          kind: kindLabel(item.kind),
          name: item.name,
          integration: integrationNames.get(item.integrationId) ?? item.integrationId ?? "—",
          connection: connectionNames.get(item.connectionId) ?? item.connectionId ?? "—",
          status: item.status,
          duration: durationLabel(item.startedAt, item.completedAt),
          time: formatRelative(item.startedAt),
          messages: item.events?.map((event: { message: string }) => event.message) ?? [],
        })),
      );
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        setAuthenticated(false);
        setUser(null);
        setBackendError("登录会话已失效，请重新登录。");
      } else {
        setBackendError(error instanceof Error ? error.message : "无法连接后端服务");
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void api
      .session()
      .then((identity) => {
        setUser(identity);
        setAuthenticated(true);
        return reload();
      })
      .catch((error) => {
        setAuthenticated(false);
        setUser(null);
        setLoading(false);
        if (!(error instanceof ApiError && error.status === 401))
          setBackendError(error instanceof Error ? error.message : "无法连接后端服务");
      });
  }, [reload]);
  useEffect(() => {
    const unauthorized = () => {
      authEpoch.current++;
      setAuthenticated(false);
      setUser(null);
    };
    window.addEventListener("apihub:unauthorized", unauthorized);
    return () => window.removeEventListener("apihub:unauthorized", unauthorized);
  }, []);

  const value = useMemo<DemoContextValue>(
    () => ({
      platformName,
      integrations,
      connections,
      tokens,
      operations,
      customSystems,
      systemGroups,
      authSchemes,
      authInstances,
      notice,
      noticeError,
      dismissNotice: () => setNotice(""),
      loading,
      authenticated,
      user,
      backendError,
      reload,
      login: async (email, password) => {
        try {
          const identity = await api.login(email, password);
          setUser(identity);
          setAuthenticated(true);
          await reload();
          return true;
        } catch (error) {
          setAuthenticated(false);
          setUser(null);
          setBackendError(error instanceof Error ? error.message : "账号或密码错误");
          return false;
        }
      },
      logout: () => {
        authEpoch.current++;
        void api.logout().finally(() => {
          setAuthenticated(false);
          setUser(null);
          setIntegrations([]);
          setConnections([]);
          setOperations([]);
        });
      },
      addIntegration: (provider, name, authInstanceId, requestedDisplayName, baseUrl = "https://api.example.com") => {
        return api
          .saveIntegration({
            integrationKey: name,
            name: requestedDisplayName ? `${requestedDisplayName} · ${name}` : name,
            systemId: systemIDs[provider],
            authInstanceId,
            baseUrl,
            status: "ready",
            settings: {},
          })
          .then(() => reload())
          .then(() => notify(`集成配置 ${name} 已创建`))
          .then(() => true)
          .catch((error) => {
            notify(error);
            return false;
          });
      },
      updateIntegrationAuthInstance: (id, authInstanceId) => {
        const current = integrations.find((item) => item.id === id);
        if (!current) return;
        void api
          .saveIntegration(
            {
              integrationKey: current.name,
              name: current.name,
              systemId: systemIDs[current.provider],
              authInstanceId,
              baseUrl: current.baseUrl,
              status: current.status,
              settings: current.settings ?? {},
            },
            id,
          )
          .then(() => reload())
          .then(() => notify("集成使用的认证实例已更新"))
          .catch((error) => notify(error));
      },
      addSystem: (name, service, group, authTemplateId) => {
        return api
          .createSystem({
            systemKey: service.trim(),
            name: name.trim(),
            groupId: groupIDs[group],
            defaultAuthTemplateId: templateIDs[authTemplateId],
            description: "自定义企业内部系统，可继续配置认证方式和 API 操作。",
          })
          .then(() => reload())
          .then(() => notify(`企业系统 ${name.trim()} 已添加`))
          .then(() => true)
          .catch((error) => {
            notify(error);
            return false;
          });
      },
      addSystemGroup: (name) => {
        const next = name.trim();
        if (!next || systemGroups.includes(next)) return;
        void api
          .saveSystemGroup({ name: next, sortOrder: systemGroups.length * 10 })
          .then(() => reload())
          .then(() => notify(`系统分组 ${next} 已添加`))
          .catch((error) => notify(error));
      },
      saveAuthScheme: (scheme) => {
        const id = templateIDs[scheme.id] ?? "";
        return api
          .saveAuthTemplate(
            {
              templateKey: scheme.id,
              name: scheme.name,
              flowType: flowKey(scheme.flow),
              status: scheme.status,
              credentialSchema: { type: "object", fields: scheme.credentialFields },
              tokenRequest: {
                method: "POST",
                bodyType: scheme.flow === "表单换取令牌" ? "form" : "json",
              },
              injectionRules: scheme.injectionRules.map((rule) => ({
                target: injectionTargetKey(rule.target),
                name: rule.name,
                template: rule.template,
              })),
            },
            id,
          )
          .then(() => reload())
          .then(() => notify(`认证模板 ${scheme.name} 已保存`))
          .then(() => true)
          .catch((error) => {
            notify(error);
            return false;
          });
      },
      copyAuthScheme: (id) => {
        const source = authSchemes.find((item) => item.id === id);
        if (source) {
          const copy = {
            ...source,
            id: `${source.id}-copy-${Date.now()}`,
            name: `${source.name} 副本`,
            status: "draft" as const,
          };
          const body = {
            templateKey: copy.id,
            name: copy.name,
            flowType: flowKey(copy.flow),
            status: copy.status,
            credentialSchema: { type: "object", fields: copy.credentialFields },
            tokenRequest: {
              method: "POST",
              bodyType: copy.flow === "表单换取令牌" ? "form" : "json",
            },
            injectionRules: copy.injectionRules.map((rule) => ({
              ...rule,
              target: injectionTargetKey(rule.target),
            })),
          };
          void api
            .saveAuthTemplate(body)
            .then(() => reload())
            .then(() => notify("认证模板已复制为草稿"))
            .catch((error) => notify(error));
        }
      },
      toggleAuthScheme: (id) => {
        const scheme = authSchemes.find((item) => item.id === id);
        if (scheme)
          void api
            .setAuthTemplateStatus(templateIDs[id], scheme.status === "published" ? "disabled" : "published")
            .then(() => reload())
            .then(() => notify("认证模板状态已更新"))
            .catch((error) => notify(error));
      },
      deleteAuthScheme: (id) => {
        const scheme = authSchemes.find((item) => item.id === id);
        if (!scheme || authInstances.some((item) => item.templateId === id)) {
          notify("认证模板已有实例，不能删除");
          return;
        }
        void api
          .deleteAuthTemplate(templateIDs[id])
          .then(() => reload())
          .then(() => notify(`认证模板 ${scheme.name} 已删除`))
          .catch((error) => notify(error));
      },
      saveAuthInstance: (instance) => {
        const editing = Boolean(instance.id && authInstances.some((item) => item.id === instance.id));
        return api
          .saveAuthInstance(
            {
              instanceKey: instance.instanceKey ?? instance.id,
              name: instance.name,
              systemId: systemIDs[instance.systemIds[0]],
              authTemplateId: templateIDs[instance.templateId],
              status: instance.status,
              tokenUrl: instance.tokenEndpoint,
              refreshUrl: instance.refreshEndpoint,
              tokenPath: instance.tokenPath,
              expiryPath: instance.expiryPath,
              headerName: instance.headerName,
              headerValueTemplate: instance.headerValueTemplate,
              publicConfig: {
                ...authInstances.find((item) => item.id === instance.id)?.publicConfig,
                ...instance.publicConfig,
                authorizationUrl: instance.authorizationUrl ?? "",
                scopes: instance.scopes ?? [],
              },
              version: instance.version ?? authInstances.find((item) => item.id === instance.id)?.version,
              secrets: Object.fromEntries(
                Object.entries({ clientId: instance.oauthClientId, clientSecret: instance.oauthClientSecret }).filter(
                  ([, value]) => Boolean(value),
                ),
              ),
            },
            editing ? instance.id : "",
          )
          .then(() => reload())
          .then(() => notify(`认证实例 ${instance.name} 已保存`))
          .then(() => true)
          .catch((error) => {
            notify(error);
            return false;
          });
      },
      toggleAuthInstance: (id) => {
        const instance = authInstances.find((item) => item.id === id);
        if (instance) {
          const updated = { ...instance, status: instance.status === "ready" ? "disabled" : ("ready" as const) };
          const editing = true;
          void api
            .saveAuthInstance(
              {
                instanceKey: instance.instanceKey ?? instance.id,
                name: instance.name,
                systemId: systemIDs[instance.systemIds[0]],
                authTemplateId: templateIDs[instance.templateId],
                status: updated.status,
                tokenUrl: instance.tokenEndpoint,
                refreshUrl: instance.refreshEndpoint,
                tokenPath: instance.tokenPath,
                expiryPath: instance.expiryPath,
                headerName: instance.headerName,
                headerValueTemplate: instance.headerValueTemplate,
                publicConfig: {
                  ...authInstances.find((item) => item.id === instance.id)?.publicConfig,
                  ...instance.publicConfig,
                  authorizationUrl: instance.authorizationUrl ?? "",
                  scopes: instance.scopes ?? [],
                },
                version: instance.version ?? authInstances.find((item) => item.id === instance.id)?.version,
                secrets: {},
              },
              editing ? instance.id : "",
            )
            .then(() => reload())
            .then(() => notify("认证实例状态已更新"))
            .catch((error) => notify(error));
        }
      },
      addConnection: (integration, endUser, credentials = { apiKey: "" }) => {
        const selected = integrations.find((item) => item.name === integration);
        if (!selected) return Promise.resolve(false);
        const connectionKey = `${selected.provider}-${Date.now().toString(36)}`;
        return api
          .saveConnection({
            integrationId: selected.id,
            name: connectionKey,
            credentials,
            connectionKey,
            endUserKey: endUser,
          })
          .then((created) => api.verifyConnection(created.id))
          .then(() => reload())
          .then(() => notify("认证配置解析完成，连接账号已创建"))
          .then(() => true)
          .catch((error) => {
            notify(error);
            return false;
          });
      },
      startOAuth: (integration, endUser) => {
        const selected = integrations.find((item) => item.name === integration);
        if (!selected) return Promise.resolve(false);
        const connectionKey = `${selected.provider}-${Date.now().toString(36)}`;
        return api
          .oauthStart({
            integrationId: selected.id,
            endUserKey: endUser,
            connectionKey,
            connectionName: connectionKey,
            returnPath: "/connections",
          })
          .then((result) => {
            window.location.assign(result.authorizationUrl);
          })
          .then(() => true)
          .catch((error) => {
            notify(error);
            return false;
          });
      },
      addToken: (name, actions = ["*"], allowedConnections = []) => {
        void api
          .createRuntimeToken({ name, allowedActions: actions, blockedActions: [], allowedConnections })
          .then(() => reload())
          .then(() => notify("运行时令牌已创建，明文仅显示一次"))
          .catch((error) => notify(error));
      },
      revokeToken: (id) => {
        void api
          .revokeRuntimeToken(id)
          .then(() => reload())
          .then(() => notify("运行时令牌已撤销"))
          .catch((error) => notify(error));
      },
      run: (kind, name, _integration = "github-production") => {
        if (kind === "Sync") {
          void api
            .syncTasks()
            .then((tasks) => {
              const task = tasks.find((item) => item.name === name || item.taskKey === name);
              if (!task) throw new Error("未找到同步任务");
              return api.runSyncTask(task.id);
            })
            .then(() => reload())
            .then(() => notify(`同步任务 ${name} 已进入队列`))
            .catch((error) => notify(error));
        } else notify(`${operationKindLabel(kind)} 请在对应页面执行真实操作`);
      },
      notify,
      reset: () => {
        void queryClient
          .invalidateQueries()
          .then(() => reload())
          .then(() => notify("已从后端重新加载数据"));
      },
    }),
    [
      authenticated,
      user,
      authInstances,
      authSchemes,
      backendError,
      connections,
      customSystems,
      groupIDs,
      integrations,
      loading,
      notice,
      noticeError,
      operations,
      platformName,
      reload,
      systemGroups,
      systemIDs,
      templateIDs,
      tokens,
    ],
  );

  return (
    <PermissionContext.Provider value={user?.role}>
      <DemoContext.Provider value={value}>{children}</DemoContext.Provider>
    </PermissionContext.Provider>
  );
}

function objectValue(value: unknown): Record<string, any> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, any>) : {};
}
function arrayValue(value: unknown): Array<Record<string, any>> {
  return Array.isArray(value) ? value.filter((item) => item && typeof item === "object") : [];
}
export function formatRelative(value?: string | null): string {
  if (!value) return "尚未";
  const seconds = Math.max(0, (Date.now() - new Date(value).getTime()) / 1000);
  if (seconds < 60) return "刚刚";
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
  return `${Math.floor(seconds / 86400)} 天前`;
}
export function durationLabel(start?: string, end?: string): string {
  if (!start || !end) return "—";
  return `${Math.max(0, new Date(end).getTime() - new Date(start).getTime())} ms`;
}
export function kindLabel(value: string): DemoOperation["kind"] {
  return value === "sync" ? "Sync" : value === "webhook" ? "Webhook" : value === "auth" ? "Auth" : "Action";
}
function flowLabel(value: string): string {
  return value === "password_token"
    ? "两步换取令牌"
    : value === "oauth2_code"
      ? "浏览器跳转回调"
      : value === "client_credentials"
        ? "表单换取令牌"
        : "静态凭据";
}
function flowKey(value: string): string {
  return value === "两步换取令牌" ? "password_token" : value === "表单换取令牌" ? "client_credentials" : "static";
}

function injectionTargetKey(value: string): string {
  if (value === "请求头") return "header";
  if (value === "查询参数") return "query";
  if (value === "Cookie") return "cookie";
  return value;
}

export function useDemo() {
  const value = useContext(DemoContext);
  if (!value) throw new Error("useDemo must be used inside DemoProvider");
  return value;
}

export function operationKindLabel(kind: string): string {
  const labels: Record<string, string> = {
    Action: "操作",
    Sync: "同步",
    Webhook: "Webhook",
    Auth: "认证",
  };
  return labels[kind] ?? kind;
}

export function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    active: "正常",
    delivered: "已送达",
    deployed: "已部署",
    disabled: "已停用",
    draft: "草稿",
    ended: "已结束",
    expired: "已过期",
    error: "异常",
    failed: "失败",
    invited: "待接受",
    processing: "处理中",
    pending: "待验证",
    ready: "就绪",
    retrying: "重试中",
    revoked: "已撤销",
    running: "运行中",
    queued: "排队中",
    cancelled: "已取消",
    unknown: "结果未知",
    success: "成功",
  };
  return labels[status] ?? status;
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}
