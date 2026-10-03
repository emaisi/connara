import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router";
import { api } from "../api";
import { useCanWrite } from "../permissions";
import { useResourcePages, PageControls } from "../pagination";
import { Button, Card, PageHeader } from "../ui";
import { WorkflowCanvas } from "./workflow-canvas";
import { useWorkflowSession } from "./workflow-editor";
import type { Definition, Layout } from "./workflow-model";
export function WorkflowHistory() {
  const { id } = useParams();
  const session = useWorkflowSession();
  const page = useResourcePages("/api/operations", { kind: "workflow", workflow: id ?? "" });
  return (
    <div className="grid gap-4">
      <PageHeader
        title="工作流运行历史"
        description="查看当次定义、执行状态和脱敏步骤摘要"
        actions={
          <Button variant="secondary" asChild>
            <Link to={`/workflows/${id}`}>返回配置</Link>
          </Button>
        }
      />
      {session?.definitionDirty && (
        <p role="status" className="text-sm">
          编辑会话有未保存修改，返回配置后继续编辑。
        </p>
      )}
      {page.items.map((run) => (
        <Card key={run.id} className="flex flex-wrap justify-between gap-3 p-4">
          <div>
            <p>
              {run.name} · 版本 {run.workflowVersion}
            </p>
            <p className="text-xs">
              {run.source} · {run.startedAt} · {run.status}
            </p>
            <code className="text-xs">{run.requestId}</code>
          </div>
          <Button variant="secondary" asChild>
            <Link to={`/workflows/${id}/runs/${run.id}`}>查看运行</Link>
          </Button>
        </Card>
      ))}
      <PageControls page={page} />
    </div>
  );
}
export function WorkflowRun() {
  const { runId, id } = useParams();
  const canRead = useCanWrite();
  const [view, setView] = useState<any>(null);
  const [operation, setOperation] = useState<any>(null);
  const [result, setResult] = useState<unknown>();
  const [selected, setSelected] = useState("@trigger");
  const [focusRequest, setFocusRequest] = useState<{ id: string; sequence: number }>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const delay = useRef(2000);
  const [refresh, setRefresh] = useState(0);
  const resultLoaded = useRef(false);
  useEffect(() => {
    let stopped = false;
    setLoading(true);
    setError("");
    setView(null);
    setOperation(null);
    setSelected("@trigger");
    setResult(undefined);
    resultLoaded.current = false;
    if (!canRead) {
      setError("您的角色可查看脱敏运行摘要，不能读取运行快照。");
      setLoading(false);
      return;
    }
    void api
      .workflowRunView(runId!)
      .then((value) => {
        if (!stopped) {
          setView(value);
          setLoading(false);
        }
      })
      .catch((error) => {
        if (!stopped) {
          setError(String(error));
          setLoading(false);
        }
      });
    return () => {
      stopped = true;
    };
  }, [runId, canRead]);
  useEffect(() => {
    if (!runId) return;
    let stopped = false;
    let timer: number | undefined;
    let terminal = false;
    let inFlight = false;
    const poll = async () => {
      if (inFlight || stopped || document.hidden) return;
      inFlight = true;
      try {
        const run = await api.operation(runId);
        if (stopped) return;
        setOperation(run);
        setError("");
        delay.current = 2000;
        terminal = run.status !== "queued" && run.status !== "running";
        if (run.status === "success" && canRead && !resultLoaded.current) {
          resultLoaded.current = true;
          try {
            const final = await api.workflowRunResult(runId);
            if (!stopped) setResult(final.output);
          } catch (error) {
            if (!stopped) setError(String(error));
          }
        }
      } catch (error) {
        if (!stopped) {
          setError(String(error));
          const status = (error as any)?.status;
          if (status === 403 || status === 404) terminal = true;
          else delay.current = Math.min(delay.current * 2, 10000);
        }
      }
      inFlight = false;
      if (!stopped && !terminal && !document.hidden) timer = window.setTimeout(() => void poll(), delay.current);
    };
    const visibility = () => {
      if (document.hidden) {
        window.clearTimeout(timer);
      } else if (!terminal) {
        window.clearTimeout(timer);
        void poll();
      }
    };
    void poll();
    document.addEventListener("visibilitychange", visibility);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", visibility);
    };
  }, [runId, canRead, refresh]);
  const statuses: Record<string, string> = {};
  const details: Record<string, any> = {};
  const events = [...new Map((operation?.events ?? []).map((event: any) => [event.sequence, event])).values()].sort(
    (a: any, b: any) => a.sequence - b.sequence,
  ) as any[];
  const finished = events.findLast((event) => event.attributes?.eventType === "workflow.run.finished")?.attributes;
  for (const event of events) {
    const data = event.attributes;
    if (!data?.stepId) continue;
    details[data.stepId] = { ...details[data.stepId], ...data };
    statuses[data.stepId] = data.status;
  }
  const terminal = operation && operation.status !== "running" && operation.status !== "queued";
  for (const step of view?.steps ?? []) {
    if (!statuses[step.id])
      statuses[step.id] = terminal
        ? finished?.stepStatuses
          ? finished.stepStatuses[step.id]
            ? `${finished.stepStatuses[step.id]}（步骤追踪不完整）`
            : "未执行"
          : "追踪信息不完整"
        : "等待";
    else if (terminal && statuses[step.id] === "running") statuses[step.id] = "追踪信息不完整";
  }
  statuses["@trigger"] = operation?.status === "queued" ? "排队中" : operation ? "已准备" : "等待";
  statuses["@result"] =
    operation?.status === "success"
      ? "success"
      : events.findLast((event) => event.attributes?.eventType === "workflow.run.finished")?.attributes?.phase ===
          "output"
        ? "failed"
        : terminal
          ? "未执行或追踪信息不完整"
          : "等待";
  const graph: Definition = {
    schemaVersion: view?.schemaVersion,
    steps: (view?.steps ?? []).map((step: any) => ({
      ...step,
      assign: Array.isArray(step.assignPreview) ? step.assignPreview : undefined,
      operations: step.type === "transform" ? (step.operations ?? []).map((op: string) => ({ op })) : undefined,
    })),
    output: {},
  };
  const layout = view?.editorLayout?.positions ? (view.editorLayout as Layout) : undefined;
  const detail = details[selected];
  return (
    <div className="grid min-w-0 gap-4">
      <PageHeader
        title={view?.name ?? "工作流运行"}
        description={`当次版本 ${view?.workflowVersion ?? "未记录"} · ${operation?.status ?? "正在加载"}`}
        actions={
          <div className="flex gap-2">
            <Button
              variant="secondary"
              onClick={() => {
                resultLoaded.current = false;
                setRefresh((value) => value + 1);
              }}
            >
              刷新记录
            </Button>
            <Button variant="secondary" asChild>
              <Link to={id ? `/workflows/${id}` : "/workflows"}>返回配置</Link>
            </Button>
          </div>
        }
      />
      {loading && <p role="status">正在加载运行快照…</p>}
      {error && <p role="alert">{error}</p>}
      {(view?.hasWarnings || operation?.output?.hasWarnings) && <p role="status">完成，部分步骤失败。</p>}
      {view && (
        <>
          <div className="flex flex-wrap gap-2 text-sm">
            <Button
              variant="secondary"
              onClick={() => {
                const running = Object.keys(statuses).find((key) => statuses[key] === "running");
                if (running) {
                  setSelected(running);
                  setFocusRequest({ id: running, sequence: Date.now() });
                }
              }}
            >
              定位执行中步骤
            </Button>
            <select
              aria-label="运行步骤"
              className="rounded border border-[var(--border)] p-2"
              value={selected}
              onChange={(e) => {
                setSelected(e.target.value);
                setFocusRequest({ id: e.target.value, sequence: Date.now() });
              }}
            >
              {["@trigger", ...graph.steps.map((step) => step.id), "@result"].map((id) => (
                <option key={id}>{id}</option>
              ))}
            </select>
          </div>
          <Card className="h-[500px] min-w-0 overflow-hidden">
            <WorkflowCanvas
              graph={graph}
              layout={layout}
              selected={selected}
              onSelect={setSelected}
              statuses={statuses}
              focusRequest={focusRequest}
              readOnly
            />
          </Card>
          <Card className="grid gap-3 p-4">
            <p className="font-semibold">
              {selected} · {statuses[selected]}
            </p>
            <p className="text-xs">
              {detail?.durationMs !== undefined ? `耗时 ${detail.durationMs} ms` : "耗时未记录"}
            </p>
            <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs">
              {JSON.stringify(
                selected === "@trigger"
                  ? {
                      trigger: view.triggerPreview,
                      runMetadata: view.runMetadata ?? "未记录",
                      initialVariables: view.initialVariablesPreview,
                    }
                  : selected === "@result"
                    ? { status: operation?.status, error: operation?.errorMessage, output: result }
                    : (detail ?? { message: "历史记录未保存步骤摘要" }),
                null,
                2,
              )}
            </pre>
          </Card>
        </>
      )}
      {result !== undefined && (
        <Card className="p-4">
          <p className="font-semibold">最终结果</p>
          <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs">
            {JSON.stringify(result, null, 2)}
          </pre>
        </Card>
      )}
    </div>
  );
}
