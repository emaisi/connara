import { typedCredentials } from "../auth-request";
import { useCanWrite } from "../permissions";
import { useResourcePages, PageControls } from "../pagination";
import { formatRelative, type DemoIntegration } from "../demo";
import { ArrowRight, CheckCircle2, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { statusLabel, useDemo } from "../demo";
import { Badge, Button, Card, Field, Modal, PageHeader, fieldClass, textAreaClass } from "../ui";
import { api } from "../api";
import {
  saveAccount,
  saveOrFindIntegration,
  validHttpUrl,
  oauthReturnPath,
  accountVerificationPath,
} from "./connection-create";

import {
  isExecutableAuthTemplate,
  connectionStatusTone,
  authTemplateName,
  authInstanceName,
  isRedirectAuth,
  ConnectionCredentialFields,
  credentialFields,
  MiniStat,
  KeyValues,
  Th,
  Td,
} from "./core-shared";

function newAccountKey() {
  return `account-${crypto.randomUUID().replaceAll("-", "").slice(0, 20)}`;
}

export function DemoConnectionsPage({ embedded = false }: { embedded?: boolean }) {
  const demo = useDemo();
  const [searchParams, setSearchParams] = useSearchParams();
  const [connectionQuery, setConnectionQuery] = useState("");
  const requestedIntegration = demo.integrations.find((item) => item.name === searchParams.get("integration"));
  const connectionPage = useResourcePages(
    "/api/connections",
    {
      q: connectionQuery,
      integration: requestedIntegration?.id ?? "",
      system: searchParams.get("system") ?? "",
      status: searchParams.get("status") ?? "",
    },
    demo.authenticated,
  );
  const connections: typeof demo.connections = connectionPage.items
    .filter((item) => !searchParams.get("authInstance") || item.authInstanceId === searchParams.get("authInstance"))
    .map((item) => ({
      id: item.id,
      name: item.name || item.connectionKey,
      connectionKey: item.connectionKey,
      integration: item.integrationKey,
      provider: item.systemKey,
      endUser: item.endUserKey,
      authInstanceId: item.authInstanceId,
      status: item.enabled === false ? "disabled" : item.status,
      lastVerified: formatRelative(item.lastVerifiedAt),
      lastUsed: formatRelative(item.lastUsedAt),
      records: 0,
      tags: item.tags ?? [],
    }));
  const navigate = useNavigate();
  const canWrite = useCanWrite();
  const [connectOpen, setConnectOpen] = useState(canWrite && searchParams.get("new") === "1");
  const readyIntegrations = demo.integrations.filter((item) => item.status === "ready");
  const requestedAuthInstance = searchParams.get("authInstance") ?? "";
  const availableIntegrations = requestedAuthInstance
    ? readyIntegrations.filter((item) => item.authInstanceId === requestedAuthInstance)
    : readyIntegrations;
  const [integration, setIntegration] = useState(() => {
    const requested = searchParams.get("integration");
    if (requested) return requested;
    const authInstanceId = searchParams.get("authInstance");
    const matching = demo.integrations.filter(
      (item) => item.status === "ready" && (!authInstanceId || item.authInstanceId === authInstanceId),
    );
    return matching.length === 1 ? matching[0].name : "";
  });
  const [verificationPending, setVerificationPending] = useState(false);
  const [verificationResult, setVerificationResult] = useState("");
  const [verificationError, setVerificationError] = useState("");
  const [credentialPending, setCredentialPending] = useState(false);
  const [verificationApis, setVerificationApis] = useState<any[]>([]);
  const [verificationApiId, setVerificationApiId] = useState("");
  const [verificationInput, setVerificationInput] = useState("{}");
  const [createdIntegration, setCreatedIntegration] = useState<DemoIntegration | null>(null);
  const [apiBaseUrl, setApiBaseUrl] = useState("");
  const [integrationKey] = useState(() => `integration-${crypto.randomUUID().replaceAll("-", "").slice(0, 20)}`);
  const [accountName, setAccountName] = useState(searchParams.get("accountName") || "服务账号");
  const [connectionKey, setConnectionKey] = useState(() => searchParams.get("connectionKey") || newAccountKey());
  const [externalEndUserKey, setExternalEndUserKey] = useState(searchParams.get("endUserKey") || "");
  const [savedConnectionId, setSavedConnectionId] = useState("");
  const [savedConnectionRevision, setSavedConnectionRevision] = useState(0);
  const [credentialsChanged, setCredentialsChanged] = useState(false);
  const [connectionSubmitted, setConnectionSubmitted] = useState(false);
  const [pending, setPending] = useState(false);
  const [credentials, setCredentials] = useState<Record<string, string>>({});
  const [updatingCredentials, setUpdatingCredentials] = useState(false);
  const [replacementCredentials, setReplacementCredentials] = useState<Record<string, string>>({});
  const [selected, setSelected] = useState<(typeof demo.connections)[number] | null>(null);
  useEffect(() => {
    setVerificationResult("");
    setVerificationError("");
    setUpdatingCredentials(false);
    setReplacementCredentials({});
    setVerificationApiId("");
    setVerificationInput("{}");
    setVerificationApis([]);
    if (!selected) return;
    let cancelled = false;
    void api
      .actions()
      .then((rows) => {
        if (!cancelled) setVerificationApis(rows.filter((row) => row.systemKey === selected.provider));
      })
      .catch((error) => demo.notify(error));
    return () => {
      cancelled = true;
    };
  }, [selected]);
  const selectedIntegration = createdIntegration ?? availableIntegrations.find((item) => item.name === integration);
  const selectedAuthInstance = demo.authInstances.find(
    (instance) => instance.id === (selectedIntegration?.authInstanceId ?? requestedAuthInstance),
  );
  const canCreateIntegration = Boolean(
    requestedAuthInstance && selectedAuthInstance?.status === "ready" && !availableIntegrations.length,
  );
  const targetSystem = demo.customSystems?.find((item) => item.service === selectedAuthInstance?.systemIds?.[0]);
  const selectedAuthScheme = demo.authSchemes.find((scheme) => scheme.id === selectedAuthInstance?.templateId);
  const selectedAuthName = selectedAuthInstance
    ? authTemplateName(selectedAuthInstance.templateId, demo.authSchemes)
    : "";
  const selectedAuthExecutable = Boolean(
    selectedAuthInstance?.status === "ready" &&
    isExecutableAuthTemplate(selectedAuthInstance.templateId, demo.authSchemes, demo.supportedAuthFlows),
  );
  const selectedDetails = demo.connections.find((item) => item.id === selected?.id) ?? selected;
  const openedAuthInstance = demo.authInstances.find((instance) => instance.id === selected?.authInstanceId);
  const openedAuthName = openedAuthInstance ? authTemplateName(openedAuthInstance.templateId, demo.authSchemes) : "";
  const defaultVerificationPath = accountVerificationPath(openedAuthInstance?.publicConfig);
  const createVerificationPath = accountVerificationPath(selectedAuthInstance?.publicConfig);
  const canVerify = Boolean(verificationApiId || defaultVerificationPath);
  const openedIntegration = demo.integrations.find((item) => item.name === selected?.integration);
  async function verifyAccount(id: string) {
    if (verificationPending || !canVerify) return;
    setVerificationPending(true);
    setVerificationResult("");
    setVerificationError("");
    try {
      const result = await api.verifyConnection(
        id,
        verificationApiId ? { actionId: verificationApiId, input: JSON.parse(verificationInput) } : undefined,
      );
      setVerificationResult(
        result.verified
          ? "验证通过：账号已通过所选接口验证。"
          : "账号已保存，待验证：本次未完成上游验证，请检查认证实例的验证设置。",
      );
      await Promise.all([demo.reload(), connectionPage.refetch()]);
    } catch (error) {
      setVerificationError(error instanceof Error ? error.message : "验证失败，请重试");
    } finally {
      setVerificationPending(false);
    }
  }
  async function saveCredentials(verifyAfterSave: boolean) {
    if (!selected || credentialPending || verificationPending) return;
    const current = connectionPage.items.find((item) => item.id === selected.id);
    if (!current) {
      setVerificationError("账号数据未加载，请刷新后重试");
      return;
    }
    const scheme = demo.authSchemes.find((scheme) => scheme.id === openedAuthInstance?.templateId);
    if (
      credentialFields(openedAuthInstance?.templateId ?? "", scheme).some(
        (field) => field.required && !replacementCredentials[field.name]?.trim(),
      )
    ) {
      demo.notify("请填写全部必填凭据");
      return;
    }
    setCredentialPending(true);
    setVerificationResult("");
    setVerificationError("");
    try {
      await api.updateConnection(selected.id, {
        connectionKey: current.connectionKey,
        name: current.name,
        integrationId: current.integrationId,
        endUserKey: current.endUserKey,
        endUserName: current.endUserName,
        endUserEmail: current.endUserEmail,
        metadata: current.metadata,
        tags: current.tags,
        revision: current.revision,
        credentials: typedCredentials(scheme?.credentialFields ?? [], replacementCredentials),
      });
      setUpdatingCredentials(false);
      setReplacementCredentials({});
      setVerificationResult("凭据已保存，待验证。请点击验证账号。");
      await Promise.all([connectionPage.refetch(), demo.reload()]);
      if (verifyAfterSave) await verifyAccount(selected.id);
    } catch (error) {
      setVerificationError(error instanceof Error ? error.message : "更新失败");
    } finally {
      setCredentialPending(false);
    }
  }
  const redirectAuth = isRedirectAuth(selectedAuthInstance?.templateId ?? "");
  useEffect(() => {
    if (demo.loading) return;
    if (!integration && availableIntegrations.length === 1) setIntegration(availableIntegrations[0].name);
    const oauth = searchParams.get("oauth");
    if (oauth === "error") {
      demo.notify("上游授权未完成，可以返回此页重新授权");
      const cleanParams = new URLSearchParams(searchParams);
      cleanParams.delete("oauth");
      cleanParams.delete("connectionId");
      cleanParams.set("new", "1");
      setAccountName(searchParams.get("accountName") || "服务账号");
      setConnectionKey(searchParams.get("connectionKey") || connectionKey);
      setExternalEndUserKey(searchParams.get("endUserKey") || "");
      setIntegration(searchParams.get("integration") || "");
      setConnectOpen(canWrite);
      cleanParams.set("section", "accounts");
      navigate(`/auth?${cleanParams}`, { replace: true });
      return;
    }
    if (oauth !== "success") return;
    const connectionId = searchParams.get("connectionId");
    const connection = demo.connections.find((item) => item.id === connectionId);
    if (!connection) return;
    demo.notify("授权完成；尚未验证 API 权限");
    navigate(
      `/auth?section=accounts&integration=${encodeURIComponent(connection.integration)}&connection=${encodeURIComponent(connection.id)}`,
      { replace: true },
    );
  }, [demo.loading, demo.connections, integration, availableIntegrations.length, searchParams, navigate]);

  useEffect(() => {
    if (canWrite && searchParams.get("new") === "1") {
      setConnectOpen(true);
      if (searchParams.get("integration")) setIntegration(searchParams.get("integration")!);
    }
  }, [canWrite, searchParams]);

  useEffect(() => {
    const id = searchParams.get("connection");
    if (!id) return;
    const account = demo.connections.find((item) => item.id === id);
    if (account) setSelected((current) => (current?.id === account.id ? current : account));
  }, [searchParams, demo.connections]);

  function openCreate(integrationName = searchParams.get("integration") ?? "") {
    const targetIntegration =
      integrationName || (availableIntegrations.length === 1 ? availableIntegrations[0].name : "");
    setIntegration(targetIntegration);
    const usedNames = new Set(
      demo.connections
        .filter((item) => !targetIntegration || item.integration === targetIntegration)
        .map((item) => item.name),
    );
    let defaultName = "服务账号";
    for (let suffix = 2; usedNames.has(defaultName); suffix++) defaultName = `服务账号 ${suffix}`;
    setAccountName(searchParams.get("accountName") || defaultName);
    setConnectionKey(searchParams.get("connectionKey") || newAccountKey());
    setExternalEndUserKey(searchParams.get("endUserKey") || "");
    setCredentials({});
    setCreatedIntegration(null);
    setSavedConnectionId("");
    setSavedConnectionRevision(0);
    setCredentialsChanged(false);
    setConnectionSubmitted(false);
    setConnectOpen(true);
  }

  async function saveConnection(verifyAfterSave = false) {
    if (pending || (!selectedIntegration && !canCreateIntegration) || !selectedAuthExecutable) return;
    if (!selectedIntegration && (!targetSystem?.id || !validHttpUrl(apiBaseUrl)))
      return demo.notify("请填写有效的 API 基础地址");
    if (!/^[a-z0-9][a-z0-9._-]{0,99}$/.test(connectionKey) || externalEndUserKey.trim().length > 255)
      return demo.notify("请检查账号 ID 和外部身份标识");
    if (!accountName.trim()) return demo.notify("请填写账号名称");
    const missing = credentialFields(selectedAuthInstance!.templateId, selectedAuthScheme).find(
      (field) => field.required && !credentials[field.name]?.trim(),
    );
    if (!redirectAuth && missing) return demo.notify(`请填写必填凭据：${missing.label}`);
    setPending(true);
    setConnectionSubmitted(true);
    let currentConnectionId = savedConnectionId;
    try {
      let targetIntegration = selectedIntegration;
      if (!targetIntegration) {
        const saved = await saveOrFindIntegration({
          integrationKey,
          name: `${selectedAuthInstance!.name} API`,
          systemId: targetSystem!.id!,
          authInstanceId: selectedAuthInstance!.id,
          baseUrl: apiBaseUrl.trim().replace(/\/$/, ""),
        });
        targetIntegration = {
          id: saved.id,
          name: saved.integrationKey,
          displayName: saved.name,
          provider: targetSystem!.service,
          authInstanceId: selectedAuthInstance!.id,
          baseUrl: saved.baseUrl,
          status: "ready",
          updatedAt: "刚刚",
        };
        setCreatedIntegration(targetIntegration);
        setIntegration(targetIntegration.name);
      }
      if (redirectAuth) {
        const result = await api.oauthStart({
          integrationId: targetIntegration.id,
          endUserKey: externalEndUserKey.trim() || connectionKey,
          endUserName: accountName.trim(),
          connectionKey,
          connectionName: accountName.trim(),
          returnPath: oauthReturnPath(
            targetIntegration.name,
            connectionKey,
            accountName.trim(),
            externalEndUserKey.trim(),
          ),
        });
        window.location.assign(result.authorizationUrl);
        return;
      }
      const result = await saveAccount({
        verifyAfterSave,
        integrationId: targetIntegration.id,
        connectionKey,
        name: accountName.trim(),
        endUserKey: externalEndUserKey.trim() || connectionKey,
        endUserName: accountName.trim(),
        credentials: typedCredentials(selectedAuthScheme?.credentialFields ?? [], credentials),
        connectionId: savedConnectionId || undefined,
        revision: savedConnectionRevision || undefined,
        credentialsChanged,
        onRevision: setSavedConnectionRevision,
        onSaved: (connection) => {
          currentConnectionId = connection.id;
          setSavedConnectionId(connection.id);
          setSavedConnectionRevision(connection.revision);
          setCredentialsChanged(false);
        },
      });
      const [reloadSucceeded, pageRefreshSucceeded] = await Promise.all([
        demo.reload(),
        connectionPage
          .refetch()
          .then(() => true)
          .catch(() => false),
      ]);
      const refreshFailed = !reloadSucceeded || !pageRefreshSucceeded;
      demo.notify(
        refreshFailed
          ? "账号已保存，但部分数据刷新失败，请手动刷新"
          : result.lastVerifiedAt
            ? "连接验证通过"
            : "账号已保存，尚未验证凭据是否有效",
      );
      setConnectOpen(false);
      setCredentials({});
      navigate(
        `/auth?section=accounts&integration=${encodeURIComponent(targetIntegration.name)}&connection=${encodeURIComponent(result.id)}`,
      );
    } catch (error) {
      setConnectionSubmitted(Boolean(currentConnectionId));
      demo.notify(
        currentConnectionId
          ? `账号已保存，请刷新账号列表查看：${error instanceof Error ? error.message : "验证失败"}`
          : error instanceof Error
            ? error
            : String(error),
      );
      await connectionPage.refetch().catch(() => undefined);
    } finally {
      setPending(false);
    }
  }
  const accountActions =
    readyIntegrations.length || canCreateIntegration ? (
      <Button write onClick={() => openCreate()}>
        <Plus className="size-4" /> 添加账号
      </Button>
    ) : (
      <Button asChild write>
        <Link to="/auth?section=instances&create=1">
          <Plus className="size-4" /> 接入系统
        </Link>
      </Button>
    );
  return (
    <div className="grid min-w-0 gap-6">
      {embedded ? (
        <div className="flex justify-end">{accountActions}</div>
      ) : (
        <PageHeader title="账号" description="管理账号凭据和验证状态。" actions={accountActions} />
      )}
      <PageControls
        page={connectionPage}
        onClear={() => {
          setConnectionQuery("");
          const params = new URLSearchParams(searchParams);
          for (const key of ["integration", "authInstance", "connection", "system", "status"]) params.delete(key);
          params.set("section", "accounts");
          navigate(`/auth?${params}`);
        }}
      />
      <div className="grid gap-3 sm:grid-cols-3">
        <select
          aria-label="账号所属系统"
          className={fieldClass}
          value={searchParams.get("system") ?? ""}
          onChange={(e) => {
            const next = new URLSearchParams(searchParams);
            next.set("system", e.target.value);
            next.delete("integration");
            setSearchParams(next);
          }}
        >
          <option value="">全部系统</option>
          {demo.customSystems.map((row) => (
            <option key={row.service} value={row.service}>
              {row.name}
            </option>
          ))}
        </select>
        <select
          aria-label="账号所属集成"
          className={fieldClass}
          value={searchParams.get("integration") ?? ""}
          onChange={(e) => {
            const next = new URLSearchParams(searchParams);
            next.set("integration", e.target.value);
            setSearchParams(next);
          }}
        >
          <option value="">全部集成</option>
          {demo.integrations
            .filter((row) => !searchParams.get("system") || row.provider === searchParams.get("system"))
            .map((row) => (
              <option key={row.id} value={row.name}>
                {row.name}
              </option>
            ))}
        </select>
        <select
          aria-label="账号验证状态"
          className={fieldClass}
          value={searchParams.get("status") ?? ""}
          onChange={(e) => {
            const next = new URLSearchParams(searchParams);
            next.set("status", e.target.value);
            setSearchParams(next);
          }}
        >
          <option value="">全部状态</option>
          <option value="active">已验证</option>
          <option value="pending">待验证</option>
          <option value="error">验证失败</option>
          <option value="disabled">已停用</option>
        </select>
      </div>
      <input
        aria-label="搜索账号"
        className={fieldClass}
        placeholder="搜索账号"
        value={connectionQuery}
        onChange={(event) => setConnectionQuery(event.target.value)}
      />
      <div className="grid gap-3 md:hidden">
        {connections.map((connection) => (
          <button
            key={connection.id}
            className="text-left"
            onClick={() => {
              setSelected(connection);
            }}
          >
            <Card className="p-4 transition hover:border-blue-300">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p translate="no" className="font-semibold">
                    {connection.name}
                  </p>
                  <code className="text-xs text-[var(--muted-text)]">{connection.connectionKey ?? connection.id}</code>
                </div>
                <Badge tone={connectionStatusTone(connection.status)}>
                  {connection.status === "active"
                    ? connection.lastVerified === "尚未"
                      ? "配置有效"
                      : "上游已验证"
                    : statusLabel(connection.status)}
                </Badge>
              </div>
              <div className="mt-4 grid grid-cols-2 gap-3 text-xs">
                <MiniStat label="集成配置" value={connection.integration} />
                <MiniStat label="账号标识" value={connection.endUser} />
                <MiniStat label="最近验证" value={connection.lastVerified} />
                {connection.status === "pending" && (
                  <p className="col-span-2 text-[var(--muted-text)]">
                    {accountVerificationPath(
                      demo.authInstances.find((item) => item.id === connection.authInstanceId)?.publicConfig,
                    )
                      ? "待验证 · 点击账号开始验证"
                      : "待验证 · 请选择验证接口"}
                  </p>
                )}
                <MiniStat label="最近使用" value={connection.lastUsed} />
              </div>
            </Card>
          </button>
        ))}
      </div>
      <Card className="hidden min-w-0 max-w-full overflow-hidden md:block">
        <div className="max-w-full overflow-x-auto [contain:layout_inline-size]">
          <table className="w-full min-w-[850px] text-left text-sm">
            <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
              <tr>
                <Th>账号</Th>
                <Th>集成配置</Th>
                <Th>最终用户</Th>
                <Th>认证方式</Th>
                <Th>状态</Th>
                <Th>最近验证</Th>
                <Th>最近使用</Th>
                <Th>
                  <span className="sr-only">操作</span>
                </Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--border)]">
              {connections.map((connection) => (
                <tr
                  key={connection.id}
                  className="cursor-pointer transition hover:bg-[var(--muted)]"
                  onClick={() => {
                    setSelected(connection);
                  }}
                >
                  <Td>
                    <p translate="no" className="font-semibold">
                      {connection.name}
                    </p>
                    <code className="text-xs text-[var(--muted-text)]">
                      {connection.connectionKey ?? connection.id}
                    </code>
                  </Td>
                  <Td>
                    {connection.integration}
                    <p translate="no" className="text-xs text-[var(--muted-text)]">
                      {connection.provider}
                    </p>
                  </Td>
                  <Td>{connection.endUser}</Td>
                  <Td>
                    <Badge>{authInstanceName(connection.authInstanceId, demo.authInstances)}</Badge>
                  </Td>
                  <Td>
                    <Badge tone={connectionStatusTone(connection.status)}>
                      {connection.status === "active"
                        ? connection.lastVerified === "尚未"
                          ? "配置有效"
                          : "上游已验证"
                        : statusLabel(connection.status)}
                    </Badge>
                  </Td>
                  <Td>
                    {connection.lastVerified}
                    {connection.status === "pending" && (
                      <p className="text-xs text-[var(--muted-text)]">
                        {accountVerificationPath(
                          demo.authInstances.find((item) => item.id === connection.authInstanceId)?.publicConfig,
                        )
                          ? "待验证 · 打开账号开始验证"
                          : "未设置验证接口"}
                      </p>
                    )}
                  </Td>
                  <Td>{connection.lastUsed}</Td>
                  <Td>
                    <button
                      type="button"
                      aria-label={`查看 ${connection.name} 连接详情`}
                      className="grid size-8 place-items-center rounded-lg text-[var(--muted-text)] transition hover:bg-[var(--surface)] hover:text-[var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                      onClick={(event) => {
                        event.stopPropagation();
                        setSelected(connection);
                      }}
                    >
                      <ArrowRight className="size-4" />
                    </button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      <Modal
        open={connectOpen}
        onOpenChange={(open) => {
          if (pending) return;
          setConnectOpen(open);
          if (!open) setCredentials({});
        }}
        title="添加账号"
        description="填写账号凭据；保存后可单独执行验证。"
        unsavedChanges={savedConnectionId ? credentialsChanged : undefined}
      >
        <form
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            void saveConnection();
          }}
        >
          {availableIntegrations.length > 1 && (
            <Field label="集成配置">
              <select
                className={fieldClass}
                value={integration}
                disabled={connectionSubmitted}
                onChange={(event) => {
                  if (
                    (Object.keys(credentials).length || externalEndUserKey || savedConnectionId) &&
                    !window.confirm("切换集成会清空已填写的账号凭据，是否继续？")
                  ) {
                    return;
                  }
                  setCreatedIntegration(null);
                  setIntegration(event.target.value);
                  setCredentials({});
                  setConnectionKey(newAccountKey());
                  setExternalEndUserKey("");
                  setSavedConnectionId("");
                  setCredentialsChanged(false);
                }}
              >
                <option value="">选择集成配置</option>
                {availableIntegrations.map((item) => (
                  <option key={item.id} value={item.name}>
                    {item.displayName} · {item.name}
                  </option>
                ))}
              </select>
            </Field>
          )}
          {selectedIntegration && selectedAuthInstance ? (
            <div className="rounded-lg bg-[var(--muted)] p-3 text-sm">
              <p className="font-medium">
                {selectedIntegration.displayName} · {selectedIntegration.name}
              </p>
              <p className="mt-1 text-[var(--muted-text)]">
                {authInstanceName(selectedAuthInstance.id, demo.authInstances)} · {selectedIntegration.baseUrl}
              </p>
            </div>
          ) : canCreateIntegration ? (
            <div className="grid gap-3">
              <p className="text-sm font-medium">{authInstanceName(selectedAuthInstance!.id, demo.authInstances)}</p>
              <Field label="API 基础地址" hint="为当前认证实例配置 API 地址并添加账号。">
                <input
                  className={fieldClass}
                  type="url"
                  value={apiBaseUrl}
                  onChange={(event) => setApiBaseUrl(event.target.value)}
                />
              </Field>
            </div>
          ) : availableIntegrations.length ? (
            <p className="text-sm text-[var(--muted-text)]">请选择要添加账号的集成配置。</p>
          ) : (
            <div className="grid gap-3">
              <p className="text-sm text-[var(--muted-text)]">此认证实例还没有可用的集成配置。</p>
              <Button asChild variant="secondary">
                <Link
                  to={`/integrations?new=1&provider=${encodeURIComponent(selectedAuthInstance?.systemIds[0] ?? "")}`}
                >
                  配置 API 地址
                </Link>
              </Button>
            </div>
          )}
          {(selectedIntegration || canCreateIntegration) && (
            <>
              <Field label="账号名称">
                <input
                  className={fieldClass}
                  value={accountName}
                  required
                  disabled={connectionSubmitted}
                  onChange={(event) => setAccountName(event.target.value)}
                />
              </Field>
              <ConnectionCredentialFields
                auth={selectedAuthInstance?.templateId ?? ""}
                authName={selectedAuthName}
                scheme={selectedAuthScheme}
                values={credentials}
                onChange={(name, value) => {
                  setCredentials((current) => ({ ...current, [name]: value }));
                  if (savedConnectionId) setCredentialsChanged(true);
                }}
              />
              <details className="text-sm">
                <summary className="cursor-pointer font-medium">高级设置</summary>
                <div className="mt-3">
                  <Field label="外部用户或租户 ID" hint="留空时使用本次账号 ID。">
                    <input
                      className={fieldClass}
                      value={externalEndUserKey}
                      disabled={connectionSubmitted}
                      onChange={(event) => setExternalEndUserKey(event.target.value)}
                    />
                  </Field>
                </div>
                <p className="mt-2 text-xs text-[var(--muted-text)]">账号 ID：{connectionKey}</p>
              </details>
            </>
          )}
          {!redirectAuth && !createVerificationPath && (
            <p className="text-sm text-[var(--muted-text)]">未设置验证接口。保存后请在账号详情选择业务验证接口。</p>
          )}
          <div className="flex flex-wrap justify-end gap-2">
            {!redirectAuth && createVerificationPath && (
              <Button
                write
                type="button"
                disabled={pending || !selectedAuthExecutable || !accountName.trim()}
                onClick={() => void saveConnection(true)}
              >
                保存并验证
              </Button>
            )}
            <Button
              write
              disabled={
                pending ||
                (!selectedIntegration && !canCreateIntegration) ||
                !selectedAuthExecutable ||
                !accountName.trim()
              }
              type="submit"
            >
              {pending ? "正在处理…" : redirectAuth ? "授权连接" : "保存账号"}
              <ArrowRight className="size-4" />
            </Button>
          </div>
        </form>
      </Modal>
      <Modal
        open={Boolean(selected)}
        onOpenChange={(open) => {
          if (open || verificationPending || credentialPending) return;
          setSelected(null);
          if (searchParams.has("connection")) {
            const params = new URLSearchParams(searchParams);
            params.delete("connection");
            params.set("section", "accounts");
            navigate(`/auth?${params}`, { replace: true });
          }
        }}
        unsavedChanges={updatingCredentials && Object.values(replacementCredentials).some((value) => value !== "")}
        title={selected?.name ?? "账号"}
        description={`${selected?.provider ?? ""} · ${selected?.endUser ?? ""}`}
      >
        {selected && (
          <div className="grid gap-5">
            <section className="rounded-lg border border-[var(--border)] p-4" aria-label="账号验证状态">
              <p className="font-medium">
                {selected.status === "disabled"
                  ? "账号已停用"
                  : verificationResult.startsWith("验证通过")
                    ? "已通过接口验证"
                    : verificationResult
                      ? "账号已保存，待验证"
                      : selectedDetails?.lastVerified !== "尚未" && selectedDetails?.lastVerified
                        ? "已通过接口验证"
                        : "账号已保存，待验证"}
              </p>
              <p className="mt-1 text-sm text-[var(--muted-text)]">
                {canVerify
                  ? "使用此账号已保存的凭据请求验证接口。验证通过仅代表该接口的访问权限。"
                  : "未设置验证接口。请选择下面的业务验证接口，或配置认证实例的凭据验证路径。"}
              </p>
            </section>
            <KeyValues
              items={[
                [
                  "状态",
                  selectedDetails?.status === "active"
                    ? selectedDetails?.lastVerified === "尚未"
                      ? "配置有效，尚未验证上游"
                      : "上游已验证"
                    : statusLabel(selectedDetails?.status ?? "pending"),
                ],
                ["认证实例", authInstanceName(selected.authInstanceId, demo.authInstances)],
                ["认证模板", openedAuthName],
                ["最近验证", selectedDetails?.lastVerified ?? "尚未"],
                ["最近使用", selected.lastUsed],
              ]}
            />
            <p className="text-xs leading-5 text-[var(--muted-text)]">
              平台不会回显已保存的明文凭据。可在保持连接标识不变的情况下更新凭据；如需更换集成，请创建替代连接并调整同步任务及运行时令牌的连接范围。
            </p>
            <Button
              write
              variant="secondary"
              disabled={
                credentialPending || verificationPending || isRedirectAuth(openedAuthInstance?.templateId ?? "")
              }
              onClick={() => {
                setUpdatingCredentials(!updatingCredentials);
                setReplacementCredentials({});
              }}
            >
              更新凭据
            </Button>
            {updatingCredentials && (
              <div className="grid gap-3">
                <ConnectionCredentialFields
                  auth={openedAuthInstance?.templateId ?? ""}
                  authName={openedAuthName}
                  scheme={demo.authSchemes.find((scheme) => scheme.id === openedAuthInstance?.templateId)}
                  values={replacementCredentials}
                  onChange={(name, value) => setReplacementCredentials((values) => ({ ...values, [name]: value }))}
                />
                {canVerify && (
                  <Button
                    write
                    disabled={credentialPending || verificationPending || selected.status === "disabled"}
                    onClick={() => void saveCredentials(true)}
                  >
                    保存并验证
                  </Button>
                )}
                <Button
                  write
                  disabled={credentialPending || verificationPending}
                  onClick={() => void saveCredentials(false)}
                >
                  {credentialPending ? "保存中…" : "仅保存凭据"}
                </Button>
              </div>
            )}
            <Field
              label="业务验证接口"
              hint="显示当前系统的全部 API。请选择需要认证的查询接口；验证会真实发送请求，请避免修改业务数据的接口。"
            >
              <select
                className={fieldClass}
                disabled={verificationPending || credentialPending}
                value={verificationApiId}
                onChange={(event) => {
                  setVerificationApiId(event.target.value);
                  setVerificationInput(
                    JSON.stringify(
                      verificationApis.find((row) => row.id === event.target.value)?.exampleInput ?? {},
                      null,
                      2,
                    ),
                  );
                }}
              >
                <option value="">
                  {defaultVerificationPath
                    ? `认证实例设置 · ${defaultVerificationPath}`
                    : "请选择验证接口（认证实例未设置验证）"}
                </option>
                {verificationApis.map((row) => (
                  <option key={row.id} value={row.id} disabled={row.status !== "active"}>
                    {row.name} · {row.httpMethod} {row.relativePath}
                    {row.status === "draft" ? " · 草稿（不可选）" : row.status !== "active" ? " · 停用（不可选）" : ""}
                  </option>
                ))}
              </select>
            </Field>
            {verificationApiId && (
              <Field label="验证参数 JSON">
                <textarea
                  className={textAreaClass}
                  disabled={verificationPending || credentialPending}
                  value={verificationInput}
                  onChange={(e) => setVerificationInput(e.target.value)}
                />
              </Field>
            )}
            <div className="flex flex-wrap gap-2">
              <Button
                write
                disabled={
                  selected.status === "disabled" ||
                  !canVerify ||
                  verificationPending ||
                  credentialPending ||
                  updatingCredentials
                }
                onClick={() => void verifyAccount(selected.id)}
              >
                <CheckCircle2 className="size-4" /> {verificationPending ? "正在验证…" : "验证账号"}
              </Button>
            </div>
            <p className="break-all text-xs text-[var(--muted-text)]">
              验证地址：{openedIntegration?.baseUrl ?? ""}
              {verificationApiId
                ? verificationApis.find((item) => item.id === verificationApiId)?.relativePath
                : defaultVerificationPath || "（未设置）"}
            </p>
            {!canVerify && (
              <Button asChild variant="secondary">
                <Link to={`/auth?section=instances&editInstance=${encodeURIComponent(selected.authInstanceId)}`}>
                  配置认证实例验证
                </Link>
              </Button>
            )}
            {verificationResult && (
              <p role="status" className="break-words text-sm">
                {verificationResult}
              </p>
            )}
            {verificationError && (
              <p role="alert" className="break-words text-sm text-red-600">
                {verificationError}
              </p>
            )}
            <details>
              <summary className="cursor-pointer text-sm">账号管理</summary>
              <div className="mt-3 flex flex-wrap gap-2">
                {" "}
                <Button
                  write
                  variant="secondary"
                  disabled={verificationPending || credentialPending}
                  onClick={async () => {
                    try {
                      const current = (await api.connections()).find((row) => row.id === selected.id);
                      if (!current) return;
                      await api.setConnectionEnabled(selected.id, current.revision, selected.status === "disabled");
                      await demo.reload();
                      await connectionPage.refetch();
                      setSelected(null);
                      demo.notify(selected.status === "disabled" ? "账号已启用" : "账号已停用");
                    } catch (error) {
                      demo.notify(error as Error);
                    }
                  }}
                >
                  {selected.status === "disabled" ? "启用账号" : "停用账号"}
                </Button>
                <Button
                  write
                  variant="secondary"
                  onClick={() => {
                    setSelected(null);
                    openCreate(selected.integration);
                  }}
                >
                  添加替代账号
                </Button>
              </div>
            </details>
          </div>
        )}
      </Modal>
    </div>
  );
}
