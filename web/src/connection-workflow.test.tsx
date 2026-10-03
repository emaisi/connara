import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router";
import { PermissionContext } from "./permissions";
import { api, type AdminConnection } from "./api";
import type { DemoAuthInstance, DemoIntegration } from "./demo";
import {
  saveAccount,
  saveOrFindIntegration,
  oauthReturnPath,
  accountVerificationPath,
} from "./pages/connection-create";
import { DemoConnectionsPage } from "./pages/connections";

const state = vi.hoisted(() => ({
  integrations: [
    {
      id: "integration-1",
      name: "test",
      provider: "test",
      displayName: "Test",
      baseUrl: "https://test.example/api",
      authInstanceId: "auth-1",
      status: "ready",
    },
  ] as DemoIntegration[],
  customSystems: [{ id: "system-1", service: "test", name: "Test" }],
  authInstances: [
    { id: "auth-1", templateId: "no_auth", name: "No auth", status: "ready", systemIds: ["test"] },
  ] as DemoAuthInstance[],
  authSchemes: [],
  supportedAuthFlows: ["none"],
  connections: [] as { id: string; provider: string; integration: string; name?: string }[],
  accountRows: [] as any[],
  authenticated: true,
  loading: false,
  reload: vi.fn(() => Promise.resolve(true)),
  notify: vi.fn(),
  refetch: vi.fn(() => Promise.resolve()),
}));
const initialIntegrations = [...state.integrations];
vi.mock("./demo", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./demo")>()),
  useDemo: () => state,
}));
vi.mock("./pagination", () => ({
  useResourcePages: () => ({ items: state.accountRows, refetch: state.refetch }),
  PageControls: () => null,
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  state.integrations = [...initialIntegrations];
  state.notify.mockClear();
  state.connections = [];
  state.accountRows = [];
  state.authSchemes = [];
});

function LocationProbe() {
  const location = useLocation();
  return <output>{location.pathname + location.search}</output>;
}

it("saves an account without silently authenticating or calling the upstream", async () => {
  const save = vi.spyOn(api, "saveConnection").mockResolvedValue({ id: "saved-1", revision: 1 } as AdminConnection);
  const verify = vi.spyOn(api, "verifyConnection").mockRejectedValue(new Error("must not call"));
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter>
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "添加账号" }));
  fireEvent.click(await screen.findByRole("button", { name: "保存账号" }));
  await waitFor(() => expect(state.notify).toHaveBeenCalledWith("账号已保存，尚未验证凭据是否有效"));
  expect(save).toHaveBeenCalledTimes(1);
  expect(verify).not.toHaveBeenCalled();
});

it("returns from cancelled OAuth with the selected integration ready to retry", async () => {
  state.notify.mockClear();
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter
        initialEntries={[
          "/connections?integration=test&connectionKey=account-original&accountName=Original&endUserKey=tenant-original&oauth=error",
        ]}
      >
        <LocationProbe />
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  await waitFor(() =>
    expect(
      screen.getByText(
        "/auth?integration=test&connectionKey=account-original&accountName=Original&endUserKey=tenant-original&new=1&section=accounts",
      ),
    ).toBeInTheDocument(),
  );
  expect(state.notify).toHaveBeenCalledWith("上游授权未完成，可以返回此页重新授权");
  expect(screen.getByLabelText("账号名称")).toHaveValue("Original");
  expect(screen.getByLabelText(/外部用户或租户 ID/)).toHaveValue("tenant-original");
  expect(screen.getByText("账号 ID：account-original")).toBeInTheDocument();
});

