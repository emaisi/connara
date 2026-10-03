import { useEffect, useState } from "react";
import { Link } from "react-router";
import { api } from "../api";
import { statusLabel, useDemo } from "../demo";
import { Badge, Button, Card, EmptyState, PageHeader } from "../ui";
import { executionBlock, type Capabilities, type WorkflowItem } from "./workflow-model";
export function WorkflowsPage() {
  const demo = useDemo();
  const [rows, setRows] = useState<WorkflowItem[]>([]);
  const [cap, setCap] = useState<Capabilities | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const load = () =>
    Promise.all([api.workflows(), api.workflowCapabilities()]).then(([rows, cap]) => {
      setRows(rows);
      setCap(cap);
      setError("");
      setLoading(false);
    });
  useEffect(() => {
    if (demo.authenticated)
      void load().catch((error) => {
        setError(String(error));
        setLoading(false);
      });
  }, [demo.authenticated]);
  return (
    <div className="grid gap-4">
      <PageHeader
        title="工作流"
        description="配置 API、数据处理、代码与条件分支，按依赖顺序串行执行。"
        actions={
          <div className="flex gap-2">
            <Button variant="secondary" onClick={() => void load().catch((error) => setError(String(error)))}>
              刷新
            </Button>
            <Button write asChild>
              <Link to="/workflows/new">新建工作流</Link>
            </Button>
          </div>
        }
      />
      {error && <p role="alert">{error}</p>}
      {loading && <p role="status">正在加载工作流…</p>}
      {!loading && !rows.length && !error && (
        <EmptyState title="尚无工作流" description="创建流程，配置输入来源和最终返回字段。" />
      )}
      {rows.map((row) => {
        const block = executionBlock(row, cap);
        return (
          <Card key={row.id} className="grid gap-3 p-4">
            <div className="flex flex-wrap justify-between gap-2">
              <div>
                <strong>{row.name}</strong>
                <p className="text-xs">
                  {row.workflowKey} · {row.graph.steps?.length ?? 0} 个执行步骤 · 版本 {row.version}
                </p>
              </div>
              <Badge>
                {statusLabel(row.status)}
                {block ? ` · ${block}` : ""}
              </Badge>
            </div>
            <p className="text-xs">
              {row.scheduleType === "manual" ? "手动" : `${row.cronExpression} · ${row.scheduleTimezone}`}
              {row.nextRunAt ? ` · ${block ? "规则计划，当前不会触发" : "下次触发"} ${row.nextRunAt}` : ""}
            </p>
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" asChild>
                <Link to={`/workflows/${row.id}`}>打开流程</Link>
              </Button>
              <Button variant="ghost" asChild>
                <Link to={`/workflows/${row.id}/history`}>运行历史</Link>
              </Button>
              {row.status === "deployed" && (
                <Button
                  write
                  variant="secondary"
                  onClick={() =>
                    void api
                      .pauseWorkflow(row.id, row.version)
                      .then(load)
                      .catch((error) => demo.notify(error))
                  }
                >
                  暂停
                </Button>
              )}
              <Button
                write
                variant="ghost"
                onClick={() => {
                  if (window.confirm(`删除工作流 ${row.workflowKey}？已接受的运行仍会完成。`))
                    void api
                      .deleteWorkflow(row.id)
                      .then(load)
                      .catch((error) => demo.notify(error));
                }}
              >
                删除
              </Button>
            </div>
          </Card>
        );
      })}
    </div>
  );
}
