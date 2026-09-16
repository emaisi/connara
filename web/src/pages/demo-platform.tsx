import { useUnsavedChanges } from "../unsaved";
import { MutationForm } from "../ui";
import { useResourcePages, PageControls } from "../pagination";
import { useCanWrite } from "../permissions";
import {
  CheckCircle2,
  ChevronRight,
  Code2,
  Download,
  Fingerprint,
  KeyRound,
  Plus,
  Search,
  ShieldCheck,
  TerminalSquare,
  Trash2,
  UserPlus,
  X,
} from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { statusLabel, useDemo } from "../demo";
import { Badge, Button, Card, CopyButton, Field, Modal, PageHeader, fieldClass, textAreaClass } from "../ui";
import { Tabs } from "./core-shared";
import { api } from "../api";

export function DemoAccessPage() {
  const demo = useDemo();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("new-agent-token");
  const [revealed, setRevealed] = useState(false);
  const [plainToken, setPlainToken] = useState("");
  const [allowedActions, setAllowedActions] = useState(new URLSearchParams(window.location.search).get("action") ?? "");
  const [allConnections, setAllConnections] = useState(false);
  const [allActions, setAllActions] = useState(false);
  const [blockedActions, setBlockedActions] = useState("");
  const [allowedConnections, setAllowedConnections] = useState<string[]>(
    new URLSearchParams(window.location.search).get("connection")
      ? [new URLSearchParams(window.location.search).get("connection")!]
      : [],
  );
  function create(event: FormEvent) {
    event.preventDefault();
    if ((!allActions && !allowedActions.trim()) || (!allConnections && !allowedConnections.length)) {
      demo.notify("请选择明确的操作与连接范围，或主动允许全部");
      return;
    }
    return api
      .createRuntimeToken({
        name,
        allowedActions: (allActions ? "*" : allowedActions)
          .split(/[,\n]+/)
          .map((value) => value.trim())
          .filter(Boolean),
        blockedActions: blockedActions
          .split(/[,\n]+/)
          .map((value) => value.trim())
          .filter(Boolean),
        allowedConnections: allConnections ? [] : allowedConnections,
      })
      .then((result) => {
        setPlainToken(result.token);
        setOpen(false);
        setRevealed(true);
        return demo.reload();
      })
      .catch((error) => demo.notify(error));
  }
  return (
    <div className="grid gap-6">
      <PageHeader
        title="访问控制"
        description="创建运行时令牌，并直接限定它可以调用的操作和连接账号。"
        actions={
          <Button adminOnly onClick={() => setOpen(true)}>
            <Plus className="size-4" />
            创建 Token
          </Button>
        }
      />
      {revealed && (
        <Card className="border-emerald-300 bg-emerald-50 p-5 dark:border-emerald-900 dark:bg-emerald-950">
          <div className="flex items-start gap-3">
            <CheckCircle2 className="mt-0.5 size-5 text-emerald-600" />
            <div className="min-w-0 flex-1">
              <p className="font-bold text-emerald-800 dark:text-emerald-200">令牌已创建，仅显示一次</p>
              <code className="mt-2 block overflow-x-auto rounded-lg bg-white/70 p-3 text-xs text-emerald-800 dark:bg-slate-950 dark:text-emerald-300">
                {plainToken}
              </code>
            </div>
            <CopyButton value={plainToken} />
            <button aria-label="关闭令牌提示" onClick={() => setRevealed(false)}>
              <X className="size-4" />
            </button>
          </div>
        </Card>
      )}
      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[880px] text-left text-sm">
            <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
              <tr>
                <Th>令牌</Th>
                <Th>状态</Th>
                <Th>操作策略</Th>
                <Th>连接账号</Th>
                <Th>最近使用</Th>
                <Th />
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--border)]">
              {demo.tokens.map((token) => (
                <tr key={token.id}>
                  <Td>
                    <p className="font-semibold">{token.name}</p>
                    <code className="text-xs text-[var(--muted-text)]">{token.id}</code>
                  </Td>
                  <Td>
                    <Badge tone={token.status === "active" ? "success" : "danger"}>{statusLabel(token.status)}</Badge>
                  </Td>
                  <Td>
                    <p>{token.actions.join(", ") || "不允许"}</p>
                    {Boolean(token.blockedActions?.length) && (
                      <p className="mt-1 text-xs text-red-600">排除：{token.blockedActions?.join(", ")}</p>
                    )}
                  </Td>
                  <Td>{token.connections.length ? token.connections.join(", ") : "全部"}</Td>
                  <Td>{token.lastUsed}</Td>
                  <Td>
                    <Button
                      adminOnly
                      aria-label={`撤销 ${token.name}`}
                      variant="ghost"
                      disabled={token.status === "revoked"}
                      onClick={() =>
                        window.confirm(`确认撤销令牌“${token.name}”吗？使用此令牌的应用将立即失去访问权限。`) &&
                        demo.revokeToken(token.id)
                      }
                    >
                      <Trash2 className="size-4" />
                      撤销
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      <Modal open={open} onOpenChange={setOpen} title="创建运行时令牌" description="明文只在创建成功后显示一次。">
        <MutationForm adminOnly className="grid gap-4" onSubmit={create}>
          <Field label="令牌名称">
            <input required className={fieldClass} value={name} onChange={(event) => setName(event.target.value)} />
          </Field>
          <label className="flex gap-2 text-sm">
            <input type="checkbox" checked={allActions} onChange={(e) => setAllActions(e.target.checked)} />
            允许全部操作（包括未来新增操作）
          </label>
          <Field label="允许的操作">
            <textarea
              className={textAreaClass}
              value={allowedActions}
              onChange={(event) => setAllowedActions(event.target.value)}
            />
          </Field>
          <Field label="禁止的操作" hint="可选；拒绝规则优先于允许规则。">
            <textarea
              className={textAreaClass}
              value={blockedActions}
              onChange={(event) => setBlockedActions(event.target.value)}
              placeholder="例如：github.delete_*"
            />
          </Field>
          <label className="flex gap-2 text-sm">
            <input type="checkbox" checked={allConnections} onChange={(e) => setAllConnections(e.target.checked)} />
            允许全部连接（包括未来新增连接）
          </label>
          <Field label="连接账号范围">
            <select
              multiple
              className={`${fieldClass} h-24`}
              value={allowedConnections}
              onChange={(event) =>
                setAllowedConnections(Array.from(event.target.selectedOptions, (option) => option.value))
              }
            >
              {demo.connections
                .filter((item) => item.status === "active")
                .map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
            </select>
          </Field>
          <p className="text-sm">
            授权范围：{allActions ? "全部操作" : allowedActions || "未选择操作"}；
            {allConnections ? "全部连接" : `${allowedConnections.length} 个指定连接`}。拒绝规则优先。
          </p>
          <Button write type="submit">
            创建令牌
          </Button>
        </MutationForm>
      </Modal>
    </div>
  );
}

export function PlatformSettingsPage() {
  const demo = useDemo();
  const canAdmin = useCanWrite(true);
  const [dirty, setDirty] = useState(false);
  useUnsavedChanges(dirty);
  const [tab, setTab] = useState("常规设置");
  const [platformName, setPlatformName] = useState("APIHub");
  const [publicBaseUrl, setPublicBaseUrl] = useState("");
  const [operationRetentionDays, setOperationRetentionDays] = useState(30);
  const [auditRetentionDays, setAuditRetentionDays] = useState(365);
  const [version, setVersion] = useState(1);
  const [runtimeParameters, setRuntimeParameters] = useState<Record<string, unknown>>({});
  useEffect(() => {
    void api
      .settings()
      .then((item) => {
        setPlatformName(item.platformName);
        setPublicBaseUrl(item.publicBaseUrl);
        setOperationRetentionDays(item.operationRetentionDays);
        setAuditRetentionDays(item.auditRetentionDays ?? 365);
        setVersion(item.version);
        setRuntimeParameters(item.runtimeParameters ?? {});
      })
      .catch((error) => demo.notify(error));
  }, []);
  function saveSettings() {
    return api
      .saveSettings({
        platformName,
        publicBaseUrl,
        runtimeParameters,
        operationRetentionDays,
        auditRetentionDays,
        version,
      })
      .then((item) => {
        setVersion(item.version);
        setDirty(false);
        return demo.reload();
      })
      .then(() => demo.notify("平台设置已保存"))
      .catch((error) => demo.notify(error));
  }
  return (
    <div className="grid gap-6">
      <PageHeader title="平台设置" description="管理平台显示信息、OAuth 公开回调地址和数据保留策略。" />
      <Tabs value={tab} onChange={setTab} items={["常规设置", "数据保留"]} />
      {tab === "常规设置" && (
        <Card className="max-w-3xl p-5">
          <fieldset disabled={!canAdmin} onChangeCapture={() => setDirty(true)} className="grid gap-4">
            <Field label="平台名称">
              <input
                className={fieldClass}
                value={platformName}
                onChange={(event) => setPlatformName(event.target.value)}
              />
            </Field>
            <Field label="公开基础地址">
              <input
                className={fieldClass}
                value={publicBaseUrl}
                onChange={(event) => setPublicBaseUrl(event.target.value)}
                placeholder="https://hub.example.com"
              />
            </Field>
            <Button adminOnly className="w-fit" onClick={saveSettings}>
              保存设置
            </Button>
          </fieldset>
        </Card>
      )}
      {tab === "数据保留" && (
        <Card className="max-w-3xl p-5">
          <fieldset disabled={!canAdmin} onChangeCapture={() => setDirty(true)} className="grid gap-4">
            <Field label="运行日志">
              <select
                className={fieldClass}
                value={operationRetentionDays}
                onChange={(event) => setOperationRetentionDays(Number(event.target.value))}
              >
                <option value={30}>30 天</option>
                <option value={90}>90 天</option>
                <option value={365}>1 年</option>
              </select>
            </Field>
            <Field label="审计日志">
              <select
                className={fieldClass}
                value={auditRetentionDays}
                onChange={(event) => setAuditRetentionDays(Number(event.target.value))}
              >
                <option value={365}>1 年</option>
                <option value={1095}>3 年</option>
                <option value={3650}>10 年</option>
              </select>
            </Field>
            <Button adminOnly onClick={saveSettings}>
              保存策略
            </Button>
          </fieldset>
        </Card>
      )}
    </div>
  );
}

export function TeamPage() {
  const demo = useDemo();
  const canAdmin = useCanWrite(true);
  const [members, setMembers] = useState<
    Array<{ id: string; name: string; email: string; role: string; status: string; version: number }>
  >([]);
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [inviteRole, setInviteRole] = useState("developer");
  const [invitationToken, setInvitationToken] = useState("");
  const loadMembers = () =>
    api.teamMembers().then((items) =>
      setMembers(
        items.map((item) => ({
          id: item.id,
          name: item.displayName,
          email: item.email,
          role: item.role,
          status: item.status,
          version: item.version,
        })),
      ),
    );
  useEffect(() => {
    void loadMembers().catch((error) => demo.notify(error));
  }, []);
  return (
    <div className="grid gap-6">
      <PageHeader
        title="团队管理"
        description="维护平台成员、邀请状态和角色目录。"
        actions={
          <Button adminOnly onClick={() => setOpen(true)}>
            <UserPlus className="size-4" />
            邀请成员
          </Button>
        }
      />
      {invitationToken && (
        <Card className="border-emerald-300 bg-emerald-50 p-4 dark:border-emerald-900 dark:bg-emerald-950">
          <p className="font-bold text-emerald-800 dark:text-emerald-200">一次性邀请链接（7 天有效）</p>
          <code className="my-3 block overflow-x-auto rounded-lg bg-white/70 p-3 text-xs dark:bg-slate-950">
            {invitationToken}
          </code>
          <CopyButton value={invitationToken} />
        </Card>
      )}
      <section className="grid gap-3 sm:grid-cols-2">
        <MiniStat label="成员" value={members.filter((item) => item.status === "active").length} />
        <MiniStat label="待接受邀请" value={members.filter((item) => item.status === "invited").length} />
      </section>
      <Card className="overflow-hidden">
        <div className="divide-y divide-[var(--border)]">
          {members.map((member) => (
            <div key={member.email} className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
              <span className="grid size-10 place-items-center rounded-full bg-gradient-to-br from-blue-500 to-violet-500 text-sm font-bold text-white">
                {member.name
                  .split(" ")
                  .map((item) => item[0])
                  .join("")}
              </span>
              <div className="flex-1">
                <p translate="no" className="font-semibold">
                  {member.name}
                </p>
                <p className="text-xs text-[var(--muted-text)]">{member.email}</p>
              </div>
              <Badge tone={member.status === "active" ? "success" : "warning"}>{statusLabel(member.status)}</Badge>
              <select
                aria-label={`${member.name} 角色`}
                className={`${fieldClass} sm:w-40`}
                value={member.role}
                disabled={!canAdmin || member.role === "owner"}
                onChange={(event) => {
                  void api
                    .updateMember(member.id, { role: event.target.value, version: member.version })
                    .then(() => loadMembers())
                    .then(() => demo.notify(`${member.name} 的角色已更新`))
                    .catch((error) => demo.notify(error));
                }}
              >
                <option value="owner">所有者</option>
                <option value="admin">管理员</option>
                <option value="developer">开发者</option>
                <option value="viewer">只读成员</option>
              </select>
              {member.status === "invited" && (
                <Button
                  adminOnly
                  variant="secondary"
                  onClick={() =>
                    api
                      .inviteMember({ email: member.email, role: member.role })
                      .then((result) => {
                        setInvitationToken(
                          `${window.location.origin}/invitation#token=${encodeURIComponent(result.invitationToken)}`,
                        );
                        return loadMembers();
                      })
                      .catch((error) => demo.notify(error))
                  }
                >
                  重新邀请
                </Button>
              )}
              <Button
                adminOnly
                aria-label={`移除 ${member.name}`}
                variant="ghost"
                disabled={!canAdmin || member.role === "owner"}
                onClick={() =>
                  window.confirm(`确认移除成员“${member.email}”吗？`) &&
                  void api
                    .removeMember(member.id)
                    .then(() => loadMembers())
                    .catch((error) => demo.notify(error))
                }
              >
                <Trash2 className="size-4" />
              </Button>
            </div>
          ))}
        </div>
      </Card>
      <Card className="p-5">
        <div className="flex items-start gap-3">
          <ShieldCheck className="mt-0.5 size-5 text-blue-600" />
          <div>
            <h2 className="font-bold">成员与角色目录</h2>
            <p className="mt-1 text-sm text-[var(--muted-text)]">
              控制台使用账号密码和服务端会话登录；会话关联成员身份，角色决定可以查看或修改的页面和操作。
            </p>
          </div>
        </div>
      </Card>
      <Modal
        open={open}
        onOpenChange={setOpen}
        title="邀请团队成员"
        description="平台不发送邮件；创建后请手动复制一次性邀请链接（7 天有效）。"
      >
        <div className="grid gap-4">
          <Field label="电子邮箱">
            <input className={fieldClass} value={email} onChange={(event) => setEmail(event.target.value)} />
          </Field>
          <Field label="角色">
            <select className={fieldClass} value={inviteRole} onChange={(event) => setInviteRole(event.target.value)}>
              <option value="developer">开发者</option>
              <option value="admin">管理员</option>
              <option value="viewer">只读成员</option>
            </select>
          </Field>
          <Button
            adminOnly
            onClick={() => {
              return api
                .inviteMember({ email, role: inviteRole })
                .then((result) => {
                  setInvitationToken(
                    `${window.location.origin}/invitation#token=${encodeURIComponent(result.invitationToken)}`,
                  );
                  return loadMembers();
                })
                .then(() => {
                  setOpen(false);
                  demo.notify(`邀请已创建，请复制邀请链接给 ${email}`);
                })
                .catch((error) => demo.notify(error));
            }}
          >
            创建邀请
          </Button>
        </div>
      </Modal>
    </div>
  );
}

export function AuditPage() {
  const demo = useDemo();
  const [query, setQuery] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const history = useResourcePages(
    "/api/audit-logs",
    { q: query, from: from ? new Date(from).toISOString() : "", to: to ? new Date(to).toISOString() : "" },
    demo.authenticated,
  );
  const events = history.items.map((item) => ({
    id: item.id,
    actor: item.actorLabel,
    action: item.action,
    resource: item.resourceLabel || item.resourceType,
    ip: item.ipAddress || "—",
    time: new Date(item.createdAt).toLocaleString("zh-CN"),
    requestId: item.requestId,
    before: item.beforeData,
    after: item.afterData,
  }));
  const [selected, setSelected] = useState<(typeof events)[number] | null>(null);
  const visible = events;
  return (
    <div className="grid gap-6">
      <PageHeader
        title="审计日志"
        description="记录控制面敏感变更的操作人、动作、目标、IP 和结构化详情。"
        actions={
          <Button
            variant="secondary"
            onClick={() => {
              const csv = [
                "操作人,动作,资源,IP,时间",
                ...visible.map((item) =>
                  [item.actor, item.action, item.resource, item.ip, item.time].map(csvCell).join(","),
                ),
              ].join("\n");
              const link = document.createElement("a");
              link.href = URL.createObjectURL(new Blob(["\ufeff" + csv], { type: "text/csv" }));
              link.download = "apihub-audit.csv";
              link.click();
              URL.revokeObjectURL(link.href);
            }}
          >
            <Download className="size-4" />
            导出已加载结果 CSV
          </Button>
        }
      />
      <PageControls
        page={history}
        onClear={() => {
          setQuery("");
          setFrom("");
          setTo("");
        }}
      />
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="开始时间">
          <input type="datetime-local" className={fieldClass} value={from} onChange={(e) => setFrom(e.target.value)} />
        </Field>
        <Field label="结束时间">
          <input type="datetime-local" className={fieldClass} value={to} onChange={(e) => setTo(e.target.value)} />
        </Field>
      </div>
      <div className="relative max-w-xl">
        <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--muted-text)]" />
        <input
          className={`${fieldClass} pl-10`}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="搜索操作人、动作或资源"
        />
      </div>
      <div className="grid gap-3 md:hidden">
        {visible.map((event) => (
          <button key={event.id} className="text-left" onClick={() => setSelected(event)}>
            <Card className="p-4">
              <p translate="no" className="font-semibold break-all">
                {event.action}
              </p>
              <p translate="no" className="mt-2 text-sm break-all">
                {event.actor} · {event.resource}
              </p>
              <p className="mt-2 text-xs">{event.time}</p>
            </Card>
          </button>
        ))}
      </div>
      <Card className="hidden overflow-hidden md:block">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[800px] text-left text-sm">
            <thead className="bg-[var(--muted)] text-xs text-[var(--muted-text)]">
              <tr>
                <Th>操作人</Th>
                <Th>动作</Th>
                <Th>资源</Th>
                <Th>IP</Th>
                <Th>时间</Th>
                <Th />
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--border)]">
              {visible.map((event) => (
                <tr
                  key={event.id}
                  onClick={() => setSelected(event)}
                  className="cursor-pointer hover:bg-[var(--muted)]"
                >
                  <Td>
                    <div className="flex items-center gap-2">
                      <span className="grid size-8 place-items-center rounded-full bg-[var(--muted)]">
                        <Fingerprint className="size-4" />
                      </span>
                      {event.actor}
                    </div>
                  </Td>
                  <Td>
                    <code>{event.action}</code>
                  </Td>
                  <Td>{event.resource}</Td>
                  <Td>{event.ip}</Td>
                  <Td>{event.time}</Td>
                  <Td>
                    <button
                      type="button"
                      aria-label={`查看 ${event.action} 审计详情`}
                      className="grid size-8 place-items-center rounded-lg text-[var(--muted-text)] transition hover:bg-[var(--surface)] hover:text-[var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                      onClick={(clickEvent) => {
                        clickEvent.stopPropagation();
                        setSelected(event);
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
        onOpenChange={(open) => !open && setSelected(null)}
        title={selected?.action ?? "审计事件"}
        description={selected?.id ?? ""}
      >
        {selected && (
          <div className="grid gap-4">
            <KeyValues
              items={[
                ["操作人", selected.actor],
                ["资源", selected.resource],
                ["IP 地址", selected.ip],
                ["时间", selected.time],
                ["请求 ID", selected.requestId],
              ]}
            />
            <div>
              <p className="mb-2 text-sm font-bold">变更内容</p>
              <pre className="overflow-auto rounded-xl bg-slate-950 p-4 text-xs text-slate-200">
                {JSON.stringify({ before: selected.before ?? null, after: selected.after ?? null }, null, 2)}
              </pre>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}

export function ResourcesPage() {
  const [tab, setTab] = useState("运行时 API");
  const snippets: Record<string, string> = {
    "运行时 API": [
      'curl "$APIHUB_URL/v1/actions/$ACTION_KEY"',
      '  -H "Authorization: Bearer $APIHUB_TOKEN"',
      '  -H "Content-Type: application/json"',
      "  -d '{\"input\":{}}'",
    ].join(" \\\n"),
    "管理 API": [
      'curl -c "$APIHUB_COOKIE_JAR" -X POST "$APIHUB_URL/api/auth/login"',
      '  -H "Content-Type: application/json"',
      '  -d \'{"email":"admin@localhost","password":"..."}\'',
      'curl -b "$APIHUB_COOKIE_JAR" "$APIHUB_URL/api/operations"',
    ]
      .map((line, index) => (index === 3 ? "\n\n" + line : line + (index < 2 ? " \\\n" : "")))
      .join(""),
    认证说明: [
      "# /v1 使用运行时 Token，只能访问策略允许的操作和连接",
      "Authorization: Bearer $APIHUB_TOKEN",
      "",
      "# /api 使用账号密码登录后的 HttpOnly 会话 Cookie",
      "Cookie: apihub_session=<server-managed>",
    ].join("\n"),
  };
  return (
    <div className="grid gap-6">
      <PageHeader
        title="开发者文档"
        description="只展示当前 Go 后端已经提供的 REST API 和认证方式。"
        actions={<Badge tone="success">REST API 可用</Badge>}
      />
      <section className="grid gap-4 lg:grid-cols-3">
        <Card className="min-w-0 overflow-hidden p-5">
          <Code2 className="size-10 rounded-xl bg-blue-50 p-2 text-blue-600 dark:bg-blue-950" />
          <h2 className="mt-4 font-bold">运行时 API</h2>
          <p className="mt-1 text-sm text-[var(--muted-text)]">供脚本和业务服务调用，受运行时 Token 权限约束。</p>
          <div className="mt-4 grid gap-2 text-xs">
            {["GET /v1/providers", "GET /v1/actions", "POST /v1/actions/{actionKey}"].map((route) => (
              <code key={route} className="break-all rounded-lg bg-[var(--muted)] px-3 py-2">
                {route}
              </code>
            ))}
          </div>
        </Card>
        <Card className="min-w-0 overflow-hidden p-5">
          <TerminalSquare className="size-10 rounded-xl bg-violet-50 p-2 text-violet-600 dark:bg-violet-950" />
          <h2 className="mt-4 font-bold">管理 API</h2>
          <p className="mt-1 text-sm text-[var(--muted-text)]">供当前管理台配置系统、连接、令牌并查询运行记录。</p>
          <div className="mt-4 grid gap-2 text-xs">
            {["GET /api/systems", "GET /api/integrations", "GET /api/connections", "GET /api/operations"].map(
              (route) => (
                <code key={route} className="break-all rounded-lg bg-[var(--muted)] px-3 py-2">
                  {route}
                </code>
              ),
            )}
          </div>
        </Card>
        <Card className="min-w-0 overflow-hidden p-5">
          <KeyRound className="size-10 rounded-xl bg-emerald-50 p-2 text-emerald-600 dark:bg-emerald-950" />
          <h2 className="mt-4 font-bold">两类访问凭据</h2>
          <p className="mt-1 text-sm text-[var(--muted-text)]">
            管理端使用账号密码和服务端会话；运行时 Token 仅用于 /v1，并执行操作级权限判断。
          </p>
          <div className="mt-4 rounded-xl bg-[var(--muted)] p-3 text-xs leading-5 text-[var(--muted-text)]">
            运行时 API 使用 Authorization: Bearer；管理 API 使用 HttpOnly Cookie，文档不会展示真实凭据。
          </div>
        </Card>
      </section>
      <Card className="min-w-0 overflow-hidden p-5">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <Tabs value={tab} onChange={setTab} items={["运行时 API", "管理 API", "认证说明"]} />
          <CopyButton value={snippets[tab]} />
        </div>
        <pre className="mt-5 max-h-96 max-w-full overflow-auto rounded-xl bg-slate-950 p-5 text-xs leading-6 text-blue-100">
          {snippets[tab]}
        </pre>
      </Card>
      <p className="text-xs text-[var(--muted-text)]">
        MCP、OpenAPI 文档、SDK 和 CLI 尚未实现，因此本页暂不提供对应入口。
      </p>
    </div>
  );
}

function MiniStat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="rounded-xl bg-[var(--muted)] p-3">
      <p className="text-xs text-[var(--muted-text)]">{label}</p>
      <p className="mt-1 font-bold">{value}</p>
    </div>
  );
}
function KeyValues({ items }: { items: Array<[string, ReactNode]> }) {
  return (
    <div className="divide-y divide-[var(--border)] rounded-xl border border-[var(--border)]">
      {items.map(([label, value]) => (
        <div key={label} className="grid grid-cols-[7rem_1fr] gap-3 p-3 text-sm">
          <span className="text-[var(--muted-text)]">{label}</span>
          <span className="font-medium">{value}</span>
        </div>
      ))}
    </div>
  );
}
function Th({ children }: { children?: ReactNode }) {
  return <th className="whitespace-nowrap px-4 py-3 font-semibold">{children}</th>;
}
function Td({ children }: { children: ReactNode }) {
  return <td className="px-4 py-3.5">{children}</td>;
}

function csvCell(value: string): string {
  return `"${value.replaceAll('"', '""')}"`;
}
