import { useCanWrite } from "../permissions";
import { MutationForm } from "../ui";
import { useResourcePages, PageControls } from "../pagination";
import { formatRelative, durationLabel, kindLabel } from "../demo";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ChevronRight,
  CircleDot,
  Clock3,
  Gauge,
  RefreshCw,
  Search,
  Send,
  TimerReset,
  Webhook,
  XCircle,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useSearchParams } from "react-router";
import { operationKindLabel, statusLabel, useDemo, type DemoOperation } from "../demo";
import { Badge, Button, Card, CopyButton, Field, Modal, PageHeader, cn, fieldClass } from "../ui";
import { Tabs } from "./core-shared";
import { api } from "../api";

export function OperationsPage() {
  const demo = useDemo();
  const [searchParams, setSearchParams] = useSearchParams();
  const [query, setQuery] = useState("");
  const [integrationFilter, setIntegrationFilter] = useState(searchParams.get("integration") ?? "");
  const [connectionFilter, setConnectionFilter] = useState(searchParams.get("connection") ?? "");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [detail, setDetail] = useState<unknown>(null);
  const [kind, setKind] = useState("全部");
  const [status, setStatus] = useState("全部状态");
  const [selected, setSelected] = useState<DemoOperation | null>(null);
  const [metrics, setMetrics] = useState({
    requests: 0,
    successes: 0,
    failures: 0,
    successRate: 0,
    averageMs: 0,
    p95Ms: 0,
    pendingJobs: 0,
    activeConnections: 0,
  });
  const operationStatusKey = demo.operations.map((item) => `${item.id}:${item.status}`).join("|");
  useEffect(() => {
    void api
      .metrics()
      .then(setMetrics)
      .catch((error) => demo.notify(error));
  }, [operationStatusKey]);
  const history = useResourcePages(
    "/api/operations",
    {
      q: query,
      kind: kind === "全部" ? "" : kind.toLowerCase(),
      status: status === "全部状态" ? "" : status,
      integration: integrationFilter,
      connection: connectionFilter,
      task: searchParams.get("task") ?? "",
      from: from ? new Date(from).toISOString() : "",
      to: to ? new Date(to).toISOString() : "",
    },
    demo.authenticated,
  );
  const filtered: DemoOperation[] = history.items.map((item) => ({
    id: item.id,
    kind: kindLabel(item.kind),
    name: item.name,
    integration: demo.integrations.find((i) => i.id === item.integrationId)?.name ?? item.integrationId ?? "—",
    connection: demo.connections.find((c) => c.id === item.connectionId)?.name ?? item.connectionId ?? "—",
    status: item.status,
    duration: durationLabel(item.startedAt, item.completedAt),
    time: formatRelative(item.startedAt),
    messages: [],
  }));

  function openOperation(operation: DemoOperation) {
    setSelected(operation);
    setDetail(null);
    if (searchParams.get("run") !== operation.id) {
      const params = new URLSearchParams(searchParams);
      params.set("run", operation.id);
      setSearchParams(params);
    }
    void api
      .operation(operation.id)
      .then((detail) => {
        setDetail(detail);
        const messages = (detail.events ?? []).map((event: { level?: string; message: string; createdAt?: string }) => {
          const level = event.level && event.level !== "info" ? `[${event.level}] ` : "";
          const time = event.createdAt ? `${new Date(event.createdAt).toLocaleTimeString("zh-CN")} ` : "";
          return `${time}${level}${event.message}`;
        });
        if (detail.errorMessage) messages.push(`失败原因：${detail.errorMessage}`);
        setSelected((current) => (current?.id === operation.id ? { ...current, messages } : current));
      })
      .catch((error) => demo.notify(error));
  }
  useEffect(() => {
    const id = searchParams.get("run");
    if (!id || selected?.id === id) return;
    const operation = filtered.find((item) => item.id === id);
    if (operation) openOperation(operation);
    else
      void api
        .operation(id)
        .then((item) =>
          openOperation({
            id: item.id,
            kind: kindLabel(item.kind),
            name: item.name,
            integration: item.integrationId,
            connection: item.connectionId,
            status: item.status,
            duration: durationLabel(item.startedAt, item.completedAt),
            time: formatRelative(item.startedAt),
            messages: [],
          }),
        )
        .catch((e) => demo.notify(e));
  }, [searchParams.get("run")]);
  if (searchParams.get("view") === "metrics") return <MetricsPage />;
  return (
    <div className="grid gap-6">
      <PageHeader
        title="运行中心"
        description="统一查看 API 调用、同步任务、授权回调和 Webhook 的运行记录。"
        actions={
          <Button
            variant="secondary"
            onClick={() =>
              void Promise.all([demo.reload(), api.metrics().then(setMetrics)]).catch((error) => demo.notify(error))
            }
          >
            <RefreshCw className="size-4" />
            刷新
          </Button>
        }
      />
      <PageControls
        page={history}
        onClear={() => {
          setQuery("");
          setKind("全部");
          setStatus("全部状态");
          setIntegrationFilter("");
          setConnectionFilter("");
          setFrom("");
          setTo("");
        }}
      />
      <Tabs
        value="运行日志"
        onChange={(value) => setSearchParams(value === "指标监控" ? { view: "metrics" } : {})}
        items={["运行日志", "指标监控"]}
      />
      <section className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        <Metric
          label="成功率"
          value={`${metrics.successRate.toFixed(1)}%`}
          hint={`${metrics.successes} 次成功`}
          icon={CheckCircle2}
          tone="success"
        />
        <Metric
          label="运行中"
          value={String(demo.operations.filter((item) => item.status === "running" || item.status === "queued").length)}
          hint={`${metrics.pendingJobs} 个后台任务待处理`}
          icon={CircleDot}
          tone="info"
        />
        <Metric
          label="P95 耗时"
          value={`${Math.round(metrics.p95Ms)} ms`}
          hint={`平均 ${Math.round(metrics.averageMs)} ms`}
          icon={Gauge}
        />
        <Metric label="失败" value={String(metrics.failures)} hint="最近 24 小时" icon={AlertTriangle} tone="danger" />
      </section>
      <div className="flex flex-col gap-3 xl:flex-row">
        <div className="relative w-full max-w-xl">
          <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--muted-text)]" />
          <input
            className={`${fieldClass} pl-10`}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            aria-label="搜索运行记录"
            placeholder="搜索运行记录、连接账号或执行 ID"
          />
        </div>
        <select
          aria-label="运行类型"
          className={`${fieldClass} xl:w-44`}
          value={kind}
          onChange={(event) => setKind(event.target.value)}
        >
          <option>全部</option>
          <option value="Action">操作</option>
          <option value="Sync">同步</option>
          <option value="Webhook">Webhook</option>
          <option value="Auth">认证</option>
        </select>
        <select
          aria-label="运行状态"
          className={`${fieldClass} xl:w-44`}
          value={status}
          onChange={(event) => setStatus(event.target.value)}
        >
          <option>全部状态</option>
          <option value="success">成功</option>
          <option value="failed">失败</option>
          <option value="running">运行中</option>
          <option value="queued">排队中</option>
          <option value="cancelled">已取消</option>
          <option value="unknown">结果未知</option>
        </select>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="集成">
          <select
            className={fieldClass}
            value={integrationFilter}
            onChange={(e) => {
              setIntegrationFilter(e.target.value);
              setConnectionFilter("");
            }}
          >
            <option value="">全部集成</option>
            {demo.integrations.map((item) => (
              <option translate="no" key={item.id} value={item.id}>
                {item.displayName}
              </option>
            ))}
          </select>
        </Field>
        <Field label="连接">
          <select className={fieldClass} value={connectionFilter} onChange={(e) => setConnectionFilter(e.target.value)}>
            <option value="">全部连接</option>
            {demo.connections
              .filter(
                (c) =>
                  !integrationFilter ||
                  c.integration === demo.integrations.find((i) => i.id === integrationFilter)?.name,
              )
              .map((item) => (
                <option translate="no" key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
          </select>
        </Field>
        <Field label="开始时间">
          <input type="datetime-local" className={fieldClass} value={from} onChange={(e) => setFrom(e.target.value)} />
        </Field>
        <Field label="结束时间">
          <input type="datetime-local" className={fieldClass} value={to} onChange={(e) => setTo(e.target.value)} />
        </Field>
      </div>
      <div className="grid gap-3 md:hidden">
        {filtered.map((operation) => (
          <button key={operation.id} className="text-left" onClick={() => openOperation(operation)}>
            <Card className="p-4">
              <Status status={operation.status} />
              <p translate="no" className="mt-2 break-all font-semibold">
                {operation.name}
              </p>
              <p className="mt-1 text-xs">
                {operationKindLabel(operation.kind)} · {operation.time} · {operation.duration}
              </p>
              <p translate="no" className="mt-2 break-all text-xs">
                {operation.integration} / {operation.connection}
              </p>
            </Card>
          </button>
        ))}
      </div>
      <Card className="hidden overflow-hidden md:block">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[980px] text-left text-sm">
            <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
              <tr>
                <Th>状态</Th>
                <Th>运行记录</Th>
                <Th>类型</Th>
                <Th>集成配置</Th>
                <Th>连接账号</Th>
                <Th>耗时</Th>
                <Th>时间</Th>
                <Th />
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--border)]">
              {filtered.map((operation) => (
                <tr
                  key={operation.id}
                  onClick={() => openOperation(operation)}
                  className="cursor-pointer transition hover:bg-[var(--muted)]"
                >
                  <Td>
                    <Status status={operation.status} />
                  </Td>
                  <Td>
                    <p className="max-w-xs truncate font-semibold">{operation.name}</p>
                    <code className="text-xs text-[var(--muted-text)]">{operation.id}</code>
                  </Td>
                  <Td>
                    <Badge>{operationKindLabel(operation.kind)}</Badge>
                  </Td>
                  <Td>{operation.integration}</Td>
                  <Td>{operation.connection}</Td>
                  <Td>{operation.duration}</Td>
                  <Td>{operation.time}</Td>
                  <Td>
                    <button
                      type="button"
                      aria-label={`查看 ${operation.name} 运行详情`}
                      className="grid size-8 place-items-center rounded-lg text-[var(--muted-text)] transition hover:bg-[var(--surface)] hover:text-[var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                      onClick={(event) => {
                        event.stopPropagation();
                        openOperation(operation);
                      }}
                    >
                      <ChevronRight className="size-4" />
                    </button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      <Modal
        open={Boolean(selected)}
        onOpenChange={(open) => {
          if (!open) {
            setSelected(null);
            const params = new URLSearchParams(searchParams);
            params.delete("run");
            setSearchParams(params);
          }
        }}
        title={selected?.name ?? "运行记录"}
        description={`${selected?.id ?? ""} · ${operationKindLabel(selected?.kind ?? "")}`}
      >
        {selected && (
          <div className="grid gap-5">
            {selected.status === "unknown" && (
              <p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
                平台无法确认上游结果。请先核对上游是否已处理本次请求，再决定是否重试，以免重复执行。
              </p>
            )}
            {detail !== null && (
              <details>
                <summary className="cursor-pointer text-sm">输入、输出和请求详情</summary>
                <pre className="mt-2 max-h-80 overflow-auto text-xs">{JSON.stringify(detail, null, 2)}</pre>
              </details>
            )}
            <div className="grid grid-cols-3 gap-3">
              <SmallStat label="状态" value={<Status status={selected.status} />} />
              <SmallStat label="耗时" value={selected.duration} />
              <SmallStat label="时间" value={selected.time} />
            </div>
            <div>
              <p className="mb-3 text-sm font-bold">执行时间线</p>
              <div className="grid gap-0">
                {!selected.messages.length && (
                  <p className="text-sm text-[var(--muted-text)]">该运行记录没有事件明细。</p>
                )}
                {selected.messages.map((message, index) => (
                  <div key={`${index}-${message}`} className="grid grid-cols-[1.25rem_1fr] gap-3">
                    <div className="grid justify-items-center">
                      <span className="mt-1 size-2.5 rounded-full bg-emerald-500" />
                      {index < selected.messages.length - 1 && <span className="h-9 w-px bg-[var(--border)]" />}
                    </div>
                    <div>
                      <p className="text-sm font-medium">{message}</p>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}

export function MetricsPage() {
  const demo = useDemo();
  const [, setSearchParams] = useSearchParams();
  const [range, setRange] = useState("24 小时");
  const [refreshedAt, setRefreshedAt] = useState("");
  const [metrics, setMetrics] = useState({
    requests: 0,
    successes: 0,
    failures: 0,
    successRate: 0,
    averageMs: 0,
    p95Ms: 0,
    pendingJobs: 0,
    activeConnections: 0,
  });
  const operationStatusKey = demo.operations.map((item) => `${item.id}:${item.status}`).join("|");
  const hours = range === "7 天" ? 168 : range === "30 天" ? 720 : 24;
  function refreshMetrics() {
    void api
      .metrics(hours)
      .then((value) => {
        setMetrics(value);
        setRefreshedAt(new Date().toLocaleTimeString("zh-CN"));
      })
      .catch((error) => demo.notify(error));
  }
  useEffect(refreshMetrics, [hours, operationStatusKey]);
  return (
    <div className="grid gap-6">
      <PageHeader
        title="运行中心"
        description="查看 API 操作、同步、认证和 Webhook 的运行指标。"
        actions={
          <select
            aria-label="统计时间范围"
            className={`${fieldClass} w-36`}
            value={range}
            onChange={(event) => setRange(event.target.value)}
          >
            <option>24 小时</option>
            <option>7 天</option>
            <option>30 天</option>
          </select>
        }
      />
      <Tabs
        value="指标监控"
        onChange={(value) => setSearchParams(value === "指标监控" ? { view: "metrics" } : {})}
        items={["运行日志", "指标监控"]}
      />
      <section className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        <Metric label="总请求" value={String(metrics.requests)} hint={`${range}内`} icon={Activity} />
        <Metric
          label="成功率"
          value={`${metrics.successRate.toFixed(1)}%`}
          hint={`${metrics.successes} 次成功`}
          icon={CheckCircle2}
          tone="success"
        />
        <Metric
          label="错误"
          value={String(metrics.failures)}
          hint={`${metrics.pendingJobs} 个任务待处理`}
          icon={XCircle}
          tone="danger"
        />
        <Metric
          label="平均耗时"
          value={`${Math.round(metrics.averageMs)} ms`}
          hint={`P95 ${Math.round(metrics.p95Ms)} ms`}
          icon={Clock3}
        />
      </section>
      <section className="grid gap-4 xl:grid-cols-2">
        <Card className="p-5">
          <h2 className="font-bold">轻量监控口径</h2>
          <div className="mt-4 grid gap-3 text-sm text-[var(--muted-text)]">
            <p>指标统计已记录的运行结果和等待执行的任务。</p>
            <p>适合当前单机或小规模部署；需要长期趋势图时再接入专用时序系统。</p>
          </div>
        </Card>
        <Card className="p-5">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="font-bold">自动刷新</h2>
              <p className="text-sm text-[var(--muted-text)]">重新计算最近的运行指标</p>
            </div>
            <Badge>手动</Badge>
          </div>
          <div className="mt-5 grid gap-3">
            {[
              ["最近刷新", refreshedAt || "尚未刷新"],
              ["统计范围", range],
              ["待处理任务", String(metrics.pendingJobs)],
              ["活跃连接", String(metrics.activeConnections)],
            ].map(([label, value]) => (
              <div key={label} className="flex justify-between rounded-xl bg-[var(--muted)] px-4 py-3 text-sm">
                <span className="text-[var(--muted-text)]">{label}</span>
                <strong>{value}</strong>
              </div>
            ))}
          </div>
          <Button className="mt-4 w-full" variant="secondary" onClick={refreshMetrics}>
            立即刷新
          </Button>
        </Card>
      </section>
    </div>
  );
}

export function WebhooksPage() {
  const canWrite = useCanWrite();
  const demo = useDemo();
  const [webhookParams, setWebhookParams] = useSearchParams();
  const [tab, setTab] = useState(webhookParams.get("delivery") ? "投递记录" : "平台通知");
  const [publicBaseUrl, setPublicBaseUrl] = useState(window.location.origin);
  useEffect(() => {
    void api
      .settings()
      .then((settings) => setPublicBaseUrl(settings.publicBaseUrl || window.location.origin))
      .catch((error) => demo.notify(error));
  }, []);
  const [endpoints, setEndpoints] = useState<any[]>([]);
  const [editingSourceId, setEditingSourceId] = useState("");
  const [sourceStatus, setSourceStatus] = useState("active");
  const [deliveryFrom, setDeliveryFrom] = useState("");
  const [deliveryTo, setDeliveryTo] = useState("");
  const [deliveryQuery, setDeliveryQuery] = useState("");
  const [deliveryStatus, setDeliveryStatus] = useState("");
  const deliveryPage = useResourcePages(
    "/api/webhook-deliveries",
    {
      q: deliveryQuery,
      status: deliveryStatus,
      from: deliveryFrom ? new Date(deliveryFrom).toISOString() : "",
      to: deliveryTo ? new Date(deliveryTo).toISOString() : "",
    },
    demo.authenticated,
  );
  const [enabled, setEnabled] = useState(true);
  const [endpointId, setEndpointId] = useState("");
  const [endpointName, setEndpointName] = useState("默认通知端点");
  const [primaryUrl, setPrimaryUrl] = useState("");
  const [fallbackUrl, setFallbackUrl] = useState("");
  const [webhookSecret, setWebhookSecret] = useState("");
  const [subscribedEvents, setSubscribedEvents] = useState(["auth.*", "connection.*", "sync.*", "action.*"]);
  const [sources, setSources] = useState<any[]>([]);
  const [sourceOpen, setSourceOpen] = useState(false);
  const [sourceKey, setSourceKey] = useState("enterprise-events");
  const [sourceName, setSourceName] = useState("企业系统事件");
  const [sourceIntegrationId, setSourceIntegrationId] = useState("");
  const [sourceSecret, setSourceSecret] = useState("");
  const [deliveries, setDeliveries] = useState<
    Array<{ id: string; event: string; target: string; status: string; code: number; attempts: number; time: string }>
  >([]);
  const [selected, setSelected] = useState<(typeof deliveries)[number] | null>(null);
  const [deliveryAttempts, setDeliveryAttempts] = useState<
    Array<{
      id: string;
      attemptNo: number;
      targetUrl: string;
      requestId: string;
      httpStatus: number;
      durationMs: number;
      errorMessage: string;
      createdAt: string;
    }>
  >([]);
  const readyIntegrations = demo.integrations.filter((item) => item.status === "ready");
  function mapDeliveries(rows: any[]) {
    return rows.map((item) => ({
      id: item.id,
      event: item.eventType,
      target: item.endpointName,
      status: item.status,
      code: item.lastHttpStatus,
      attempts: item.attemptCount,
      time: item.createdAt ? new Date(item.createdAt).toLocaleString("zh-CN") : "—",
    }));
  }
  function refreshDeliveries() {
    return deliveryPage
      .refetch()
      .then((result) => setDeliveries(mapDeliveries(result.data?.pages.flatMap((page) => page.items) ?? [])));
  }
  function openDelivery(delivery: (typeof deliveries)[number]) {
    setSelected(delivery);
    const params = new URLSearchParams(webhookParams);
    params.set("delivery", delivery.id);
    setWebhookParams(params);
    setDeliveryAttempts([]);
    void api
      .webhookDeliveryAttempts(delivery.id)
      .then(setDeliveryAttempts)
      .catch((error) => demo.notify(error));
  }
  useEffect(() => {
    void Promise.all([api.webhookEndpoints(), api.webhookSources(), api.webhookDeliveries()])
      .then(([endpoints, sourceRows, deliveryRows]) => {
        setEndpoints(endpoints);
        setSources(sourceRows);
        setDeliveries(mapDeliveries(deliveryRows));
      })
      .catch((error) => demo.notify(error));
  }, [demo.operations.length]);
  useEffect(() => {
    setDeliveries(mapDeliveries(deliveryPage.items));
  }, [deliveryPage.items]);
  useEffect(() => {
    const id = webhookParams.get("delivery");
    const row = mapDeliveries(deliveryPage.items).find((item) => item.id === id);
    if (row && selected?.id !== id) openDelivery(row);
    else if (id && selected?.id !== id)
      void api
        .webhookDelivery(id)
        .then((item) => openDelivery(mapDeliveries([item])[0]))
        .catch((error) => demo.notify(error));
  }, [webhookParams.get("delivery"), deliveryPage.items]);
  function selectEndpoint(id: string) {
    const endpoint = endpoints.find((item) => item.id === id);
    setEndpointId(id);
    setEndpointName(endpoint?.name ?? "新通知端点");
    setPrimaryUrl(endpoint?.primaryUrl ?? "");
    setFallbackUrl(endpoint?.fallbackUrl ?? "");
    setWebhookSecret("");
    setEnabled(endpoint ? endpoint.status === "active" : true);
    setSubscribedEvents(endpoint?.subscribedEvents ?? ["action.*"]);
  }
  function saveEndpoint() {
    return api
      .saveWebhookEndpoint(
        {
          name: endpointName,
          primaryUrl,
          fallbackUrl,
          secret: webhookSecret,
          status: enabled ? "active" : "disabled",
          subscribedEvents,
        },
        endpointId,
      )
      .then((item) => {
        setEndpointId(item.id);
        setEndpoints((items) => [item, ...items.filter((row) => row.id !== item.id)]);
        setWebhookSecret("");
        demo.notify("出站 Webhook 设置已保存");
      })
      .catch((error) => demo.notify(error));
  }
  return (
    <div className="grid gap-6">
      <PageHeader
        title="Webhook"
        description="按需配置系统入站事件、平台出站通知，以及签名、重试和投递记录。"
        actions={
          tab === "平台通知" ? (
            <Button
              write
              disabled={!endpointId || !enabled}
              onClick={() =>
                endpointId &&
                void api
                  .testWebhookEndpoint(endpointId)
                  .then(() => refreshDeliveries())
                  .then(() => {
                    setTab("投递记录");
                    demo.notify("Webhook 测试事件已定向进入该端点的投递队列");
                  })
                  .catch((error) => demo.notify(error))
              }
            >
              <Send className="size-4" />
              发送测试
            </Button>
          ) : undefined
        }
      />
      <Tabs value={tab} onChange={setTab} items={["平台通知", "系统事件", "投递记录"]} />
      {tab === "平台通知" && (
        <Field label="通知端点">
          <select className={fieldClass} value={endpointId} onChange={(e) => selectEndpoint(e.target.value)}>
            <option value="">创建新端点</option>
            {endpoints.map((item) => (
              <option translate="no" key={item.id} value={item.id}>
                {item.name}
              </option>
            ))}
          </select>
        </Field>
      )}
      {tab === "平台通知" && (
        <div className="grid gap-4 xl:grid-cols-[1.25fr_.75fr]">
          <Card className="p-5">
            <fieldset disabled={!canWrite} className="contents">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <h2 className="font-bold">平台通知端点</h2>
                  <p className="mt-1 text-sm text-[var(--muted-text)]">
                    事件经签名后投递到主地址，失败时切换备用地址。
                  </p>
                </div>
                <Toggle enabled={enabled} onChange={setEnabled} />
              </div>
              <div className="mt-5 grid gap-4">
                <Field label="端点名称">
                  <input
                    className={fieldClass}
                    value={endpointName}
                    onChange={(e) => setEndpointName(e.target.value)}
                  />
                </Field>
                <Field label="主地址">
                  <input
                    className={fieldClass}
                    value={primaryUrl}
                    onChange={(event) => setPrimaryUrl(event.target.value)}
                    placeholder="https://example.com/webhooks/apihub"
                  />
                </Field>
                <Field label="备用地址">
                  <input
                    className={fieldClass}
                    value={fallbackUrl}
                    onChange={(event) => setFallbackUrl(event.target.value)}
                    placeholder="https://backup.example.com/hooks"
                  />
                </Field>
                <Field label="签名密钥">
                  <input
                    className={fieldClass}
                    type="password"
                    value={webhookSecret}
                    onChange={(event) => setWebhookSecret(event.target.value)}
                    placeholder={endpointId ? "留空表示不修改" : "请输入签名密钥"}
                  />
                </Field>
                <Field label="订阅事件">
                  <div className="flex flex-wrap gap-2">
                    {subscribedEvents.map((event) => (
                      <button
                        key={event}
                        aria-label={`移除 ${event} 事件`}
                        onClick={() => setSubscribedEvents((items) => items.filter((item) => item !== event))}
                        className="rounded-lg border border-blue-300 bg-blue-50 px-3 py-2 text-xs font-semibold text-blue-700 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-300"
                      >
                        {event} ×
                      </button>
                    ))}
                    <select
                      aria-label="添加订阅事件"
                      className="rounded-lg border border-[var(--border)] bg-[var(--surface)] px-3 py-2 text-xs font-semibold"
                      value=""
                      onChange={(event) => {
                        const value = event.target.value;
                        if (value) setSubscribedEvents((items) => [...new Set([...items, value])]);
                      }}
                    >
                      <option value="">+ 添加事件</option>
                      {["auth.*", "connection.*", "sync.*", "action.*", "audit.*"].map((event) => (
                        <option key={event} value={event} disabled={subscribedEvents.includes(event)}>
                          {event}
                        </option>
                      ))}
                    </select>
                  </div>
                </Field>
                <Button write onClick={saveEndpoint}>
                  保存设置
                </Button>
              </div>
            </fieldset>
          </Card>
          <Card className="p-5">
            <h2 className="font-bold">可靠性策略</h2>
            <div className="mt-4 grid gap-3">
              <PolicyRow icon={TimerReset} title="失败重试" text="最多 8 次，按指数退避重新排队" />
              <PolicyRow icon={RefreshCw} title="备用地址" text="主地址首次失败后改投备用地址" />
              <PolicyRow icon={CheckCircle2} title="HMAC-SHA256" text="对原始请求体计算签名" />
            </div>
          </Card>
        </div>
      )}
      {tab === "系统事件" && (
        <div className="grid gap-4 md:grid-cols-2">
          {sources.map((source) => (
            <div key={source.id}>
              <EndpointCard
                key={source.id}
                provider={source.name}
                path={new URL(`/webhooks/inbound/${encodeURIComponent(source.sourceKey)}`, publicBaseUrl).href}
                events={(source.subscribedEvents ?? []).join(", ")}
                status={source.status}
              />
              <Button
                write
                variant="secondary"
                onClick={() => {
                  setEditingSourceId(source.id);
                  setSourceKey(source.sourceKey);
                  setSourceName(source.name);
                  setSourceIntegrationId(source.integrationId);
                  setSourceStatus(source.status);
                  setSourceSecret("");
                  setSourceOpen(true);
                }}
              >
                编辑入口 / 更换密钥
              </Button>
              <Button
                write
                variant="ghost"
                onClick={() =>
                  api
                    .saveWebhookSource(
                      {
                        sourceKey: source.sourceKey,
                        name: source.name,
                        integrationId: source.integrationId,
                        signatureType: source.signatureType,
                        subscribedEvents: source.subscribedEvents,
                        secret: "",
                        status: source.status === "active" ? "disabled" : "active",
                        settings: source.settings ?? {},
                      },
                      source.id,
                    )
                    .then(() => api.webhookSources())
                    .then(setSources)
                    .catch((e) => demo.notify(e))
                }
              >
                {source.status === "active" ? "停用" : "启用"}
              </Button>
            </div>
          ))}
          <Card className="grid place-items-center border-dashed p-6">
            <Button
              write
              variant="secondary"
              onClick={() => {
                setEditingSourceId("");
                setSourceKey("enterprise-events");
                setSourceName("企业系统事件");
                setSourceStatus("active");
                setSourceSecret("");
                setSourceOpen(true);
              }}
            >
              <Webhook className="size-4" />
              添加服务提供商 Webhook
            </Button>
          </Card>
        </div>
      )}
      {tab === "投递记录" && (
        <>
          <PageControls page={deliveryPage} />
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="投递开始时间">
              <input
                type="datetime-local"
                className={fieldClass}
                value={deliveryFrom}
                onChange={(e) => setDeliveryFrom(e.target.value)}
              />
            </Field>
            <Field label="投递结束时间">
              <input
                type="datetime-local"
                className={fieldClass}
                value={deliveryTo}
                onChange={(e) => setDeliveryTo(e.target.value)}
              />
            </Field>
          </div>
          <Field label="搜索投递事件 / 目标">
            <input className={fieldClass} value={deliveryQuery} onChange={(e) => setDeliveryQuery(e.target.value)} />
          </Field>
          <Field label="投递状态">
            <select className={fieldClass} value={deliveryStatus} onChange={(e) => setDeliveryStatus(e.target.value)}>
              <option value="">全部状态</option>
              {["pending", "delivered", "retrying", "dead"].map((item) => (
                <option key={item} value={item}>
                  {statusLabel(item)}
                </option>
              ))}
            </select>
          </Field>
        </>
      )}
      {tab === "投递记录" && (
        <div className="grid gap-3 md:hidden">
          {deliveries.map((delivery) => (
            <button key={delivery.id} className="text-left" onClick={() => openDelivery(delivery)}>
              <Card className="p-4">
                <Badge>{statusLabel(delivery.status)}</Badge>
                <p className="mt-2 font-semibold break-all">{delivery.event}</p>
                <p translate="no" className="text-sm break-all">
                  {delivery.target}
                </p>
                <p className="mt-2 text-xs">
                  HTTP {delivery.code || "—"} · {delivery.attempts} 次 · {delivery.time}
                </p>
              </Card>
            </button>
          ))}
        </div>
      )}
      {tab === "投递记录" && (
        <Card className="hidden overflow-hidden md:block">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-left text-sm">
              <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
                <tr>
                  <Th>事件</Th>
                  <Th>目标</Th>
                  <Th>状态</Th>
                  <Th>HTTP</Th>
                  <Th>次数</Th>
                  <Th>时间</Th>
                  <Th />
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border)]">
                {deliveries.map((delivery) => (
                  <tr
                    key={delivery.id}
                    className="cursor-pointer hover:bg-[var(--muted)]"
                    onClick={() => openDelivery(delivery)}
                  >
                    <Td>
                      <p className="font-semibold">{delivery.event}</p>
                      <code className="text-xs text-[var(--muted-text)]">{delivery.id}</code>
                    </Td>
                    <Td>{delivery.target}</Td>
                    <Td>
                      <Badge
                        tone={
                          delivery.status === "delivered"
                            ? "success"
                            : delivery.status === "retrying"
                              ? "warning"
                              : "danger"
                        }
                      >
                        {statusLabel(delivery.status)}
                      </Badge>
                    </Td>
                    <Td>{delivery.code || "—"}</Td>
                    <Td>{delivery.attempts}</Td>
                    <Td>{delivery.time}</Td>
                    <Td>
                      <button
                        type="button"
                        aria-label={`查看 ${delivery.event} 投递详情`}
                        className="grid size-8 place-items-center rounded-lg text-[var(--muted-text)] transition hover:bg-[var(--surface)] hover:text-[var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                        onClick={(event) => {
                          event.stopPropagation();
                          openDelivery(delivery);
                        }}
                      >
                        <ChevronRight className="size-4" />
                      </button>
                    </Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}
      <Modal
        open={Boolean(selected)}
        onOpenChange={(open) => {
          if (!open) {
            setSelected(null);
            const params = new URLSearchParams(webhookParams);
            params.delete("delivery");
            setWebhookParams(params);
          }
        }}
        title={selected?.event ?? "Webhook 投递记录"}
        description={selected?.id ?? ""}
      >
        {selected && (
          <div className="grid gap-4">
            <div className="grid grid-cols-3 gap-3">
              <SmallStat label="状态" value={statusLabel(selected.status)} />
              <SmallStat label="HTTP" value={selected.code || "—"} />
              <SmallStat label="尝试次数" value={selected.attempts} />
            </div>
            <div>
              <p className="mb-3 text-sm font-bold">投递尝试</p>
              <div className="grid gap-2">
                {!deliveryAttempts.length && (
                  <p className="text-sm text-[var(--muted-text)]">尚未产生 HTTP 投递尝试。</p>
                )}
                {deliveryAttempts.map((attempt) => (
                  <div key={attempt.id} className="rounded-xl bg-[var(--muted)] p-3 text-xs">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <strong>第 {attempt.attemptNo} 次</strong>
                      <span>
                        HTTP {attempt.httpStatus || "—"} · {attempt.durationMs} ms ·{" "}
                        {new Date(attempt.createdAt).toLocaleString("zh-CN")}
                      </span>
                    </div>
                    <code className="mt-2 block break-all text-[var(--muted-text)]">{attempt.targetUrl}</code>
                    {attempt.errorMessage && <p className="mt-2 text-red-600">{attempt.errorMessage}</p>}
                  </div>
                ))}
              </div>
            </div>
            <Button
              write
              disabled={!(["retrying", "dead"] as string[]).includes(selected.status)}
              title={
                (["retrying", "dead"] as string[]).includes(selected.status)
                  ? "将失败投递重新加入队列"
                  : "只有重试中或已终止的失败投递可以手动重排"
              }
              onClick={() =>
                void api
                  .retryWebhookDelivery(selected.id)
                  .then(() => {
                    demo.notify("Webhook 投递任务已重新排队");
                    setSelected(null);
                  })
                  .catch((error) => demo.notify(error))
              }
            >
              <RefreshCw className="size-4" />
              重新投递
            </Button>
          </div>
        )}
      </Modal>
      <Modal
        open={sourceOpen}
        onOpenChange={setSourceOpen}
        title="添加系统事件入口"
        description="创建入站地址并使用 HMAC-SHA256 验证外部系统事件。"
      >
        <MutationForm
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            return api
              .saveWebhookSource(
                {
                  sourceKey,
                  name: sourceName,
                  integrationId: sourceIntegrationId,
                  status: sourceStatus,
                  signatureType: "hmac-sha256",
                  secret: sourceSecret,
                  subscribedEvents: ["*"],
                  settings: {},
                },
                editingSourceId,
              )
              .then((item) => {
                setSources((rows) => [item, ...rows.filter((row) => row.id !== item.id)]);
                setSourceOpen(false);
                setSourceSecret("");
                demo.notify("系统事件入口已创建");
              })
              .catch((error) => demo.notify(error));
          }}
        >
          <Field label="入口标识">
            <input
              className={fieldClass}
              value={sourceKey}
              onChange={(event) => setSourceKey(event.target.value)}
              required
            />
          </Field>
          <Field label="显示名称">
            <input
              className={fieldClass}
              value={sourceName}
              onChange={(event) => setSourceName(event.target.value)}
              required
            />
          </Field>
          <Field label="集成配置">
            <select
              className={fieldClass}
              value={sourceIntegrationId}
              onChange={(event) => setSourceIntegrationId(event.target.value)}
              required
            >
              <option value="">请选择集成</option>
              {readyIntegrations.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.displayName} · {item.name}
                </option>
              ))}
            </select>
          </Field>
          {!readyIntegrations.length && (
            <div className="rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
              请先创建一个就绪的集成配置，再添加系统事件入口。
            </div>
          )}
          <Field label="验签密钥">
            <input
              className={fieldClass}
              type="password"
              value={sourceSecret}
              onChange={(event) => setSourceSecret(event.target.value)}
              required
            />
          </Field>
          <Button write disabled={!readyIntegrations.length}>
            创建入口
          </Button>
        </MutationForm>
      </Modal>
    </div>
  );
}

function Metric({
  label,
  value,
  hint,
  icon: Icon,
  tone = "info",
}: {
  label: string;
  value: string;
  hint: string;
  icon: typeof Activity;
  tone?: "info" | "success" | "danger";
}) {
  const colors =
    tone === "success"
      ? "bg-emerald-50 text-emerald-600 dark:bg-emerald-950"
      : tone === "danger"
        ? "bg-red-50 text-red-600 dark:bg-red-950"
        : "bg-blue-50 text-blue-600 dark:bg-blue-950";
  return (
    <Card className="p-4">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-xs font-medium text-[var(--muted-text)]">{label}</p>
          <p className="mt-2 text-2xl font-extrabold">{value}</p>
        </div>
        <span className={cn("grid size-9 place-items-center rounded-xl", colors)}>
          <Icon className="size-4" />
        </span>
      </div>
      <p className="mt-3 text-xs text-[var(--muted-text)]">{hint}</p>
    </Card>
  );
}

function Status({ status }: { status: DemoOperation["status"] }) {
  return (
    <span className="inline-flex items-center gap-2">
      <span
        className={cn(
          "size-2 rounded-full",
          status === "success"
            ? "bg-emerald-500"
            : status === "failed"
              ? "bg-red-500"
              : status === "cancelled" || status === "unknown"
                ? "bg-slate-400"
                : "bg-blue-500 animate-pulse",
        )}
      />
      <span>{statusLabel(status)}</span>
    </span>
  );
}
function SmallStat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="rounded-xl bg-[var(--muted)] p-3">
      <p className="text-xs text-[var(--muted-text)]">{label}</p>
      <div className="mt-1 text-sm font-bold">{value}</div>
    </div>
  );
}
function Toggle({ enabled, onChange }: { enabled: boolean; onChange: (value: boolean) => void }) {
  return (
    <button
      role="switch"
      aria-label="启用出站 Webhook"
      aria-checked={enabled}
      onClick={() => onChange(!enabled)}
      className={cn("relative h-6 w-11 rounded-full transition", enabled ? "bg-blue-600" : "bg-slate-300")}
    >
      <span className={cn("absolute top-1 size-4 rounded-full bg-white transition", enabled ? "left-6" : "left-1")} />
    </button>
  );
}
function PolicyRow({ icon: Icon, title, text }: { icon: typeof TimerReset; title: string; text: string }) {
  return (
    <div className="flex gap-3 rounded-xl bg-[var(--muted)] p-3">
      <Icon className="mt-0.5 size-4 shrink-0 text-blue-600" />
      <div>
        <p className="text-sm font-semibold">{title}</p>
        <p className="mt-1 text-xs text-[var(--muted-text)]">{text}</p>
      </div>
    </div>
  );
}
function EndpointCard({
  provider,
  path,
  events,
  status,
}: {
  provider: string;
  path: string;
  events: string;
  status: string;
}) {
  return (
    <Card className="p-5">
      <div className="flex items-start justify-between">
        <span className="grid size-10 place-items-center rounded-xl bg-[var(--muted)]">
          <Webhook className="size-5 text-blue-600" />
        </span>
        <Badge tone={status === "active" ? "success" : "neutral"}>{statusLabel(status)}</Badge>
      </div>
      <h2 className="mt-4 font-bold">{provider}</h2>
      <code className="mt-1 block text-xs text-[var(--muted-text)]">{path}</code>
      <p className="mt-3 text-sm text-[var(--muted-text)]">{events}</p>
      <div className="mt-4">
        <CopyButton value={`${window.location.origin}${path}`} />
      </div>
    </Card>
  );
}
function Th({ children }: { children?: ReactNode }) {
  return <th className="whitespace-nowrap px-4 py-3 font-semibold">{children}</th>;
}
function Td({ children }: { children: ReactNode }) {
  return <td className="px-4 py-3.5">{children}</td>;
}
