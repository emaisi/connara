import {
  Activity,
  AlertTriangle,
  ArrowRight,
  Blocks,
  Cable,
  CirclePlay,
  KeyRound,
  Layers3,
  Sparkles,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { operationKindLabel, useDemo } from "../demo";
import { Badge, Button, Card, PageHeader } from "../ui";
import { api } from "../api";

import { QuickLink, StatusDot } from "./core-shared";
export function DemoOverviewPage() {
  const demo = useDemo();
  const [metrics, setMetrics] = useState({ requests: 0, successes: 0, successRate: 0 });
  const operationStatusKey = demo.operations.map((item) => `${item.id}:${item.status}`).join("|");
  useEffect(() => {
    if (demo.authenticated)
      void api
        .metrics()
        .then(setMetrics)
        .catch(() => undefined);
  }, [demo.authenticated, operationStatusKey]);
  const failedOperations = demo.operations.filter(
    (operation) => operation.status === "failed" || operation.status === "unknown",
  ).length;
  const unhealthyConnections = demo.connections.filter((connection) => connection.status !== "active").length;
  const readyIntegrations = demo.integrations.filter((integration) => integration.status === "ready").length;
  const activeConnections = demo.connections.filter((connection) => connection.status === "active").length;
  const stats = [
    { label: "系统目录", value: demo.customSystems.length, hint: "内置与企业系统", icon: Cable },
    { label: "集成配置", value: demo.integrations.length, hint: `${readyIntegrations} 个已配置`, icon: Blocks },
    { label: "连接账号", value: demo.connections.length, hint: `${activeConnections} 个已配置`, icon: Layers3 },
    { label: "近 24h 调用", value: metrics.requests, hint: `${metrics.successRate.toFixed(1)}% 成功`, icon: Activity },
  ];
  return (
    <div className="grid gap-7">
      <PageHeader
        title="集成总览"
        description="这里统一展示系统接入、连接账号、API 操作和运行状态。"
        actions={
          <>
            {unhealthyConnections > 0 && (
              <Button
                asChild
                variant="secondary"
                className="h-8 border-amber-200 bg-amber-50 text-amber-700 hover:bg-amber-100 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-300"
              >
                <Link to="/connections">
                  <AlertTriangle className="size-4" />
                  {unhealthyConnections} 个连接待处理
                </Link>
              </Button>
            )}
            {failedOperations > 0 && (
              <Button
                asChild
                variant="secondary"
                className="h-8 border-red-200 bg-red-50 text-red-700 hover:bg-red-100 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
              >
                <Link to="/operations">
                  <AlertTriangle className="size-4" />
                  {failedOperations} 次运行需处理
                </Link>
              </Button>
            )}
            {unhealthyConnections === 0 && failedOperations === 0 && (
              <Badge tone="neutral">
                {demo.connections.length ? "暂无已知异常；上游状态以实际调用为准" : "尚未接入连接"}
              </Badge>
            )}
          </>
        }
      />
      <section className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        {stats.map((stat) => (
          <Card key={stat.label} className="p-4 sm:p-5">
            <div className="flex items-start justify-between">
              <div>
                <p className="text-sm text-[var(--muted-text)]">{stat.label}</p>
                <strong className="mt-2 block text-2xl tracking-tight sm:text-3xl">{stat.value}</strong>
              </div>
              <span className="rounded-xl bg-blue-50 p-2 text-blue-600 dark:bg-blue-950 dark:text-blue-300 sm:p-2.5">
                <stat.icon className="size-5" />
              </span>
            </div>
            <p className="mt-3 text-xs text-[var(--muted-text)] sm:mt-4">{stat.hint}</p>
          </Card>
        ))}
      </section>
      <section className="grid gap-4 xl:grid-cols-[1.5fr_1fr]">
        <Card className="order-2 p-5 xl:order-1">
          <div className="mb-5 flex items-start justify-between">
            <div>
              <h2 className="font-bold">运行组成</h2>
              <p className="text-sm text-[var(--muted-text)]">当前已加载的真实运行记录</p>
            </div>
            <Badge tone="info">{demo.operations.length} 条</Badge>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            {(["Action", "Sync", "Webhook", "Auth"] as const).map((kind) => (
              <div key={kind} className="rounded-xl bg-[var(--muted)] p-4">
                <p className="text-xs text-[var(--muted-text)]">{operationKindLabel(kind)}</p>
                <p className="mt-1 text-2xl font-bold">{demo.operations.filter((item) => item.kind === kind).length}</p>
              </div>
            ))}
          </div>
        </Card>
        <Card className="order-1 p-5 xl:order-2">
          <h2 className="font-bold">最近活动</h2>
          <p className="mb-4 text-sm text-[var(--muted-text)]">最近 API 调用、授权、同步和事件记录</p>
          <div className="grid gap-1">
            {demo.operations.slice(0, 5).map((operation) => (
              <Link
                to={`/operations?run=${encodeURIComponent(operation.id)}`}
                key={operation.id}
                className="flex items-center gap-3 rounded-xl p-2.5 transition hover:bg-[var(--muted)]"
              >
                <StatusDot status={operation.status} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-semibold">{operation.name}</p>
                  <p className="text-xs text-[var(--muted-text)]">
                    {operationKindLabel(operation.kind)} · {operation.time}
                  </p>
                </div>
                <ArrowRight className="size-4 text-[var(--muted-text)]" />
              </Link>
            ))}
          </div>
        </Card>
      </section>
      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <QuickLink to="/getting-started" icon={Sparkles} title="开始构建集成" text="按五步完成系统接入和验证" />
        <QuickLink to="/providers" icon={Cable} title="添加企业系统" text="设置系统分组和允许的认证模板" />
        <QuickLink to="/auth" icon={KeyRound} title="配置认证" text="从模板创建认证实例并绑定系统" />
        <QuickLink to="/actions" icon={CirclePlay} title="验证 API 操作" text="选择连接账号并验证调用结果" />
      </section>
    </div>
  );
}
