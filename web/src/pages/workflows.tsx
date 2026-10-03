import { MutationForm } from "../ui";
import { Link } from "react-router";
import { ArrowDown, ArrowUp, CirclePlay, Plus, RefreshCw, Trash2, Workflow as WorkflowIcon } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";

import { statusLabel, useDemo } from "../demo";
import { Badge, Button, Card, EmptyState, Field, Modal, PageHeader, fieldClass, textAreaClass } from "../ui";
import { api } from "../api";

import { MiniStat, scheduleLabel, FeatureCard, Th, Td } from "./core-shared";

interface WorkflowStepForm {
  id: string;
  title: string;
  actionId: string;
  integrationId: string;
  connectionId: string;
  inputText: string;
  dependsOn: string[];
  runIfEnabled: boolean;
  runIfPath: string;
  runIfOp: string;
  runIfValue: string;
  runIfGroup: string;
  onError: string;
}

interface WorkflowRow {
  id: string;
  workflowKey: string;
  name: string;
  description: string;
  status: string;
  graph: { steps?: Array<{ id: string; title?: string; action: string }>; output?: Record<string, unknown> };
  scheduleType: string;
  cronExpression?: string;
  scheduleTimezone: string;
  input: Record<string, unknown>;
  version: number;
  updatedAt?: string;
}

interface ActionOption {
  id: string;
  actionKey: string;
  systemKey: string;
  status: string;
  executable: boolean;
}

const emptyStep = (index: number): WorkflowStepForm => ({
  id: `step${index + 1}`,
  title: "",
  actionId: "",
  integrationId: "",
  connectionId: "",
  inputText: "{}",
  dependsOn: [],
  runIfEnabled: false,
  runIfPath: "",
  runIfOp: "exists",
  runIfValue: "",
  runIfGroup: "",
  onError: "fail",
});

function parseObjectText(text: string, fallback: string): { value: Record<string, unknown>; error?: string } {
  const trimmed = text.trim();
  if (!trimmed) return { value: JSON.parse(fallback) };
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return { value: {}, error: "必须是有效的 JSON" };
  }
  if (Array.isArray(parsed) || typeof parsed !== "object" || parsed === null) {
    return { value: {}, error: "必须是 JSON 对象" };
  }
  return { value: parsed as Record<string, unknown> };
}

