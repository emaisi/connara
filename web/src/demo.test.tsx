import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { api } from "./api";
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

  it("keeps enterprise authentication templates visible before login", () => {
    render(
      <MemoryRouter initialEntries={["/auth"]}>
        <DemoProvider>
          <AuthMethodsPage />
        </DemoProvider>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("tab", { name: "企业认证" }));
    fireEvent.click(screen.getByRole("button", { name: /用户名密码换 Token/ }));
    expect(screen.getByRole("button", { name: "使用此模板添加实例" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "预览登录流程" })).not.toBeInTheDocument();
  });

  it("documents only the supported REST boundary", () => {
    render(
      <MemoryRouter initialEntries={["/resources"]}>
        <DemoProvider>
          <ResourcesPage />
        </DemoProvider>
      </MemoryRouter>,
    );
    expect(screen.getByRole("heading", { name: "开发者文档" })).toBeInTheDocument();
    expect(screen.queryByText(/\/v1\/proxy/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "MCP" })).not.toBeInTheDocument();
  });
});

function OAuthHarness() {
  const { authInstances, toggleAuthInstance } = useDemo();
  return (
    <>
      <output data-testid="scopes">{JSON.stringify(authInstances[0]?.scopes)}</output>
      <button onClick={() => toggleAuthInstance("oauth1")}>切换认证状态</button>
    </>
  );
}
it("preserves OAuth scopes and authorization parameters across status updates", async () => {
  mockBackend();
  const base = vi.mocked(fetch).getMockImplementation()!;
  let saved: any;
  vi.mocked(fetch).mockImplementation((input, init) => {
    if (String(input) === "/api/auth-instances")
      return json([
        {
          id: "oauth1",
          version: 4,
          instanceKey: "oauth",
          name: "OAuth",
          authTemplateKey: "api_key",
          systemKey: "github",
          status: "ready",
          publicConfig: {
            scopes: ["read", "write"],
            authorizationParams: { prompt: "consent" },
            verificationPath: "/me",
          },
        },
      ]);
    if (String(input) === "/api/auth-instances/oauth1") {
      saved = JSON.parse(String(init?.body));
      return json({ id: "oauth1" });
    }
    return base(input, init);
  });
  render(
    <DemoProvider>
      <OAuthHarness />
    </DemoProvider>,
  );
  await waitFor(() => expect(screen.getByTestId("scopes").textContent).toBe('["read","write"]'));
  fireEvent.click(screen.getByText("切换认证状态"));
  await waitFor(() =>
    expect(saved?.publicConfig).toEqual({
      scopes: ["read", "write"],
      authorizationParams: { prompt: "consent" },
      verificationPath: "/me",
      authorizationUrl: "",
    }),
  );
  expect(saved.version).toBe(4);
});
it("keeps healthy resources visible when another resource fails", async () => {
  mockBackend();
  const base = vi.mocked(fetch).getMockImplementation()!;
  vi.mocked(fetch).mockImplementation((input, init) =>
    String(input) === "/api/operations" ? json({ message: "history unavailable" }, 503) : base(input, init),
  );
  render(
    <DemoProvider>
      <Harness />
    </DemoProvider>,
  );
  await waitFor(() => expect(screen.getByText("1 systems")).toBeInTheDocument(), { timeout: 3000 });
  expect(screen.getByText("connected")).toBeInTheDocument();
});