it("creates an integration for the explicitly selected existing instance in the account form", async () => {
  state.integrations = [];
  const saveIntegration = vi
    .spyOn(api, "saveIntegration")
    .mockRejectedValueOnce(new Error("invalid API address"))
    .mockImplementation(async (input: any) => ({ ...input, id: "integration-new" }));
  vi.spyOn(api, "integrations").mockResolvedValue([]);
  const save = vi.spyOn(api, "saveConnection").mockResolvedValue({ id: "account-new", revision: 1 } as AdminConnection);
  vi.spyOn(api, "verifyConnection").mockResolvedValue({
    verified: false,
    connection: { id: "account-new" } as AdminConnection,
  });
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter initialEntries={["/connections?new=1&authInstance=auth-1"]}>
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  fireEvent.change(screen.getByLabelText(/API 基础地址/), { target: { value: "https://bad.example" } });
  fireEvent.click(screen.getByRole("button", { name: "保存账号" }));
  await waitFor(() =>
    expect(state.notify).toHaveBeenCalledWith(expect.objectContaining({ message: "invalid API address" })),
  );
  expect(screen.getByLabelText(/API 基础地址/)).toBeEnabled();
  fireEvent.change(screen.getByLabelText(/API 基础地址/), { target: { value: "https://good.example" } });
  fireEvent.click(screen.getByRole("button", { name: "保存账号" }));
  await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
  expect(saveIntegration).toHaveBeenLastCalledWith(
    expect.objectContaining({ authInstanceId: "auth-1", systemId: "system-1", baseUrl: "https://good.example" }),
  );
  expect(save).toHaveBeenCalledWith(expect.objectContaining({ integrationId: "integration-new" }));
});

it("recovers a committed integration after losing its save response and refuses a mismatched binding", async () => {
  const input = {
    integrationKey: "stable",
    name: "Test",
    systemId: "system-1",
    authInstanceId: "auth-1",
    baseUrl: "https://test.example",
  };
  vi.spyOn(api, "saveIntegration").mockRejectedValue(new Error("response lost"));
  const list = vi
    .spyOn(api, "integrations")
    .mockResolvedValueOnce([])
    .mockResolvedValueOnce([{ ...input, id: "integration-1" }]);
  await expect(saveOrFindIntegration(input)).resolves.toMatchObject({ id: "integration-1" });
  list.mockResolvedValue([{ ...input, id: "integration-other", authInstanceId: "auth-other" }]);
  await expect(saveOrFindIntegration(input)).rejects.toThrow("集成标识已被其他配置占用");
});

it("refreshes the revision after an uncertain credential update and waits for explicit retry", async () => {
  const update = vi.spyOn(api, "updateConnection").mockRejectedValue(new Error("response lost"));
  vi.spyOn(api, "connections").mockResolvedValue([{ id: "account-1", revision: 2 } as AdminConnection]);
  const verify = vi.spyOn(api, "verifyConnection");
  const onRevision = vi.fn();
  await expect(
    saveAccount({
      verifyAfterSave: true,
      integrationId: "integration-1",
      connectionId: "account-1",
      connectionKey: "stable",
      name: "Test",
      endUserKey: "tenant",
      credentials: { password: "secret" },
      revision: 1,
      credentialsChanged: true,
      onSaved: vi.fn(),
      onRevision,
    }),
  ).rejects.toThrow("请确认凭据后重试更新");
  expect(onRevision).toHaveBeenCalledWith(2);
  expect(update).toHaveBeenCalledTimes(1);
  expect(verify).not.toHaveBeenCalled();
});

it("preserves only non-sensitive OAuth retry context with URL encoding", () => {
  const path = oauthReturnPath("integration one", "account-original", "账号 & one", "tenant/a");
  const params = new URL(path, "https://console.example").searchParams;
  expect(params.get("connectionKey")).toBe("account-original");
  expect(params.get("accountName")).toBe("账号 & one");
  expect(params.get("endUserKey")).toBe("tenant/a");
  expect([...params.keys()]).toEqual(["section", "integration", "connectionKey", "accountName", "endUserKey"]);
});

it("reports OAuth authorization separately from API verification and locates the server account", async () => {
  state.connections = [{ id: "oauth-account", provider: "test", integration: "test" }];
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter initialEntries={["/connections?oauth=success&connectionId=oauth-account"]}>
        <LocationProbe />
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  await waitFor(() =>
    expect(screen.getByText("/auth?section=accounts&integration=test&connection=oauth-account")).toBeInTheDocument(),
  );
  expect(state.notify).toHaveBeenCalledWith("授权完成；尚未验证 API 权限");
});

