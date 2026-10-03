import { AuthOptionsEditor, AuthRequestEditor } from "./auth-request-editor";
import {
  defaultAuthRequest,
  defaultAuthOptions,
  defaultVerificationRequest,
  normalizeAuthRequest,
  typedCredentials,
  type AuthRequest,
  type AuthRequestOptions,
  type AuthTestResult,
} from "../auth-request";
import { ApiError } from "../api";
import { ArrowRight, KeyRound, Plus } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useDemo, type DemoAuthInstance, type DemoAuthScheme } from "../demo";
import { DemoConnectionsPage } from "./connections";
import { useLanguage } from "../i18n";
import { Badge, Button, Card, Field, Modal, PageHeader, fieldClass } from "../ui";
import { api, type AdminAuthInstance } from "../api";
import { saveAccount, saveOrFindIntegration, validHttpUrl, oauthReturnPath } from "./connection-create";

import {
  isExecutableAuthTemplate,
  ConnectionCredentialFields,
  credentialFields as accountCredentialFields,
  isRedirectAuth,
  type AuthMethod,
  authMethodCatalog,
  availableAuthMethods,
  authTemplateName,
  type CustomCredentialField,
  type CustomInjectionRule,
  Tabs,
  KeyValues,
  Th,
  Td,
} from "./core-shared";

function newKey(prefix: string) {
  return `${prefix}-${crypto.randomUUID().replaceAll("-", "").slice(0, 20)}`;
}

