import { EmptyState, MutationForm } from "../ui";
import { useCanWrite } from "../permissions";
import { ArrowRight, Plus, ShieldCheck } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { statusLabel, useDemo } from "../demo";
import { Badge, Button, Card, Field, Modal, PageHeader, fieldClass } from "../ui";
import { api } from "../api";

import { authInstancesFor, authTemplateName, authInstanceName, BuildFlow, Tabs, Logo, KeyValues } from "./core-shared";
export function DemoIntegrationsPage() {
  const demo = useDemo();
  const canWrite = useCanWrite();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const systems = demo.customSystems;
  const draft = (() => {
    try {
      return JSON.parse(sessionStorage.getItem("apihub.integration-draft") || "null");
    } catch {
      return null;
    }
  })() as {
    provider?: string;
    name?: string;
    baseUrl?: string;
  } | null;
  const requestedProvider = searchParams.get("provider") ?? searchParams.get("system") ?? draft?.provider;
  const initialProvider = requestedProvider ?? systems[0]?.service ?? "";
  const [open, setOpen] = useState(canWrite && searchParams.get("new") === "1");
  const [selected, setSelected] = useState<(typeof demo.integrations)[number] | null>(null);
  const [selectedAuthDraft, setSelectedAuthDraft] = useState("");
  const [selectedBaseUrlDraft, setSelectedBaseUrlDraft] = useState("");
  const [selectedStatusDraft, setSelectedStatusDraft] = useState<(typeof demo.integrations)[number]["status"]>("draft");
  const [provider, setProvider] = useState(initialProvider);
  const [authInstanceId, setAuthInstanceId] = useState(
    authInstancesFor(
      systems.find((item) => item.service === initialProvider),
      demo.authInstances,
      demo.authSchemes,
      true,
    )[0]?.id ?? "",
  );
  const [name, setName] = useState(draft?.name ?? "");
  const [query, setQuery] = useState("");
  const [filterStatus, setFilterStatus] = useState("");
  const [baseUrl, setBaseUrl] = useState(draft?.baseUrl ?? "");
  useEffect(() => {
    if (open) sessionStorage.setItem("apihub.integration-draft", JSON.stringify({ provider, name, baseUrl }));
  }, [open, provider, name, baseUrl]);
  const [tab, setTab] = useState("概览");
  const availableAuthInstances = authInstancesFor(
    systems.find((item) => item.service === provider),
    demo.authInstances,
    demo.authSchemes,
    true,
  );
  useEffect(() => {
    if (!provider && systems[0]) setProvider(systems[0].service);
    if (!authInstanceId && availableAuthInstances[0]) setAuthInstanceId(availableAuthInstances[0].id);
  }, [provider, authInstanceId, systems, availableAuthInstances]);
  const selectedHasConnections = Boolean(
    selected && demo.connections.some((connection) => connection.integration === selected.name),
  );
  const selectedAuthInstance = demo.authInstances.find((instance) => instance.id === selectedAuthDraft);
  function saveSelectedIntegration() {
    if (!selected?.systemId || !selectedAuthDraft) return;
    return api
      .saveIntegration(
        {
          integrationKey: selected.name,
          name: selected.name,
          systemId: selected.systemId,
          authInstanceId: selectedAuthDraft,
          baseUrl: selectedBaseUrlDraft,
          status: selectedStatusDraft,
          settings: selected.settings ?? {},
        },
        selected.id,
      )
      .then(() => demo.reload())
      .then(() => {
        setSelected({
          ...selected,
          authInstanceId: selectedAuthDraft,
          baseUrl: selectedBaseUrlDraft,
          status: selectedStatusDraft,
        });
        demo.notify(`集成配置 ${selected.name} 已更新`);
      })
      .catch((error) => demo.notify(error));
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    const selectedProvider = systems.find((item) => item.service === provider);
    const integrationName = name || `${provider}-demo`;
    if (!authInstanceId) return;
    if (!(await demo.addIntegration(provider, integrationName, authInstanceId, selectedProvider?.name, baseUrl)))
      return;
    setOpen(false);
    sessionStorage.removeItem("apihub.integration-draft");
    setName("");
    navigate(`/connections?new=1&integration=${encodeURIComponent(integrationName)}`);
  }
  return (
    <div className="grid gap-6">
      <PageHeader
        title="集成配置"
        description="为系统配置 API 地址并选择兼容的认证实例；真实账号凭据在连接账号中保存。"
        actions={
          <Button write onClick={() => setOpen(true)}>
            <Plus className="size-4" />
            新建集成
          </Button>
        }
      />
      <BuildFlow current="integrations" />
      {!demo.integrations.length && (
        <EmptyState
          title="尚未创建集成"
          description="选择系统、API 地址和认证实例，开始接入。"
          action={
            <Button write onClick={() => setOpen(true)}>
              创建集成
            </Button>
          }
        />
      )}
      <div className="flex gap-3">
        <input
          aria-label="搜索集成"
          className={fieldClass}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索集成名称、系统或地址"
        />
        <select
          aria-label="集成状态"
          className={fieldClass}
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
        >
          <option value="">全部状态</option>
          {["ready", "draft", "disabled"].map((item) => (
            <option key={item} value={item}>
              {statusLabel(item)}
            </option>
          ))}
        </select>
      </div>
      <div className="grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
        {demo.integrations
          .filter(
            (item) =>
              `${item.name} ${item.provider} ${item.baseUrl}`.toLowerCase().includes(query.toLowerCase()) &&
              (!filterStatus || item.status === filterStatus),
          )
          .map((integration) => (
            <button
              key={integration.id}
              className="text-left"
              onClick={() => {
                setSelected(integration);
                setSelectedAuthDraft(integration.authInstanceId);
                setSelectedBaseUrlDraft(integration.baseUrl);
                setSelectedStatusDraft(integration.status);
                setTab("概览");
              }}
            >
              <Card className="h-full p-5 transition hover:border-blue-300 hover:shadow-md">
                <div className="flex items-start justify-between">
                  <Logo letters={integration.displayName.slice(0, 2).toUpperCase()} />
                  <Badge
                    tone={
                      integration.status === "ready" ? "success" : integration.status === "draft" ? "warning" : "danger"
                    }
                  >
                    {statusLabel(integration.status)}
                  </Badge>
                </div>
                <h2 translate="no" className="mt-4 font-bold">
                  {integration.name}
                </h2>
                <p className="mt-1 text-sm text-[var(--muted-text)]">
                  {integration.displayName} · {authInstanceName(integration.authInstanceId, demo.authInstances)}
                </p>
                <div className="mt-5 grid grid-cols-2 gap-3 border-t border-[var(--border)] pt-4 text-xs">
                  <span className="text-[var(--muted-text)]">
                    连接账号
                    <strong className="ml-1 text-[var(--text)]">
                      {demo.connections.filter((item) => item.integration === integration.name).length}
                    </strong>
                  </span>
                  <span className="text-right text-[var(--muted-text)]">{integration.updatedAt}</span>
                </div>
              </Card>
            </button>
          ))}
      </div>
      <Modal
        open={open}
        onOpenChange={setOpen}
        title="新建集成"
        description="选择系统与该系统已绑定的认证实例，然后继续创建测试连接。"
      >
        <MutationForm className="grid gap-4" onSubmit={submit}>
          <Field label="系统">
            <select
              className={fieldClass}
              value={provider}
              onChange={(event) => {
                const nextProvider = systems.find((item) => item.service === event.target.value);
                setProvider(event.target.value);
                setAuthInstanceId(
                  authInstancesFor(nextProvider, demo.authInstances, demo.authSchemes, true)[0]?.id ?? "",
                );
              }}
            >
              {systems.map((item) => (
                <option key={item.service} value={item.service}>
                  {item.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="API 基础地址" hint="操作只能访问这个地址下的相对路径。">
            <input
              className={fieldClass}
              value={baseUrl}
              onChange={(event) => setBaseUrl(event.target.value)}
              placeholder="https://erp.corp.example/api"
              type="url"
              required
            />
          </Field>
          <Field label="认证实例" hint="只显示与当前系统认证模板兼容且状态可用的实例。">
            <select
              className={fieldClass}
              value={authInstanceId}
              onChange={(event) => setAuthInstanceId(event.target.value)}
            >
              <option value="">选择认证实例</option>
              {availableAuthInstances.map((instance) => (
                <option key={instance.id} value={instance.id}>
                  {instance.name}
                </option>
              ))}
            </select>
          </Field>
          {!availableAuthInstances.length && (
            <div className="rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
              当前系统还没有可用的认证实例，请先在认证中心添加并绑定。
            </div>
          )}
          <Field label="集成配置 ID" hint="用于 API 和配置引用，创建后保持稳定。">
            <input
              className={fieldClass}
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={`${provider}-production`}
            />
          </Field>
          <Link className="text-sm font-semibold text-blue-600 hover:underline" to="/providers">
            找不到目标系统？添加企业内部系统
          </Link>
          <Link
            className="text-sm font-semibold text-blue-600 hover:underline"
            to={`/auth?section=instances&system=${encodeURIComponent(provider)}&returnTo=${encodeURIComponent(`/integrations?new=1&system=${provider}`)}`}
          >
            找不到认证配置？添加认证实例
          </Link>
          <div className="rounded-xl bg-blue-50 p-3 text-sm text-blue-700 dark:bg-blue-950 dark:text-blue-300">
            <ShieldCheck className="mr-2 inline size-4" />
            平台会加密保存凭据，不会向其他成员回显明文。
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button write disabled={!authInstanceId}>
              创建并继续连接 <ArrowRight className="size-4" />
            </Button>
          </div>
        </MutationForm>
      </Modal>
      <Modal
        open={Boolean(selected)}
        onOpenChange={(value) => !value && setSelected(null)}
        title={selected?.name ?? "集成配置"}
        description="查看系统地址并调整该集成使用的认证实例。"
      >
        {selected && (
          <div className="grid gap-5">
            {selectedHasConnections && (
              <p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
                此集成已有连接账号。更换 API 地址或认证实例需要新建集成和替代连接；仅更新账号凭据可到连接详情完成。
              </p>
            )}
            <Tabs value={tab} onChange={setTab} items={["概览", "认证"]} />
            {tab === "概览" && (
              <div className="grid gap-4">
                <KeyValues
                  items={[
                    ["系统", selected.displayName],
                    ["配置标识", selected.name],
                    ["资源 UUID", selected.id],
                    ["最后更新", selected.updatedAt],
                  ]}
                />
                <Field label="API 基础地址" hint="只允许 HTTPS；API 操作只能访问此地址下的相对路径。">
                  <input
                    className={fieldClass}
                    type="url"
                    disabled={!canWrite || selectedHasConnections}
                    value={selectedBaseUrlDraft}
                    onChange={(event) => setSelectedBaseUrlDraft(event.target.value)}
                    required
                  />
                </Field>
                <Field label="配置状态">
                  <select
                    className={fieldClass}
                    disabled={!canWrite}
                    value={selectedStatusDraft}
                    onChange={(event) =>
                      setSelectedStatusDraft(event.target.value as (typeof demo.integrations)[number]["status"])
                    }
                  >
                    <option value="ready">就绪</option>
                    <option value="draft">草稿</option>
                    <option value="disabled">已停用</option>
                  </select>
                </Field>
                <Button write className="w-fit" onClick={saveSelectedIntegration}>
                  保存集成配置
                </Button>
                <div className="grid gap-3 sm:grid-cols-2">
                  <Link
                    to="/sync"
                    className="rounded-xl border border-[var(--border)] p-4 transition hover:border-blue-300"
                  >
                    <p className="text-sm font-bold">同步任务</p>
                    <p className="mt-1 text-xs text-[var(--muted-text)]">可选能力 · 定时同步外部数据</p>
                  </Link>
                  <Link
                    to="/webhooks"
                    className="rounded-xl border border-[var(--border)] p-4 transition hover:border-blue-300"
                  >
                    <p className="text-sm font-bold">Webhook</p>
                    <p className="mt-1 text-xs text-[var(--muted-text)]">可选能力 · 查看事件和投递设置</p>
                  </Link>
                </div>
              </div>
            )}
            {tab === "认证" && (
              <div className="grid gap-3">
                <Field label="认证实例">
                  <select
                    className={fieldClass}
                    disabled={!canWrite || selectedHasConnections}
                    value={selectedAuthDraft}
                    onChange={(event) => setSelectedAuthDraft(event.target.value)}
                  >
                    {authInstancesFor(
                      systems.find((item) => item.service === selected.provider),
                      demo.authInstances,
                      demo.authSchemes,
                      true,
                    ).map((instance) => (
                      <option key={instance.id} value={instance.id}>
                        {instance.name}
                      </option>
                    ))}
                  </select>
                </Field>
                {selectedAuthInstance && (
                  <KeyValues
                    items={[
                      ["认证模板", authTemplateName(selectedAuthInstance.templateId, demo.authSchemes)],
                      ["Token 地址", selectedAuthInstance.tokenEndpoint || "不需要"],
                      ["Token JSON 路径", selectedAuthInstance.tokenPath || "不需要"],
                      ["请求头注入", `${selectedAuthInstance.headerName}: ${selectedAuthInstance.headerValueTemplate}`],
                    ]}
                  />
                )}
                <div className="flex justify-end gap-2">
                  <Button asChild variant="secondary">
                    <Link
                      to={`/auth?section=instances&system=${encodeURIComponent(provider)}&returnTo=${encodeURIComponent(`/integrations?new=1&system=${provider}`)}`}
                    >
                      管理认证实例
                    </Link>
                  </Button>
                  <Button
                    write
                    disabled={!selectedAuthDraft}
                    onClick={() => {
                      saveSelectedIntegration();
                    }}
                  >
                    保存选择
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
}