it("recovers a committed account after losing its save response without creating another account", async () => {
  const existing = {
    id: "account-1",
    connectionKey: "stable",
    integrationId: "integration-1",
    endUserKey: "tenant",
    name: "Test",
    revision: 1,
  } as AdminConnection;
  const save = vi.spyOn(api, "saveConnection").mockRejectedValue(new Error("response lost"));
  vi.spyOn(api, "connections").mockResolvedValue([existing]);
  const verify = vi.spyOn(api, "verifyConnection").mockResolvedValue({ verified: false, connection: existing });
  const onSaved = vi.fn();
  await expect(
    saveAccount({
      verifyAfterSave: true,
      integrationId: "integration-1",
      connectionKey: "stable",
      endUserKey: "tenant",
      name: "Test",
      credentials: {},
      onSaved,
    }),
  ).resolves.toEqual(existing);
  expect(save).toHaveBeenCalledTimes(1);
  expect(onSaved).toHaveBeenCalledWith(existing);
  expect(verify).toHaveBeenCalledWith("account-1");
});

it("keeps failed account creation editable so a duplicate name can be corrected", async () => {
  vi.spyOn(api, "saveConnection").mockRejectedValue(new Error("该集成中已存在同名账号，请更换账号名称"));
  vi.spyOn(api, "connections").mockResolvedValue([]);
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter>
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "添加账号" }));
  fireEvent.change(screen.getByLabelText("账号名称"), { target: { value: "Duplicate" } });
  fireEvent.click(screen.getByRole("button", { name: "保存账号" }));
  await waitFor(() =>
    expect(state.notify).toHaveBeenCalledWith(
      expect.objectContaining({ message: "该集成中已存在同名账号，请更换账号名称" }),
    ),
  );
  expect(screen.getByLabelText("账号名称")).toBeEnabled();
  fireEvent.change(screen.getByLabelText("账号名称"), { target: { value: "Another account" } });
  expect(screen.getByLabelText("账号名称")).toHaveValue("Another account");
});

it("closes a saved account without triggering an unsaved-input warning", async () => {
  vi.spyOn(api, "saveConnection").mockResolvedValue({ id: "saved-account", revision: 1 } as AdminConnection);
  vi.spyOn(api, "verifyConnection").mockRejectedValue(new Error("verification failed"));
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter>
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "添加账号" }));
  fireEvent.change(screen.getByLabelText("账号名称"), { target: { value: "Saved account" } });
  fireEvent.click(screen.getByRole("button", { name: "保存账号" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(confirm).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("asks only once before discarding genuinely unsaved account inputs", async () => {
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter>
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "添加账号" }));
  fireEvent.change(screen.getByLabelText("账号名称"), { target: { value: "Unsaved account" } });
  fireEvent.click(screen.getByRole("button", { name: "关闭" }));
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("suggests an unused default account name in the selected integration", () => {
  state.connections = [{ id: "existing", provider: "test", integration: "test", name: "服务账号" }];
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter>
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "添加账号" }));
  expect(screen.getByLabelText("账号名称")).toHaveValue("服务账号 2");
});

it("blocks upstream verification when the account has no verification endpoint", async () => {
  state.connections = [
    {
      id: "pending-account",
      provider: "test",
      integration: "test",
      name: "Pending",
      authInstanceId: "auth-1",
      status: "pending",
      lastVerified: "尚未",
    } as any,
  ];
  vi.spyOn(api, "actions").mockResolvedValue([]);
  const verify = vi.spyOn(api, "verifyConnection");
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter initialEntries={["/auth?section=accounts&connection=pending-account"]}>
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  expect(await screen.findByRole("button", { name: "验证账号" })).toBeDisabled();
  expect(screen.getByRole("link", { name: "配置认证实例验证" })).toHaveAttribute(
    "href",
    "/auth?section=instances&editInstance=auth-1",
  );
  expect(verify).not.toHaveBeenCalled();
});

