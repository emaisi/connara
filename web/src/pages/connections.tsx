import { useCanWrite } from "../permissions";
import { useResourcePages, PageControls } from "../pagination";
import { formatRelative } from "../demo";
import { ArrowRight, CheckCircle2, LockKeyhole, Plus } from "lucide-react";
import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { statusLabel, useDemo } from "../demo";
import { Badge, Button, Card, Field, Modal, PageHeader, cn, fieldClass } from "../ui";
import { api } from "../api";

import {
  isExecutableAuthTemplate,
  connectionStatusTone,
  authTemplateName,
  authInstanceName,
  BuildFlow,
  isRedirectAuth,
  ConnectionCredentialFields,
  credentialFields,
  Logo,
  MiniStat,
  KeyValues,
  StepBar,
  Th,
  Td,
} from "./core-shared";
export function DemoConnectionsPage() {
  const demo = useDemo();
  const [connectionQuery, setConnectionQuery] = useState("");
  const connectionPage = useResourcePages("/api/connections", { q: connectionQuery }, demo.authenticated);
  const connections: typeof demo.connections = connectionPage.items.map((item) => ({
    id: item.id,
    name: item.connectionKey,
    integration: item.integrationKey,
    provider: item.systemKey,
    endUser: item.endUserKey,
    authInstanceId: item.authInstanceId,
    status: item.status,
    lastVerified: formatRelative(item.lastVerifiedAt),
    lastUsed: formatRelative(item.lastUsedAt),
    records: 0,
    tags: item.tags ?? [],
  }));
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const canWrite = useCanWrite();
  const [connectOpen, setConnectOpen] = useState(canWrite && searchParams.get("new") === "1");
  const [step, setStep] = useState(1);
  const readyIntegrations = demo.integrations.filter((item) => item.status === "ready");
  const [integration, setIntegration] = useState(searchParams.get("integration") ?? readyIntegrations[0]?.name ?? "");
  const [endUser, setEndUser] = useState("");
  const [organization, setOrganization] = useState("");
  const [savedConnectionId, setSavedConnectionId] = useState("");
  const [pending, setPending] = useState(false);
  const [credentials, setCredentials] = useState<Record<string, string>>({});
  const [updatingCredentials, setUpdatingCredentials] = useState(false);
  const [replacementCredentials, setReplacementCredentials] = useState<Record<string, string>>({});
  const [selected, setSelected] = useState<(typeof demo.connections)[number] | null>(null);
  const selectedIntegration = readyIntegrations.find((item) => item.name === integration);
  const selectedAuthInstance = demo.authInstances.find(
    (instance) => instance.id === selectedIntegration?.authInstanceId,
  );
  const selectedAuthScheme = demo.authSchemes.find((scheme) => scheme.id === selectedAuthInstance?.templateId);
  const selectedAuthName = selectedAuthInstance
    ? authTemplateName(selectedAuthInstance.templateId, demo.authSchemes)
    : "";
  const selectedAuthExecutable = Boolean(
    selectedAuthInstance && isExecutableAuthTemplate(selectedAuthInstance.templateId, demo.authSchemes),
  );
  const openedAuthInstance = demo.authInstances.find((instance) => instance.id === selected?.authInstanceId);
  const openedAuthName = openedAuthInstance ? authTemplateName(openedAuthInstance.templateId, demo.authSchemes) : "";
  const redirectAuth = isRedirectAuth(selectedAuthName);
  const passwordTokenAuth = selectedAuthName.includes("用户名密码换 Token");
  async function next() {
    if (
      step === 2 &&
      credentialFields(selectedAuthName, selectedAuthScheme).some(
        (field) => field.required && !credentials[field.name]?.trim(),
      )
    ) {
      demo.notify("请填写全部必填凭据");
      return;
    }
    if (step < 3) return setStep((value) => value + 1);
    if (pending || !selectedIntegration) return;
    setPending(true);
    let createdId = savedConnectionId;
    try {
      if (redirectAuth) {
        await demo.startOAuth(integration, endUser);
        return;
      }
      let id = savedConnectionId;
      if (!id) {
        const created = await api.saveConnection({
          integrationId: selectedIntegration.id,
          name: `${selectedIntegration.provider}-${Date.now().toString(36)}`,
          endUserKey: endUser.trim(),
          endUserName: organization.trim(),
          credentials,
        });
        id = created.id;
        createdId = id;
        setSavedConnectionId(id);
      }
      const result = await api.verifyConnection(id);
      await Promise.all([demo.reload(), connectionPage.refetch()]);
      demo.notify(
        result.connection?.lastVerifiedAt
          ? "上游凭据验证通过，连接已保存"
          : "连接已保存，配置检查通过；请运行 API 操作验证上游",
      );
      setConnectOpen(false);
      setStep(1);
      setCredentials({});
      setSavedConnectionId("");
      navigate(
        `/actions?system=${encodeURIComponent(selectedIntegration.provider)}&integration=${encodeURIComponent(integration)}&connection=${encodeURIComponent(id)}`,
      );
    } catch (error) {
      demo.notify(
        (createdId ? "连接已保存，请重试验证：" : "") + (error instanceof Error ? error.message : "创建失败"),
      );
      await connectionPage.refetch();
    } finally {
      setPending(false);
    }
  }
  return (
    <div className="grid gap-6">
      <PageHeader
        title="连接账号"
        description="为最终用户、企业租户或服务账号保存真实凭据，并检查授权状态。"
        actions={
          <Button
            write
            onClick={() => {
              setCredentials({});
              setSavedConnectionId("");
              setConnectOpen(true);
            }}
            disabled={!readyIntegrations.length}
          >
            <Plus className="size-4" />
            创建连接账号
          </Button>
        }
      />
      <PageControls
        page={connectionPage}
        onClear={() => {
          setConnectionQuery("");
        }}
      />
      <BuildFlow current="connections" />
      <input
        aria-label="搜索连接"
        className={fieldClass}
        placeholder="搜索全部连接"
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
                  <code className="text-xs text-[var(--muted-text)]">{connection.id}</code>
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
                <MiniStat label="最近使用" value={connection.lastUsed} />
              </div>
            </Card>
          </button>
        ))}
      </div>
      <Card className="hidden overflow-hidden md:block">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[850px] text-left text-sm">
            <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
              <tr>
                <Th>连接账号</Th>
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
                    <code className="text-xs text-[var(--muted-text)]">{connection.id}</code>
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
                  <Td>{connection.lastVerified}</Td>
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
          if (
            !open &&
            (Object.keys(credentials).length || savedConnectionId) &&
            !window.confirm(
              savedConnectionId ? "连接已保存，关闭后可从列表继续验证。确认关闭？" : "凭据尚未保存，确认放弃本次输入？",
            )
          )
            return;
          setConnectOpen(open);
          if (!open) setStep(1);
        }}
        title="创建连接账号"
        description="按三步选择集成、填写账号凭据并验证连接。"
      >
        <div className="grid gap-5">
          <StepBar step={step} labels={["选择集成", "身份与凭据", "确认连接"]} />
          {step === 1 && (
            <div className="grid gap-2">
              {readyIntegrations.map((item) => (
                <button
                  key={item.id}
                  onClick={() => {
                    if (
                      item.name !== integration &&
                      Object.keys(credentials).length &&
                      !window.confirm("切换集成将清空已填写凭据，确认继续？")
                    )
                      return;
                    setIntegration(item.name);
                    setCredentials({});
                    setSavedConnectionId("");
                  }}
                  className={cn(
                    "flex items-center gap-3 rounded-xl border p-3 text-left",
                    integration === item.name
                      ? "border-blue-500 bg-blue-50 dark:bg-blue-950"
                      : "border-[var(--border)]",
                  )}
                >
                  <Logo letters={item.displayName.slice(0, 2).toUpperCase()} small />
                  <div className="flex-1">
                    <p translate="no" className="text-sm font-bold">
                      {item.displayName}
                    </p>
                    <p className="text-xs text-[var(--muted-text)]">
                      {item.name} · {authInstanceName(item.authInstanceId, demo.authInstances)}
                    </p>
                  </div>
                  {integration === item.name && <CheckCircle2 className="size-5 text-blue-600" />}
                </button>
              ))}
            </div>
          )}
          {step === 2 && (
            <div className="grid gap-4">
              <Field label="最终用户 ID">
                <input
                  placeholder="用户、租户或服务账号的稳定标识"
                  className={fieldClass}
                  value={endUser}
                  onChange={(event) => setEndUser(event.target.value)}
                />
              </Field>
              <Field label="组织显示名">
                <input
                  className={fieldClass}
                  value={organization}
                  onChange={(event) => setOrganization(event.target.value)}
                />
              </Field>
              <Field label="允许的集成配置">
                <input className={fieldClass} value={integration} readOnly />
              </Field>
              {selectedIntegration && (
                <ConnectionCredentialFields
                  auth={selectedAuthName}
                  scheme={selectedAuthScheme}
                  values={credentials}
                  onChange={(name, value) => setCredentials((current) => ({ ...current, [name]: value }))}
                />
              )}
            </div>
          )}
          {step === 3 && (
            <div className="grid place-items-center gap-4 py-6 text-center">
              <span className="grid size-16 place-items-center rounded-full bg-emerald-50 text-emerald-600 dark:bg-emerald-950">
                <LockKeyhole className="size-7" />
              </span>
              <div>
                <h3 className="font-bold">准备连接 {integration}</h3>
                <p className="mt-1 text-sm text-[var(--muted-text)]">
                  {passwordTokenAuth
                    ? "将使用用户名密码登录，提取 Token 并验证 Header 注入配置。"
                    : redirectAuth
                      ? `将打开 ${selectedAuthName} 授权页面，并等待真实回调完成令牌交换。`
                      : `将加密保存 ${selectedAuthName || "凭据"}，并由后端解析认证配置后创建连接账号。`}
                </p>
                <Badge tone="info">{selectedAuthInstance?.name ?? "请选择认证实例"}</Badge>
              </div>
              {!selectedAuthExecutable && (
                <div className="rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
                  此认证模板尚无可执行的 Go 扩展，当前不能创建可运行连接。
                </div>
              )}
            </div>
          )}
          <div className="flex justify-between">
            <Button variant="secondary" disabled={step === 1} onClick={() => setStep((value) => value - 1)}>
              上一步
            </Button>
            <Button
              write
              disabled={pending || !integration || (step > 1 && !endUser.trim()) || !selectedAuthExecutable}
              onClick={next}
            >
              {pending
                ? "正在处理…"
                : step === 3
                  ? savedConnectionId
                    ? "重试验证并继续"
                    : "保存并验证连接"
                  : "下一步"}
              <ArrowRight className="size-4" />
            </Button>
          </div>
        </div>
      </Modal>
      <Modal
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
        title={selected?.name ?? "连接账号"}
        description={`${selected?.provider ?? ""} · ${selected?.endUser ?? ""}`}
      >
        {selected && (
          <div className="grid gap-5">
            <KeyValues
              items={[
                [
                  "状态",
                  selected.status === "active"
                    ? selected.lastVerified === "尚未"
                      ? "配置有效，尚未验证上游"
                      : "上游已验证"
                    : statusLabel(selected.status),
                ],
                ["认证实例", authInstanceName(selected.authInstanceId, demo.authInstances)],
                ["认证模板", openedAuthName],
                ["最近验证", selected.lastVerified],
                ["最近使用", selected.lastUsed],
              ]}
            />
            <p className="text-xs leading-5 text-[var(--muted-text)]">
              平台不会回显已保存的明文凭据。可在保持连接标识不变的情况下更新凭据；如需更换集成，请创建替代连接并调整同步任务及运行时令牌的连接范围。
            </p>
            <Button
              write
              variant="secondary"
              disabled={isRedirectAuth(openedAuthName)}
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
                  auth={openedAuthName}
                  scheme={demo.authSchemes.find((scheme) => scheme.id === openedAuthInstance?.templateId)}
                  values={replacementCredentials}
                  onChange={(name, value) => setReplacementCredentials((values) => ({ ...values, [name]: value }))}
                />
                <Button
                  write
                  onClick={async () => {
                    const current = connectionPage.items.find((item) => item.id === selected.id);
                    if (!current) return;
                    const scheme = demo.authSchemes.find((scheme) => scheme.id === openedAuthInstance?.templateId);
                    if (
                      credentialFields(openedAuthName, scheme).some(
                        (field) => field.required && !replacementCredentials[field.name]?.trim(),
                      )
                    ) {
                      demo.notify("请填写全部必填凭据");
                      return;
                    }
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
                        credentials: replacementCredentials,
                      });
                      setUpdatingCredentials(false);
                      setReplacementCredentials({});
                      demo.notify("凭据已更新，请重新验证认证配置");
                      await Promise.all([connectionPage.refetch(), demo.reload()]);
                      setSelected(null);
                    } catch (error) {
                      demo.notify(error instanceof Error ? error : new Error("更新失败"));
                    }
                  }}
                >
                  保存新凭据
                </Button>
              </div>
            )}
            <div className="flex flex-wrap gap-2">
              <Button
                write
                onClick={() =>
                  api
                    .verifyConnection(selected.id)
                    .then((verified) => {
                      demo.notify(
                        verified.connection?.lastVerifiedAt ? "上游凭据验证通过" : "配置检查通过；尚未进行上游验证",
                      );
                      return demo.reload();
                    })
                    .catch((error) => demo.notify(error))
                }
              >
                <CheckCircle2 className="size-4" /> 验证认证配置
              </Button>
              <Button
                write
                variant="secondary"
                onClick={() => {
                  setIntegration(selected.integration);
                  setSelected(null);
                  setConnectOpen(true);
                  setStep(1);
                }}
              >
                新建替代连接
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
