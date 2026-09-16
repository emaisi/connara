import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { DemoProvider } from "./demo";
import { Shell } from "./shell";

afterEach(() => {
  cleanup();
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

describe("mobile navigation", () => {
  it("keeps the closed drawer out of the focus order and restores focus after closing", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        const value =
          path === "/api/auth/session"
            ? { userId: "u1", email: "admin@localhost", displayName: "Administrator", role: "owner" }
            : path === "/api/meta"
              ? { name: "APIHub", version: "0.2.0", providerCount: 0 }
              : [];
        return Promise.resolve(
          new Response(JSON.stringify(value), { status: 200, headers: { "Content-Type": "application/json" } }),
        );
      }),
    );
    const values = new Map<string, string>();
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
      },
    });
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: () => ({
        matches: false,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
      }),
    });

    render(
      <MemoryRouter>
        <DemoProvider>
          <Shell />
        </DemoProvider>
      </MemoryRouter>,
    );

    const open = await screen.findByRole("button", { name: "打开导航" });
    const drawer = document.querySelector("aside");
    expect(drawer).toHaveAttribute("inert");

    fireEvent.click(open);
    expect(drawer).not.toHaveAttribute("inert");
    const close = screen.getByRole("button", { name: "关闭导航" });
    expect(close).toHaveFocus();

    fireEvent.click(close);
    expect(drawer).toHaveAttribute("inert");
    expect(open).toHaveFocus();
  });

  it("uses email and password instead of an administrator token", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ message: "signed out" }), {
            status: 401,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      ),
    );
    render(
      <MemoryRouter>
        <DemoProvider>
          <Shell />
        </DemoProvider>
      </MemoryRouter>,
    );
    expect(await screen.findByLabelText("邮箱账号")).toHaveAttribute("autocomplete", "username");
    expect(screen.getByLabelText("密码")).toHaveAttribute("autocomplete", "current-password");
    expect(screen.queryByText("管理令牌")).not.toBeInTheDocument();
  });

  it("opens account details instead of signing out when the user card is clicked", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      const value =
        path === "/api/auth/session"
          ? { userId: "u1", email: "admin@localhost", displayName: "Administrator", role: "owner" }
          : path === "/api/meta"
            ? { name: "APIHub", version: "0.2.0", providerCount: 0 }
            : [];
      return Promise.resolve(
        new Response(JSON.stringify(value), { status: 200, headers: { "Content-Type": "application/json" } }),
      );
    });
    vi.stubGlobal("fetch", fetchMock);
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: () => ({
        matches: true,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
      }),
    });

    render(
      <MemoryRouter>
        <DemoProvider>
          <Shell />
        </DemoProvider>
      </MemoryRouter>,
    );

    fireEvent.click(await screen.findByRole("button", { name: "打开用户菜单" }));

    expect(screen.getByRole("menu", { name: "用户菜单" })).toBeInTheDocument();
    expect(screen.getByText("当前账号")).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "团队与权限" })).toHaveAttribute("href", "/team");
    expect(screen.getByRole("menuitem", { name: "退出登录" })).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([input]) => String(input) === "/api/auth/logout")).toBe(false);
  });
  it("opens the current group on deep links and keeps only one group expanded while navigating", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) =>
        Promise.resolve(
          new Response(
            JSON.stringify(
              String(input) === "/api/auth/session"
                ? { userId: "u1", email: "admin@localhost", displayName: "Administrator", role: "owner" }
                : [],
            ),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        ),
      ),
    );
    vi.stubGlobal("matchMedia", () => ({
      matches: true,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }));
    render(
      <MemoryRouter initialEntries={["/webhooks"]}>
        <DemoProvider>
          <Shell />
        </DemoProvider>
      </MemoryRouter>,
    );
    const nav = within(await screen.findByRole("navigation", { name: "功能导航" }));
    expect(nav.getByRole("button", { name: "自动化" })).toHaveAttribute("aria-expanded", "true");
    expect(nav.getByRole("link", { name: "Webhook" })).toHaveAttribute("aria-current", "page");
    expect(nav.queryByRole("link", { name: "认证中心" })).not.toBeInTheDocument();
    fireEvent.click(nav.getByRole("button", { name: "集成管理" }));
    expect(nav.getByRole("button", { name: "自动化" })).toHaveAttribute("aria-expanded", "false");
    expect(nav.queryByRole("link", { name: "Webhook" })).not.toBeInTheDocument();
    fireEvent.click(nav.getByRole("link", { name: "连接账号" }));
    expect(nav.getByRole("link", { name: "连接账号" })).toHaveAttribute("aria-current", "page");
    expect(nav.getByRole("button", { name: "集成管理" })).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(nav.getByRole("link", { name: "概览" }));
    expect(nav.getAllByRole("button").every((button) => button.getAttribute("aria-expanded") === "false")).toBe(true);
    expect(nav.getAllByRole("link")).toHaveLength(3);
    const help = within(screen.getByRole("navigation", { name: "帮助导航" }));
    expect(help.getByRole("link", { name: "快速开始" })).toHaveAttribute("href", "/getting-started");
    expect(help.getByRole("link", { name: "开发者文档" })).toHaveAttribute("href", "/resources");
  });
});
