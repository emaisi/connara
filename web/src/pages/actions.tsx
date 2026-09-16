import { MutationForm } from "../ui";
import { useResourcePages, PageControls } from "../pagination";

import { ArrowRight, Braces, CirclePlay, Plus } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router";
import { useDemo } from "../demo";
import { Badge, Button, Card, CopyButton, Field, Modal, PageHeader, fieldClass, textAreaClass } from "../ui";
import { api } from "../api";

import { type ActionCatalogItem, BuildFlow, SearchBox } from "./core-shared";
export function DemoActionsPage() {
  const demo = useDemo();
  const [searchParams] = useSearchParams();
  const requestedSystem = searchParams.get("system") ?? "";
  const [actions, setActions] = useState<ActionCatalogItem[]>([]);
  const [query, setQuery] = useState("");
  const [provider, setProvider] = useState("全部");
  const actionPage = useResourcePages(
    "/api/actions",
    { q: query, system: provider === "全部" ? "" : provider, status: "active", executable: "true" },
    demo.authenticated,
  );
  const [selected, setSelected] = useState<ActionCatalogItem | null>(null);
  const [connection, setConnection] = useState("");
  const [executionIntegration, setExecutionIntegration] = useState("");
  const [pending, setPending] = useState(false);
  const [actionSchema, setActionSchema] = useState('{"type":"object"}');
  const [input, setInput] = useState("");
  const [runId, setRunId] = useState("");
  const [resultFailed, setResultFailed] = useState(false);
  const [result, setResult] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [actionIntegration, setActionIntegration] = useState("");
  const [actionId, setActionId] = useState("github.custom_action");
  const [actionDescription, setActionDescription] = useState("调用企业自定义 API 接口。");
  const [actionMethod, setActionMethod] = useState("GET");
  const [actionPath, setActionPath] = useState("/api/v1/resources");
  const [actionScope, setActionScope] = useState("resources:read");
  const [actionInput, setActionInput] = useState('{\n  "page": 1,\n  "pageSize": 20\n}');
  const filtered = actions;
  const compatibleConnections = selected
    ? demo.connections.filter((item) => item.integration === executionIntegration && item.status === "active")
    : [];
  const readyIntegrations = demo.integrations.filter((item) => item.status === "ready");
  useEffect(() => {
    if (!requestedSystem) return;
    const system = demo.customSystems.find((item) => item.service === requestedSystem);
    if (system) setProvider(system.service);
  }, [demo.customSystems, requestedSystem]);
  useEffect(() => {
    if (!demo.authenticated) return;
    void Promise.resolve(actionPage.items)
      .then((rows) =>
        setActions(
          rows
            .filter((row) => row.status === "active" && row.executable)
            .map((row) => {
              const configured =
                demo.integrations.find((item) => item.status === "ready" && item.id === row.integrationId) ??
                demo.integrations.find((item) => item.status === "ready" && item.provider === row.systemKey);
              return {
                backendId: row.id,
                id: row.actionKey,
                systemKey: row.systemKey,
                provider: demo.customSystems.find((item) => item.service === row.systemKey)?.name ?? row.systemKey,
                integration: configured?.name ?? "",
                description: row.description,
                scopes: row.requiredScopes ?? [],
                mode: "local",
                method: row.httpMethod,
                path: row.relativePath,
                source: row.source === "custom" ? "custom" : "built-in",
                input: JSON.stringify(row.exampleInput ?? {}, null, 2),
                inputSchema: row.inputSchema,
                integrationId: row.integrationId,
              } as ActionCatalogItem;
            }),
        ),
      )
      .catch((error) => demo.notify(error));
  }, [demo.authenticated, demo.integrations, demo.customSystems, actionPage.items]);
  function openAction(action: ActionCatalogItem) {
    setSelected(action);
    const requested = readyIntegrations.find(
      (item) => item.name === searchParams.get("integration") && item.provider === action.systemKey,
    );
    const candidates = readyIntegrations.filter((item) => item.provider === action.systemKey);
    const chosen =
      requested?.name ??
      (action.source === "custom" ? action.integration : candidates.length === 1 ? candidates[0].name : "");
    setExecutionIntegration(chosen);
    setConnection(
      demo.connections.find((item) => item.id === searchParams.get("connection") && item.integration === chosen)
        ?.name ?? "",
    );
    setInput(action.input);
    setResult("");
    setRunId("");
  }
  function createAction(event: FormEvent) {
    event.preventDefault();
    const integration = demo.integrations.find((item) => item.name === actionIntegration && item.status === "ready");
    const id = actionId.trim();
    const path = actionPath.trim();
    if (!integration || !id || !path.startsWith("/")) return;
    try {
      JSON.parse(actionInput || "{}");
      const schema = JSON.parse(actionSchema);
      if (!schema || Array.isArray(schema) || typeof schema !== "object") throw new Error();
    } catch {
      demo.notify("输入示例必须是有效 JSON");
      return;
    }
    const action: ActionCatalogItem = {
      id,
      systemKey: integration.provider,
      provider: integration.displayName,
      integration: integration.name,
      description: actionDescription.trim() || "企业自定义 API 操作。",
      scopes: actionScope
        .split(/[ ,\n]+/)
        .map((item) => item.trim())
        .filter(Boolean),
      mode: "local",
      method: actionMethod,
      path,
      source: "custom",
      input: actionInput || "{}",
    };
    return api
      .saveAction({
        actionKey: id,
        name: id,
        description: action.description,
        systemId: integration.systemId,
        integrationId: integration.id,
        httpMethod: action.method,
        relativePath: action.path,
        requiredScopes: action.scopes,
        inputSchema: JSON.parse(actionSchema),
        outputSchema: {},
        exampleInput: JSON.parse(action.input),
        status: "active",
      })
      .then((saved) => {
        const created = { ...action, backendId: saved.id };
        setActions((items) => [created, ...items.filter((item) => item.id !== id)]);
        setCreateOpen(false);
        setProvider("全部");
        setQuery("");
        openAction(created);
        demo.notify(`API 操作 ${id} 已保存，可以开始验证`);
      })
      .catch((error) => demo.notify(error));
  }
  async function execute() {
    if (!selected || pending) return;
    let parsed: unknown;
    try {
      parsed = JSON.parse(input || "{}");
    } catch {
      setResult(JSON.stringify({ code: "invalid_input", message: "输入不是有效 JSON" }, null, 2));
      return;
    }
    const selectedConnection = demo.connections.find(
      (item) => item.name === connection && item.integration === executionIntegration,
    );
    if (!selectedConnection) return;
    setPending(true);
    const started = performance.now();
    try {
      const value = await api.testAction(selected.backendId ?? selected.id, {
        input: parsed,
        connectionKey: selectedConnection.id,
        integrationId: demo.integrations.find((item) => item.name === executionIntegration)?.id,
      });
      setResultFailed(false);
      setRunId(value.meta?.operationId ?? "");
      setResult(
        JSON.stringify(
          {
            httpStatus: value.meta?.providerStatus,
            status: "success",
            durationMs: Math.round(performance.now() - started),
            result: value,
          },
          null,
          2,
        ),
      );
    } catch (error) {
      setResultFailed(true);
      const e = error as {
        code?: string;
        requestId?: string;
        operationId?: string;
        rawMessage?: string;
        message: string;
      };
      setRunId(e.operationId ?? "");
      setResult(
        JSON.stringify(
          {
            status: e.code === "provider_request_failed" ? "unknown" : "failed",
            code: e.code || "execution_failed",
            requestId: e.requestId,
            message: e.message,
            detail: e.rawMessage,
            durationMs: Math.round(performance.now() - started),
          },
          null,
          2,
        ),
      );
    } finally {
      setPending(false);
      await demo.reload();
    }
  }
  let parsedInput: unknown = {};
  let inputValid = true;
  try {
    parsedInput = JSON.parse(input || "{}");
  } catch {
    inputValid = false;
  }
  const requestBody = JSON.stringify({
    input: parsedInput,
    connectionKey:
      demo.connections.find((c) => c.name === connection && c.integration === executionIntegration)?.id ?? "",
    integrationId: demo.integrations.find((i) => i.name === executionIntegration)?.id ?? "",
  });
  const quote = (value: string) => "'" + value.replaceAll("'", "'\\''") + "'";
  const runtimeCurl = selected
    ? `curl -X POST "$APIHUB_URL/v1/actions/${selected.id}" -H "Authorization: Bearer $APIHUB_TOKEN" -H "Content-Type: application/json" -d ${quote(requestBody)}`
    : "";
  return (
    <div className="grid gap-6">
      <PageHeader
        title="API 操作"
        description="把系统接口定义成可复用操作，并选择连接账号验证实际调用。"
        actions={
          <>
            <Badge tone="info">{actions.length} 个可执行定义</Badge>
            <Button
              write
              disabled={!readyIntegrations.length}
              onClick={() => {
                setActionIntegration(readyIntegrations[0]?.name ?? "");
                setCreateOpen(true);
              }}
            >
              <Plus className="size-4" />
              添加自定义操作
            </Button>
          </>
        }
      />
      <PageControls
        page={actionPage}
        onClear={() => {
          setQuery("");
          setProvider("全部");
        }}
      />
      <BuildFlow current="actions" />
      <Card className="grid gap-4 p-4 md:grid-cols-3">
        {[
          ["1. 定义接口", "选择所属集成，填写 HTTP 方法、相对路径和输入示例。"],
          ["2. 选择账号", "从该集成的连接账号中选择一个，由服务端注入真实凭据。"],
          ["3. 验证使用", "运行测试并查看结果；确认后可通过 REST API 或 cURL 使用。"],
        ].map(([title, description]) => (
          <div key={title} className="rounded-xl bg-[var(--muted)] p-3">
            <p className="text-sm font-bold">{title}</p>
            <p className="mt-1 text-xs leading-5 text-[var(--muted-text)]">{description}</p>
          </div>
        ))}
      </Card>
      <div className="flex flex-col gap-3 sm:flex-row">
        <SearchBox value={query} onChange={setQuery} placeholder="搜索操作标识、请求路径或系统" />
        <select
          className={`${fieldClass} sm:w-52`}
          aria-label="操作所属系统"
          value={provider}
          onChange={(event) => setProvider(event.target.value)}
        >
          <option>全部</option>
          {demo.customSystems.map((item) => (
            <option translate="no" key={item.service} value={item.service}>
              {item.name} · {item.service}
            </option>
          ))}
        </select>
      </div>
      <Card className="overflow-hidden">
        <div className="divide-y divide-[var(--border)]">
          {filtered.map((action) => (
            <button
              key={action.id}
              onClick={() => openAction(action)}
              className="flex w-full flex-col gap-3 p-4 text-left transition hover:bg-[var(--muted)] sm:flex-row sm:items-center"
            >
              <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-[var(--muted)] text-blue-600">
                <Braces className="size-5" />
              </span>
              <div className="min-w-0 flex-1">
                <code className="text-sm font-bold">{action.id}</code>
                <p translate="no" className="mt-1 text-sm text-[var(--muted-text)]">
                  {action.description}
                </p>
                <code className="mt-2 block truncate text-xs text-[var(--muted-text)]">
                  {action.method} {action.path}
                </code>
              </div>
              <div className="flex flex-wrap gap-2">
                <Badge>{action.provider}</Badge>
                <Badge tone={action.source === "custom" ? "warning" : "neutral"}>
                  {action.source === "custom" ? "自定义" : "内置"}
                </Badge>
                <Badge tone={action.integration ? "success" : "warning"}>
                  {action.integration ? "可验证" : "待配置集成"}
                </Badge>
              </div>
              <ArrowRight className="hidden size-4 text-[var(--muted-text)] sm:block" />
            </button>
          ))}
        </div>
      </Card>
      <Modal
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
        title={selected?.id ?? "API 操作"}
        description={selected?.description ?? ""}
      >
        {selected && (
          <div className="grid gap-5">
            <div className="flex flex-wrap gap-2">
              <Badge>{selected.method}</Badge>
              <Badge>{selected.path}</Badge>
              <Badge tone={selected.integration ? "success" : "warning"}>
                {selected.integration ? "可验证" : "待配置集成"}
              </Badge>
              {selected.scopes.map((scope) => (
                <Badge key={scope}>{scope}</Badge>
              ))}
            </div>
            <Field label="执行集成">
              <select
                className={fieldClass}
                value={executionIntegration}
                onChange={(event) => {
                  setExecutionIntegration(event.target.value);
                  setConnection("");
                }}
              >
                <option value="">请选择执行集成</option>
                {readyIntegrations
                  .filter(
                    (item) =>
                      item.provider === selected.systemKey &&
                      (selected.source !== "custom" || item.name === selected.integration),
                  )
                  .map((item) => (
                    <option translate="no" key={item.id} value={item.name}>
                      {item.displayName} · {item.name}
                    </option>
                  ))}
              </select>
            </Field>
            <Field label="连接账号" hint="验证时会使用该账号保存的凭据。">
              <select className={fieldClass} value={connection} onChange={(event) => setConnection(event.target.value)}>
                <option value="">选择连接账号</option>
                {compatibleConnections.map((item) => (
                  <option key={item.id} value={item.name}>
                    {item.name} · {item.endUser}
                  </option>
                ))}
              </select>
            </Field>
            {!selected.integration && (
              <div className="flex items-center justify-between gap-3 rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
                该系统还没有就绪的集成配置。
                <Button asChild variant="secondary">
                  <Link to={`/integrations?new=1&provider=${encodeURIComponent(selected.systemKey)}`}>
                    创建集成配置
                  </Link>
                </Button>
              </div>
            )}
            {Boolean(selected.integration) && !compatibleConnections.length && (
              <div className="flex items-center justify-between gap-3 rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
                该集成还没有可用的连接账号。
                <Button asChild variant="secondary">
                  <Link
                    to={`/connections?new=1&integration=${encodeURIComponent(executionIntegration)}&system=${encodeURIComponent(selected.systemKey)}`}
                  >
                    创建连接账号
                  </Link>
                </Button>
              </div>
            )}
            <Field label="输入 JSON">
              <textarea
                className={`${textAreaClass} min-h-40 font-mono text-xs`}
                value={input}
                onChange={(event) => setInput(event.target.value)}
              />
            </Field>
            {!inputValid && (
              <p role="alert" className="text-red-600 text-sm">
                输入不是有效 JSON
              </p>
            )}
            <div className="flex flex-wrap gap-2">
              <Button write disabled={pending || !inputValid || !connection || !executionIntegration} onClick={execute}>
                <CirclePlay className="size-4" />
                {pending ? "正在验证…" : "运行验证"}
              </Button>
              {connection && inputValid && (
                <CopyButton
                  value={`curl -X POST "$APIHUB_URL/api/actions/${selected?.backendId ?? selected?.id}/test" -b "$APIHUB_COOKIE_JAR" -H "Content-Type: application/json" -d ${quote(requestBody)}`}
                />
              )}
            </div>
            <details className="rounded-xl border p-3">
              <summary className="cursor-pointer text-sm">交付给业务调用方</summary>
              <p className="my-2 text-sm">创建限定此操作与连接的运行时令牌，再复制调用示例。</p>
              <Link
                className="text-blue-600 text-sm"
                to={`/access?action=${encodeURIComponent(selected.id)}&connection=${encodeURIComponent(demo.connections.find((c) => c.name === connection && c.integration === executionIntegration)?.id ?? "")}`}
              >
                创建运行时令牌
              </Link>
              <pre className="my-2 overflow-auto text-xs">{runtimeCurl}</pre>
              {connection && inputValid && <CopyButton value={runtimeCurl} />}
            </details>
            {result && (
              <div>
                <p className="mb-2 text-sm font-bold">执行结果</p>
                <Link
                  className="text-sm text-blue-600"
                  to={
                    runId
                      ? `/operations?run=${encodeURIComponent(runId)}`
                      : `/operations?integration=${demo.integrations.find((item) => item.name === executionIntegration)?.id ?? ""}&connection=${demo.connections.find((item) => item.name === connection)?.id ?? ""}`
                  }
                >
                  查看本连接运行详情
                </Link>
                <pre
                  className={`max-h-64 overflow-auto rounded-xl bg-slate-950 p-4 text-xs ${resultFailed ? "text-red-300" : "text-emerald-300"}`}
                >
                  {result}
                </pre>
              </div>
            )}
            <details className="rounded-xl border border-[var(--border)] p-4">
              <summary className="cursor-pointer text-sm font-bold">操作定义</summary>
              <pre className="mt-3 overflow-auto text-xs text-[var(--muted-text)]">
                {JSON.stringify(
                  {
                    integration: selected.integration,
                    method: selected.method,
                    path: selected.path,
                    requiredScopes: selected.scopes,
                    inputSchema: selected.inputSchema ?? {},
                  },
                  null,
                  2,
                )}
              </pre>
            </details>
          </div>
        )}
      </Modal>
      <Modal
        open={createOpen}
        onOpenChange={setCreateOpen}
        title="添加自定义 API 操作"
        description="把某个集成的 HTTP 接口定义成可以复用、验证和审计的操作。"
      >
        <MutationForm className="grid gap-4" onSubmit={createAction}>
          <Field label="所属集成" hint="操作只能使用该集成下的连接账号。">
            <select
              className={fieldClass}
              value={actionIntegration}
              onChange={(event) => setActionIntegration(event.target.value)}
              required
            >
              <option value="">请选择就绪的集成</option>
              {readyIntegrations.map((item) => (
                <option key={item.id} value={item.name}>
                  {item.displayName} · {item.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="操作标识" hint="建议使用 system.action，例如 erp.list_orders。">
            <input
              className={fieldClass}
              value={actionId}
              onChange={(event) => setActionId(event.target.value)}
              placeholder="erp.list_orders"
              required
            />
          </Field>
          <Field label="用途说明">
            <input
              className={fieldClass}
              value={actionDescription}
              onChange={(event) => setActionDescription(event.target.value)}
              placeholder="查询 ERP 订单列表"
            />
          </Field>
          <div className="grid gap-4 sm:grid-cols-[9rem_1fr]">
            <Field label="请求方法">
              <select
                className={fieldClass}
                value={actionMethod}
                onChange={(event) => setActionMethod(event.target.value)}
              >
                {["GET", "POST", "PUT", "PATCH", "DELETE"].map((method) => (
                  <option key={method}>{method}</option>
                ))}
              </select>
            </Field>
            <Field label="相对路径" hint="必须以 / 开头，可使用 {变量}。">
              <input
                className={fieldClass}
                value={actionPath}
                onChange={(event) => setActionPath(event.target.value)}
                placeholder="/api/v1/orders/{orderId}"
                pattern="/.*"
                required
              />
            </Field>
          </div>
          <Field label="所需权限" hint="多个权限可用空格、逗号或换行分隔。">
            <input
              className={fieldClass}
              value={actionScope}
              onChange={(event) => setActionScope(event.target.value)}
              placeholder="orders:read"
            />
          </Field>
          <Field label="输入约束 Schema JSON" hint="后端按此约束校验调用参数。">
            <textarea
              className={textAreaClass}
              value={actionSchema}
              onChange={(e) => setActionSchema(e.target.value)}
            />
          </Field>
          <Field label="输入 JSON 示例" hint="保存后会直接进入验证控制台。">
            <textarea
              className={`${textAreaClass} min-h-32 font-mono text-xs`}
              value={actionInput}
              onChange={(event) => setActionInput(event.target.value)}
            />
          </Field>
          <div className="rounded-xl bg-blue-50 p-3 text-xs leading-5 text-blue-800 dark:bg-blue-950 dark:text-blue-200">
            这里只填写相对路径，不允许操作自行指定任意主机。基础地址来自集成配置，认证 Header、Token
            或签名由服务端按连接账号注入。
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setCreateOpen(false)}>
              取消
            </Button>
            <Button write type="submit" disabled={!actionIntegration}>
              <CirclePlay className="size-4" />
              保存并验证
            </Button>
          </div>
        </MutationForm>
      </Modal>
    </div>
  );
}
