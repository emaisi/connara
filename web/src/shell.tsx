import {
  Activity,
  Blocks,
  Cable,
  ChevronRight,
  CircleGauge,
  ClipboardCheck,
  ExternalLink,
  KeyRound,
  LifeBuoy,
  LogOut,
  LoaderCircle,
  Menu,
  Moon,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  RefreshCw,
  SearchCode,
  Settings2,
  ShieldCheck,
  Sparkles,
  Sun,
  Users,
  Webhook,
  Workflow,
  X,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import { useDemo } from "./demo";
import { useLanguage } from "./i18n";
import { Badge, Button, Card, cn } from "./ui";

const navigation = [
  { path: "/", label: "概览", icon: CircleGauge },
  { path: "/providers", label: "系统", icon: Cable },
  { path: "/actions", label: "API", icon: SearchCode },
  { path: "/integrations", label: "集成", icon: Blocks },
  { path: "/auth", label: "认证中心", icon: KeyRound },
  { path: "/operations", label: "运行记录", icon: Activity },
  {
    id: "advanced",
    label: "更多功能",
    icon: Settings2,
    items: [
      { path: "/sync", label: "同步任务", icon: RefreshCw },
      { path: "/workflows", label: "工作流", icon: Workflow },
      { path: "/webhooks", label: "Webhook", icon: Webhook },
      { path: "/access", label: "访问控制", icon: ShieldCheck },
      { path: "/audit", label: "审计日志", icon: ClipboardCheck },
      { path: "/team", label: "团队管理", icon: Users },
      { path: "/settings", label: "平台设置", icon: Settings2 },
    ],
  },
] as const;

const helpNavigation = [
  { path: "/getting-started", label: "快速开始", icon: Sparkles },
  { path: "/resources", label: "开发者文档", icon: LifeBuoy },
] as const;

const pageTitles: Record<string, string> = Object.fromEntries([
  ...navigation.flatMap<string[]>((entry) =>
    "items" in entry ? entry.items.map((item) => [item.path, item.label]) : [[entry.path, entry.label]],
  ),
  ...helpNavigation.map((item) => [item.path, item.label]),
]);

const roleLabels: Record<string, string> = {
  owner: "所有者",
  admin: "管理员",
  developer: "开发者",
  viewer: "只读成员",
};

// 本地开发预填的登录密码，需与服务器上的 .admin-password 保持一致；对外部署前删除此预填。
const devPrefillPassword = "31dd4998f3e46048f70e9dcc01a31031";

export function Shell() {
  const location = useLocation();
  const demo = useDemo();
  const activeGroup = navigation.find(
    (entry) => "items" in entry && entry.items.some((item) => item.path === location.pathname),
  );
  const activeGroupId = activeGroup && "id" in activeGroup ? activeGroup.id : null;
  const [expandedGroup, setExpandedGroup] = useState<string | null>(activeGroupId);
  useEffect(() => {
    setExpandedGroup(activeGroupId);
  }, [location.pathname, activeGroupId]);
  const { language, setLanguage, t } = useLanguage();
  const [email, setEmail] = useState("admin@localhost");
  const [password, setPassword] = useState(devPrefillPassword);
  const [signingIn, setSigningIn] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [desktop, setDesktop] = useState(() => matchMedia("(min-width: 1024px)").matches);
  const collapsed = desktop && sidebarCollapsed;
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const accountMenuRef = useRef<HTMLDivElement>(null);
  const wasMobileOpen = useRef(false);
  const [dark, setDark] = useState(
    () =>
      localStorage.getItem("apihub.theme") === "dark" ||
      (!localStorage.getItem("apihub.theme") && matchMedia("(prefers-color-scheme: dark)").matches),
  );

  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
    localStorage.setItem("apihub.theme", dark ? "dark" : "light");
  }, [dark]);
  useEffect(() => {
    setMobileOpen(false);
    setAccountOpen(false);
  }, [location.pathname]);
  useEffect(() => {
    if (!accountOpen) return;
    const closeOnOutsideClick = (event: PointerEvent) => {
      if (!accountMenuRef.current?.contains(event.target as Node)) setAccountOpen(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setAccountOpen(false);
    };
    document.addEventListener("pointerdown", closeOnOutsideClick);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOnOutsideClick);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [accountOpen]);
  useEffect(() => {
    const media = matchMedia("(min-width: 1024px)");
    const update = () => {
      setDesktop(media.matches);
      if (media.matches) setMobileOpen(false);
    };
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  useEffect(() => {
    if (mobileOpen) closeButtonRef.current?.focus();
    const trap = (event: KeyboardEvent) => {
      if (!mobileOpen || desktop) return;
      if (event.key === "Escape") {
        event.preventDefault();
        setMobileOpen(false);
        return;
      }
      if (event.key !== "Tab") return;
      const dialog = closeButtonRef.current?.closest('[role="dialog"]');
      const items = Array.from(
        dialog?.querySelectorAll<HTMLElement>(
          'a[href],button:not(:disabled),select:not(:disabled),input:not(:disabled),[tabindex="0"]',
        ) ?? [],
      ).filter((item) => item.getClientRects().length);
      const first = items[0],
        last = items[items.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first?.focus();
      }
    };
    document.addEventListener("keydown", trap);
    if (!mobileOpen && wasMobileOpen.current) menuButtonRef.current?.focus();
    wasMobileOpen.current = mobileOpen;
    document.body.style.overflow = !desktop && mobileOpen ? "hidden" : "";
    return () => {
      document.body.style.overflow = "";
      document.removeEventListener("keydown", trap);
    };
  }, [desktop, mobileOpen]);

  const currentTitle = t(pageTitles[location.pathname] ?? demo.platformName);
  const currentSection = t(activeGroup?.label ?? "工作区");

  if (!demo.authenticated && demo.loading) {
    return (
      <main className="grid min-h-svh place-items-center bg-[var(--background)] text-[var(--text)]">
        <p role="status" className="flex items-center gap-2 text-sm text-[var(--muted-text)]">
          <LoaderCircle aria-hidden="true" className="size-4 animate-spin" />
          {t("正在恢复登录状态…")}
        </p>
      </main>
    );
  }

  if (!demo.authenticated) {
    return (
      <main className="grid min-h-svh place-items-center bg-[var(--background)] p-5 text-[var(--text)]">
        <Card className="w-full max-w-md p-6 sm:p-8">
          <div className="flex items-start justify-between">
            <span className="grid size-12 place-items-center rounded-2xl bg-gradient-to-br from-blue-600 to-cyan-500 text-white shadow-lg shadow-blue-500/20">
              <Network className="size-6" />
            </span>
            <Button
              type="button"
              variant="secondary"
              className="h-9 px-3 text-xs"
              aria-label="切换语言"
              onClick={() => setLanguage(language === "zh-CN" ? "en" : "zh-CN")}
            >
              {language === "zh-CN" ? "EN" : "中文"}
            </Button>
          </div>
          <h1 className="mt-5 text-2xl font-extrabold">登录 {demo.platformName} 控制台</h1>
          <p className="mt-2 text-sm leading-6 text-[var(--muted-text)]">
            使用平台账号和密码登录，平台会安全维护登录会话。
          </p>
          <form
            className="mt-6 grid gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              setSigningIn(true);
              void demo.login(email, password).finally(() => setSigningIn(false));
            }}
          >
            <label className="grid gap-2 text-sm font-semibold">
              邮箱账号
              <input
                className="h-11 rounded-xl border border-[var(--border)] bg-[var(--surface)] px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20"
                type="email"
                autoComplete="username"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                placeholder="admin@localhost"
                autoFocus
                required
              />
            </label>
            <label className="grid gap-2 text-sm font-semibold">
              密码
              <input
                className="h-11 rounded-xl border border-[var(--border)] bg-[var(--surface)] px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder="请输入登录密码"
                required
              />
            </label>
            {demo.backendError && (
              <p
                role="alert"
                className="rounded-xl bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
              >
                {demo.backendError}
              </p>
            )}
            <Button type="submit" loading={signingIn} disabled={signingIn || !email.trim() || !password}>
              {signingIn ? "正在登录…" : "登录控制台"}
            </Button>
          </form>
        </Card>
      </main>
    );
  }

  return (
    <div className="min-h-svh bg-[var(--background)] text-[var(--text)]">
      {mobileOpen && (
        <button
          aria-label="关闭导航遮罩"
          aria-hidden="true"
          tabIndex={-1}
          className="fixed inset-0 z-30 bg-slate-950/45 lg:hidden"
          onClick={() => setMobileOpen(false)}
        />
      )}
      <aside
        id="main-sidebar"
        aria-hidden={!desktop && !mobileOpen}
        aria-label="主导航"
        aria-modal={!desktop && mobileOpen ? true : undefined}
        inert={!desktop && !mobileOpen}
        role={!desktop ? "dialog" : undefined}
        className={cn(
          "fixed inset-y-0 left-0 z-40 flex flex-col border-r border-[var(--border)] bg-[var(--sidebar)] transition-transform lg:translate-x-0",
          collapsed ? "w-20" : "w-64",
          mobileOpen ? "translate-x-0" : "-translate-x-full",
        )}
      >
        <div
          className={cn(
            "flex h-16 shrink-0 items-center border-b border-[var(--border)]",
            collapsed ? "justify-center" : "justify-between px-3",
          )}
        >
          <div className={cn("flex min-w-0 items-center gap-3", collapsed && "hidden")}>
            <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-gradient-to-br from-blue-600 to-cyan-500 text-white shadow-md shadow-blue-500/20">
              <Network className="size-5" />
            </span>
            <div className="min-w-0">
              <div className="truncate font-extrabold tracking-tight">{demo.platformName}</div>
              <div className="text-[10px] font-bold uppercase tracking-[0.16em] text-[var(--muted-text)]">
                集成运行平台
              </div>
            </div>
          </div>
          <Button
            variant="ghost"
            className="hidden size-8 shrink-0 p-0 lg:inline-flex"
            aria-label={t(collapsed ? "展开侧边栏" : "折叠侧边栏")}
            title={t(collapsed ? "展开侧边栏" : "折叠侧边栏")}
            aria-expanded={!collapsed}
            aria-controls="main-sidebar"
            onClick={() => setSidebarCollapsed((value) => !value)}
          >
            {collapsed ? <PanelLeftOpen className="size-4" /> : <PanelLeftClose className="size-4" />}
          </Button>
          <Button
            ref={closeButtonRef}
            variant="ghost"
            className="size-8 p-0 lg:hidden"
            onClick={() => setMobileOpen(false)}
            aria-label="关闭导航"
          >
            <X className="size-4" />
          </Button>
        </div>
        <nav aria-label="功能导航" className="flex-1 overflow-y-auto p-3">
          <div className="grid gap-1">
            {navigation.map((entry) =>
              "items" in entry ? (
                <div key={entry.id}>
                  <button
                    type="button"
                    title={collapsed ? t(entry.label) : undefined}
                    aria-expanded={expandedGroup === entry.id}
                    aria-controls={`navigation-${entry.id}`}
                    onClick={() => setExpandedGroup((current) => (current === entry.id ? null : entry.id))}
                    className={cn(
                      "flex min-h-11 w-full items-center gap-3 rounded-xl px-3 py-2.5 text-left text-[13px] font-semibold transition hover:bg-[var(--muted)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500",
                      activeGroupId === entry.id
                        ? "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300"
                        : "text-[var(--muted-text)] hover:text-[var(--text)]",
                    )}
                  >
                    <entry.icon className="size-[18px] shrink-0" />
                    <span className={collapsed ? "sr-only" : "truncate"}>{t(entry.label)}</span>
                    <ChevronRight
                      aria-hidden="true"
                      className={cn(
                        "ml-auto size-4 shrink-0 transition-transform",
                        collapsed && "hidden",
                        expandedGroup === entry.id && "rotate-90",
                      )}
                    />
                  </button>
                  <div id={`navigation-${entry.id}`} hidden={expandedGroup !== entry.id}>
                    <div className={cn("my-1 grid gap-1", !collapsed && "ml-5 border-l border-[var(--border)] pl-3")}>
                      {entry.items.map((item) => (
                        <NavLink
                          key={item.path}
                          to={item.path}
                          title={collapsed ? t(item.label) : undefined}
                          onClick={() => setMobileOpen(false)}
                          className={({ isActive }) =>
                            cn(
                              "flex min-h-10 items-center rounded-lg px-3 py-2 text-[13px] transition hover:bg-[var(--muted)] focus-visible:outline-2 focus-visible:outline-blue-500",
                              isActive
                                ? "bg-blue-50 font-semibold text-blue-700 dark:bg-blue-950 dark:text-blue-300"
                                : "text-[var(--muted-text)] hover:text-[var(--text)]",
                            )
                          }
                        >
                          {collapsed && <item.icon className="size-[18px] shrink-0" />}
                          <span className={collapsed ? "sr-only" : undefined}>{t(item.label)}</span>
                        </NavLink>
                      ))}
                    </div>
                  </div>
                </div>
              ) : (
                <NavLink
                  key={entry.path}
                  to={entry.path}
                  title={collapsed ? t(entry.label) : undefined}
                  end={entry.path === "/"}
                  onClick={() => setMobileOpen(false)}
                  className={({ isActive }) =>
                    cn(
                      "flex min-h-11 items-center gap-3 rounded-xl px-3 py-2.5 text-[13px] font-semibold transition hover:bg-[var(--muted)] focus-visible:outline-2 focus-visible:outline-blue-500",
                      isActive
                        ? "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300"
                        : "text-[var(--muted-text)] hover:text-[var(--text)]",
                    )
                  }
                >
                  <entry.icon className="size-[18px] shrink-0" />
                  <span className={collapsed ? "sr-only" : "truncate"}>{t(entry.label)}</span>
                </NavLink>
              ),
            )}
          </div>
        </nav>
        <nav aria-label="帮助导航" className="shrink-0 border-t border-[var(--border)] px-3 py-2">
          {helpNavigation.map((item) => (
            <NavLink
              key={item.path}
              to={item.path}
              title={collapsed ? t(item.label) : undefined}
              onClick={() => setMobileOpen(false)}
              className={({ isActive }) =>
                cn(
                  "flex min-h-10 items-center gap-3 rounded-xl px-3 py-2 text-xs transition hover:bg-[var(--muted)] focus-visible:outline-2 focus-visible:outline-blue-500",
                  isActive
                    ? "bg-blue-50 font-semibold text-blue-700 dark:bg-blue-950 dark:text-blue-300"
                    : "text-[var(--muted-text)] hover:text-[var(--text)]",
                )
              }
            >
              <item.icon className="size-4 shrink-0" />
              <span className={collapsed ? "sr-only" : undefined}>{t(item.label)}</span>
            </NavLink>
          ))}
          <a
            href="https://github.com/emaisi/connara"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="GitHub 仓库（在新标签页打开）"
            title="GitHub 仓库（在新标签页打开）"
            onClick={() => setMobileOpen(false)}
            className="flex min-h-10 items-center gap-3 rounded-xl px-3 py-2 text-xs text-[var(--muted-text)] transition hover:bg-[var(--muted)] hover:text-[var(--text)] focus-visible:outline-2 focus-visible:outline-blue-500"
          >
            <ExternalLink aria-hidden="true" className="size-4 shrink-0" />
            <span className={collapsed ? "sr-only" : undefined}>GitHub</span>
          </a>
        </nav>
        <div ref={accountMenuRef} className="relative border-t border-[var(--border)] p-3">
          {accountOpen && (
            <div
              role="menu"
              aria-label="用户菜单"
              className={cn(
                "absolute bottom-full left-3 mb-2 overflow-hidden rounded-2xl border border-[var(--border)] bg-[var(--surface)] p-2 shadow-xl",
                collapsed ? "w-64" : "right-3",
              )}
            >
              <div className="border-b border-[var(--border)] px-3 py-2.5">
                <p className="text-[10px] font-bold uppercase tracking-[0.12em] text-[var(--muted-text)]">当前账号</p>
                <p className="mt-1 truncate text-sm font-bold">{demo.user?.displayName || t("管理员")}</p>
                <p className="truncate text-xs text-[var(--muted-text)]">{demo.user?.email}</p>
                <Badge tone="info">{t(roleLabels[demo.user?.role ?? ""] ?? demo.user?.role ?? "成员")}</Badge>
              </div>
              <NavLink
                to="/team"
                role="menuitem"
                className="mt-1 flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-sm font-medium transition hover:bg-[var(--muted)]"
              >
                <Users className="size-4 text-[var(--muted-text)]" />
                团队与权限
              </NavLink>
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setAccountOpen(false);
                  demo.logout();
                }}
                className="flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-left text-sm font-medium text-red-600 transition hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950"
              >
                <LogOut className="size-4" />
                退出登录
              </button>
            </div>
          )}
          <button
            type="button"
            onClick={() => setAccountOpen((value) => !value)}
            aria-haspopup="menu"
            aria-expanded={accountOpen}
            aria-label="打开用户菜单"
            className="flex w-full items-center gap-3 rounded-xl p-2.5 text-left transition hover:bg-[var(--muted)]"
          >
            <span className="grid size-9 shrink-0 place-items-center rounded-full bg-gradient-to-br from-blue-500 to-violet-500 text-xs font-bold text-white">
              {(demo.user?.displayName || demo.user?.email || "A").slice(0, 1).toUpperCase()}
            </span>
            <div className={cn("min-w-0 flex-1", collapsed && "hidden")}>
              <p className="truncate text-xs font-bold">{demo.user?.displayName || t("管理员")}</p>
              <p className="truncate text-[10px] text-[var(--muted-text)]">{demo.user?.email}</p>
            </div>
            <ChevronRight
              className={cn(
                "size-4 text-[var(--muted-text)] transition-transform",
                collapsed && "hidden",
                accountOpen && "-rotate-90",
              )}
            />
          </button>
        </div>
      </aside>

      <div className={collapsed ? "lg:pl-20" : "lg:pl-64"} inert={!desktop && mobileOpen}>
        <header className="sticky top-0 z-20 flex h-16 items-center border-b border-[var(--border)] bg-[color-mix(in_srgb,var(--background)_88%,transparent)] px-4 backdrop-blur-xl sm:px-7">
          <Button
            ref={menuButtonRef}
            variant="ghost"
            className="mr-2 size-9 p-0 lg:hidden"
            onClick={() => setMobileOpen(true)}
            aria-label="打开导航"
          >
            <Menu className="size-5" />
          </Button>
          <div className="flex min-w-0 items-center gap-2 text-sm">
            <span className="hidden text-[var(--muted-text)] lg:inline">{currentSection}</span>
            <ChevronRight className="hidden size-3.5 text-[var(--muted-text)] lg:block" />
            <span className="truncate font-bold">{currentTitle}</span>
          </div>
          <Badge tone={demo.backendError ? "danger" : "success"}>
            {t(demo.loading ? "同步中" : demo.backendError ? "连接异常" : "后端已连接")}
          </Badge>
          <div className="ml-auto flex items-center gap-1">
            <Button
              variant="ghost"
              className="h-9 px-2 text-xs"
              aria-label="切换语言"
              title="切换语言"
              onClick={() => setLanguage(language === "zh-CN" ? "en" : "zh-CN")}
            >
              {language === "zh-CN" ? "EN" : "中文"}
            </Button>
            <Button variant="ghost" className="hidden sm:inline-flex" title="重新加载后端数据" onClick={demo.reset}>
              <RefreshCw className="size-4" />
              刷新数据
            </Button>
            <Button
              variant="ghost"
              className="size-9 p-0"
              title="切换主题"
              aria-label="切换主题"
              onClick={() => setDark((value) => !value)}
            >
              {dark ? <Sun className="size-4" /> : <Moon className="size-4" />}
            </Button>
          </div>
        </header>
        <main
          className={
            /^\/workflows\/[^/]+$/.test(location.pathname)
              ? "w-full p-3"
              : "mx-auto w-full max-w-[1440px] p-4 sm:p-7 lg:p-8"
          }
        >
          {demo.user?.role === "viewer" && (
            <p role="status" className="mb-4 text-sm text-amber-700">
              当前为只读角色，可以浏览数据；保存、运行和管理操作需要更高权限。
            </p>
          )}
          {demo.user?.role === "developer" && (
            <p className="mb-4 text-sm text-[var(--muted-text)]">
              开发者可以管理集成和运行任务；令牌、团队和平台设置由管理员管理。
            </p>
          )}
          <Outlet />
        </main>
      </div>
      {demo.notice && (
        <div
          role={demo.noticeError ? "alert" : "status"}
          className={`fixed bottom-5 right-5 z-[60] flex max-w-[calc(100vw-2rem)] items-center gap-3 rounded-xl border px-4 py-3 text-sm font-semibold shadow-xl ${demo.noticeError ? "border-red-300 bg-red-50 text-red-800 dark:bg-red-950 dark:text-red-200" : "border-blue-300 bg-blue-50 text-blue-800 dark:bg-blue-950 dark:text-blue-200"}`}
        >
          <span aria-hidden="true">{demo.noticeError ? "!" : "✓"}</span>
          {t(demo.notice)}
          <button type="button" onClick={demo.dismissNotice} aria-label="关闭提示">
            ×
          </button>
        </div>
      )}
    </div>
  );
}