export function WorkflowsPage() {
  const demo = useDemo();
  const [rows, setRows] = useState<WorkflowRow[]>([]);
  const [availableActions, setAvailableActions] = useState<ActionOption[]>([]);
  const [open, setOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingVersion, setEditingVersion] = useState(0);
  const [error, setError] = useState("");
  // Definition form
  const [workflowKey, setWorkflowKey] = useState("order-flow");
  const [name, setName] = useState("订单组合流程");
  const [description, setDescription] = useState("");
  const [scheduleType, setScheduleType] = useState("manual");
  const [interval, setIntervalValue] = useState("每 30 分钟");
  const [cronExpression, setCronExpression] = useState("*/30 * * * *");
  const [timezone, setTimezone] = useState("UTC");
  const [defaultInput, setDefaultInput] = useState("{}");
  const [outputText, setOutputText] = useState("{}");
  const [steps, setSteps] = useState<WorkflowStepForm[]>([emptyStep(0)]);
  const [sampleContext, setSampleContext] = useState('{"trigger":{},"status":{}}');
  const [samplePreview, setSamplePreview] = useState("");
  // Run modal
  const [runTarget, setRunTarget] = useState<WorkflowRow | null>(null);
  const [runInput, setRunInput] = useState("{}");
  const [runOperationId, setRunOperationId] = useState("");
  const [runStatus, setRunStatus] = useState("");
  const [runError, setRunError] = useState("");
  const [runOutput, setRunOutput] = useState<unknown>(null);

  const readyIntegrations = demo.integrations.filter((item) => item.status === "ready");

  function load() {
    return Promise.all([api.workflows(), api.actions()]).then(([workflowRows, actionRows]) => {
      setAvailableActions(actionRows.filter((item: ActionOption) => item.status === "active" && item.executable));
      setRows(workflowRows as WorkflowRow[]);
    });
  }
  useEffect(() => {
    if (!demo.authenticated) return;
    void load().catch((loadError) => demo.notify(loadError));
  }, [demo.authenticated, demo.operations.length]);

  useEffect(() => {
    if (!runOperationId || !runTarget) return;
    if (runStatus && runStatus !== "queued" && runStatus !== "running") return;
    let stopped = false;
    let timer: number;
    async function poll() {
      let finished = false;
      try {
        const operation = await api.operation(runOperationId);
        if (stopped) return;
        setRunStatus(operation.status);
        if (operation.errorMessage) setRunError(`${operation.errorCode ?? ""} ${operation.errorMessage}`);
        finished = operation.status !== "queued" && operation.status !== "running";
        if (operation.status === "success") {
          const result = await api.workflowRunResult(runOperationId);
          if (!stopped) setRunOutput(result.output);
        }
      } catch (pollError) {
        if (!stopped) setRunError(String(pollError instanceof Error ? pollError.message : pollError));
      }
      if (!stopped && !finished) timer = window.setTimeout(() => void poll(), 2000);
    }
    timer = window.setTimeout(() => void poll(), 2000);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, [runOperationId, runTarget]);

  function editWorkflow(row?: WorkflowRow) {
    setError("");
    setSamplePreview("");
    setSampleContext(JSON.stringify({ trigger: row?.input ?? {}, status: {} }, null, 2));
    setEditingId(row?.id ?? null);
    setEditingVersion(row?.version ?? 0);
    setWorkflowKey(row?.workflowKey ?? "order-flow");
    setName(row?.name ?? "订单组合流程");
    setDescription(row?.description ?? "");
    setScheduleType(row?.scheduleType ?? "manual");
    setIntervalValue(row?.scheduleType === "interval" && row.cronExpression ? row.cronExpression : "每 30 分钟");
    setCronExpression(row?.scheduleType === "cron" && row.cronExpression ? row.cronExpression : "*/30 * * * *");
    setTimezone(row?.scheduleTimezone ?? "UTC");
    setDefaultInput(JSON.stringify(row?.input ?? {}, null, 2));
    setOutputText(JSON.stringify(row?.graph?.output ?? {}, null, 2));
    const graphSteps = row?.graph?.steps ?? [];
    setSteps(
      graphSteps.length
        ? graphSteps.map((step) => ({
            id: step.id,
            title: step.title ?? "",
            actionId: "",
            integrationId: "",
            connectionId: "",
            inputText: JSON.stringify({}, null, 2),
            dependsOn: [],
            runIfEnabled: false,
            runIfPath: "",
            runIfOp: "exists",
            runIfValue: "",
            runIfGroup: "",
            onError: "fail",
          }))
        : [emptyStep(0)],
    );
    // Preserve raw graph fields the compact editor does not model yet.
    if (row?.graph?.steps?.length) {
      setSteps((current) =>
        current.map((step, index) => {
          const source = (row.graph!.steps ?? [])[index] as Record<string, unknown>;
          return {
            ...step,
            actionId:
              availableActions.find((action) => action.actionKey === source.action || action.id === source.action)
                ?.id ?? "",
            integrationId: String(source.integrationId ?? ""),
            connectionId: String(source.connectionKey ?? ""),
            inputText: JSON.stringify(source.input ?? {}, null, 2),
            dependsOn: Array.isArray(source.dependsOn) ? (source.dependsOn as string[]) : [],
            runIfEnabled: Boolean(source.runIf),
            runIfPath: source.runIf ? String((source.runIf as Record<string, unknown>).path ?? "") : "",
            runIfOp: source.runIf ? String((source.runIf as Record<string, unknown>).op ?? "exists") : "exists",
            runIfValue: source.runIf ? JSON.stringify((source.runIf as Record<string, unknown>).value ?? "") : '""',
            runIfGroup:
              source.runIf && ("all" in (source.runIf as object) || "any" in (source.runIf as object))
                ? JSON.stringify(source.runIf, null, 2)
                : "",
            onError: String(source.onError ?? "fail"),
          };
        }),
      );
    }
    setOpen(true);
  }

  function updateStep(index: number, patch: Partial<WorkflowStepForm>) {
    setSteps((current) => current.map((step, i) => (i === index ? { ...step, ...patch } : step)));
  }

  function moveStep(index: number, delta: number) {
    setSteps((current) => {
      const target = index + delta;
      if (target < 0 || target >= current.length) return current;
      const next = [...current];
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
  }

  function saveWorkflow(event: FormEvent) {
    event.preventDefault();
    setError("");
    const key = workflowKey.trim();
    if (!key || !name.trim()) {
      setError("请填写工作流标识和名称");
      return;
    }
    const defaultInputValue = parseObjectText(defaultInput, "{}");
    if (defaultInputValue.error) {
      setError(`默认触发输入${defaultInputValue.error}`);
      return;
    }
    const outputValue = parseObjectText(outputText, "{}");
    if (outputValue.error) {
      setError(`输出映射${outputValue.error}`);
      return;
    }
    if (Object.keys(outputValue.value).length === 0) {
      setError("部署前必须配置输出映射；草稿可先保存");
    }
    const graphSteps: Array<Record<string, unknown>> = [];
    for (const [index, step] of steps.entries()) {
      const inputValue = parseObjectText(step.inputText, "{}");
      if (inputValue.error) {
        setError(`步骤 ${step.id} 的输入模板${inputValue.error}`);
        return;
      }
      const action = availableActions.find((item) => item.id === step.actionId);
      const integration = readyIntegrations.find((item) => item.id === step.integrationId);
      const connection = demo.connections.find(
        (item) => item.id === step.connectionId && item.integration === integration?.name && item.status === "active",
      );
      if (!step.id.trim() || !action || !integration || !connection) {
        setError(`步骤 ${index + 1} 需要别名、API 操作、集成和连接的显式绑定`);
        return;
      }
      const entry: Record<string, unknown> = {
        id: step.id.trim(),
        title: step.title.trim(),
        action: action.actionKey,
        integrationId: integration.id,
        connectionKey: connection.id,
        input: inputValue.value,
        dependsOn: step.dependsOn,
        onError: step.onError,
      };
      if (step.runIfEnabled) {
        let runIfValue: unknown = step.runIfValue.trim();
        try {
          runIfValue = JSON.parse(step.runIfValue);
        } catch {
          /* keep the raw string */
        }
        if (step.runIfGroup.trim()) {
          const group = parseObjectText(step.runIfGroup, "{}");
          if (group.error || !("all" in group.value || "any" in group.value)) {
            setError(`步骤 ${step.id} 的组合条件必须是含 all 或 any 的 JSON 对象`);
            return;
          }
          entry.runIf = group.value;
        } else {
          entry.runIf = { path: step.runIfPath.trim(), op: step.runIfOp, value: runIfValue };
        }
      }
      graphSteps.push(entry);
    }
    const schedule =
      scheduleType === "interval"
        ? { scheduleType, cronExpression: interval, scheduleTimezone: timezone }
        : scheduleType === "cron"
          ? { scheduleType, cronExpression: cronExpression.trim(), scheduleTimezone: timezone }
          : { scheduleType, cronExpression: "", scheduleTimezone: timezone };
    return api
      .saveWorkflow(
        {
          workflowKey: key,
          name: name.trim(),
          description: description.trim(),
          graph: { steps: graphSteps, output: outputValue.value },
          ...schedule,
          retryPolicy: { maxAttempts: 1 },
          input: defaultInputValue.value,
        },
        editingId ?? "",
        editingVersion,
      )
      .then(() => {
        setOpen(false);
        return Promise.all([load(), demo.reload()]);
      })
      .then(() => demo.notify(`工作流 ${key} 已保存；部署前需重新校验绑定`))
      .catch((saveError) => setError(String(saveError instanceof Error ? saveError.message : saveError)));
  }

  function startRun(row: WorkflowRow) {
    setRunTarget(row);
    setRunInput(JSON.stringify(row.input ?? {}, null, 2));
    setRunOperationId("");
    setRunStatus("");
    setRunError("");
    setRunOutput(null);
  }

  function submitRun() {
    if (!runTarget) return;
    const inputValue = parseObjectText(runInput, "{}");
    if (inputValue.error) {
      setRunError(`触发输入${inputValue.error}`);
      return;
    }
    setRunError("");
    setRunStatus("queued");
    void api
      .runWorkflow(runTarget.id, inputValue.value)
      .then((operation) => {
        setRunOperationId(operation.id);
        demo.reload().catch(() => undefined);
      })
      .catch((runRequestError) => {
        setRunStatus("failed");
        setRunError(String(runRequestError instanceof Error ? runRequestError.message : runRequestError));
      });
  }

  const runStatusLabel: Record<string, string> = {
    queued: "排队中",
    running: "执行中",
    success: "已成功",
    failed: "已失败",
    unknown: "结果未知",
  };

  return (
    <div className="grid gap-6">
      <PageHeader
        title="工作流"
        description="把多个 API 操作编排成步骤链或 DAG：模板传值、条件跳过、失败容忍与输出映射。"
        actions={
          <Button write onClick={() => editWorkflow()} disabled={!readyIntegrations.length}>
            <Plus className="size-4" />
            新建工作流
          </Button>
        }
      />
      {!rows.length && (
        <EmptyState
          title="尚无工作流"
          description="先创建集成与连接，再编排多步骤工作流；已接受的运行使用固定定义执行。"
          action={
            readyIntegrations.length ? (
              <Button write onClick={() => editWorkflow()}>
                新建工作流
              </Button>
            ) : (
              <Button asChild variant="secondary">
                <Link to="/integrations?new=1">创建集成</Link>
              </Button>
            )
          }
        />
      )}
      <div className="grid gap-3 md:hidden">
        {rows.map((row) => (
          <Card className="p-4" key={row.id}>
            <div className="flex items-start justify-between gap-3">
              <div>
                <code className="font-semibold">{row.workflowKey}</code>
                <p className="mt-1 text-xs text-[var(--muted-text)]">{row.name}</p>
              </div>
              <Badge tone={row.status === "deployed" ? "success" : row.status === "disabled" ? "danger" : "warning"}>
                {statusLabel(row.status)}
              </Badge>
            </div>
            <div className="mt-4 grid grid-cols-2 gap-3">
              <MiniStat label="调度" value={scheduleLabel(row)} />
              <MiniStat label="步骤数" value={String(row.graph?.steps?.length ?? 0)} />
            </div>
            <div className="mt-4 flex flex-wrap gap-2">
              <Button variant="secondary" onClick={() => editWorkflow(row)}>
                编辑
              </Button>
              {row.status === "deployed" ? (
                <>
                  <Button
                    write
                    variant="secondary"
                    onClick={() =>
                      void api
                        .pauseWorkflow(row.id)
                        .then(load)
                        .catch((pauseError) => demo.notify(pauseError))
                    }
                  >
                    暂停
                  </Button>
                  <Button write onClick={() => startRun(row)}>
                    <CirclePlay className="size-4" />
                    立即运行
                  </Button>
                </>
              ) : (
                <Button
                  write
                  onClick={() =>
                    void api
                      .deployWorkflow(row.id)
                      .then(() => Promise.all([load(), demo.reload()]))
                      .then(() => demo.notify(`工作流 ${row.workflowKey} 已部署`))
                      .catch((deployError) => demo.notify(deployError))
                  }
                >
                  部署
                </Button>
              )}
              <Button variant="ghost" asChild>
                <Link to={`/operations?workflow=${row.id}`}>运行历史</Link>
              </Button>
              <Button
                write
                variant="ghost"
                onClick={() => {
                  if (!window.confirm(`确认删除工作流“${row.workflowKey}”吗？已接受的运行仍会完成。`)) return;
                  void api
                    .deleteWorkflow(row.id)
                    .then(load)
                    .catch((deleteError) => demo.notify(deleteError));
                }}
              >
                <Trash2 className="size-4" />
              </Button>
            </div>
          </Card>
        ))}
      </div>
      <Card className="hidden overflow-hidden md:block">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-left text-sm">
            <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
              <tr>
                <Th>工作流</Th>
                <Th>调度</Th>
                <Th>步骤数</Th>
                <Th>状态</Th>
                <Th>更新时间</Th>
                <Th />
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--border)]">
              {rows.map((row) => (
                <tr key={row.id}>
                  <Td>
                    <div className="flex items-center gap-3">
                      <span className="grid size-9 place-items-center rounded-lg bg-[var(--muted)] text-blue-600">
                        <WorkflowIcon className="size-4" />
                      </span>
                      <div>
                        <code className="font-semibold">{row.workflowKey}</code>
                        <p className="text-xs text-[var(--muted-text)]">{row.name}</p>
                      </div>
                    </div>
                  </Td>
                  <Td>{scheduleLabel(row)}</Td>
                  <Td>{row.graph?.steps?.length ?? 0}</Td>
                  <Td>
                    <Badge
                      tone={row.status === "deployed" ? "success" : row.status === "disabled" ? "danger" : "warning"}
                    >
                      {statusLabel(row.status)}
                    </Badge>
                  </Td>
                  <Td>
                    <span className="text-xs text-[var(--muted-text)]">
                      {row.updatedAt ? new Date(row.updatedAt).toLocaleString("zh-CN") : "—"}
                    </span>
                  </Td>
                  <Td>
                    <div className="flex justify-end gap-1">
                      <Button variant="secondary" onClick={() => editWorkflow(row)}>
                        编辑
                      </Button>
                      {row.status === "deployed" ? (
                        <>
                          <Button
                            write
                            variant="secondary"
                            onClick={() =>
                              void api
                                .pauseWorkflow(row.id)
                                .then(load)
                                .catch((pauseError) => demo.notify(pauseError))
                            }
                          >
                            暂停
                          </Button>
                          <Button write onClick={() => startRun(row)}>
                            <CirclePlay className="size-4" />
                            立即运行
                          </Button>
                        </>
                      ) : (
                        <Button
                          write
                          onClick={() =>
                            void api
                              .deployWorkflow(row.id)
                              .then(() => Promise.all([load(), demo.reload()]))
                              .then(() => demo.notify(`工作流 ${row.workflowKey} 已部署`))
                              .catch((deployError) => demo.notify(deployError))
                          }
                        >
                          部署
                        </Button>
                      )}
                      <Button variant="ghost" asChild>
                        <Link to={`/operations?workflow=${row.id}`}>运行历史</Link>
                      </Button>
                      <Button
                        write
                        variant="ghost"
                        aria-label={`删除 ${row.workflowKey}`}
                        onClick={() => {
                          if (!window.confirm(`确认删除工作流“${row.workflowKey}”吗？已接受的运行仍会完成。`)) return;
                          void api
                            .deleteWorkflow(row.id)
                            .then(() => Promise.all([load(), demo.reload()]))
                            .catch((deleteError) => demo.notify(deleteError));
                        }}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      <section className="grid gap-4 md:grid-cols-3">
        <FeatureCard
          icon={WorkflowIcon}
          title="步骤编排"
          text="用 dependsOn 组合任意 DAG；引用上游响应字段时保持 JSON 类型。"
        />
        <FeatureCard
          icon={RefreshCw}
          title="条件与容错"
          text="runIf 受限断言决定跳过；onError=continue 容忍失败并标记警告。"
        />
        <FeatureCard
          icon={CirclePlay}
          title="固定定义运行"
          text="触发即固定图、输入与版本；编辑不影响已排队或运行中的执行。"
        />
      </section>

      <Modal
        open={open}
        onOpenChange={setOpen}
        title={editingId ? "编辑工作流" : "新建工作流"}
        description="步骤可引用 trigger 与依赖闭包内的上游别名；编辑已部署工作流会回到草稿。"
      >
        <MutationForm className="grid gap-4" onSubmit={saveWorkflow}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="工作流标识">
              <input
                className={fieldClass}
                value={workflowKey}
                onChange={(event) => setWorkflowKey(event.target.value)}
                placeholder="order-flow"
                required
              />
            </Field>
            <Field label="名称">
              <input className={fieldClass} value={name} onChange={(event) => setName(event.target.value)} required />
            </Field>
          </div>
          <Field label="说明">
            <input
              className={fieldClass}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="组合下单流程"
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="调度方式">
              <select
                className={fieldClass}
                value={scheduleType}
                onChange={(event) => setScheduleType(event.target.value)}
              >
                <option value="manual">仅手动 / API 触发</option>
                <option value="interval">固定间隔</option>
                <option value="cron">Cron 表达式</option>
              </select>
            </Field>
            {scheduleType === "interval" && (
              <Field label="间隔">
                <select
                  className={fieldClass}
                  value={interval}
                  onChange={(event) => setIntervalValue(event.target.value)}
                >
                  <option>每 15 分钟</option>
                  <option>每 30 分钟</option>
                  <option>每小时</option>
                  <option>每天</option>
                </select>
              </Field>
            )}
            {scheduleType === "cron" && (
              <Field label="Cron 表达式（五字段）" hint="分钟 小时 日 月 周，IANA 时区生效。">
                <input
                  className={fieldClass}
                  value={cronExpression}
                  onChange={(event) => setCronExpression(event.target.value)}
                  placeholder="*/30 * * * *"
                />
              </Field>
            )}
            {scheduleType !== "manual" && (
              <Field label="时区">
                <input
                  className={fieldClass}
                  value={timezone}
                  onChange={(event) => setTimezone(event.target.value)}
                  placeholder="Asia/Shanghai"
                />
              </Field>
            )}
          </div>
          <Field label="默认触发输入 JSON 对象" hint="省略 body 的运行时触发与定时运行使用该输入；不要写入凭据。">
            <textarea
              className={`${textAreaClass} min-h-24 font-mono text-xs`}
              value={defaultInput}
              onChange={(event) => setDefaultInput(event.target.value)}
            />
          </Field>
          <Field
            label="输出映射 JSON 对象"
            hint='例如 {"orderId": "{{create_order.data.orderId}}"}；条件字段可用 {{?step.path}}。'
          >
            <textarea
              className={`${textAreaClass} min-h-24 font-mono text-xs`}
              value={outputText}
              onChange={(event) => setOutputText(event.target.value)}
            />
          </Field>
          <div className="grid gap-3">
            <div className="flex items-center justify-between">
              <p className="text-sm font-bold">步骤（同层按此顺序执行）</p>
              <Button
                type="button"
                write
                variant="secondary"
                onClick={() => setSteps((current) => [...current, emptyStep(current.length)])}
              >
                <Plus className="size-4" />
                添加步骤
              </Button>
            </div>
            {steps.map((step, index) => {
              const closure = steps
                .filter((other, otherIndex) => otherIndex !== index)
                .flatMap((other) => [other.id, ...other.dependsOn]);
              const selectedAction = availableActions.find((item) => item.id === step.actionId);
              const actionIntegrations = readyIntegrations.filter(
                (integration) => !selectedAction || integration.provider === selectedAction.systemKey,
              );
              return (
                <Card className="grid gap-3 p-4" key={index}>
                  <div className="flex items-center justify-between gap-2">
                    <p className="text-sm font-bold">步骤 {index + 1}</p>
                    <div className="flex gap-1">
                      <Button
                        type="button"
                        variant="ghost"
                        aria-label="上移"
                        onClick={() => moveStep(index, -1)}
                        disabled={index === 0}
                      >
                        <ArrowUp className="size-4" />
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        aria-label="下移"
                        onClick={() => moveStep(index, 1)}
                        disabled={index === steps.length - 1}
                      >
                        <ArrowDown className="size-4" />
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        aria-label="删除步骤"
                        onClick={() => setSteps((current) => current.filter((_, i) => i !== index))}
                        disabled={steps.length <= 1}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </div>
                  </div>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Field label="别名">
                      <input
                        className={fieldClass}
                        value={step.id}
                        onChange={(event) => updateStep(index, { id: event.target.value })}
                        placeholder="get_customer"
                        required
                      />
                    </Field>
                    <Field label="展示说明">
                      <input
                        className={fieldClass}
                        value={step.title}
                        onChange={(event) => updateStep(index, { title: event.target.value })}
                        placeholder="查询客户"
                      />
                    </Field>
                  </div>
                  <div className="grid gap-3 sm:grid-cols-3">
                    <Field label="API 操作">
                      <select
                        className={fieldClass}
                        value={step.actionId}
                        onChange={(event) =>
                          updateStep(index, { actionId: event.target.value, integrationId: "", connectionId: "" })
                        }
                        required
                      >
                        <option value="">请选择操作</option>
                        {availableActions.map((item) => (
                          <option key={item.id} value={item.id}>
                            {item.actionKey}
                          </option>
                        ))}
                      </select>
                    </Field>
                    <Field label="集成（部署时固定）">
                      <select
                        className={fieldClass}
                        value={step.integrationId}
                        onChange={(event) => updateStep(index, { integrationId: event.target.value, connectionId: "" })}
                        required
                      >
                        <option value="">请选择集成</option>
                        {actionIntegrations.map((item) => (
                          <option key={item.id} value={item.id}>
                            {item.name}
                          </option>
                        ))}
                      </select>
                    </Field>
                    <Field label="执行账号（部署时固定）">
                      <select
                        className={fieldClass}
                        value={step.connectionId}
                        onChange={(event) => updateStep(index, { connectionId: event.target.value })}
                        required
                      >
                        <option value="">请选择连接</option>
                        {demo.connections
                          .filter(
                            (item) =>
                              item.integration === readyIntegrations.find((i) => i.id === step.integrationId)?.name &&
                              item.status === "active",
                          )
                          .map((item) => (
                            <option key={item.id} value={item.id}>
                              {item.name} · {item.endUser}
                            </option>
                          ))}
                      </select>
                    </Field>
                  </div>
                  <Field
                    label="输入模板 JSON 对象"
                    hint={`可引用 trigger.* 与依赖闭包内别名：${closure.length ? [...new Set(closure)].join("、") : "（先为其他步骤声明依赖）"}`}
                  >
                    <textarea
                      className={`${textAreaClass} min-h-24 font-mono text-xs`}
                      value={step.inputText}
                      onChange={(event) => updateStep(index, { inputText: event.target.value })}
                    />
                  </Field>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Field label="依赖步骤（dependsOn）">
                      <div className="flex flex-wrap gap-2">
                        {steps
                          .filter((other) => other.id !== step.id)
                          .map((other) => (
                            <label key={other.id} className="flex items-center gap-1 text-xs">
                              <input
                                type="checkbox"
                                checked={step.dependsOn.includes(other.id)}
                                onChange={(event) =>
                                  updateStep(index, {
                                    dependsOn: event.target.checked
                                      ? [...step.dependsOn, other.id]
                                      : step.dependsOn.filter((id) => id !== other.id),
                                  })
                                }
                              />
                              {other.id}
                            </label>
                          ))}
                        {steps.length === 1 && (
                          <span className="text-xs text-[var(--muted-text)]">仅一个步骤时无需依赖</span>
                        )}
                      </div>
                    </Field>
                    <Field label="失败处理">
                      <select
                        className={fieldClass}
                        value={step.onError}
                        onChange={(event) => updateStep(index, { onError: event.target.value })}
                      >
                        <option value="fail">失败即终止整图</option>
                        <option value="continue">记录警告并继续</option>
                      </select>
                    </Field>
                  </div>
                  <label className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={step.runIfEnabled}
                      onChange={(event) => updateStep(index, { runIfEnabled: event.target.checked })}
                    />
                    条件跳过（runIf）
                  </label>
                  {step.runIfEnabled && (
                    <Field
                      label="组合条件 JSON（可选）"
                      hint="填写 all/any 对象后使用组合条件；留空使用下面的单断言。最多 8 个条件、2 层嵌套，由服务端校验。"
                    >
                      <textarea
                        className={`${textAreaClass} font-mono text-xs`}
                        value={step.runIfGroup}
                        onChange={(event) => updateStep(index, { runIfGroup: event.target.value })}
                        placeholder='{"all":[{"path":"trigger.vip","op":"eq","value":true}]}'
                      />
                    </Field>
                  )}
                  {step.runIfEnabled && !step.runIfGroup.trim() && (
                    <div className="grid gap-3 sm:grid-cols-3">
                      <Field label="路径">
                        <input
                          className={fieldClass}
                          value={step.runIfPath}
                          onChange={(event) => updateStep(index, { runIfPath: event.target.value })}
                          placeholder="status.get_customer 或 trigger.vip"
                        />
                      </Field>
                      <Field label="操作符">
                        <select
                          className={fieldClass}
                          value={step.runIfOp}
                          onChange={(event) => updateStep(index, { runIfOp: event.target.value })}
                        >
                          {["eq", "ne", "gt", "gte", "lt", "lte", "exists", "not_exists"].map((op) => (
                            <option key={op} value={op}>
                              {op}
                            </option>
                          ))}
                        </select>
                      </Field>
                      <Field label="比较值（JSON）">
                        <input
                          className={fieldClass}
                          value={step.runIfValue}
                          onChange={(event) => updateStep(index, { runIfValue: event.target.value })}
                          placeholder='"success"'
                        />
                      </Field>
                    </div>
                  )}
                </Card>
              );
            })}
          </div>
          <Field
            label="脱敏样本上下文"
            hint='粘贴 {"trigger":{...},"status":{"step1":"success"},"step1":{...}}；仅在浏览器解析模板，请先移除样本中的敏感信息。'
          >
            <textarea
              className={`${textAreaClass} font-mono text-xs`}
              value={sampleContext}
              onChange={(event) => setSampleContext(event.target.value)}
            />
          </Field>
          <Button
            type="button"
            variant="secondary"
            onClick={() => {
              try {
                const context = JSON.parse(sampleContext);
                const preview = {
                  steps: steps.map((step) => ({
                    id: step.id,
                    input: previewTemplate(JSON.parse(step.inputText), context, false),
                  })),
                  output: previewTemplate(JSON.parse(outputText), context, true),
                };
                setSamplePreview(JSON.stringify(preview, null, 2));
              } catch (previewError) {
                setSamplePreview(String(previewError));
              }
            }}
          >
            预览输入和输出映射
          </Button>
          {samplePreview && (
            <pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded-lg bg-[var(--muted)] p-3 text-xs">
              {samplePreview}
            </pre>
          )}
          {error && <p className="text-sm text-red-600">{error}</p>}
          <div className="flex justify-end">
            <Button write type="submit">
              保存草稿
            </Button>
          </div>
        </MutationForm>
      </Modal>

      <Modal
        open={Boolean(runTarget)}
        onOpenChange={(next) => !next && setRunTarget(null)}
        title={`运行 ${runTarget?.workflowKey ?? ""}`}
        description="管理端触发进入队列并返回 202；此处轮询运行状态，完成后展示最终输出。"
      >
        <Field label="触发输入 JSON 对象">
          <textarea
            className={`${textAreaClass} min-h-24 font-mono text-xs`}
            value={runInput}
            onChange={(event) => setRunInput(event.target.value)}
          />
        </Field>
        <div className="mt-3 flex items-center gap-2">
          <Button write onClick={submitRun} disabled={!runTarget || runStatus === "queued" || runStatus === "running"}>
            <CirclePlay className="size-4" />
            提交运行
          </Button>
          {runStatus && (
            <Badge
              tone={
                runStatus === "success"
                  ? "success"
                  : runStatus === "failed" || runStatus === "unknown"
                    ? "danger"
                    : "warning"
              }
            >
              {runStatusLabel[runStatus] ?? runStatus}
            </Badge>
          )}
          {runOperationId && <code className="text-xs text-[var(--muted-text)]">{runOperationId}</code>}
        </div>
        {runError && <p className="mt-3 text-sm text-red-600">{runError}</p>}
        {runOutput !== null && (
          <div className="mt-3">
            <p className="text-sm font-bold">最终输出</p>
            <pre className="mt-1 max-h-64 overflow-auto rounded-lg bg-[var(--muted)] p-3 text-xs">
              {JSON.stringify(runOutput, null, 2)}
            </pre>
          </div>
        )}
      </Modal>
    </div>
  );
}

// Preview uses pasted samples only; the server remains the source of validation.
function previewTemplate(value: unknown, context: Record<string, unknown>, output: boolean): unknown {
  if (Array.isArray(value)) return value.map((item) => previewTemplate(item, context, output));
  if (value && typeof value === "object")
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, previewTemplate(item, context, output)]),
    );
  if (typeof value !== "string") return value;
  const lookup = (path: string): unknown => {
    const optional = path.startsWith("?");
    if (optional && !output) throw new Error("可选引用仅允许用于输出映射");
    const source = optional ? path.slice(1) : path;
    const root = /^[A-Za-z0-9_-]+/.exec(source);
    if (!root) throw new Error(`无效路径：${source}`);
    const tokens = [root[0]];
    let remaining = source.slice(root[0].length);
    while (remaining) {
      const next = /^(?:\.([A-Za-z0-9_-]+)|(\[(?:\d+|"(?:[^"\\]|\\.)*")\]))/.exec(remaining);
      if (!next) throw new Error(`无效路径：${source}`);
      tokens.push(next[1] ?? next[2]);
      remaining = remaining.slice(next[0].length);
    }
    let current: unknown = context;
    for (const token of tokens) {
      const key = token.startsWith("[") ? JSON.parse(token.slice(1, -1)) : token;
      if (!current || typeof current !== "object" || !Object.hasOwn(current, key)) {
        if (optional) return null;
        throw new Error(`样本缺少路径：${source}`);
      }
      current = (current as Record<string, unknown>)[key];
    }
    if (!tokens.length) throw new Error("引用路径不能为空");
    return current;
  };
  const whole = /^\{\{([^{}]+)\}\}$/.exec(value);
  if (whole) return lookup(whole[1]);
  return value.replace(/\{\{([^{}]+)\}\}/g, (_, path: string) => {
    const item = lookup(path);
    if (item === null || typeof item === "object") throw new Error(`混排文本不能插入对象或 null：${path}`);
    return String(item);
  });
}