it("keeps verification errors and successful results in the account dialog", async () => {
  state.connections = [
    {
      id: "pending-account",
      provider: "test",
      integration: "test",
      name: "Pending",
      authInstanceId: "auth-1",
      status: "pending",
      lastVerified: "尚未",
    } as any,
  ];
  vi.spyOn(api, "actions").mockResolvedValue([
    {
      id: "me",
      systemKey: "test",
      status: "active",
      httpMethod: "GET",
      relativePath: "/me",
      name: "Current user",
      exampleInput: {},
    },
  ] as any);
  const verify = vi
    .spyOn(api, "verifyConnection")
    .mockRejectedValueOnce(new Error("HTTP 401"))
    .mockResolvedValueOnce({ verified: true, connection: { id: "pending-account" } as AdminConnection });
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter initialEntries={["/auth?section=accounts&connection=pending-account"]}>
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  await screen.findByRole("option", { name: "Current user · GET /me" });
  fireEvent.change(screen.getByLabelText(/业务验证接口/), { target: { value: "me" } });
  fireEvent.click(screen.getByRole("button", { name: "验证账号" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("HTTP 401");
  expect(screen.getByLabelText(/业务验证接口/)).toHaveValue("me");
  fireEvent.click(screen.getByRole("button", { name: "验证账号" }));
  await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("验证通过"));
  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(verify).toHaveBeenLastCalledWith("pending-account", { actionId: "me", input: {} });
});

it("matches backend verification settings for legacy and advanced instances", () => {
  expect(accountVerificationPath({ verificationPath: "/me" })).toBe("/me");
  expect(
    accountVerificationPath({
      verificationPath: "/me",
      authRequest: { schemaVersion: 2, verification: { enabled: false } },
    }),
  ).toBe("");
  expect(
    accountVerificationPath({
      verificationPath: "/me",
      authRequest: { schemaVersion: 2, verification: { enabled: true } },
    }),
  ).toBe("/me");
  expect(accountVerificationPath({})).toBe("");
});

it("uses the opened account template and saved revision when saving and verifying replacement credentials", async () => {
  state.authInstances.push({
    ...state.authInstances[0],
    id: "number-auth",
    templateId: "number-template",
    name: "Number auth",
    status: "ready",
    systemIds: ["test"],
    publicConfig: { verificationPath: "/me" },
  } as DemoAuthInstance);
  state.authSchemes = [
    {
      id: "number-template",
      name: "Number template",
      credentialFields: [{ name: "tenant", label: "Tenant number", type: "number", required: true }],
      injectionRules: [],
    },
  ] as any;
  state.connections = [
    {
      id: "number-account",
      provider: "test",
      integration: "test",
      name: "Number account",
      authInstanceId: "number-auth",
      status: "pending",
      lastVerified: "尚未",
    } as any,
  ];
  state.accountRows = [
    {
      id: "number-account",
      name: "Number account",
      connectionKey: "number",
      systemKey: "test",
      integrationKey: "test",
      integrationId: "integration-1",
      authInstanceId: "number-auth",
      endUserKey: "tenant",
      status: "pending",
      revision: 7,
    },
  ];
  vi.spyOn(api, "actions").mockResolvedValue([]);
  const update = vi.spyOn(api, "updateConnection").mockResolvedValue({ ...state.accountRows[0], revision: 8 });
  const verify = vi.spyOn(api, "verifyConnection").mockRejectedValue(new Error("upstream unavailable"));
  try {
    render(
      <PermissionContext.Provider value="owner">
        <MemoryRouter initialEntries={["/auth?section=accounts&connection=number-account"]}>
          <DemoConnectionsPage />
        </MemoryRouter>
      </PermissionContext.Provider>,
    );
    fireEvent.click(await screen.findByRole("button", { name: "更新凭据" }));
    fireEvent.change(screen.getByLabelText(/Tenant number/), { target: { value: "42" } });
    fireEvent.click(screen.getByRole("button", { name: "保存并验证" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("upstream unavailable");
    expect(update).toHaveBeenCalledWith(
      "number-account",
      expect.objectContaining({ revision: 7, credentials: { tenant: 42 } }),
    );
    expect(verify).toHaveBeenCalledWith("number-account", undefined);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "验证账号" })).toBeEnabled();
  } finally {
    state.authInstances.pop();
  }
});
