import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { api } from "./api";
import { PermissionContext } from "./permissions";
import { DemoProvider, useDemo } from "./demo";
import { AuthMethodsPage } from "./pages/demo-core";
import { ResourcesPage } from "./pages/demo-platform";

afterEach(() => {
  cleanup();
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

function json(value: unknown, status = 200) {
  return Promise.resolve(
    new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } }),
  );
}

function mockBackend() {
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL) => {
      const path = String(input);
      if (path === "/api/auth/session")
        return json({ userId: "u1", email: "admin@localhost", displayName: "Administrator", role: "owner" });
      if (path === "/api/lookups")
        return json({
          systems: [
            {
              id: "s1",
              systemKey: "github",
              name: "GitHub",
              source: "imported",
              groupName: "开发工具",
              description: "代码平台",
              authTemplateIds: ["t1"],
              actionCount: 2,
              executableCount: 2,
              connectionCount: 0,
            },
          ],
          connections: [],
        });
      if (path === "/api/meta") return json({ name: "APIHub", version: "0.2.0" });
      if (path === "/api/system-groups") return json([{ id: "g1", name: "开发工具" }]);
      if (path === "/api/systems")
        return json([
          {
            id: "s1",
            systemKey: "github",
            name: "GitHub",
            source: "imported",
            groupName: "开发工具",
            description: "代码平台",
            authTemplateIds: ["t1"],
            actionCount: 2,
            executableCount: 2,
            connectionCount: 0,
          },
        ]);
      if (path === "/api/auth-templates")
        return json([
          {
            id: "t1",
            templateKey: "api_key",
            name: "API 密钥",
            source: "builtin",
            flowType: "static",
            status: "published",
            credentialSchema: {},
            tokenRequest: {},
            injectionRules: [],
          },
        ]);
      if (
        path === "/api/auth-instances" ||
        path === "/api/integrations" ||
        path === "/api/connections" ||
        path === "/api/runtime-tokens" ||
        path === "/api/operations"
      )
        return json([]);
      return json({ message: "not found" }, 404);
    }),
  );
}

function Harness() {
  const state = useDemo();
  return (
    <div>
      <span>{state.customSystems.length} systems</span>
      <span>{state.systemGroups.join(",")}</span>
      <span>{state.authenticated ? "connected" : "signed-out"}</span>
    </div>
  );
}

