import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { Plus, CirclePlay, ArrowLeft } from "lucide-react";
import { useResourcePages, PageControls } from "../pagination";
import { useDemo } from "../demo";
import { api, ApiError } from "../api";
import { Badge, Button, Card, CopyButton, Field, PageHeader, fieldClass, textAreaClass } from "../ui";
import { SearchBox, Tabs } from "./core-shared";
import { RequestResponseDetails } from "./request-response-details";
import { ApiDefinitionEditor, type ApiDefinition } from "./api-definition-editor";

interface TargetOption {
  integration: { id: string; name: string; systemKey: string; baseUrl: string; version: number; targetVersion: number };
  requiresAccount: boolean;
  accounts: {
    id: string;
    name: string;
    revision: number;
    status: string;
    enabled?: boolean;
    lastVerifiedAt?: string;
    verifiedRevision: number;
    verifiedTargetVersion: number;
  }[];
}
interface RequestPreview {
  request: { method: string; url: string; headers: Record<string, string[]>; body: unknown };
  apiVersion: number;
  integrationVersion: number;
  accountRevision: number;
  authentication: { required: boolean; accountName: string; instanceName: string };
  verified: boolean;
}
export function DemoActionsPage() {
  const demo = useDemo();
  const [params, setParams] = useSearchParams();
  const system = params.get("system") ?? "";
  const detailId = params.get("api") ?? "";
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState(system);
  const [status, setStatus] = useState("");
  const page = useResourcePages(
    "/api/actions",
    { q: query, system: filter, status, executable: "true" },
    demo.authenticated,
  );
  const [selected, setSelected] = useState<ApiDefinition | null>(null);
  const [editing, setEditing] = useState(false);
  const [creating, setCreating] = useState(params.get("new") === "1");
  const [tab, setTab] = useState("定义");
  const [targets, setTargets] = useState<TargetOption[]>([]);
  const [targetId, setTargetId] = useState("");
  const [accountId, setAccountId] = useState("");
  const [input, setInput] = useState("{}");
  const [preview, setPreview] = useState<RequestPreview | null>(null);
  const [previewError, setPreviewError] = useState("");
  const [targetError, setTargetError] = useState("");
  const [pending, setPending] = useState(false);
  const [confirmTarget, setConfirmTarget] = useState(false);
  const [result, setResult] = useState<any>(null);
  const [runId, setRunId] = useState("");
  const [records, setRecords] = useState<any[]>([]);
  const epoch = useRef(0);
  const target = targets.find((t) => t.integration.id === targetId);
  const validAccounts =
    target?.accounts.filter(
      (a) =>
        a.enabled !== false &&
        a.status === "active" &&
        a.lastVerifiedAt &&
        a.verifiedRevision === a.revision &&
        a.verifiedTargetVersion === target.integration.targetVersion,
    ) ?? [];
  function chooseTarget(id: string, options = targets) {
    setTargetId(id);
    setResult(null);
    setRunId("");
    setPreview(null);
    const t = options.find((v) => v.integration.id === id);
    const accounts =
      t?.accounts.filter(
        (a) =>
          a.enabled !== false &&
          a.status === "active" &&
          a.lastVerifiedAt &&
          a.verifiedRevision === a.revision &&
          a.verifiedTargetVersion === t.integration.targetVersion,
      ) ?? [];
    const requested = params.get("connection");
    setAccountId(accounts.find((a) => a.id === requested)?.id ?? (accounts.length === 1 ? accounts[0].id : ""));
  }
  async function open(row: ApiDefinition) {
    const generation = ++epoch.current;
    setSelected(row);
    setConfirmTarget(false);
    setInput(JSON.stringify(row.exampleInput ?? {}, null, 2));
    setPreview(null);
    setResult(null);
    setRunId("");
    setTab("定义");
    setTargets([]);
    setTargetId("");
    setAccountId("");
    setTargetError("");
    const next = new URLSearchParams(params);
    next.set("api", row.id);
    next.delete("new");
    setParams(next);
    try {
      const options = (await api.actionExecutionOptions(row.id)) as TargetOption[];
      if (generation !== epoch.current) return;
      setTargets(options);
      const requested = options.find(
        (t) =>
          t.integration.id === params.get("integration") ||
          demo.integrations.find((i) => i.id === t.integration.id)?.name === params.get("integration"),
      );
      chooseTarget(requested?.integration.id ?? (options.length === 1 ? options[0].integration.id : ""), options);
    } catch (e) {
      if (generation === epoch.current) setTargetError((e as Error).message);
    }
  }
  useEffect(() => {
    if (detailId && selected?.id !== detailId) {
      const row = page.items.find((r) => r.id === detailId);
      if (row) void open(row);
      else
        void api
          .action(detailId)
          .then((row) => open(row))
          .catch((e) => demo.notify(e));
    }
  }, [detailId, page.items]);
  useEffect(() => {
    if (system) setFilter(system);
  }, [system]);
  let parsed: Record<string, unknown> = {};
  let inputError = "";
  try {
    const p = JSON.parse(input);
    if (!p || Array.isArray(p) || typeof p !== "object") throw new Error();
    parsed = p;
  } catch {
    inputError = "输入必须是 JSON 对象";
  }
  const serialized = JSON.stringify(parsed);
  useEffect(() => {
    setPreview(null);
    setPreviewError("");
    if (!selected || !target || inputError || (target.requiresAccount && !accountId)) return;
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void api
        .previewAction(selected.id, {
          integrationId: targetId,
          connectionKey: accountId,
          input: JSON.parse(serialized),
        })
        .then((value) => {
          if (!cancelled) setPreview(value);
        })
        .catch((e) => {
          if (!cancelled) setPreviewError((e as Error).message);
        });
    }, 250);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [selected, target, targetId, accountId, serialized, inputError]);
  useEffect(() => {
    if (!selected || tab !== "运行记录") return;
    let cancelled = false;
    void api
      .runs(100, selected.id)
      .then((rows) => {
        if (!cancelled) setRecords(rows.filter((r: any) => r.actionId === selected.id));
      })
      .catch((e) => demo.notify(e));
    return () => {
      cancelled = true;
    };
  }, [selected, tab, runId]);
  function close() {
    ++epoch.current;
    setSelected(null);
    setEditing(false);
    setCreating(false);
    const next = new URLSearchParams(params);
    next.delete("api");
    next.delete("new");
    setParams(next);
  }
  async function execute() {
    if (!selected || !target || !preview || !preview.verified || pending || confirmTarget) return;
    setPending(true);
    const generation = epoch.current;
    const started = performance.now();
    try {
      const value = await api.testAction(selected.id, {
        input: parsed,
        integrationId: targetId,
        connectionKey: accountId || undefined,
        expectedApiVersion: preview.apiVersion,
        expectedIntegrationVersion: preview.integrationVersion,
        expectedAccountRevision: preview.accountRevision,
      });
      if (generation !== epoch.current) return;
      setRunId(value.meta?.operationId ?? "");
      setResult({
        status: value.meta?.outcome ?? "success",
        durationMs: Math.round(performance.now() - started),
        response: value,
      });
    } catch (e) {
      if (generation !== epoch.current) return;
      const error = e as ApiError;
      setRunId(error.operationId);
      setResult({
        status: error.outcome ?? "failed",
        checks: error.meta?.checks,
        code: error.code,
        message: error.message,
        requestId: error.requestId,
        durationMs: Math.round(performance.now() - started),
      });
      if (error.status === 409) {
        setPreview(null);
        setConfirmTarget(true);
        try {
          const [current, options] = await Promise.all([
            api.action(selected.id),
            api.actionExecutionOptions(selected.id),
          ]);
          if (generation !== epoch.current) return;
          setSelected(current);
          setTargets(options);
          const next = options.find((t: TargetOption) => t.integration.id === targetId);
          setTargetId(next?.integration.id ?? "");
          const accounts =
            next?.accounts.filter(
              (a: TargetOption["accounts"][number]) =>
                a.enabled !== false &&
                a.status === "active" &&
                a.lastVerifiedAt &&
                a.verifiedRevision === a.revision &&
                a.verifiedTargetVersion === next.integration.targetVersion,
            ) ?? [];
          setAccountId(
            accounts.some((a: TargetOption["accounts"][number]) => a.id === accountId)
              ? accountId
              : accounts.length === 1
                ? accounts[0].id
                : "",
          );
        } catch (refreshError) {
          setTargetError((refreshError as Error).message);
        }
      }
    } finally {
      setPending(false);
    }
  }
  if (creating || editing)
    return (
      <ApiDefinitionEditor
        key={editing ? selected?.id : "new"}
        initial={editing && selected ? selected : undefined}
        systemKey={system || filter}
        onCancel={() => (editing ? setEditing(false) : close())}
        onSaved={(row) => {
          setCreating(false);
          setEditing(false);
          void open(row);
          demo.notify("API 定义已保存，尚未调用上游。");
        }}
      />
    );
  if (selected)
    return (
      <div className="grid min-w-0 gap-5">
        <PageHeader
          title={selected.name || selected.actionKey}
          description={`${selected.httpMethod} ${selected.relativePath}`}
          actions={
            <Button variant="secondary" onClick={close}>
              <ArrowLeft className="size-4" />
              返回 API 列表
            </Button>
          }
        />
        <div className="flex flex-wrap gap-2">
          <Badge>{demo.customSystems.find((s) => s.service === selected.systemKey)?.name ?? selected.systemKey}</Badge>
          <Badge>{selected.status === "active" ? "可调用" : selected.status === "draft" ? "草稿" : "停用"}</Badge>
        </div>
        <Tabs value={tab} onChange={setTab} items={["定义", "测试", "运行记录"]} />
        {tab === "定义" && (
          <Card className="min-w-0 p-5">
            <div className="grid gap-4">
              <p>{selected.description}</p>
              <p className="text-sm text-[var(--muted-text)]">API 属于系统；测试和调用时再选择集成与账号。</p>
              <pre className="max-h-96 overflow-auto text-xs">
                {JSON.stringify(
                  {
                    method: selected.httpMethod,
                    path: selected.relativePath,
                    request: selected.requestConfig,
                    response: selected.responseConfig,
                    inputSchema: selected.inputSchema,
                    outputSchema: selected.outputSchema,
                  },
                  null,
                  2,
                )}
              </pre>
              {selected.source === "custom" && (
                <Button write className="w-fit" onClick={() => setEditing(true)}>
                  编辑 API
                </Button>
              )}
            </div>
          </Card>
        )}
        {tab === "测试" && (
          <div className="grid min-w-0 gap-5 lg:grid-cols-2">
            <Card className="grid min-w-0 content-start gap-4 p-5">
              {targetError && (
                <p role="alert" className="break-words text-sm text-red-600">
                  {targetError}
                </p>
              )}
              <Field label="执行集成">
                <select
                  className={fieldClass}
                  value={targetId}
                  disabled={pending}
                  onChange={(e) => chooseTarget(e.target.value)}
                >
                  <option value="">请选择执行集成</option>
                  {targets.map((t) => (
                    <option key={t.integration.id} value={t.integration.id}>
                      {t.integration.name}
                    </option>
                  ))}
                </select>
              </Field>
              {!targets.length && !targetError && (
                <p className="text-sm">
                  此系统尚未配置访问环境。
                  <Link
                    className="ml-2 text-blue-600"
                    to={`/integrations?new=1&system=${encodeURIComponent(selected.systemKey)}`}
                  >
                    配置集成
                  </Link>
                </p>
              )}
              {target?.requiresAccount && (
                <Field label="执行账号">
                  <select
                    className={fieldClass}
                    value={accountId}
                    disabled={pending}
                    onChange={(e) => {
                      setAccountId(e.target.value);
                      setResult(null);
                      setRunId("");
                    }}
                  >
                    <option value="">选择执行账号</option>
                    {target.accounts.map((a) => (
                      <option key={a.id} value={a.id} disabled={!validAccounts.some((valid) => valid.id === a.id)}>
                        {a.name}
                        {validAccounts.some((valid) => valid.id === a.id)
                          ? " · 已验证"
                          : a.enabled === false || a.status === "disabled"
                            ? " · 已停用"
                            : " · 需要重新验证"}
                      </option>
                    ))}
                  </select>
                </Field>
              )}
              {target && !target.requiresAccount && <p className="text-sm">无需认证</p>}
              {target?.requiresAccount && !validAccounts.length && (
                <div className="grid gap-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
                  {target.accounts.length
                    ? "该集成已有账号，但尚未就绪，请验证或修复已有账号。"
                    : "该集成尚未添加执行账号。"}
                  <Link
                    className="text-blue-600"
                    to={`/auth?section=accounts&integration=${encodeURIComponent(demo.integrations.find((i) => i.id === targetId)?.name ?? targetId)}${target.accounts.length ? `&connection=${encodeURIComponent(target.accounts[0].id)}` : ""}`}
                  >
                    {target.accounts.length ? "查看已有账号" : "前往账号管理"}
                  </Link>
                </div>
              )}
              {selected.requestConfig?.schemaVersion === 1 && selected.requestConfig.parameters.length > 0 ? (
                <div className="grid gap-3">
                  {selected.requestConfig.parameters.map((p) => (
                    <Field key={p.name} label={`${p.name}${p.required ? " *" : ""}`} hint={`${p.in} · ${p.type}`}>
                      <input
                        className={fieldClass}
                        disabled={pending}
                        value={
                          parsed[p.name] === undefined
                            ? ""
                            : typeof parsed[p.name] === "string"
                              ? String(parsed[p.name])
                              : JSON.stringify(parsed[p.name])
                        }
                        onChange={(e) => {
                          const next = { ...parsed };
                          if (e.target.value === "") delete next[p.name];
                          else if (p.type === "string") next[p.name] = e.target.value;
                          else {
                            try {
                              next[p.name] = JSON.parse(e.target.value);
                            } catch {
                              next[p.name] = e.target.value;
                            }
                          }
                          setInput(JSON.stringify(next, null, 2));
                          setResult(null);
                        }}
                      />
                    </Field>
                  ))}
                </div>
              ) : (
                <Field label="输入 JSON">
                  <textarea
                    className={textAreaClass}
                    disabled={pending}
                    value={input}
                    onChange={(e) => {
                      setInput(e.target.value);
                      setResult(null);
                    }}
                  />
                </Field>
              )}
              {inputError && (
                <p role="alert" className="text-sm text-red-600">
                  {inputError}
                </p>
              )}
              {!["GET", "HEAD"].includes(selected.httpMethod) && (
                <p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
                  本次 {selected.httpMethod} 请求可能修改上游数据，请先确认右侧目标地址。请求只执行一次。
                </p>
              )}
              {confirmTarget && (
                <div className="grid gap-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
                  <p>配置已更新，请确认新的请求地址和账号后再执行。</p>
                  <Button
                    variant="secondary"
                    disabled={pending || !preview || !preview.verified}
                    onClick={() => setConfirmTarget(false)}
                  >
                    确认新目标
                  </Button>
                </div>
              )}
              <Button
                write
                disabled={confirmTarget || pending || !preview || !preview.verified || selected.status !== "active"}
                onClick={() => void execute()}
              >
                <CirclePlay className="size-4" />
                {pending ? "执行中…" : "运行测试"}
              </Button>
            </Card>
            <Card className="grid min-w-0 content-start gap-4 p-5">
              <h2 className="font-bold">请求预览</h2>
              {previewError && (
                <p role="alert" className="break-words text-sm text-red-600">
                  {previewError}
                </p>
              )}
              {preview ? (
                <>
                  <p className="break-all font-mono text-sm">
                    {preview.request.method} {preview.request.url}
                  </p>
                  <CopyButton value={preview.request.url} />
                  <p className="text-sm">
                    {preview.authentication.required
                      ? `认证：${preview.authentication.accountName} · ${preview.authentication.instanceName}（凭据隐藏）`
                      : "无需认证"}
                  </p>
                  <pre className="max-h-64 overflow-auto text-xs">
                    {JSON.stringify({ headers: preview.request.headers, body: preview.request.body }, null, 2)}
                  </pre>
                  <details>
                    <summary className="cursor-pointer text-sm">交付给业务调用方</summary>
                    <p className="my-2 text-sm">创建限定此 API 和账号的运行时令牌。</p>
                    <Link
                      className="text-blue-600"
                      to={`/access?action=${encodeURIComponent(selected.actionKey)}&connection=${encodeURIComponent(accountId)}`}
                    >
                      创建运行时令牌
                    </Link>
                    <pre className="mt-2 overflow-auto text-xs">{`POST /v1/actions/${selected.id}\n${JSON.stringify({ integrationId: targetId, connectionKey: accountId || undefined, input: parsed, expectedApiVersion: preview.apiVersion, expectedIntegrationVersion: preview.integrationVersion, expectedAccountRevision: preview.accountRevision || undefined }, null, 2)}`}</pre>
                  </details>
                </>
              ) : (
                <p className="text-sm text-[var(--muted-text)]">
                  选择执行目标并填写参数后显示实际请求，预览不会调用上游。
                </p>
              )}
              <h2 className="font-bold">执行结果</h2>
              {result ? (
                <>
                  {(result.response?.meta?.checks || result.checks) && (
                    <div className="grid gap-2 text-sm">
                      {Object.entries(result.response?.meta?.checks || result.checks).map(([key, value]) => (
                        <div key={key} className="flex justify-between gap-2">
                          <span>
                            {
                              (
                                {
                                  http: "HTTP 状态检查",
                                  json: "JSON 解析",
                                  business: "业务条件检查",
                                  structure: "响应结构检查",
                                } as Record<string, string>
                              )[key]
                            }
                          </span>
                          <Badge tone={value === "passed" ? "success" : value === "failed" ? "danger" : "neutral"}>
                            {value === "passed" ? "通过" : value === "failed" ? "失败" : "未配置或未执行"}
                          </Badge>
                        </div>
                      ))}
                    </div>
                  )}

                  <pre className="max-h-96 overflow-auto rounded-lg bg-slate-950 p-4 text-xs text-slate-100">
                    {JSON.stringify(result, null, 2)}
                  </pre>
                  {runId && <RequestResponseDetails runId={runId} />}
                  {runId && (
                    <Link className="text-sm text-blue-600" to={`/operations?run=${encodeURIComponent(runId)}`}>
                      查看运行详情
                    </Link>
                  )}
                </>
              ) : (
                <p className="text-sm text-[var(--muted-text)]">尚未执行</p>
              )}
            </Card>
          </div>
        )}
        {tab === "运行记录" && (
          <Card className="grid gap-3 p-5">
            {records.length ? (
              records.map((r) => (
                <Link
                  key={r.id}
                  className="flex flex-wrap justify-between gap-2 rounded-lg border p-3 text-sm"
                  to={`/operations?run=${encodeURIComponent(r.id)}`}
                >
                  <span>{r.startedAt}</span>
                  <Badge>{r.status}</Badge>
                </Link>
              ))
            ) : (
              <p className="text-sm text-[var(--muted-text)]">暂无运行记录</p>
            )}
          </Card>
        )}
      </div>
    );
  return (
    <div className="grid gap-5">
      <PageHeader
        title="API"
        description="按系统定义接口，在不同集成环境中复用。"
        actions={
          <Button write disabled={!demo.customSystems.length} onClick={() => setCreating(true)}>
            <Plus className="size-4" />
            添加 API
          </Button>
        }
      />
      <div className="grid gap-3 sm:grid-cols-[1fr_12rem_9rem]">
        <SearchBox value={query} onChange={setQuery} placeholder="搜索 API 名称、标识或路径" />
        <select aria-label="所属系统" className={fieldClass} value={filter} onChange={(e) => setFilter(e.target.value)}>
          <option value="">全部系统</option>
          {demo.customSystems.map((s) => (
            <option key={s.service} value={s.service}>
              {s.name}
            </option>
          ))}
        </select>
        <select aria-label="定义状态" className={fieldClass} value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="">全部状态</option>
          <option value="active">可调用</option>
          <option value="draft">草稿</option>
          <option value="disabled">停用</option>
        </select>
      </div>
      <PageControls
        page={page}
        onClear={() => {
          setFilter("");
          setStatus("");
          setQuery("");
        }}
      />
      <Card className="divide-y overflow-hidden">
        {page.items.map((row: ApiDefinition) => (
          <button
            key={row.id}
            className="flex w-full flex-wrap items-center gap-3 p-4 text-left hover:bg-[var(--muted)]"
            onClick={() => void open(row)}
          >
            <Badge>{row.httpMethod}</Badge>
            <span className="min-w-0 flex-1">
              <span className="block truncate font-semibold" title={row.name || row.actionKey}>
                {row.name || row.actionKey}
              </span>
              <code className="mt-1 block truncate text-xs text-[var(--muted-text)]">
                {row.actionKey} · {row.relativePath}
              </code>
            </span>
            <Badge>{demo.customSystems.find((s) => s.service === row.systemKey)?.name ?? row.systemKey}</Badge>
            <Badge>{row.status === "active" ? "可调用" : row.status === "draft" ? "草稿" : "停用"}</Badge>
          </button>
        ))}
      </Card>
    </div>
  );
}