export function AuthMethodsPage() {
  const demo = useDemo();
  const navigate = useNavigate();
  const { t, language } = useLanguage();
  const [searchParams] = useSearchParams();
  const requestedSection = searchParams.get("section");
  const sectionNames: Record<string, string> = {
    templates: "认证方式",
    custom: "自定义模板",
    instances: "认证实例",
    accounts: "账号",
  };
  const [section, setSection] = useState(
    sectionNames[requestedSection ?? ""] ?? (demo.authInstances.length ? "认证实例" : "认证方式"),
  );
  const initialSection = useRef(false);
  const openedInstanceLink = useRef("");
  const [pending, setPending] = useState(false);
  useEffect(() => {
    if (demo.loading) return;
    const editId = searchParams.get("editInstance") ?? "";
    if (!editId) openedInstanceLink.current = "";
    const linkedInstance = demo.authInstances.find((item) => item.id === editId);
    if (linkedInstance && openedInstanceLink.current !== editId) {
      openedInstanceLink.current = editId;
      editAuthInstance(linkedInstance);
    }
    if (searchParams.get("create") !== "1") initialSection.current = false;
    if (searchParams.get("create") === "1" && !initialSection.current) {
      initialSection.current = true;
      setSection("认证实例");
      newAuthInstance();
    } else if (requestedSection) {
      setSection(sectionNames[requestedSection] ?? "认证方式");
    } else if (demo.authInstances.length) {
      setSection("认证实例");
    }
  }, [demo.loading, demo.authInstances.length, searchParams]);

  function selectSection(value: string) {
    setSection(value);
    const params = new URLSearchParams(searchParams);
    params.set("section", Object.keys(sectionNames).find((key) => sectionNames[key] === value) ?? "templates");
    for (const key of ["create", "new", "oauth", "connectionId", "editInstance"]) params.delete(key);
    navigate(`/auth?${params}`);
  }
  const [selected, setSelected] = useState<AuthMethod | null>(null);
  const [showSchemeEditor, setShowSchemeEditor] = useState(false);
  const [showInstanceEditor, setShowInstanceEditor] = useState(false);
  const [editingSchemeId, setEditingSchemeId] = useState<string | null>(null);
  const [editingInstanceId, setEditingInstanceId] = useState<string | null>(null);
  const [customId, setCustomId] = useState("enterprise-ticket-auth");
  const [customName, setCustomName] = useState("票据认证");
  const [tokenRequest, setTokenRequest] = useState<AuthRequest>(defaultAuthRequest);
  const [requestOptions, setRequestOptions] = useState<AuthRequestOptions | null>(null);
  const [instanceSecrets, setInstanceSecrets] = useState<Record<string, string>>({});
  const testEpoch = useRef(0);
  const [testOpen, setTestOpen] = useState(false);
  const [testCredentials, setTestCredentials] = useState<Record<string, string>>({});
  const [testResult, setTestResult] = useState<AuthTestResult | null>(null);
  const [testError, setTestError] = useState("");
  const [testPending, setTestPending] = useState(false);
  const [testIntegration, setTestIntegration] = useState("");
  const [testBaseURL, setTestBaseURL] = useState("");
  const [verifyTestAPI, setVerifyTestAPI] = useState(false);
  const [flow, setFlow] = useState("静态凭据");
  const [credentialFields, setCredentialFields] = useState<CustomCredentialField[]>([
    { name: "api_token", label: "访问令牌", secret: true },
    { name: "tenant_id", label: "租户 ID", secret: false },
  ]);
  const [injectionRules, setInjectionRules] = useState<CustomInjectionRule[]>([
    { target: "请求头", name: "Authorization", template: "Bearer {{api_token}}" },
    { target: "请求头", name: "X-Tenant-ID", template: "{{tenant_id}}" },
  ]);
  const [instanceId, setInstanceId] = useState(() => newKey("auth"));
  const [instanceName, setInstanceName] = useState("新认证实例");
  const [instanceTemplateId, setInstanceTemplateId] = useState("oauth2");
  const [instanceStatus, setInstanceStatus] = useState<DemoAuthInstance["status"]>("ready");
  const [instanceSystemIds, setInstanceSystemIds] = useState<string[]>([searchParams.get("system") ?? "internal-api"]);
  const [instanceTokenEndpoint, setInstanceTokenEndpoint] = useState("");
  const [instanceRefreshEndpoint, setInstanceRefreshEndpoint] = useState("");
  const [instanceTokenPath, setInstanceTokenPath] = useState("$.access_token");
  const [instanceExpiryPath, setInstanceExpiryPath] = useState("$.expires_in");
  const [instanceHeaderName, setInstanceHeaderName] = useState("Authorization");
  const [instanceHeaderValue, setInstanceHeaderValue] = useState("Bearer {{access_token}}");
  const [instanceAuthorizationUrl, setInstanceAuthorizationUrl] = useState("");
  const [verificationPath, setVerificationPath] = useState("");
  const [instanceScopes, setInstanceScopes] = useState("");
  const [instanceOAuthClientId, setInstanceOAuthClientId] = useState("");
  const [instanceOAuthClientSecret, setInstanceOAuthClientSecret] = useState("");
  const [instanceOAuthPrivateKey, setInstanceOAuthPrivateKey] = useState("");
  const [instanceOAuthCertificate, setInstanceOAuthCertificate] = useState("");
  const [instanceOAuthTLSPrivateKey, setInstanceOAuthTLSPrivateKey] = useState("");
  const [advancedConfig, setAdvancedConfig] = useState<Record<string, string | boolean>>({});
  const [apiBaseUrl, setApiBaseUrl] = useState("");
  const [integrationKey, setIntegrationKey] = useState("");
  const [accountName, setAccountName] = useState("服务账号");
  const [connectionKey, setConnectionKey] = useState("");
  const [externalEndUserKey, setExternalEndUserKey] = useState("");
  const [connectionCredentials, setConnectionCredentials] = useState<Record<string, string>>({});
  const [connectionCredentialsChanged, setConnectionCredentialsChanged] = useState(false);
  const [savedInstanceId, setSavedInstanceId] = useState("");
  const [instanceConfigChanged, setInstanceConfigChanged] = useState(false);
  const [savedInstanceVersion, setSavedInstanceVersion] = useState(0);
  const [savedIntegrationId, setSavedIntegrationId] = useState("");
  const [savedConnectionId, setSavedConnectionId] = useState("");
  const [savedConnectionRevision, setSavedConnectionRevision] = useState(0);
  const [accountError, setAccountError] = useState("");
  const methodExecutable = (id: string) => isExecutableAuthTemplate(id, demo.authSchemes, demo.supportedAuthFlows);
  const capabilitiesLoading = demo.loading;
  const configText = (key: string) => String(advancedConfig[key] ?? "");
  const setConfigText = (key: string, value: string) => setAdvancedConfig((current) => ({ ...current, [key]: value }));
  const systems = demo.customSystems;
  const instanceScheme = demo.authSchemes.find((scheme) => scheme.id === instanceTemplateId);
  const instanceMethod = authMethodCatalog.find((method) => method.id === instanceTemplateId);
  const instanceUsesTokenEndpoint =
    instanceTemplateId === "oauth2" ||
    instanceTemplateId === "oidc" ||
    instanceTemplateId === "jwt-bearer-grant" ||
    instanceTemplateId === "oauth2-token-exchange" ||
    instanceTemplateId === "username-password-token" ||
    instanceTemplateId === "client-credentials" ||
    (instanceScheme != null && instanceScheme.flow !== "静态凭据");

  function newCustomScheme() {
    setShowSchemeEditor(true);
    setEditingSchemeId(null);
    setCustomId(`custom-auth-${demo.authSchemes.length + 1}`);
    setTokenRequest(defaultAuthRequest());
    setCustomName("新认证模板");
    setFlow("静态凭据");
    setCredentialFields([{ name: "api_token", label: "访问令牌", secret: true }]);
    setInjectionRules([{ target: "请求头", name: "Authorization", template: "Bearer {{api_token}}" }]);
  }

  function editCustomScheme(scheme: DemoAuthScheme) {
    setShowSchemeEditor(true);
    setEditingSchemeId(scheme.id);
    setCustomId(scheme.id);
    setCustomName(scheme.name);
    setFlow(scheme.flow);
    setTokenRequest(normalizeAuthRequest(scheme.tokenRequest));
    setCredentialFields(scheme.credentialFields.map((field) => ({ ...field })));
    setInjectionRules(scheme.injectionRules.map((rule) => ({ ...rule })));
  }

  async function saveCustomScheme() {
    const name = customName.trim();
    const id = customId.trim();
    if (!name || !id) return;
    const current = demo.authSchemes.find((scheme) => scheme.id === editingSchemeId);
    const saved = await demo.saveAuthScheme({
      id,
      name,
      flow,
      status: current?.status ?? "draft",
      credentialFields,
      tokenRequest:
        flow === "表单换取令牌"
          ? { ...tokenRequest, schemaVersion: 2, method: "POST", bodyType: "form" }
          : tokenRequest,
      version: current?.version,
      injectionRules,
      tokenEndpoint: "",
      refreshEndpoint: "",
      tokenPath: "",
      expiryPath: "",
      updatedAt: "刚刚",
    });
    if (!saved) return;
    setEditingSchemeId(id);
    setShowSchemeEditor(false);
  }

  function selectInstanceTemplate(templateId: string) {
    if (
      showInstanceEditor &&
      templateId !== instanceTemplateId &&
      (editingInstanceId || instanceConfigChanged) &&
      !window.confirm("切换认证模板会重置请求配置和凭据输入，确认切换？")
    )
      return;
    const scheme = demo.authSchemes.find((item) => item.id === templateId);
    const headerInjection = scheme?.injectionRules.find((rule) => rule.target === "请求头");
    setAdvancedConfig((current) => {
      const next = { ...current };
      delete next.authRequest;
      return next;
    });
    setRequestOptions(
      templateId === "username-password-token" || scheme?.flow === "两步换取令牌" ? defaultAuthOptions() : null,
    );
    setInstanceSecrets({});
    testEpoch.current++;
    setTestPending(false);
    setTestOpen(false);
    setTestCredentials({});
    setTestResult(null);
    setTestError("");
    setInstanceTemplateId(templateId);
    setConnectionCredentials({});
    setConnectionCredentialsChanged(false);
    setAccountError("");
    setInstanceOAuthClientId("");
    setInstanceOAuthClientSecret("");
    setInstanceOAuthPrivateKey("");
    setInstanceOAuthCertificate("");
    setInstanceOAuthTLSPrivateKey("");
    setInstanceTokenEndpoint(templateId === "username-password-token" ? "https://erp.corp.example/api/login" : "");
    setInstanceRefreshEndpoint("");
    setInstanceTokenPath(
      ["oauth2", "oidc", "client-credentials", "jwt-bearer-grant", "oauth2-token-exchange"].includes(templateId)
        ? "$.access_token"
        : templateId === "username-password-token"
          ? "$.data.token"
          : scheme && scheme.flow !== "静态凭据"
            ? "$.data.access_token"
            : "",
    );
    setInstanceExpiryPath(
      ["oauth2", "oidc", "client-credentials", "jwt-bearer-grant", "oauth2-token-exchange"].includes(templateId)
        ? "$.expires_in"
        : templateId === "username-password-token"
          ? "$.data.expires_in"
          : scheme && scheme.flow !== "静态凭据"
            ? "$.data.expires_in"
            : "",
    );
    const builtinHeaderValues: Record<string, string> = {
      oauth2: "Bearer {{access_token}}",
      oidc: "Bearer {{access_token}}",
      "jwt-bearer-grant": "Bearer {{access_token}}",
      "oauth2-token-exchange": "Bearer {{access_token}}",
      api_key: "Bearer {{apiKey}}",
      basic: "Basic {{basic_token}}",
      "username-password-token": "Bearer {{token}}",
      "client-credentials": "Bearer {{access_token}}",
    };
    setInstanceHeaderName(
      headerInjection?.name ??
        (scheme || ["no_auth", "mtls", "aws-sigv4", "jwt-direct"].includes(templateId) ? "" : "Authorization"),
    );
    setInstanceHeaderValue(headerInjection?.template ?? builtinHeaderValues[templateId] ?? "");
    setInstanceSystemIds((items) =>
      items.filter((systemId) =>
        systems.some((system) => system.service === systemId && system.authTemplateIds.includes(templateId)),
      ),
    );
  }

  function newAuthInstance(templateId?: string) {
    if (capabilitiesLoading) return;
    const requestedSystem = systems.find((system) => system.service === searchParams.get("system"));
    const preferredTemplateId = requestedSystem?.defaultAuthTemplateId;
    const resolvedTemplateId =
      templateId ??
      (preferredTemplateId && methodExecutable(preferredTemplateId) ? preferredTemplateId : undefined) ??
      requestedSystem?.authTemplateIds.find(methodExecutable) ??
      "oauth2";
    if (!methodExecutable(resolvedTemplateId)) {
      demo.notify("请先发布可执行的认证模板，再添加可用实例");
      return;
    }
    setSection("认证实例");
    setShowInstanceEditor(true);
    setEditingInstanceId(null);
    setInstanceId(newKey("auth"));
    setInstanceName("新认证实例");
    setInstanceStatus("ready");
    setInstanceAuthorizationUrl("");
    setInstanceScopes("");
    setInstanceOAuthClientId("");
    setVerificationPath("");
    setInstanceOAuthClientSecret("");
    setInstanceOAuthPrivateKey("");
    setInstanceOAuthCertificate("");
    setInstanceOAuthTLSPrivateKey("");
    setAdvancedConfig({});
    setApiBaseUrl("");
    setIntegrationKey(newKey("integration"));
    setAccountName("服务账号");
    setConnectionKey(newKey("account"));
    setExternalEndUserKey("");
    setConnectionCredentials({});
    setConnectionCredentialsChanged(false);
    setSavedInstanceId("");
    setSavedInstanceVersion(0);
    setSavedIntegrationId("");
    setSavedConnectionId("");
    setSavedConnectionRevision(0);
    setAccountError("");
    const compatibleRequestedSystem = requestedSystem?.authTemplateIds.includes(resolvedTemplateId)
      ? requestedSystem
      : undefined;
    setInstanceSystemIds(compatibleRequestedSystem ? [compatibleRequestedSystem.service] : []);
    selectInstanceTemplate(resolvedTemplateId);
  }

  function editAuthInstance(instance: DemoAuthInstance) {
    setShowInstanceEditor(true);
    setEditingInstanceId(instance.id);
    setSavedInstanceId("");
    setSavedInstanceVersion(0);
    setSavedIntegrationId("");
    setSavedConnectionId("");
    setApiBaseUrl("");
    setAccountError("");
    setInstanceId(instance.instanceKey ?? instance.id);
    setInstanceName(instance.name);
    setInstanceTemplateId(instance.templateId);
    setInstanceStatus(instance.status);
    setInstanceSystemIds([...instance.systemIds]);
    setInstanceTokenEndpoint(instance.tokenEndpoint);
    setInstanceRefreshEndpoint(instance.refreshEndpoint);
    setInstanceTokenPath(instance.tokenPath);
    setInstanceExpiryPath(instance.expiryPath);
    setInstanceHeaderName(instance.headerName);
    setInstanceHeaderValue(instance.headerValueTemplate);
    setInstanceAuthorizationUrl(instance.authorizationUrl ?? "");
    setInstanceScopes((instance.scopes ?? []).join(" "));
    setVerificationPath(String(instance.publicConfig?.verificationPath ?? ""));
    setInstanceOAuthClientId("");
    setInstanceOAuthClientSecret("");
    setInstanceOAuthPrivateKey("");
    setInstanceOAuthCertificate("");
    setInstanceOAuthTLSPrivateKey("");
    setAdvancedConfig((instance.publicConfig ?? {}) as Record<string, string | boolean>);
    setRequestOptions((instance.publicConfig?.authRequest as AuthRequestOptions | undefined) ?? null);
    const secretNames = new Set<string>();
    const options = instance.publicConfig?.authRequest as AuthRequestOptions | undefined;
    for (const request of [
      options?.requestOverride,
      options?.refresh.request,
      demo.authSchemes.find((item) => item.id === instance.templateId)?.tokenRequest,
    ])
      for (const value of [
        ...(request?.parameters ?? []).map((p) => p.value),
        ...(request?.headers ?? []).map((h) => h.value),
      ])
        if (value.source === "instance_secret" && value.name) secretNames.add(value.name);
    setInstanceSecrets(Object.fromEntries([...secretNames].map((name) => [name, ""])));
    testEpoch.current++;
    setTestPending(false);
    setTestOpen(false);
    setTestCredentials({});
    setTestResult(null);
    setTestError("");
  }

  function closeInstanceEditor() {
    setTestCredentials({});
    testEpoch.current++;
    setTestPending(false);
    setTestOpen(false);
    setInstanceSecrets({});
    setShowInstanceEditor(false);
    setConnectionCredentials({});
    setInstanceOAuthClientId("");
    setInstanceOAuthClientSecret("");
    setInstanceOAuthPrivateKey("");
    setInstanceOAuthCertificate("");
    setInstanceOAuthTLSPrivateKey("");
  }

  async function runCurrentTest(authenticate: boolean) {
    const epoch = ++testEpoch.current;
    setTestPending(true);
    setTestError("");
    setTestResult(null);
    try {
      const [templates, systems] = await Promise.all([api.authTemplates(), api.systems()]);
      const template = templates.find((item) => item.templateKey === instanceTemplateId);
      const system = systems.find((item) => item.systemKey === instanceSystemIds[0]);
      if (!template || !system) throw new Error("请选择认证模板和适用系统");
      const secrets = Object.fromEntries(
        Object.entries({
          ...instanceSecrets,
          clientId: instanceOAuthClientId,
          clientSecret: instanceOAuthClientSecret,
          oauth_private_key: instanceOAuthPrivateKey,
          certificate: instanceOAuthCertificate,
          tls_private_key: instanceOAuthTLSPrivateKey,
        }).filter(([, value]) => Boolean(value)),
      );
      const result = await api.checkAuthInstance(
        {
          instanceKey: instanceId,
          name: instanceName,
          systemId: system.id,
          authTemplateId: template.id,
          status: instanceStatus,
          tokenUrl: instanceTokenEndpoint,
          refreshUrl: instanceRefreshEndpoint,
          tokenPath: instanceTokenPath,
          expiryPath: instanceExpiryPath,
          headerName: instanceHeaderName,
          headerValueTemplate: instanceHeaderValue,
          version: editingInstanceId
            ? demo.authInstances.find((item) => item.id === editingInstanceId)?.version
            : undefined,
          publicConfig: {
            ...advancedConfig,
            authorizationUrl: instanceAuthorizationUrl,
            scopes: instanceScopes.split(/[ ,\n]+/).filter(Boolean),
            verificationPath,
            ...(requestOptions ? { authRequest: requestOptions } : {}),
          },
          secrets,
          authenticate,
          testCredentials: typedCredentials(instanceScheme?.credentialFields ?? [], testCredentials),
          verifyApi: authenticate && verifyTestAPI,
          testTarget: verifyTestAPI
            ? testIntegration
              ? { integrationId: testIntegration }
              : { baseUrl: testBaseURL }
            : {},
        },
        editingInstanceId ?? "",
      );
      if (epoch !== testEpoch.current) return;
      setTestResult(
        result.steps
          ? result
          : {
              ...result,
              steps: [
                { name: "configuration", status: "passed" },
                { name: "token_request", status: "skipped" },
                { name: "token_response", status: "skipped" },
                { name: "api_verification", status: "skipped" },
              ],
            },
      );
    } catch (error) {
      if (epoch !== testEpoch.current) return;
      if (error instanceof ApiError && error.details) setTestResult(error.details as AuthTestResult);
      const message = error instanceof Error ? error.message : String(error);
      setTestError(message);
      const field = message.includes("expiry")
        ? "expiryPath"
        : message.includes("token path")
          ? "tokenPath"
          : message.includes("authentication URL")
            ? "tokenUrl"
            : "";
      if (field) document.querySelector<HTMLInputElement>(`[data-auth-field="${field}"]`)?.focus();
    } finally {
      if (epoch === testEpoch.current) setTestPending(false);
    }
  }

  async function saveAuthInstance(createConnection = false) {
    if (pending) return;
    const id = instanceId.trim();
    const name = instanceName.trim();
    if (!/^[a-z0-9][a-z0-9._-]{0,99}$/.test(id) || !name || !instanceTemplateId || !instanceSystemIds[0]) {
      demo.notify("请填写实例名称、标识并选择所属系统");
      return;
    }
    if (instanceStatus === "ready" && !methodExecutable(instanceTemplateId)) {
      demo.notify("只有已发布且后端支持的认证模板才能创建可用实例");
      return;
    }
    if (createConnection && (editingInstanceId || instanceStatus !== "ready")) {
      demo.notify("编辑已有实例时，请从关联集成添加账号");
      return;
    }
    if (
      (instanceTemplateId === "oauth2" || instanceTemplateId === "oidc") &&
      (!instanceTokenEndpoint || !instanceAuthorizationUrl || (!editingInstanceId && !instanceOAuthClientId))
    ) {
      demo.notify("OAuth 2.0 实例需要授权地址、Token 地址和客户端 ID");
      return;
    }
    if (
      ![instanceTokenEndpoint, instanceRefreshEndpoint, instanceAuthorizationUrl].every(
        (value) => !value || validHttpUrl(value),
      )
    ) {
      demo.notify("Token 地址、刷新地址和授权地址必须使用 http(s):// 开头的完整 URL");
      return;
    }
    if (
      createConnection &&
      ["password_token", "client_credentials"].includes(
        instanceScheme?.flow ??
          (instanceTemplateId === "username-password-token"
            ? "password_token"
            : instanceTemplateId === "client-credentials"
              ? "client_credentials"
              : ""),
      ) &&
      (!instanceTokenEndpoint.trim() || !instanceTokenPath.trim())
    ) {
      demo.notify("请填写 Token 地址和 Token 提取路径");
      return;
    }
    const system = systems.find((item) => item.service === instanceSystemIds[0]);
    const fields = accountCredentialFields(instanceTemplateId, instanceScheme);
    const missing = fields.find((field) => field.required && !connectionCredentials[field.name]?.trim());
    if (
      createConnection &&
      (!system?.id ||
        !validHttpUrl(apiBaseUrl) ||
        !accountName.trim() ||
        !/^[a-z0-9][a-z0-9._-]{0,99}$/.test(integrationKey) ||
        !/^[a-z0-9][a-z0-9._-]{0,99}$/.test(connectionKey) ||
        externalEndUserKey.trim().length > 255 ||
        missing)
    ) {
      demo.notify(
        !system?.id
          ? "请选择系统"
          : !validHttpUrl(apiBaseUrl)
            ? "API 基础地址必须以 http(s):// 开头"
            : !accountName.trim()
              ? "请填写账号名称"
              : !/^[a-z0-9][a-z0-9._-]{0,99}$/.test(integrationKey)
                ? "集成 ID 只能包含小写字母、数字、点、下划线或连字符，且不能超过 100 个字符"
                : !/^[a-z0-9][a-z0-9._-]{0,99}$/.test(connectionKey)
                  ? "账号 ID 只能包含小写字母、数字、点、下划线或连字符，且不能超过 100 个字符"
                  : externalEndUserKey.trim().length > 255
                    ? "外部用户或租户 ID 不能超过 255 个字符"
                    : `请填写必填账号凭据：${missing?.label ?? ""}`,
      );
      return;
    }

    setPending(true);
    let currentInstanceId = savedInstanceId || editingInstanceId || "";
    let currentIntegrationId = savedIntegrationId;
    let currentConnectionId = savedConnectionId;
    try {
      let saved: AdminAuthInstance;
      try {
        saved =
          createConnection && savedInstanceId && !instanceConfigChanged
            ? { id: savedInstanceId, version: savedInstanceVersion }
            : await demo.saveAuthInstance(
                {
                  id: currentInstanceId,
                  instanceKey: id,
                  version: savedInstanceVersion || undefined,
                  name,
                  templateId: instanceTemplateId,
                  systemIds: instanceSystemIds,
                  status: instanceStatus,
                  tokenEndpoint: instanceTokenEndpoint,
                  refreshEndpoint: instanceRefreshEndpoint,
                  tokenPath: instanceTokenPath,
                  expiryPath: instanceExpiryPath,
                  headerName: instanceHeaderName,
                  headerValueTemplate: instanceHeaderValue,
                  publicConfig: {
                    ...advancedConfig,
                    verificationPath,
                    ...(requestOptions ? { authRequest: requestOptions } : {}),
                  },
                  secrets: instanceSecrets,
                  authorizationUrl: instanceAuthorizationUrl,
                  scopes: instanceScopes.split(/[ ,\n]+/).filter(Boolean),
                  oauthClientId: instanceOAuthClientId,
                  oauthClientSecret: instanceOAuthClientSecret,
                  oauthPrivateKey: instanceOAuthPrivateKey,
                  oauthCertificate: instanceOAuthCertificate,
                  oauthTLSPrivateKey: instanceOAuthTLSPrivateKey,
                  updatedAt: "刚刚",
                },
                false,
              );
      } catch (saveError) {
        if (currentInstanceId) {
          const current = (await api.authInstances()).find((item) => item.id === currentInstanceId);
          if (current && current.version !== savedInstanceVersion) {
            setSavedInstanceVersion(current.version);
            throw new Error("认证实例版本已变化，请确认配置后重试保存。");
          }
          throw saveError;
        }
        const existing = (await api.authInstances()).find(
          (item) =>
            item.instanceKey === id &&
            item.systemKey === system?.service &&
            item.authTemplateKey === instanceTemplateId &&
            item.name === name &&
            (item.tokenUrl ?? "") === instanceTokenEndpoint,
        );
        if (!existing || existing.authTemplateKey !== instanceTemplateId) throw saveError;
        saved = existing;
      }
      currentInstanceId = saved.id;
      setSavedInstanceId(saved.id);
      setSavedInstanceVersion(saved.version ?? 0);
      setInstanceConfigChanged(false);

      if (!createConnection) {
        const refreshed = await demo.reload();
        closeInstanceEditor();
        const target = searchParams.get("returnTo");
        if (target?.startsWith("/integrations?")) navigate(target);
        else demo.notify(refreshed ? "认证实例已保存" : "认证实例已保存，但部分列表刷新失败，请手动刷新");
        return;
      }

      const integration = savedIntegrationId
        ? { id: savedIntegrationId }
        : await saveOrFindIntegration({
            integrationKey,
            name: `${instanceName.trim()} API`,
            systemId: system!.id!,
            authInstanceId: currentInstanceId,
            baseUrl: apiBaseUrl.trim().replace(/\/$/, ""),
          });
      currentIntegrationId = integration.id;
      setSavedIntegrationId(integration.id);

      if (isRedirectAuth(instanceTemplateId)) {
        const returnPath = oauthReturnPath(
          integrationKey,
          connectionKey,
          accountName.trim(),
          externalEndUserKey.trim(),
        );
        const result = await api.oauthStart({
          integrationId: integration.id,
          endUserKey: externalEndUserKey.trim() || connectionKey,
          endUserName: accountName.trim(),
          connectionKey,
          connectionName: accountName.trim() || connectionKey,
          returnPath,
        });
        window.location.assign(result.authorizationUrl);
        return;
      }

      const connection = await saveAccount({
        integrationId: integration.id,
        connectionKey,
        name: accountName.trim(),
        endUserKey: externalEndUserKey.trim() || connectionKey,
        endUserName: accountName.trim(),
        credentials: typedCredentials(instanceScheme?.credentialFields ?? [], connectionCredentials),
        connectionId: savedConnectionId || undefined,
        revision: savedConnectionRevision || undefined,
        credentialsChanged: connectionCredentialsChanged,
        onRevision: setSavedConnectionRevision,
        onSaved: (item) => {
          currentConnectionId = item.id;
          setSavedConnectionId(item.id);
          setSavedConnectionRevision(item.revision);
          setConnectionCredentialsChanged(false);
        },
      });
      currentConnectionId = connection.id;
      const refreshed = await demo.reload();
      closeInstanceEditor();
      demo.notify(
        !refreshed
          ? "账号已保存，但部分数据刷新失败，请手动刷新"
          : connection.lastVerifiedAt
            ? "账号已保存，连接验证通过"
            : "账号已保存，尚未验证凭据是否有效",
      );
      navigate(
        `/auth?section=accounts&integration=${encodeURIComponent(integrationKey)}&connection=${encodeURIComponent(connection.id)}`,
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : "保存失败，请重试";
      setAccountError(message);
      demo.notify(
        currentConnectionId
          ? `账号已保存，验证失败，请检查凭据后重试：${message}`
          : currentIntegrationId
            ? `认证实例和集成已保存，可继续添加账号：${message}`
            : message,
      );
      if (currentInstanceId) setSavedInstanceId(currentInstanceId);
      if (currentIntegrationId) setSavedIntegrationId(currentIntegrationId);
    } finally {
      setPending(false);
    }
  }

  function testAuthInstance(id: string) {
    return api
      .testAuthInstance(id)
      .then((result) => demo.notify(result.message ?? "认证实例配置有效"))
      .then(() => demo.reload())
      .catch((error) => demo.notify(error));
  }

  return (
    <div className="grid gap-6">
      <PageHeader title="认证中心" description="管理认证模板、实例和账号。" />
      <details className="text-sm text-[var(--muted-text)]">
        <summary className="cursor-pointer">模板、实例和账号有什么区别？</summary>
        <p className="mt-2">模板定义认证规则，实例保存目标系统配置，账号保存各用户或服务账号的独立凭据。</p>
      </details>
      <Tabs
        value={section === "认证方式" || section === "自定义模板" ? "认证模板" : section}
        onChange={(value) => selectSection(value === "认证模板" ? "认证方式" : value)}
        items={["认证模板", "认证实例", "账号"]}
        labels={language === "en" ? { 认证模板: "Templates", 认证实例: "Instances" } : undefined}
      />
      {(section === "认证方式" || section === "自定义模板") && (
        <Tabs
          value={section === "认证方式" ? "内置模板" : "自定义模板"}
          onChange={(value) => selectSection(value === "内置模板" ? "认证方式" : "自定义模板")}
          items={["内置模板", "自定义模板"]}
        />
      )}
      {section === "账号" && <DemoConnectionsPage embedded />}
      {section === "认证方式" && (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {availableAuthMethods.map((method) => (
            <button key={method.id} onClick={() => setSelected(method)} className="text-left">
              <Card className="h-full p-5 transition hover:-translate-y-0.5 hover:border-blue-300 hover:shadow-md">
                <div className="flex items-start justify-between gap-3">
                  <span className="grid size-10 place-items-center rounded-xl bg-blue-50 text-blue-600 dark:bg-blue-950 dark:text-blue-300">
                    <KeyRound className="size-5" />
                  </span>
                  <Badge tone={capabilitiesLoading ? "neutral" : methodExecutable(method.id) ? "success" : "warning"}>
                    {capabilitiesLoading ? "正在确认…" : methodExecutable(method.id) ? "可直接使用" : "当前服务未启用"}
                  </Badge>
                </div>
                <h2 className="mt-4 font-bold">{method.name}</h2>
                <p className="mt-2 min-h-12 text-sm leading-6 text-[var(--muted-text)]">{method.summary}</p>
                <p className="mt-4 border-t border-[var(--border)] pt-4 text-xs leading-5 text-[var(--muted-text)]">
                  {method.lifecycle}
                </p>
              </Card>
            </button>
          ))}
        </div>
      )}
      {section === "自定义模板" && (
        <>
          {!showSchemeEditor && (
            <Card className="overflow-hidden">
              <div className="flex flex-col justify-between gap-3 border-b border-[var(--border)] p-5 sm:flex-row sm:items-center">
                <div>
                  <h2 className="font-bold">自定义认证模板</h2>
                  <p className="mt-1 text-sm text-[var(--muted-text)]">
                    模板只定义字段和执行规则；保存后请基于模板创建认证实例。
                  </p>
                </div>
                <Button write onClick={newCustomScheme}>
                  <Plus className="size-4" /> 新建认证模板
                </Button>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[980px] text-left text-sm">
                  <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
                    <tr>
                      <Th>模板名称</Th>
                      <Th>获取方式</Th>
                      <Th>注入位置</Th>
                      <Th>实例数量</Th>
                      <Th>状态</Th>
                      <Th>更新时间</Th>
                      <Th />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[var(--border)]">
                    {demo.authSchemes.map((scheme) => (
                      <tr key={scheme.id}>
                        <Td>
                          <p translate="no" className="font-semibold">
                            {scheme.name}
                          </p>
                          <code className="text-xs text-[var(--muted-text)]">{scheme.id}</code>
                        </Td>
                        <Td>{scheme.flow}</Td>
                        <Td>
                          {scheme.injectionRules[0]
                            ? `${scheme.injectionRules[0].target} · ${scheme.injectionRules[0].name}`
                            : "未配置"}
                        </Td>
                        <Td>{demo.authInstances.filter((instance) => instance.templateId === scheme.id).length}</Td>
                        <Td>
                          <Badge
                            tone={
                              scheme.status === "published"
                                ? "success"
                                : scheme.status === "draft"
                                  ? "warning"
                                  : "neutral"
                            }
                          >
                            {scheme.status === "published" ? "已发布" : scheme.status === "draft" ? "草稿" : "已停用"}
                          </Badge>
                        </Td>
                        <Td>{scheme.updatedAt}</Td>
                        <Td>
                          <div className="flex justify-end gap-1">
                            <Button write variant="ghost" onClick={() => editCustomScheme(scheme)}>
                              编辑
                            </Button>
                            <Button variant="ghost" onClick={() => demo.copyAuthScheme(scheme.id)}>
                              复制
                            </Button>
                            <Button
                              write
                              variant="ghost"
                              disabled={scheme.status !== "published"}
                              title={scheme.status === "published" ? "基于此模板添加实例" : "请先发布模板"}
                              onClick={() => newAuthInstance(scheme.id)}
                            >
                              添加实例
                            </Button>
                            <Button write variant="ghost" onClick={() => demo.toggleAuthScheme(scheme.id)}>
                              {scheme.status === "published" ? "停用" : "发布"}
                            </Button>
                            <Button
                              write
                              variant="ghost"
                              disabled={demo.authInstances.some((instance) => instance.templateId === scheme.id)}
                              title={
                                demo.authInstances.some((instance) => instance.templateId === scheme.id)
                                  ? "请先移除基于该模板的认证实例"
                                  : "删除模板"
                              }
                              onClick={() =>
                                window.confirm(`确认删除认证模板“${scheme.name}”吗？此操作无法撤销。`) &&
                                demo.deleteAuthScheme(scheme.id)
                              }
                            >
                              删除
                            </Button>
                          </div>
                        </Td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}
          {showSchemeEditor && (
            <div className="grid gap-5 xl:grid-cols-[1.35fr_0.65fr]">
              <Card className="grid gap-6 p-5">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <h2 className="font-bold">{editingSchemeId ? `编辑 ${customName}` : "新建认证模板"}</h2>
                    <p className="mt-1 text-sm leading-6 text-[var(--muted-text)]">
                      定义最终用户需要填写的凭据、如何换取令牌，以及请求发出前如何注入认证信息。
                    </p>
                  </div>
                  <Button className="shrink-0" variant="secondary" onClick={() => setShowSchemeEditor(false)}>
                    返回列表
                  </Button>
                </div>
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="模板 ID" hint="发布后建议保持稳定。">
                    <input
                      className={fieldClass}
                      value={customId}
                      disabled={Boolean(editingSchemeId)}
                      onChange={(event) => setCustomId(event.target.value)}
                    />
                  </Field>
                  <Field label="方案名称">
                    <input
                      className={fieldClass}
                      value={customName}
                      onChange={(event) => setCustomName(event.target.value)}
                    />
                  </Field>
                  <Field label="凭据获取方式">
                    <select
                      className={fieldClass}
                      value={flow}
                      onChange={(event) => {
                        const next = event.target.value;
                        setFlow(next);
                        if (
                          next !== "静态凭据" &&
                          !editingSchemeId &&
                          credentialFields.length === 1 &&
                          credentialFields[0].name === "api_token"
                        )
                          setCredentialFields(
                            next === "表单换取令牌"
                              ? [
                                  { name: "client_id", label: "客户端 ID", secret: false, required: true },
                                  { name: "client_secret", label: "客户端密钥", secret: true, required: true },
                                ]
                              : [
                                  { name: "username", label: "用户名", secret: false, required: true },
                                  { name: "password", label: "密码", secret: true, required: true },
                                ],
                          );
                        if (next !== "静态凭据")
                          setInjectionRules([
                            { target: "请求头", name: "Authorization", template: "Bearer {{token}}" },
                          ]);
                      }}
                    >
                      <option>静态凭据</option>
                      <option value="两步换取令牌">登录接口换 Token</option>
                      <option value="表单换取令牌">OAuth2 客户端凭据</option>
                    </select>
                  </Field>
                </div>
                {editingSchemeId && demo.authInstances.some((item) => item.templateId === editingSchemeId) && (
                  <p className="text-sm text-amber-700">
                    此模板被 {demo.authInstances.filter((item) => item.templateId === editingSchemeId).length}{" "}
                    个实例引用；保存后，下次认证将使用新配置，请重新测试。
                  </p>
                )}
                {flow === "两步换取令牌" && (
                  <section className="grid gap-3">
                    <h3 className="text-sm font-bold">如何获取 Token</h3>
                    <AuthRequestEditor value={tokenRequest} onChange={setTokenRequest} fields={credentialFields} />
                  </section>
                )}
                {flow === "表单换取令牌" && (
                  <p className="text-sm text-[var(--muted-text)]">OAuth2 客户端凭据使用协议规定的 POST 表单请求。</p>
                )}
                <div className="rounded-xl bg-blue-50 p-3 text-sm leading-6 text-blue-700 dark:bg-blue-950 dark:text-blue-300">
                  模板不直接绑定系统。请保存模板后创建认证实例，在实例中填写实际地址并选择适用系统。
                </div>
                <section className="grid gap-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <h3 className="text-sm font-bold">凭据字段</h3>
                      <p className="text-xs text-[var(--muted-text)]">决定账号界面向用户收集哪些值。</p>
                    </div>
                    <Button
                      write
                      type="button"
                      variant="secondary"
                      onClick={() =>
                        setCredentialFields((items) => [
                          ...items,
                          { name: `field_${items.length + 1}`, label: "新凭据字段", secret: true },
                        ])
                      }
                    >
                      <Plus className="size-4" /> 添加字段
                    </Button>
                  </div>
                  {credentialFields.map((field, index) => (
                    <div
                      className="grid gap-2 rounded-xl border border-[var(--border)] p-3 sm:grid-cols-2"
                      key={`${field.name}-${index}`}
                    >
                      <input
                        aria-label={`凭据字段 ${index + 1} 标识`}
                        className={fieldClass}
                        value={field.name}
                        onChange={(event) =>
                          setCredentialFields((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, name: event.target.value } : item,
                            ),
                          )
                        }
                      />
                      <input
                        aria-label={`凭据字段 ${index + 1} 标签`}
                        className={fieldClass}
                        value={field.label}
                        onChange={(event) =>
                          setCredentialFields((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, label: event.target.value } : item,
                            ),
                          )
                        }
                      />
                      <label className="flex items-center gap-2 whitespace-nowrap text-xs text-[var(--muted-text)]">
                        <input
                          type="checkbox"
                          checked={field.secret}
                          onChange={(event) =>
                            setCredentialFields((items) =>
                              items.map((item, itemIndex) =>
                                itemIndex === index ? { ...item, secret: event.target.checked } : item,
                              ),
                            )
                          }
                        />
                        敏感字段
                      </label>
                      <label className="flex items-center gap-2 text-xs">
                        <input
                          type="checkbox"
                          checked={field.required !== false}
                          onChange={(e) =>
                            setCredentialFields((items) =>
                              items.map((item, i) => (i === index ? { ...item, required: e.target.checked } : item)),
                            )
                          }
                        />
                        必填
                      </label>
                      <details>
                        <summary className="cursor-pointer text-xs">字段类型</summary>
                        <select
                          aria-label={`凭据字段 ${index + 1} 类型`}
                          className={fieldClass}
                          value={field.type ?? "string"}
                          onChange={(e) =>
                            setCredentialFields((items) =>
                              items.map((item, i) => (i === index ? { ...item, type: e.target.value } : item)),
                            )
                          }
                        >
                          <option value="string">文本</option>
                          <option value="number">数字</option>
                          <option value="boolean">布尔</option>
                        </select>
                      </details>
                      <Button
                        write
                        type="button"
                        variant="ghost"
                        disabled={credentialFields.length === 1}
                        onClick={() =>
                          setCredentialFields((items) => items.filter((_, itemIndex) => itemIndex !== index))
                        }
                      >
                        删除
                      </Button>
                    </div>
                  ))}
                </section>
                <section className="grid gap-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <h3 className="text-sm font-bold">请求注入规则</h3>
                      <p className="text-xs text-[var(--muted-text)]">仅允许受控模板写入请求头、查询参数或 Cookie。</p>
                    </div>
                    <Button
                      write
                      type="button"
                      variant="secondary"
                      onClick={() =>
                        setInjectionRules((items) => [
                          ...items,
                          { target: "请求头", name: `X-Custom-${items.length + 1}`, template: "{{api_token}}" },
                        ])
                      }
                    >
                      <Plus className="size-4" /> 添加规则
                    </Button>
                  </div>
                  {injectionRules.map((rule, index) => (
                    <div
                      className="grid gap-2 rounded-xl border border-[var(--border)] p-3 md:grid-cols-[8rem_1fr_1.4fr_auto]"
                      key={`${rule.name}-${index}`}
                    >
                      <select
                        aria-label={`注入规则 ${index + 1} 位置`}
                        className={fieldClass}
                        value={rule.target}
                        onChange={(event) =>
                          setInjectionRules((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, target: event.target.value } : item,
                            ),
                          )
                        }
                      >
                        <option>请求头</option>
                        <option>查询参数</option>
                        <option>Cookie</option>
                      </select>
                      <input
                        aria-label={`注入规则 ${index + 1} 名称`}
                        className={fieldClass}
                        value={rule.name}
                        onChange={(event) =>
                          setInjectionRules((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, name: event.target.value } : item,
                            ),
                          )
                        }
                      />
                      <input
                        aria-label={`注入规则 ${index + 1} 模板`}
                        className={fieldClass}
                        value={rule.template}
                        onChange={(event) =>
                          setInjectionRules((items) =>
                            items.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, template: event.target.value } : item,
                            ),
                          )
                        }
                      />
                      <Button
                        write
                        type="button"
                        variant="ghost"
                        disabled={injectionRules.length === 1}
                        onClick={() =>
                          setInjectionRules((items) => items.filter((_, itemIndex) => itemIndex !== index))
                        }
                      >
                        删除
                      </Button>
                    </div>
                  ))}
                </section>
                <div className="rounded-xl bg-amber-50 p-3 text-sm leading-6 text-amber-800 dark:bg-amber-950 dark:text-amber-200">
                  复杂加密算法、多阶段挑战或厂商 SDK 认证应实现为经过审核的 Go 认证扩展，不能在控制台执行任意脚本。
                </div>
                <div className="flex justify-end gap-2">
                  <Button variant="ghost" onClick={newCustomScheme}>
                    清空
                  </Button>
                  <Button write onClick={saveCustomScheme}>
                    {editingSchemeId ? "保存修改" : "保存为草稿"}
                  </Button>
                </div>
              </Card>
              <div className="grid content-start gap-4">
                <Card className="p-5">
                  <h2 className="font-bold">运行时执行顺序</h2>
                  <ol className="mt-4 grid gap-3 text-sm text-[var(--muted-text)]">
                    {["校验用户输入", "读取加密凭据", "换取或刷新令牌", "注入请求并脱敏日志"].map((item, index) => (
                      <li className="flex items-center gap-3" key={item}>
                        <span className="grid size-7 shrink-0 place-items-center rounded-full bg-blue-50 text-xs font-bold text-blue-700 dark:bg-blue-950 dark:text-blue-300">
                          {index + 1}
                        </span>
                        {item}
                      </li>
                    ))}
                  </ol>
                </Card>
                <Card className="p-5">
                  <h2 className="font-bold">模板概况</h2>
                  <div className="mt-3 grid gap-3 text-sm">
                    <KeyValues
                      items={[
                        ["模板总数", demo.authSchemes.length],
                        ["已发布", demo.authSchemes.filter((scheme) => scheme.status === "published").length],
                        ["草稿", demo.authSchemes.filter((scheme) => scheme.status === "draft").length],
                        ["已停用", demo.authSchemes.filter((scheme) => scheme.status === "disabled").length],
                      ]}
                    />
                  </div>
                </Card>
              </div>
            </div>
          )}
        </>
      )}
      {section === "认证实例" && (
        <>
          {!showInstanceEditor && (
            <Card className="overflow-hidden">
              <div className="flex flex-col justify-between gap-3 border-b border-[var(--border)] p-5 sm:flex-row sm:items-center">
                <div>
                  <h2 className="font-bold">认证实例</h2>
                </div>
                <Button write disabled={capabilitiesLoading} onClick={() => newAuthInstance()}>
                  <Plus className="size-4" /> 添加认证实例
                </Button>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[1080px] text-left text-sm">
                  <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
                    <tr>
                      <Th>实例名称</Th>
                      <Th>认证模板</Th>
                      <Th>绑定系统</Th>
                      <Th>Token 配置</Th>
                      <Th>状态</Th>
                      <Th />
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[var(--border)]">
                    {demo.authInstances.map((instance) => (
                      <tr key={instance.id}>
                        <Td>
                          <p translate="no" className="font-semibold">
                            {instance.name}
                          </p>
                          <code className="text-xs text-[var(--muted-text)]">
                            {instance.instanceKey ?? instance.id}
                          </code>
                        </Td>
                        <Td>{authTemplateName(instance.templateId, demo.authSchemes)}</Td>
                        <Td>
                          {systems
                            .filter((system) => instance.systemIds.includes(system.service))
                            .map((system) => system.name)
                            .join("、") || "未绑定"}
                        </Td>
                        <Td>
                          <p className="max-w-56 truncate">{instance.tokenEndpoint || "无需交换令牌"}</p>
                          <code className="text-xs text-[var(--muted-text)]">{instance.tokenPath || "—"}</code>
                        </Td>
                        <Td>
                          <Badge
                            tone={
                              instance.status === "ready"
                                ? "success"
                                : instance.status === "draft"
                                  ? "warning"
                                  : "neutral"
                            }
                          >
                            {instance.status === "ready" ? "可用" : instance.status === "draft" ? "草稿" : "已停用"}
                          </Badge>
                        </Td>
                        <Td>
                          <div className="flex justify-end gap-1">
                            <Button
                              aria-label={`检查配置 ${instance.name}`}
                              variant="ghost"
                              onClick={() => testAuthInstance(instance.id)}
                            >
                              检查配置
                            </Button>
                            <Button asChild variant="ghost">
                              <Link to={`/auth?section=accounts&authInstance=${encodeURIComponent(instance.id)}`}>
                                管理账号
                              </Link>
                            </Button>
                            <Button
                              write
                              variant="ghost"
                              onClick={() => {
                                const linked = demo.integrations.filter((item) => item.authInstanceId === instance.id);
                                if (linked.length === 1) {
                                  navigate(
                                    `/auth?section=accounts&new=1&integration=${encodeURIComponent(linked[0].name)}`,
                                  );
                                } else {
                                  navigate(
                                    `/auth?section=accounts&new=1&authInstance=${encodeURIComponent(instance.id)}`,
                                  );
                                }
                              }}
                            >
                              添加账号
                            </Button>
                            <Button
                              write
                              aria-label={`编辑 ${instance.name}`}
                              variant="ghost"
                              onClick={() => editAuthInstance(instance)}
                            >
                              编辑
                            </Button>
                            <Button
                              write
                              aria-label={`${instance.status === "ready" ? "停用" : "启用"} ${instance.name}`}
                              variant="ghost"
                              onClick={() => demo.toggleAuthInstance(instance.id)}
                            >
                              {instance.status === "ready" ? "停用" : "启用"}
                            </Button>
                          </div>
                        </Td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}
          {showInstanceEditor && (
            <div className="grid gap-5 xl:grid-cols-[1.35fr_0.65fr]">
              <form
                className="min-w-0"
                onSubmit={(event) => {
                  event.preventDefault();
                  void saveAuthInstance();
                }}
              >
                <Card className="grid gap-6 p-5">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <h2 className="font-bold">{editingInstanceId ? `编辑 ${instanceName}` : "添加认证实例"}</h2>
                      <p className="mt-1 text-sm leading-6 text-[var(--muted-text)]">
                        选择模板，填写实际认证端点，再绑定与该模板兼容的系统。
                      </p>
                    </div>
                    <Button type="button" variant="secondary" disabled={pending} onClick={closeInstanceEditor}>
                      返回实例列表
                    </Button>
                  </div>
                  <fieldset
                    className="grid gap-6 min-w-0"
                    disabled={pending}
                    onChange={() => setInstanceConfigChanged(true)}
                  >
                    <div className="grid gap-4 sm:grid-cols-2">
                      <Field label="实例名称">
                        <input
                          className={fieldClass}
                          value={instanceName}
                          onChange={(event) => setInstanceName(event.target.value)}
                        />
                      </Field>
                      <Field label="认证模板">
                        <select
                          className={fieldClass}
                          value={instanceTemplateId}
                          disabled={Boolean(savedInstanceId)}
                          onChange={(event) => selectInstanceTemplate(event.target.value)}
                        >
                          <optgroup label={t("内置认证模板")}>
                            {editingInstanceId &&
                              !availableAuthMethods.some((method) => method.id === instanceTemplateId) &&
                              !demo.authSchemes.some((scheme) => scheme.id === instanceTemplateId) && (
                                <option value={instanceTemplateId} disabled>
                                  {authTemplateName(instanceTemplateId, demo.authSchemes)}（旧实例）
                                </option>
                              )}
                            {availableAuthMethods.map((method) => (
                              <option key={method.id} value={method.id}>
                                {method.name}
                                {capabilitiesLoading
                                  ? "（能力检查中）"
                                  : methodExecutable(method.id)
                                    ? ""
                                    : "（当前服务未启用）"}
                              </option>
                            ))}
                          </optgroup>
                          <optgroup label={t("自定义模板")}>
                            {demo.authSchemes.map((scheme) => (
                              <option key={scheme.id} value={scheme.id} disabled={scheme.status !== "published"}>
                                {scheme.name} · {scheme.status === "published" ? "已发布" : "未发布"}
                              </option>
                            ))}
                          </optgroup>
                        </select>
                      </Field>
                      <Field label="实例状态">
                        <select
                          className={fieldClass}
                          value={instanceStatus}
                          onChange={(event) => setInstanceStatus(event.target.value as DemoAuthInstance["status"])}
                        >
                          <option value="ready">可用</option>
                          <option value="draft">草稿</option>
                          <option value="disabled">已停用</option>
                        </select>
                      </Field>
                    </div>
                    <Field label="所属系统" hint="一个认证实例只保存一个目标系统的实际端点和应用配置。">
                      <select
                        className={fieldClass}
                        value={instanceSystemIds[0] ?? ""}
                        disabled={Boolean(savedInstanceId)}
                        onChange={(event) => setInstanceSystemIds(event.target.value ? [event.target.value] : [])}
                        required
                      >
                        <option value="">选择系统</option>
                        {systems
                          .filter((system) => system.authTemplateIds.includes(instanceTemplateId))
                          .map((system) => (
                            <option key={system.service} value={system.service}>
                              {system.name} · {system.category}
                            </option>
                          ))}
                      </select>
                    </Field>
                    <section className="grid gap-4 sm:grid-cols-2">
                      {(instanceTemplateId === "oauth2" || instanceTemplateId === "oidc") && (
                        <>
                          <Field label="授权地址" hint="用户授权跳转地址，必须是 http(s):// 开头的完整 URL。">
                            <input
                              className={fieldClass}
                              value={instanceAuthorizationUrl}
                              onChange={(event) => setInstanceAuthorizationUrl(event.target.value)}
                              placeholder="https://provider.example.com/oauth/authorize"
                            />
                          </Field>
                          <Field label="权限范围" hint="多个 Scope 使用空格分隔。">
                            <input
                              className={fieldClass}
                              value={instanceScopes}
                              onChange={(event) => setInstanceScopes(event.target.value)}
                              placeholder="openid profile api.read"
                            />
                          </Field>
                          <Field label="客户端 ID" hint={editingInstanceId ? "留空表示不修改" : undefined}>
                            <input
                              className={fieldClass}
                              value={instanceOAuthClientId}
                              onChange={(event) => setInstanceOAuthClientId(event.target.value)}
                            />
                          </Field>
                          <Field label="客户端密钥" hint="加密保存；编辑时留空表示不修改。">
                            <input
                              type="password"
                              className={fieldClass}
                              value={instanceOAuthClientSecret}
                              onChange={(event) => setInstanceOAuthClientSecret(event.target.value)}
                            />
                          </Field>
                        </>
                      )}
                      {["client-credentials", "oauth2-token-exchange"].includes(instanceTemplateId) && (
                        <>
                          <Field label="客户端 ID" hint="Token Exchange 可留空，使用无客户端认证的端点。">
                            <input
                              className={fieldClass}
                              value={instanceOAuthClientId}
                              onChange={(event) => setInstanceOAuthClientId(event.target.value)}
                            />
                          </Field>
                          <Field label="客户端密钥" hint="加密保存；编辑时留空表示不修改。">
                            <input
                              type="password"
                              className={fieldClass}
                              value={instanceOAuthClientSecret}
                              onChange={(event) => setInstanceOAuthClientSecret(event.target.value)}
                            />
                          </Field>
                        </>
                      )}
                      {["oauth2", "oidc", "client-credentials", "oauth2-token-exchange"].includes(
                        instanceTemplateId,
                      ) && (
                        <>
                          <Field label="令牌端点客户端认证方式">
                            <select
                              className={fieldClass}
                              value={configText("clientAuthMethod")}
                              onChange={(event) => setConfigText("clientAuthMethod", event.target.value)}
                            >
                              <option value="">客户端密钥放在表单中（兼容现有配置）</option>
                              <option value="client_secret_basic">HTTP Basic</option>
                              <option value="private_key_jwt">私钥 JWT</option>
                              <option value="tls_client_auth">mTLS 客户端认证</option>
                            </select>
                          </Field>
                          {configText("clientAuthMethod") === "private_key_jwt" && (
                            <Field label="客户端私钥 PEM" hint="加密保存；编辑时留空表示保留原私钥。">
                              <textarea
                                className={`${fieldClass} min-h-28`}
                                value={instanceOAuthPrivateKey}
                                onChange={(event) => setInstanceOAuthPrivateKey(event.target.value)}
                              />
                            </Field>
                          )}
                        </>
                      )}
                      {instanceTemplateId === "oidc" && (
                        <Field label="OIDC Issuer HTTPS 地址">
                          <input
                            className={fieldClass}
                            value={configText("issuer")}
                            onChange={(event) => setConfigText("issuer", event.target.value)}
                            placeholder="https://id.example.com"
                          />
                        </Field>
                      )}
                      {["jwt-direct", "jwt-bearer-grant"].includes(instanceTemplateId) && (
                        <>
                          <Field label="Issuer">
                            <input
                              className={fieldClass}
                              value={configText("issuer")}
                              onChange={(event) => setConfigText("issuer", event.target.value)}
                            />
                          </Field>
                          <Field label="Subject">
                            <input
                              className={fieldClass}
                              value={configText("subject")}
                              onChange={(event) => setConfigText("subject", event.target.value)}
                            />
                          </Field>
                          <Field label="Audience">
                            <input
                              className={fieldClass}
                              value={configText("audience")}
                              onChange={(event) => setConfigText("audience", event.target.value)}
                            />
                          </Field>
                          <Field label="Key ID">
                            <input
                              className={fieldClass}
                              value={configText("keyId")}
                              onChange={(event) => setConfigText("keyId", event.target.value)}
                            />
                          </Field>
                        </>
                      )}
                      {instanceTemplateId === "aws-sigv4" && (
                        <>
                          <Field label="AWS 区域">
                            <input
                              className={fieldClass}
                              value={configText("region")}
                              onChange={(event) => setConfigText("region", event.target.value)}
                              placeholder="ap-southeast-1"
                            />
                          </Field>
                          <Field label="AWS 服务名">
                            <input
                              className={fieldClass}
                              value={configText("service")}
                              onChange={(event) => setConfigText("service", event.target.value)}
                              placeholder="execute-api"
                            />
                          </Field>
                        </>
                      )}
                      {instanceTemplateId === "oauth2-token-exchange" && (
                        <>
                          <Field label="Subject Token 类型">
                            <select
                              className={fieldClass}
                              value={configText("subjectTokenType")}
                              onChange={(event) => setConfigText("subjectTokenType", event.target.value)}
                            >
                              <option value="">选择 Token 类型</option>
                              <option value="urn:ietf:params:oauth:token-type:access_token">Access Token</option>
                              <option value="urn:ietf:params:oauth:token-type:jwt">JWT</option>
                              <option value="urn:ietf:params:oauth:token-type:saml2">
                                外部已签发的 SAML 2.0 Assertion
                              </option>
                            </select>
                          </Field>
                          <Field label="请求的 Token 类型">
                            <select
                              className={fieldClass}
                              value={configText("requestedTokenType")}
                              onChange={(event) => setConfigText("requestedTokenType", event.target.value)}
                            >
                              <option value="">由令牌端点决定</option>
                              <option value="urn:ietf:params:oauth:token-type:access_token">Access Token</option>
                              <option value="urn:ietf:params:oauth:token-type:jwt">JWT</option>
                            </select>
                          </Field>
                          <Field label="目标 Audience">
                            <input
                              className={fieldClass}
                              value={configText("audience")}
                              onChange={(event) => setConfigText("audience", event.target.value)}
                            />
                          </Field>
                          <Field label="目标 Resource">
                            <input
                              className={fieldClass}
                              value={configText("resource")}
                              onChange={(event) => setConfigText("resource", event.target.value)}
                            />
                          </Field>
                        </>
                      )}
                      {["jwt-bearer-grant", "client-credentials", "oauth2-token-exchange"].includes(
                        instanceTemplateId,
                      ) && (
                        <Field label="Scope">
                          <input
                            className={fieldClass}
                            value={configText("scope")}
                            onChange={(event) => setConfigText("scope", event.target.value)}
                          />
                        </Field>
                      )}
                      {["mtls", "oauth2", "oidc", "client-credentials", "oauth2-token-exchange"].includes(
                        instanceTemplateId,
                      ) && (
                        <>
                          <Field label="额外信任的 CA 证书 PEM">
                            <textarea
                              className={`${fieldClass} min-h-28`}
                              value={configText("caCertificate")}
                              onChange={(event) => setConfigText("caCertificate", event.target.value)}
                            />
                          </Field>
                          {instanceTemplateId !== "mtls" && (
                            <>
                              <Field label="Token 端点客户端证书 PEM" hint="加密保存；编辑时留空表示保留原证书。">
                                <textarea
                                  className={`${fieldClass} min-h-28`}
                                  value={instanceOAuthCertificate}
                                  onChange={(event) => setInstanceOAuthCertificate(event.target.value)}
                                />
                              </Field>
                              <Field label="Token 端点客户端私钥 PEM" hint="加密保存；编辑时留空表示保留原私钥。">
                                <textarea
                                  className={`${fieldClass} min-h-28`}
                                  value={instanceOAuthTLSPrivateKey}
                                  onChange={(event) => setInstanceOAuthTLSPrivateKey(event.target.value)}
                                />
                              </Field>
                              <label className="flex items-center gap-2 text-sm">
                                <input
                                  type="checkbox"
                                  checked={Boolean(advancedConfig.tlsForToken)}
                                  onChange={(event) =>
                                    setAdvancedConfig((current) => ({ ...current, tlsForToken: event.target.checked }))
                                  }
                                />
                                Token 端点使用客户端证书
                              </label>
                              <label className="flex items-center gap-2 text-sm">
                                <input
                                  type="checkbox"
                                  checked={Boolean(advancedConfig.tlsForApi)}
                                  onChange={(event) =>
                                    setAdvancedConfig((current) => ({ ...current, tlsForApi: event.target.checked }))
                                  }
                                />
                                API 请求使用客户端证书
                              </label>
                            </>
                          )}
                        </>
                      )}
                      {instanceUsesTokenEndpoint && (
                        <>
                          <Field
                            label="认证 / Token 地址"
                            hint="该系统实例的登录或换取令牌地址，必须是 http(s):// 开头的完整 URL，内网地址可用 http。"
                          >
                            <input
                              className={fieldClass}
                              data-auth-field="tokenUrl"
                              value={instanceTokenEndpoint}
                              onChange={(event) => setInstanceTokenEndpoint(event.target.value)}
                              placeholder="https://system.example.com/api/login"
                            />
                          </Field>
                          {(!requestOptions || requestOptions.refresh.mode === "refresh_token") && (
                            <Field
                              label="刷新地址"
                              hint="可选，同样要求 http(s):// 完整 URL；OAuth 未填写时复用 Token 地址。"
                            >
                              <input
                                className={fieldClass}
                                value={instanceRefreshEndpoint}
                                onChange={(event) => setInstanceRefreshEndpoint(event.target.value)}
                              />
                            </Field>
                          )}
                          <Field label="访问令牌 JSON 路径">
                            <input
                              className={fieldClass}
                              data-auth-field="tokenPath"
                              value={instanceTokenPath}
                              onChange={(event) => setInstanceTokenPath(event.target.value)}
                              placeholder="$.data.token"
                            />
                          </Field>
                          {(!requestOptions || requestOptions.response.expiry.mode === "field") && (
                            <Field label="过期时间 JSON 路径">
                              <input
                                className={fieldClass}
                                data-auth-field="expiryPath"
                                value={instanceExpiryPath}
                                onChange={(event) => setInstanceExpiryPath(event.target.value)}
                                placeholder="$.data.expires_in"
                              />
                            </Field>
                          )}
                        </>
                      )}
                      {!requestOptions && (
                        <Field
                          label="凭据验证路径"
                          hint="可选。保存后使用 GET 请求验证，例如 /me；留空仅检查配置，不标记为已验证。"
                        >
                          <input
                            className={fieldClass}
                            value={verificationPath}
                            onChange={(event) => setVerificationPath(event.target.value)}
                            placeholder="/me"
                          />
                        </Field>
                      )}
                      {instanceTemplateId !== "no_auth" && requestOptions?.injectionOverride == null && (
                        <>
                          <Field label="Header 名称">
                            <input
                              className={fieldClass}
                              value={instanceHeaderName}
                              onChange={(event) => setInstanceHeaderName(event.target.value)}
                              placeholder="Authorization"
                            />
                          </Field>
                          <Field label="Header 值模板">
                            <input
                              className={fieldClass}
                              value={instanceHeaderValue}
                              onChange={(event) => setInstanceHeaderValue(event.target.value)}
                              placeholder="Bearer {{token}}"
                            />
                          </Field>
                        </>
                      )}
                    </section>
                  </fieldset>
                  <section className="grid min-w-0 gap-3">
                    {!requestOptions && (
                      <p className="text-sm text-[var(--muted-text)]">
                        {instanceScheme?.tokenRequest?.method ?? "POST"} ·{" "}
                        {instanceScheme?.tokenRequest?.bodyType ?? "协议默认"} · 继承模板
                      </p>
                    )}
                    {!requestOptions && (
                      <Button
                        type="button"
                        variant="secondary"
                        onClick={() => {
                          const options = defaultAuthOptions();
                          if (
                            instanceTemplateId !== "username-password-token" &&
                            instanceScheme?.flow !== "两步换取令牌"
                          )
                            options.response.expiry = { mode: "none" };
                          if (verificationPath) {
                            options.verification.enabled = true;
                            options.verification.request = defaultVerificationRequest();
                          }
                          setRequestOptions(options);
                          setInstanceConfigChanged(true);
                        }}
                      >
                        配置响应与业务验证
                      </Button>
                    )}
                    {requestOptions && (
                      <AuthOptionsEditor
                        value={requestOptions}
                        onChange={(options) => {
                          setRequestOptions(options);
                          setInstanceConfigChanged(true);
                        }}
                        templateRequest={normalizeAuthRequest(instanceScheme?.tokenRequest)}
                        fields={
                          instanceScheme?.credentialFields ??
                          accountCredentialFields(instanceTemplateId, instanceScheme)
                        }
                        login={
                          instanceTemplateId === "username-password-token" || instanceScheme?.flow === "两步换取令牌"
                        }
                        verificationPath={verificationPath}
                        onVerificationPath={setVerificationPath}
                      />
                    )}
                    {requestOptions && (
                      <details>
                        <summary className="cursor-pointer text-sm">实例固定密钥</summary>
                        <p className="mt-2 text-xs text-[var(--muted-text)]">
                          用于高级请求中的实例密钥引用；加密保存，编辑时留空保留原值。
                        </p>
                        {Object.keys(instanceSecrets).map((name) => (
                          <div className="mt-2 grid gap-2 sm:grid-cols-2" key={name}>
                            <span className="self-center break-all text-sm">{name}</span>
                            <input
                              aria-label={`实例密钥 ${name}`}
                              className={fieldClass}
                              type="password"
                              value={instanceSecrets[name]}
                              onChange={(e) =>
                                setInstanceSecrets((current) => ({ ...current, [name]: e.target.value }))
                              }
                            />
                          </div>
                        ))}
                        <Field label="新增密钥名称">
                          <input
                            className={fieldClass}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") {
                                e.preventDefault();
                                const name = e.currentTarget.value.trim();
                                if (/^[a-zA-Z0-9_]+$/.test(name)) {
                                  setInstanceSecrets((current) => ({ ...current, [name]: "" }));
                                  e.currentTarget.value = "";
                                }
                              }
                            }}
                            placeholder="输入名称后按 Enter"
                          />
                        </Field>
                      </details>
                    )}
                  </section>
                  <section className="grid gap-3 rounded-lg border border-[var(--border)] p-4">
                    <p className="font-medium">业务地址与账号</p>
                    <p className="text-sm text-[var(--muted-text)]">
                      业务 API 地址在集成中配置，账号凭据在账号页面管理。
                    </p>
                    {editingInstanceId &&
                      demo.integrations
                        .filter((item) => item.authInstanceId === editingInstanceId)
                        .map((item) => (
                          <Link
                            key={item.id}
                            className="break-all text-sm text-blue-600"
                            to={`/integrations?integration=${encodeURIComponent(item.id)}`}
                          >
                            {item.name} · {item.baseUrl}
                          </Link>
                        ))}
                    <details>
                      <summary className="cursor-pointer text-sm">实例标识</summary>
                      <input
                        aria-label="实例 ID"
                        className={fieldClass}
                        value={instanceId}
                        disabled={Boolean(editingInstanceId || savedInstanceId)}
                        onChange={(event) => setInstanceId(event.target.value)}
                      />
                    </details>
                  </section>
                  {testOpen && (
                    <section className="grid gap-3 rounded-lg border border-[var(--border)] p-4">
                      <h3 className="font-medium">测试当前配置</h3>
                      <ConnectionCredentialFields
                        auth={instanceTemplateId}
                        scheme={instanceScheme}
                        enforceRequired={false}
                        values={testCredentials}
                        onChange={(name, value) => setTestCredentials((current) => ({ ...current, [name]: value }))}
                      />
                      {requestOptions?.verification.enabled || (!requestOptions && verificationPath) ? (
                        <>
                          <label className="flex items-center gap-2 text-sm">
                            <input
                              type="checkbox"
                              checked={verifyTestAPI}
                              onChange={(e) => setVerifyTestAPI(e.target.checked)}
                            />
                            同时验证 API
                          </label>
                          {verifyTestAPI && (
                            <>
                              <select
                                aria-label="测试集成"
                                className={fieldClass}
                                value={testIntegration}
                                onChange={(e) => setTestIntegration(e.target.value)}
                              >
                                <option value="">使用下面的 API 基础地址</option>
                                {demo.integrations
                                  .filter((item) => item.authInstanceId === editingInstanceId)
                                  .map((item) => (
                                    <option value={item.id} key={item.id}>
                                      {item.name}
                                    </option>
                                  ))}
                              </select>
                              {!testIntegration && (
                                <input
                                  aria-label="测试 API 基础地址"
                                  className={fieldClass}
                                  value={testBaseURL}
                                  placeholder="https://api.example.com"
                                  onChange={(e) => setTestBaseURL(e.target.value)}
                                />
                              )}
                            </>
                          )}
                        </>
                      ) : (
                        <p className="text-sm text-[var(--muted-text)]">未配置业务验证</p>
                      )}
                      <div className="flex gap-2">
                        <Button type="button" disabled={testPending} onClick={() => runCurrentTest(true)}>
                          {testPending ? "正在测试…" : "开始测试"}
                        </Button>
                        <Button
                          type="button"
                          variant="secondary"
                          disabled={testPending}
                          onClick={() => runCurrentTest(false)}
                        >
                          仅检查结构
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          onClick={() => {
                            testEpoch.current++;
                            setTestPending(false);
                            setTestOpen(false);
                            setTestCredentials({});
                            setTestResult(null);
                            setTestError("");
                          }}
                        >
                          关闭测试
                        </Button>
                      </div>
                      {testResult && (
                        <ul className="grid gap-1 text-sm" aria-label="测试步骤">
                          {testResult.steps?.map((step) => (
                            <li key={step.name}>
                              {step.status === "passed" ? "✓" : step.status === "failed" ? "✗" : "—"}{" "}
                              {{
                                configuration: "配置有效",
                                token_request: "认证请求",
                                token_response: "Token 与有效期",
                                api_verification: "业务验证",
                              }[step.name] ?? step.name}{" "}
                              · {{ passed: "通过", failed: "失败", skipped: "未执行" }[step.status]}
                            </li>
                          ))}
                        </ul>
                      )}
                      {testError && (
                        <p role="alert" className="break-words text-sm text-red-600">
                          {testError}
                        </p>
                      )}
                    </section>
                  )}
                  <div className="flex flex-wrap justify-end gap-2">
                    <Button
                      type="button"
                      variant="secondary"
                      disabled={pending}
                      onClick={() => {
                        setTestOpen(true);
                        setTestBaseURL(apiBaseUrl);
                        setVerifyTestAPI(Boolean(requestOptions?.verification.enabled || verificationPath));
                      }}
                    >
                      测试
                    </Button>
                    <Button type="submit" write disabled={pending}>
                      {pending ? "保存中…" : editingInstanceId ? "保存修改" : "保存认证实例"}
                    </Button>
                  </div>
                </Card>
              </form>
              <div className="grid content-start gap-4">
                <Card className="p-5">
                  <h2 className="font-bold">当前模板</h2>
                  <div className="mt-3">
                    <KeyValues
                      items={[
                        ["模板", instanceMethod?.name ?? instanceScheme?.name ?? "未选择"],
                        ["认证流程", instanceMethod?.mode ?? instanceScheme?.flow ?? "—"],
                        [
                          "凭据字段",
                          instanceMethod?.fields.join("、") ??
                            instanceScheme?.credentialFields.map((field) => field.label).join("、") ??
                            "—",
                        ],
                        [
                          "注入规则",
                          instanceScheme?.injectionRules.map((rule) => `${rule.target} ${rule.name}`).join("、") ??
                            "由内置模板管理",
                        ],
                      ]}
                    />
                  </div>
                </Card>
              </div>
            </div>
          )}
        </>
      )}
      <Modal
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
        title={`认证模板 · ${selected?.name ?? "认证方式"}`}
        description={selected?.summary ?? "查看模板定义，并基于模板创建认证实例。"}
      >
        {selected && (
          <div className="grid gap-4">
            <KeyValues
              items={[
                ["运行模式", selected.mode],
                ["凭据生命周期", selected.lifecycle],
                ["配置字段", selected.fields.join("、")],
              ]}
            />
            {!capabilitiesLoading && !methodExecutable(selected.id) && (
              <div className="rounded-xl bg-amber-50 p-3 text-sm leading-6 text-amber-800 dark:bg-amber-950 dark:text-amber-200">
                当前服务未提供该认证能力，不能创建可用实例。
              </div>
            )}
            <div className="flex justify-end gap-2">
              <Button
                write
                disabled={capabilitiesLoading || !methodExecutable(selected.id)}
                onClick={() => {
                  newAuthInstance(selected.id);
                  setSelected(null);
                }}
              >
                使用此模板添加实例
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