describe("backend-backed console state", () => {
  it("explains a missing development backend", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response("", { status: 502 }))),
    );
    await expect(api.meta()).rejects.toThrow("后端服务未启动或无法连接，请先启动 127.0.0.1:8080 的 APIHub 后端。");
  });

  it("loads systems and groups from the management API", async () => {
    mockBackend();
    render(
      <DemoProvider>
        <Harness />
      </DemoProvider>,
    );
    await waitFor(() => expect(screen.getByText("1 systems")).toBeInTheDocument());
    expect(screen.getByText("开发工具")).toBeInTheDocument();
    expect(screen.getByText("connected")).toBeInTheDocument();
  });

  it("shows built-in and enterprise authentication methods together", () => {
    render(
      <MemoryRouter initialEntries={["/auth"]}>
        <DemoProvider>
          <AuthMethodsPage />
        </DemoProvider>
      </MemoryRouter>,
    );
    expect(screen.getByRole("tab", { name: "认证模板" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "企业认证" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /OAuth 2.0 授权码/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /用户名密码换 Token/ }));
    expect(screen.getByRole("button", { name: "使用此模板添加实例" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "预览登录流程" })).not.toBeInTheDocument();
  });

  it("manages accounts inside the authentication center and switches tabs on the same route", async () => {
    mockBackend();
    render(
      <PermissionContext.Provider value="owner">
        <MemoryRouter initialEntries={["/auth?section=accounts"]}>
          <DemoProvider>
            <AuthMethodsPage />
          </DemoProvider>
        </MemoryRouter>
      </PermissionContext.Provider>,
    );
    await waitFor(() => expect(screen.getByRole("tab", { name: "账号" })).toHaveAttribute("aria-selected", "true"));
    expect(screen.getAllByRole("heading", { name: "认证中心" })).toHaveLength(1);
    expect(screen.queryByRole("heading", { name: "账号" })).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "集成构建流程" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "认证模板" }));
    expect(await screen.findByRole("tab", { name: "内置模板" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "账号" }));
    expect(await screen.findByRole("textbox", { name: "搜索账号" })).toBeInTheDocument();
  });

  it("waits for backend capabilities before labeling auth methods", async () => {
    mockBackend();
    const base = vi.mocked(fetch).getMockImplementation()!;
    let resolveMeta!: () => void;
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (String(input) === "/api/meta")
        return new Promise<Response>((resolve) => {
          resolveMeta = () => {
            resolve(
              new Response(JSON.stringify({ name: "APIHub", supportedAuthFlows: ["oauth2_code", "static"] }), {
                headers: { "Content-Type": "application/json" },
              }),
            );
          };
        });
      return base(input, init);
    });
    render(
      <MemoryRouter initialEntries={["/auth"]}>
        <DemoProvider>
          <AuthMethodsPage />
        </DemoProvider>
      </MemoryRouter>,
    );
    await waitFor(() => expect(resolveMeta).toBeTypeOf("function"));
    expect(screen.getAllByText("正在确认…").length).toBeGreaterThan(0);
    expect(screen.queryByText("当前服务未启用")).not.toBeInTheDocument();
    resolveMeta();
    await waitFor(() => expect(screen.getAllByText("可直接使用")).toHaveLength(3));
    expect(screen.queryByText("正在确认…")).not.toBeInTheDocument();
  });

  it("saves an authentication instance without an environment, account or upstream call", async () => {
    mockBackend();
    const base = vi.mocked(fetch).getMockImplementation()!;
    const calls: { path: string; method: string; body?: Record<string, unknown> }[] = [];
    vi.mocked(fetch).mockImplementation((input, init) => {
      const path = String(input).split("?")[0];
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, method, body });
      if (path === "/api/auth-templates")
        return json([
          {
            id: "t1",
            templateKey: "username-password-token",
            name: "User token",
            flowType: "password_token",
            source: "builtin",
            status: "published",
            credentialSchema: {},
            tokenRequest: {},
            injectionRules: [],
          },
        ]);
      if (path === "/api/meta") return json({ name: "APIHub", supportedAuthFlows: ["password_token"] });
      if (path === "/api/auth-instances" && method === "POST")
        return json({
          id: "auth-1",
          instanceKey: body?.instanceKey,
          name: body?.name,
          systemKey: "github",
          authTemplateKey: "username-password-token",
          status: "ready",
          version: 1,
        });
      return base(input, init);
    });
    render(
      <PermissionContext.Provider value="owner">
        <MemoryRouter initialEntries={["/auth?section=instances&system=github"]}>
          <DemoProvider>
            <AuthMethodsPage />
          </DemoProvider>
        </MemoryRouter>
      </PermissionContext.Provider>,
    );
    await waitFor(() => expect(screen.getByRole("button", { name: "添加认证实例" })).not.toBeDisabled());
    fireEvent.click(screen.getByRole("button", { name: "添加认证实例" }));
    fireEvent.change(screen.getByLabelText(/认证 \/ Token 地址/), {
      target: { value: "https://api.example.com/token" },
    });
    expect(screen.queryByLabelText(/API 基础地址/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/账号名称/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "保存认证实例" }));
    await waitFor(() =>
      expect(calls.filter((call) => call.path === "/api/auth-instances" && call.method === "POST")).toHaveLength(1),
    );
    const save = calls.find((call) => call.path === "/api/auth-instances" && call.method === "POST");
    expect(save?.body).toMatchObject({ systemId: "s1", authTemplateId: "t1" });
    expect(
      calls.some(
        (call) =>
          call.method !== "GET" &&
          (call.path === "/api/integrations" ||
            call.path === "/api/connections" ||
            call.path.endsWith("/verify") ||
            call.path.includes("/check")),
      ),
    ).toBe(false);
  });
});
