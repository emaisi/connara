import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { PermissionContext } from "../permissions";
import { DemoActionsPage } from "./actions";

const state = vi.hoisted(() => ({
  authenticated: true,
  customSystems: [{ id: "system-1", service: "test", name: "Test" }],
  integrations: [{ id: "integration-1", name: "test-integration", provider: "test" }],
  notify: vi.fn(),
  reload: vi.fn(),
}));
const row = vi.hoisted(() => ({
  id: "action-1",
  actionKey: "test.list",
  name: "test.list",
  systemId: "system-1",
  systemKey: "test",
  source: "custom",
  status: "active",
  executable: true,
  version: 1,
  description: "List",
  httpMethod: "GET",
  relativePath: "/items",
  exampleInput: {},
}));
const mocks = vi.hoisted(() => ({ options: vi.fn(), preview: vi.fn(), save: vi.fn(), action: vi.fn(), test: vi.fn() }));
vi.mock("../demo", () => ({ useDemo: () => state }));
vi.mock("../pagination", () => ({ useResourcePages: () => ({ items: [row] }), PageControls: () => null }));
vi.mock("../api", () => ({
  api: {
    actionExecutionOptions: mocks.options,
    previewAction: mocks.preview,
    saveAction: mocks.save,
    action: mocks.action,
    testAction: mocks.test,
    operation: vi.fn().mockResolvedValue({ events: [], httpStatus: 200 }),
  },
  ApiError: class extends Error {},
}));
function option(id = "integration-1", active = false) {
  return {
    integration: { id, name: id, baseUrl: "http://localhost:18001", version: 1, targetVersion: 1 },
    requiresAccount: true,
    accounts: [
      {
        id: "existing-account",
        name: "Existing",
        revision: 1,
        status: active ? "active" : "pending",
        lastVerifiedAt: active ? "2026-10-03" : undefined,
        verifiedRevision: active ? 1 : 0,
        verifiedTargetVersion: active ? 1 : 0,
      },
    ],
  };
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.options.mockResolvedValue([option()]);
  mocks.preview.mockResolvedValue({
    request: { method: "GET", url: "http://localhost:18001/items", headers: {}, body: null },
    apiVersion: 1,
    integrationVersion: 1,
    accountRevision: 1,
    authentication: { required: true, accountName: "Existing", instanceName: "Test" },
    verified: true,
  });
  mocks.save.mockResolvedValue(row);
});
afterEach(cleanup);
function mount() {
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter>
        <DemoActionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
}
async function openTest() {
  mount();
  fireEvent.click(await screen.findByRole("button", { name: /test.list/ }));
  fireEvent.click(await screen.findByRole("tab", { name: "测试" }));
}
it("directs an existing pending account to verification rather than account creation", async () => {
  await openTest();
  expect(await screen.findByText("该集成已有账号，但尚未就绪，请验证或修复已有账号。")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "查看已有账号" })).toHaveAttribute(
    "href",
    "/auth?section=accounts&integration=test-integration&connection=existing-account",
  );
  expect(screen.queryByRole("link", { name: "添加账号" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "运行测试" })).toBeDisabled();
});
it("keeps account creation in account management", async () => {
  const target = option();
  target.accounts = [];
  mocks.options.mockResolvedValue([target]);
  await openTest();
  expect(await screen.findByRole("link", { name: "前往账号管理" })).toBeInTheDocument();
  expect(screen.queryByRole("link", { name: "添加账号" })).not.toBeInTheDocument();
});
it("creates a system API without any execution environment and never calls upstream", async () => {
  mocks.options.mockResolvedValue([]);
  mount();
  fireEvent.click(screen.getByRole("button", { name: "添加 API" }));
  fireEvent.change(screen.getByLabelText("所属系统"), { target: { value: "test" } });
  fireEvent.change(screen.getByLabelText("API 名称"), { target: { value: "List" } });
  fireEvent.change(screen.getByLabelText(/API 标识/), { target: { value: "list" } });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(mocks.save).toHaveBeenCalled());
  const body = mocks.save.mock.calls[0][0];
  expect(body.systemId).toBe("system-1");
  expect(body).not.toHaveProperty("integrationId");
  expect(mocks.test).not.toHaveBeenCalled();
});
it("lets a custom API choose every matching environment and clears the old account", async () => {
  mocks.options.mockResolvedValue([option("integration-1", true), { ...option("integration-2"), accounts: [] }]);
  await openTest();
  const select = await screen.findByLabelText("执行集成");
  await waitFor(() => expect(screen.getByRole("option", { name: "integration-2" })).toBeInTheDocument());
  fireEvent.change(select, { target: { value: "integration-1" } });
  await waitFor(() => expect(screen.getByLabelText("执行账号")).toHaveValue("existing-account"));
  expect(await screen.findByText("GET http://localhost:18001/items")).toBeInTheDocument();
  fireEvent.change(select, { target: { value: "integration-2" } });
  expect(screen.getByLabelText("执行账号")).toHaveValue("");
  expect(screen.queryByText("GET http://localhost:18001/items")).not.toBeInTheDocument();
});
it("permits a no-auth environment without a placeholder account", async () => {
  mocks.options.mockResolvedValue([{ ...option(), requiresAccount: false, accounts: [] }]);
  await openTest();
  expect(await screen.findByText("GET http://localhost:18001/items")).toBeInTheDocument();
  expect(screen.queryByLabelText("执行账号")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "运行测试" })).toBeEnabled();
});

