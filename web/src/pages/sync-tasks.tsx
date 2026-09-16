import { MutationForm } from "../ui";
import { useResourcePages, PageControls } from "../pagination";
import { Link } from "react-router";
import { Activity, CirclePlay, Clock3, Database, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";

import { statusLabel, useDemo } from "../demo";
import { Badge, Button, Card, EmptyState, Field, Modal, PageHeader, fieldClass, textAreaClass } from "../ui";
import { api } from "../api";

import { type SyncTask, MiniStat, scheduleLabel, FeatureCard, Th, Td } from "./core-shared";
export function SyncTasksPage() {
  const demo = useDemo();
  const readyIntegrations = demo.integrations.filter((item) => item.status === "ready");
  const [recordsTask, setRecordsTask] = useState<SyncTask | null>(null);
  const [recordPreview, setRecordPreview] = useState<unknown>(null);
  const records = useResourcePages(`/api/sync-tasks/${recordsTask?.id ?? "none"}/records`, {}, Boolean(recordsTask));
  const [tasks, setTasks] = useState<SyncTask[]>([]);
  const [availableActions, setAvailableActions] = useState<
    Array<{
      id: string;
      actionKey: string;
      systemKey: string;
      status: string;
      executable: boolean;
      integrationId?: string;
      httpMethod?: string;
    }>
  >([]);
  const [open, setOpen] = useState(false);
  const [editingTask, setEditingTask] = useState<string | null>(null);
  const [taskName, setTaskName] = useState("github-new-sync");
  const [integration, setIntegration] = useState(readyIntegrations[0]?.name ?? "");
  const [schedule, setSchedule] = useState("每 30 分钟");
  const [taskActionId, setTaskActionId] = useState("");
  const [taskConnectionId, setTaskConnectionId] = useState("");
  const [taskInput, setTaskInput] = useState("{}");
  const [syncConfig, setSyncConfig] = useState<Record<string, unknown>>({});

  function loadSyncTasks() {
    return Promise.all([api.syncTasks(), api.actions()]).then(([taskRows, actionRows]) => {
      setAvailableActions(actionRows);
      setTasks(
        taskRows.map((item) => ({
          id: item.id,
          name: item.taskKey,
          integration: item.integrationKey,
          actionId: item.actionId,
          connectionId: item.connectionId,
          schedule: scheduleLabel(item),
          checkpoint: JSON.stringify(item.checkpoint ?? {}),
          syncConfig: item.syncConfig ?? {},
          input: JSON.stringify(item.input ?? {}, null, 2),
          status: ["deployed", "paused", "disabled"].includes(item.status) ? item.status : "draft",
          lastRun: item.lastSuccessAt ? new Date(item.lastSuccessAt).toLocaleString("zh-CN") : "尚未运行",
        })),
      );
    });
  }
  useEffect(() => {
    if (!demo.authenticated) return;
    void loadSyncTasks().catch((error) => demo.notify(error));
  }, [demo.authenticated, demo.operations.length]);

  function editTask(task?: SyncTask) {
    setEditingTask(task?.name ?? null);
    setTaskName(task?.name ?? "new-sync-task");
    setIntegration(task?.integration ?? readyIntegrations[0]?.name ?? "");
    setSchedule(task?.schedule ?? "每 30 分钟");
    setTaskActionId(task?.actionId ?? "");
    setTaskConnectionId(task?.connectionId ?? "");
    setTaskInput(task?.input ?? "{}");
    setSyncConfig(task?.syncConfig ?? {});
    setOpen(true);
  }

  function saveSyncTask(event: FormEvent) {
    event.preventDefault();
    const name = taskName.trim();
    if (!name || !integration) return;
    const previous = tasks.find((item) => item.name === editingTask);
    const selectedIntegration = readyIntegrations.find((item) => item.name === integration);
    const selectedConnection =
      demo.connections.find(
        (item) => item.id === taskConnectionId && item.integration === integration && item.status === "active",
      ) ?? demo.connections.find((item) => item.integration === integration && item.status === "active");
    const selectedAction =
      availableActions.find(
        (item) =>
          item.id === taskActionId &&
          item.systemKey === selectedIntegration?.provider &&
          (!item.integrationId || item.integrationId === selectedIntegration?.id) &&
          item.httpMethod === "GET" &&
          item.status === "active" &&
          item.executable,
      ) ??
      availableActions.find(
        (item) =>
          item.systemKey === selectedIntegration?.provider &&
          (!item.integrationId || item.integrationId === selectedIntegration?.id) &&
          item.httpMethod === "GET" &&
          item.status === "active" &&
          item.executable,
      );
    if (!selectedIntegration || !selectedConnection || !selectedAction) {
      demo.notify("请选择集成、连接账号和 API 操作");
      return;
    }
    let parsedInput: Record<string, unknown>;
    try {
      parsedInput = JSON.parse(taskInput || "{}");
      if (!parsedInput || Array.isArray(parsedInput) || typeof parsedInput !== "object") throw new Error();
    } catch {
      demo.notify("同步输入必须是有效的 JSON 对象");
      return;
    }
    return api
      .saveSyncTask(
        {
          taskKey: name,
          name,
          integrationId: selectedIntegration.id,
          connectionId: selectedConnection.id,
          actionId: selectedAction.id,
          status: "draft",
          scheduleType: schedule === "仅手动运行" ? "manual" : "interval",
          cronExpression: schedule,
          scheduleTimezone: "Asia/Singapore",
          retryPolicy: { maxAttempts: 3 },
          input: parsedInput,
          syncConfig,
        },
        previous?.id,
      )
      .then(() => {
        setOpen(false);
        return Promise.all([loadSyncTasks(), demo.reload()]);
      })
      .then(() => demo.notify(`同步任务 ${name} 已保存`))
      .catch((error) => demo.notify(error));
  }

  return (
    <div className="grid gap-6">
      <PageHeader
        title="同步任务"
        description="按计划调用读取操作，去重保存结果，并记录检查点、重试和运行状态。"
        actions={
          <Button write onClick={() => editTask()} disabled={!readyIntegrations.length}>
            <Plus className="size-4" />
            新建同步任务
          </Button>
        }
      />
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--border)] bg-[var(--muted)] p-4">
        <div>
          <p className="text-sm font-bold">可选能力</p>
          <p className="mt-1 text-xs text-[var(--muted-text)]">
            实时接口调用请使用“API 操作”；外部事件接收请使用“Webhook”。
          </p>
        </div>
        <Button variant="secondary" onClick={() => void demo.reload()}>
          <RefreshCw className="size-4" />
          刷新状态
        </Button>
      </div>
      {!tasks.length && (
        <EmptyState
          title="尚无同步任务"
          description="先创建集成和连接，再选择读取操作配置同步。"
          action={
            readyIntegrations.length ? (
              <Button write onClick={() => editTask()}>
                新建同步任务
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
        {tasks.map((item) => (
          <Card className="p-4" key={item.name}>
            <div className="flex items-start justify-between gap-3">
              <div>
                <code className="font-semibold">{item.name}</code>
                <p className="mt-1 text-xs text-[var(--muted-text)]">{item.integration}</p>
              </div>
              <Badge tone={item.status === "deployed" ? "success" : item.status === "disabled" ? "danger" : "warning"}>
                {statusLabel(item.status)}
              </Badge>
            </div>
            <div className="mt-4 grid grid-cols-2 gap-3">
              <MiniStat label="执行计划" value={item.schedule} />
              <MiniStat label="最近运行" value={item.lastRun} />
            </div>
            <div className="mt-4 flex flex-wrap gap-2">
              <Button
                variant="secondary"
                onClick={() => {
                  setRecordsTask(item);
                  setRecordPreview(null);
                }}
              >
                查看记录
              </Button>
              <Button
                write
                variant="secondary"
                onClick={() =>
                  item.id &&
                  api
                    .pauseSyncTask(item.id)
                    .then(loadSyncTasks)
                    .catch((e) => demo.notify(e))
                }
                disabled={item.status !== "deployed"}
              >
                暂停
              </Button>
              <Button
                write
                variant="ghost"
                onClick={() =>
                  item.id &&
                  window.confirm(`确认删除同步任务“${item.name}”吗？`) &&
                  api
                    .deleteSyncTask(item.id)
                    .then(loadSyncTasks)
                    .catch((e) => demo.notify(e))
                }
              >
                删除
              </Button>
              <Button write variant="secondary" onClick={() => editTask(item)}>
                {item.status === "deployed" ? "编辑" : "继续配置"}
              </Button>
              {item.status !== "deployed" ? (
                <Button
                  write
                  onClick={() => {
                    if (item.id)
                      void api
                        .deploySyncTask(item.id)
                        .then(() => Promise.all([loadSyncTasks(), demo.reload()]))
                        .then(() => demo.notify(`同步任务 ${item.name} 已部署`))
                        .catch((error) => demo.notify(error));
                  }}
                >
                  部署
                </Button>
              ) : (
                <Button write onClick={() => demo.run("Sync", item.name, item.integration)}>
                  立即运行
                </Button>
              )}
            </div>
          </Card>
        ))}
      </div>
      <Card className="hidden overflow-hidden md:block">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-left text-sm">
            <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
              <tr>
                <Th>同步任务</Th>
                <Th>集成配置</Th>
                <Th>执行计划</Th>
                <Th>运行检查点</Th>
                <Th>状态</Th>
                <Th>最近运行</Th>
                <Th />
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--border)]">
              {tasks.map((item) => (
                <tr key={item.name}>
                  <Td>
                    <div className="flex items-center gap-3">
                      <span className="grid size-9 place-items-center rounded-lg bg-[var(--muted)] text-blue-600">
                        <RefreshCw className="size-4" />
                      </span>
                      <code className="font-semibold">{item.name}</code>
                    </div>
                  </Td>
                  <Td>{item.integration}</Td>
                  <Td>{item.schedule}</Td>
                  <Td>
                    <code className="text-xs text-[var(--muted-text)]">{item.checkpoint}</code>
                  </Td>
                  <Td>
                    <Badge
                      tone={item.status === "deployed" ? "success" : item.status === "disabled" ? "danger" : "warning"}
                    >
                      {statusLabel(item.status)}
                    </Badge>
                  </Td>
                  <Td>{item.lastRun}</Td>
                  <Td>
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="secondary"
                        onClick={() => {
                          setRecordsTask(item);
                          setRecordPreview(null);
                        }}
                      >
                        查看记录
                      </Button>
                      <Button
                        write
                        variant="secondary"
                        onClick={() =>
                          item.id &&
                          api
                            .pauseSyncTask(item.id)
                            .then(loadSyncTasks)
                            .catch((e) => demo.notify(e))
                        }
                        disabled={item.status !== "deployed"}
                      >
                        暂停
                      </Button>
                      <Button
                        write
                        variant="ghost"
                        onClick={() =>
                          item.id &&
                          window.confirm(`确认删除同步任务“${item.name}”吗？`) &&
                          api
                            .deleteSyncTask(item.id)
                            .then(loadSyncTasks)
                            .catch((e) => demo.notify(e))
                        }
                      >
                        删除
                      </Button>
                      <Button write variant="secondary" onClick={() => editTask(item)}>
                        {item.status === "deployed" ? "编辑" : "继续配置"}
                      </Button>
                      {item.status !== "deployed" ? (
                        <Button
                          write
                          onClick={() => {
                            if (item.id)
                              void api
                                .deploySyncTask(item.id)
                                .then(() => Promise.all([loadSyncTasks(), demo.reload()]))
                                .then(() => demo.notify(`同步任务 ${item.name} 已部署`))
                                .catch((error) => demo.notify(error));
                          }}
                        >
                          部署
                        </Button>
                      ) : (
                        <Button write variant="secondary" onClick={() => demo.run("Sync", item.name, item.integration)}>
                          <CirclePlay className="size-4" />
                          立即运行
                        </Button>
                      )}
                      <Button
                        write
                        aria-label={`删除 ${item.name}`}
                        variant="ghost"
                        onClick={() =>
                          item.id &&
                          window.confirm(`确认删除同步任务“${item.name}”吗？`) &&
                          void api
                            .deleteSyncTask(item.id)
                            .then(() => Promise.all([loadSyncTasks(), demo.reload()]))
                            .catch((error) => demo.notify(error))
                        }
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
        <FeatureCard icon={Database} title="结果落库" text="按外部记录 ID 去重保存响应数据，并维护最近运行检查点。" />
        <FeatureCard icon={Clock3} title="计划与重试" text="按固定周期运行，失败后自动重试并支持手动补跑。" />
        <FeatureCard icon={Activity} title="运行记录" text="查看每次同步的状态、耗时、数量和错误信息。" />
      </section>
      <Modal
        open={Boolean(recordsTask)}
        onOpenChange={(open) => !open && setRecordsTask(null)}
        title={`${recordsTask?.name ?? ""} 同步记录`}
        description="查看已保存的结果；点击记录预览完整数据。"
      >
        <PageControls page={records} />
        <Link className="text-blue-600 text-sm" to={`/operations?task=${recordsTask?.id ?? ""}`}>
          查看同步运行
        </Link>
        <div className="mt-3 grid gap-2">
          {records.items.map((item) => (
            <button
              key={item.id}
              className="rounded-lg border p-3 text-left text-sm"
              onClick={() => setRecordPreview(item.payload)}
            >
              {item.externalId} · {new Date(item.lastSeenAt).toLocaleString()}
            </button>
          ))}
        </div>
        {recordPreview !== null && (
          <pre className="mt-3 max-h-64 overflow-auto text-xs">{JSON.stringify(recordPreview, null, 2)}</pre>
        )}
      </Modal>
      <Modal
        open={open}
        onOpenChange={setOpen}
        title={editingTask ? "编辑同步任务" : "新建同步任务"}
        description="选择集成、连接和拉取操作；编辑已部署任务会回到草稿，需要重新部署。"
      >
        <MutationForm className="grid gap-4" onSubmit={saveSyncTask}>
          <Field label="名称">
            <input
              className={fieldClass}
              value={taskName}
              onChange={(event) => setTaskName(event.target.value)}
              placeholder="erp-orders-sync"
              required
            />
          </Field>
          <Field label="集成配置">
            <select
              className={fieldClass}
              value={integration}
              onChange={(event) => {
                setIntegration(event.target.value);
                setTaskConnectionId("");
                setTaskActionId("");
              }}
              required
            >
              <option value="">请选择已就绪集成</option>
              {readyIntegrations.map((item) => (
                <option key={item.id} value={item.name}>
                  {item.displayName} · {item.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="执行计划">
            <select className={fieldClass} value={schedule} onChange={(event) => setSchedule(event.target.value)}>
              <option>每 15 分钟</option>
              <option>每 30 分钟</option>
              <option>每小时</option>
              <option>每天</option>
              <option>仅手动运行</option>
            </select>
          </Field>
          <Field label="连接账号">
            <select
              className={fieldClass}
              value={taskConnectionId}
              onChange={(event) => setTaskConnectionId(event.target.value)}
              required
            >
              <option value="">请选择连接账号</option>
              {demo.connections
                .filter((item) => item.integration === integration && item.status === "active")
                .map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name} · {item.endUser}
                  </option>
                ))}
            </select>
          </Field>
          <Field label="拉取操作">
            <select
              className={fieldClass}
              value={taskActionId}
              onChange={(event) => setTaskActionId(event.target.value)}
              required
            >
              <option value="">请选择 API 操作</option>
              {availableActions
                .filter(
                  (item) =>
                    item.systemKey === readyIntegrations.find((row) => row.name === integration)?.provider &&
                    (!item.integrationId ||
                      item.integrationId === readyIntegrations.find((row) => row.name === integration)?.id) &&
                    item.httpMethod === "GET" &&
                    item.status === "active" &&
                    item.executable,
                )
                .map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.actionKey}
                  </option>
                ))}
            </select>
          </Field>
          <details>
            <summary className="cursor-pointer text-sm font-semibold">高级分页与去重设置</summary>
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              {[
                ["recordsPath", "记录数组路径", "data"],
                ["idPath", "记录主键路径", "id"],
                ["cursorPath", "下一页游标路径", "meta.next"],
                ["cursorParam", "请求游标参数", "after"],
                ["pageSizeParam", "请求页大小参数", "limit"],
              ].map(([key, label, placeholder]) => (
                <Field key={key} label={label}>
                  <input
                    className={fieldClass}
                    placeholder={placeholder}
                    value={String(syncConfig[key] ?? "")}
                    onChange={(event) => setSyncConfig((current) => ({ ...current, [key]: event.target.value }))}
                  />
                </Field>
              ))}
              <Field label="每页条数">
                <input
                  type="number"
                  min={1}
                  max={1000}
                  className={fieldClass}
                  value={Number(syncConfig.pageSize ?? 100)}
                  onChange={(event) =>
                    setSyncConfig((current) => ({ ...current, pageSize: Number(event.target.value) }))
                  }
                />
              </Field>
            </div>
          </details>
          <Field label="操作输入 JSON" hint="必须满足所选 API 操作的输入约束，例如分页大小或查询条件。">
            <textarea
              className={`${textAreaClass} min-h-28 font-mono text-xs`}
              value={taskInput}
              onChange={(event) => setTaskInput(event.target.value)}
            />
          </Field>
          <div className="flex justify-end">
            <Button write type="submit">
              保存任务
            </Button>
          </div>
        </MutationForm>
      </Modal>
    </div>
  );
}
