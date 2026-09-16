import { useResourcePages } from "../pagination";
import { ErrorState } from "../ui";
import { useState } from "react";
import { ArrowRight, Blocks, Cable, Check, CirclePlay, Layers3, RefreshCw, Webhook } from "lucide-react";

import { Link } from "react-router";
import { useDemo } from "../demo";
import { Button, Card, PageHeader, cn } from "../ui";

import { QuickLink } from "./core-shared";
export function GettingStartedPage() {
  const demo = useDemo();
  const [selectedKey, setSelectedKey] = useState(new URLSearchParams(window.location.search).get("integration") ?? "");
  const [selectedConnectionId, setSelectedConnectionId] = useState(
    new URLSearchParams(window.location.search).get("connection") ?? "",
  );
  const selected = demo.integrations.find((item) => item.name === selectedKey);
  const selectedConnection = demo.connections.find(
    (item) => item.id === selectedConnectionId && item.integration === selected?.name,
  );
  const successfulCalls = useResourcePages(
    "/api/operations",
    {
      integration: selected?.id ?? "",
      connection: selectedConnection?.id ?? "",
      kind: "action",
      status: "success",
      limit: "1",
    },
    demo.authenticated && Boolean(selected && selectedConnection),
  );
  const context = selected
    ? `?system=${encodeURIComponent(selected.provider)}&integration=${encodeURIComponent(selected.name)}${selectedConnection ? `&connection=${encodeURIComponent(selectedConnection.id)}` : ""}`
    : "";
  const steps = [
    ["选择或添加系统", "从公共平台目录中选择系统，或者添加企业内部系统、分组和认证实例。", Cable, "/providers"],
    ["配置认证实例", "为所选系统配置认证方式和请求注入规则。", Layers3, "/auth"],
    ["创建集成配置", "选择系统、API 基础地址与已绑定的认证实例。", Blocks, "/integrations"],
    ["创建连接账号", "为最终用户、企业租户或服务账号安全保存真实凭据。", Layers3, "/connections"],
    ["验证第一个操作", "选择连接账号，验证权限、凭据注入和 API 返回结构。", CirclePlay, "/actions"],
  ] as const;
  const completed = [
    Boolean(selected),
    Boolean(
      selected &&
      demo.authInstances.some((instance) => instance.id === selected.authInstanceId && instance.status === "ready"),
    ),
    Boolean(selected?.status === "ready"),
    Boolean(selected && selectedConnection?.status === "active"),
    Boolean(selected && selectedConnection && successfulCalls.items.length),
  ];
  const done = completed.filter(Boolean).length;
  return (
    <div className="grid gap-6">
      <PageHeader title="快速开始" description="按五个核心步骤完成系统接入、连接账号和操作验证。" />
      <label className="grid gap-2 text-sm">
        接入目标
        <select
          className="rounded-lg border p-2 bg-[var(--surface)]"
          value={selectedKey}
          onChange={(event) => {
            setSelectedKey(event.target.value);
            setSelectedConnectionId("");
          }}
        >
          <option value="">请选择集成；尚无集成时从第一步开始</option>
          {demo.integrations.map((item) => (
            <option translate="no" key={item.id} value={item.name}>
              {item.displayName} · {item.name}
            </option>
          ))}
        </select>
      </label>
      {selected && (
        <label className="grid gap-2 text-sm">
          验证连接
          <select
            className="rounded-lg border p-2 bg-[var(--surface)]"
            value={selectedConnectionId}
            onChange={(event) => setSelectedConnectionId(event.target.value)}
          >
            <option value="">请选择本次接入的连接</option>
            {demo.connections
              .filter((item) => item.integration === selected.name)
              .map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name} · {item.endUser}
                </option>
              ))}
          </select>
        </label>
      )}
      {successfulCalls.error && <ErrorState error={successfulCalls.error} />}
      <Card className="overflow-hidden">
        <div className="border-b border-[var(--border)] bg-gradient-to-r from-blue-600 to-cyan-500 p-6 text-white">
          <div className="flex items-center justify-between gap-4">
            <div>
              <p className="text-sm font-semibold text-blue-100">系统接入进度</p>
              <h2 className="mt-1 text-2xl font-bold">
                {done} / {steps.length} 已完成
              </h2>
            </div>
            <div className="grid size-16 place-items-center rounded-full border-4 border-white/25 text-lg font-bold">
              {Math.round((done / steps.length) * 100)}%
            </div>
          </div>
          <div className="mt-5 h-2 overflow-hidden rounded-full bg-white/20">
            <div
              className="h-full rounded-full bg-white transition-all"
              style={{ width: `${(done / steps.length) * 100}%` }}
            />
          </div>
        </div>
        <div className="divide-y divide-[var(--border)]">
          {steps.map(([title, description, Icon, to], index) => {
            const isCompleted = completed[index];
            return (
              <div key={title} className="flex flex-col gap-4 p-5 sm:flex-row sm:items-center">
                <span
                  aria-label={`${title}${isCompleted ? "已完成" : "未完成"}`}
                  className={cn(
                    "grid size-9 shrink-0 place-items-center rounded-full border",
                    isCompleted
                      ? "border-emerald-500 bg-emerald-500 text-white"
                      : "border-[var(--border)] bg-[var(--surface)]",
                  )}
                >
                  {isCompleted ? <Check className="size-4" /> : <span className="text-sm font-bold">{index + 1}</span>}
                </span>
                <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-[var(--muted)] text-blue-600">
                  <Icon className="size-5" />
                </span>
                <div className="flex-1">
                  <p className="font-bold">{title}</p>
                  <p className="mt-1 text-sm text-[var(--muted-text)]">{description}</p>
                </div>
                <Button asChild variant="secondary">
                  <Link to={to + context}>
                    打开{title} <ArrowRight className="size-4" />
                  </Link>
                </Button>
              </div>
            );
          })}
        </div>
      </Card>
      <section>
        <div className="mb-3">
          <h2 className="font-bold">接下来可以做</h2>
          <p className="mt-1 text-sm text-[var(--muted-text)]">这些是可选能力，不影响基础集成投入使用。</p>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <QuickLink to="/sync" icon={RefreshCw} title="同步任务" text="定时调用读取操作并保存结果和运行检查点。" />
          <QuickLink to="/webhooks" icon={Webhook} title="Webhook" text="配置系统事件、平台通知和投递重试。" />
        </div>
      </section>
    </div>
  );
}