it("refreshes a conflicted preview, preserves input, and requires target confirmation", async () => {
  mocks.options.mockResolvedValue([option("integration-1", true)]);
  mocks.action.mockResolvedValue({ ...row, version: 2 });
  mocks.test.mockRejectedValueOnce(Object.assign(new Error("changed"), { status: 409, code: "configuration_changed" }));
  await openTest();
  fireEvent.change(screen.getByLabelText("输入 JSON"), { target: { value: '{"page":2}' } });
  await waitFor(() => expect(screen.getByRole("button", { name: "运行测试" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "运行测试" }));
  await waitFor(() => expect(mocks.action).toHaveBeenCalledWith("action-1"));
  const confirm = await screen.findByRole("button", { name: "确认新目标" });
  await waitFor(() => expect(confirm).toBeEnabled());
  expect(screen.getByLabelText("输入 JSON")).toHaveValue('{"page":2}');
  expect(screen.getByRole("button", { name: "运行测试" })).toBeDisabled();
  expect(mocks.test).toHaveBeenCalledTimes(1);
  fireEvent.click(confirm);
  expect(screen.getByRole("button", { name: "运行测试" })).toBeEnabled();
});

async function createEditor() {
  mount();
  fireEvent.click(screen.getByRole("button", { name: "添加 API" }));
  fireEvent.change(screen.getByLabelText("所属系统"), { target: { value: "test" } });
  fireEvent.change(screen.getByLabelText("API 名称"), { target: { value: "List" } });
  fireEvent.change(screen.getByLabelText(/API 标识/), { target: { value: "list" } });
}
it("keeps an invalid default error while another field changes and blocks saving", async () => {
  await createEditor();
  fireEvent.click(screen.getByRole("button", { name: "添加参数" }));
  fireEvent.change(screen.getByLabelText("参数名"), { target: { value: "page" } });
  fireEvent.change(screen.getByLabelText("类型"), { target: { value: "integer" } });
  fireEvent.change(screen.getByLabelText("默认值"), { target: { value: "not-a-number" } });
  fireEvent.change(screen.getByLabelText("示例"), { target: { value: "1" } });
  expect(screen.getByRole("alert")).toHaveTextContent("默认值必须符合所选类型");
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(screen.getByLabelText("默认值")).toHaveFocus());
  expect(mocks.save).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("默认值"), { target: { value: "0" } });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(mocks.save).toHaveBeenCalled());
  expect(mocks.save.mock.calls[0][0].requestConfig.parameters[0]).toMatchObject({ default: 0, example: 1 });
});
it("preserves the remaining parameter input when deleting another row", async () => {
  await createEditor();
  fireEvent.click(screen.getByRole("button", { name: "添加参数" }));
  fireEvent.click(screen.getByRole("button", { name: "添加参数" }));
  fireEvent.change(screen.getAllByLabelText("参数名")[0], { target: { value: "first" } });
  fireEvent.change(screen.getAllByLabelText("参数名")[1], { target: { value: "second" } });
  fireEvent.change(screen.getAllByLabelText("默认值")[1], { target: { value: "kept" } });
  fireEvent.click(screen.getAllByRole("button", { name: "删除参数" })[0]);
  expect(screen.getByLabelText("参数名")).toHaveValue("second");
  expect(screen.getByLabelText("默认值")).toHaveValue("kept");
});
it("confirms removal of path parameters and can restore the original path", async () => {
  await createEditor();
  const path = screen.getByLabelText(/接口路径/);
  fireEvent.change(path, { target: { value: "/items/{id}" } });
  fireEvent.blur(path);
  fireEvent.change(path, { target: { value: "/items" } });
  fireEvent.blur(path);
  expect(await screen.findByRole("dialog")).toHaveTextContent("移除路径参数");
  fireEvent.click(screen.getByRole("button", { name: "保留原路径" }));
  expect(path).toHaveValue("/items/{id}");
  expect(screen.getByLabelText("参数名")).toHaveValue("id");
  fireEvent.change(path, { target: { value: "/items" } });
  fireEvent.blur(path);
  fireEvent.click(screen.getByRole("button", { name: "确认移除参数" }));
  expect(screen.queryByLabelText("参数名")).not.toBeInTheDocument();
  expect(path).toHaveValue("/items");
});
it("focuses the rejected schema and keeps the editor open with the input preserved", async () => {
  mocks.save.mockRejectedValueOnce(
    Object.assign(new Error("schema error"), {
      details: { fieldErrors: [{ field: "outputSchema", path: "$", constraint: "type" }] },
    }),
  );
  await createEditor();
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(screen.getByLabelText(/输出结构 Schema/)).toHaveFocus());
  expect(screen.getByLabelText("API 名称")).toHaveValue("List");
  expect(screen.getByRole("button", { name: /outputSchema/ })).toBeInTheDocument();
});

it("retains custom input constraints when reopening and saving an API", async () => {
  const { ApiDefinitionEditor } = await import("./api-definition-editor");
  const schema = { type: "object", properties: { page: { type: "integer", minimum: 1 } }, required: ["page"] };
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter>
        <ApiDefinitionEditor
          initial={{
            ...row,
            inputSchema: schema,
            requestConfig: {
              schemaVersion: 1,
              bodyFormat: "none",
              headers: [],
              parameters: [{ name: "page", in: "query", type: "integer", required: true, default: 2 }],
            },
          }}
          systemKey="test"
          onSaved={vi.fn()}
          onCancel={vi.fn()}
        />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  expect(screen.getByLabelText("使用自定义输入 Schema")).toBeChecked();
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(mocks.save).toHaveBeenCalled());
  expect(mocks.save.mock.calls[0][0].inputSchema).toEqual(schema);
});
