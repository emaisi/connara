import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes, createMemoryRouter, RouterProvider } from "react-router";
import { PermissionContext } from "../permissions";
import { api } from "../api";
import { WorkflowsPage } from "./workflows";
import { WorkflowSession } from "./workflow-editor";
import { blankWorkflow, type WorkflowItem } from "./workflow-model";

const state = vi.hoisted(() => ({ authenticated: true, reload: vi.fn(() => Promise.resolve(true)), notify: vi.fn() }));
vi.mock("../demo", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../demo")>()),
  useDemo: () => state,
}));
vi.mock("../api", () => ({
  api: {
    workflows: vi.fn(() => Promise.resolve([])),
    workflowCapabilities: vi.fn(() =>
      Promise.resolve({ v2Enabled: true, code: { enabled: true, local: { available: true } } }),
    ),
    workflow: vi.fn(),
    actions: vi.fn(() => Promise.resolve([])),
    saveWorkflow: vi.fn(),
    saveWorkflowLayout: vi.fn(),
    deployWorkflow: vi.fn(),
    pauseWorkflow: vi.fn(),
    runWorkflow: vi.fn(),
    actionExecutionOptions: vi.fn(() => Promise.resolve([])),
  },
  ApiError: class extends Error {},
}));
vi.mock("./workflow-canvas", () => ({
  WorkflowCanvas: ({ onSelect, onConfigure, onAdd, onClear, tools }: any) => (
    <div>
      {tools}
      测试画布<button onClick={() => onSelect("customer")}>选择客户节点</button>
      <button onClick={() => onConfigure("customer")}>打开客户配置</button>
      <button onClick={() => onAdd("customer")}>客户后添加</button>
      <button onClick={onClear}>画布空白</button>
    </div>
  ),
  autoLayout: () => ({ schemaVersion: 1, direction: "LR", positions: {} }),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
const fixture = (): WorkflowItem => ({
  ...blankWorkflow(),
  id: "wf-1",
  workflowKey: "orders",
  name: "订单流程",
  version: 3,
  status: "deployed",
  graph: {
    steps: [{ id: "customer", action: "act-1", input: {}, integrationId: "int-1", connectionKey: "conn-1" }],
    output: { customer: "{{customer}}" },
  },
});
function renderEditor(initial = fixture()) {
  vi.mocked(api.workflow).mockResolvedValue(initial);
  vi.mocked(api.saveWorkflow).mockImplementation(
    async (body) => ({ ...initial, ...(body as WorkflowItem), version: initial.version + 1, status: "draft" }) as never,
  );
  const router = createMemoryRouter(
    [
      {
        path: "/workflows/:id",
        element: <WorkflowSession />,
        children: [
          { index: true, element: <></> },
          { path: "history", element: <a href="/workflows/wf-1">返回配置</a> },
        ],
      },
      { path: "/outside", element: <p>离开工作流</p> },
    ],
    { initialEntries: ["/workflows/wf-1"] },
  );
  render(
    <PermissionContext.Provider value="owner">
      <RouterProvider router={router} />
    </PermissionContext.Provider>,
  );
  return router;
}
describe("workflow editor", () => {
  it("opens the dedicated editor route from the empty list", async () => {
    render(
      <MemoryRouter initialEntries={["/workflows"]}>
        <PermissionContext.Provider value="owner">
          <Routes>
            <Route path="/workflows" element={<WorkflowsPage />} />
            <Route path="/workflows/:id" element={<WorkflowSession />} />
          </Routes>
        </PermissionContext.Provider>
      </MemoryRouter>,
    );
    await screen.findByText("尚无工作流");
    fireEvent.click(screen.getByRole("link", { name: "新建工作流" }));
    fireEvent.click(await screen.findByRole("button", { name: "配置工作流" }));
    expect(await screen.findByLabelText("工作流标识")).toBeTruthy();
  });
  it("preserves v1 semantics and explicit bindings on an ordinary save", async () => {
    renderEditor();
    fireEvent.click(await screen.findByRole("button", { name: "配置工作流" }));
    await screen.findByLabelText("工作流名称");
    fireEvent.change(screen.getByLabelText("工作流名称"), { target: { value: "订单流程更新" } });
    fireEvent.click(screen.getByRole("button", { name: "更多" }));
    fireEvent.click(screen.getByRole("button", { name: "仅保存草稿" }));
    await waitFor(() => expect(api.saveWorkflow).toHaveBeenCalled());
    const body = vi.mocked(api.saveWorkflow).mock.calls[0][0] as any;
    expect(body.graph.schemaVersion).toBeUndefined();
    expect(body.graph.steps[0]).toMatchObject({ id: "customer", integrationId: "int-1", connectionKey: "conn-1" });
  });
  it("retains edits made while an earlier save is pending", async () => {
    renderEditor();
    fireEvent.click(await screen.findByRole("button", { name: "配置工作流" }));
    const name = await screen.findByLabelText("工作流名称");
    let finish!: (value: any) => void;
    vi.mocked(api.saveWorkflow).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    fireEvent.change(name, { target: { value: "提交 A" } });
    fireEvent.click(screen.getByRole("button", { name: "保存并部署" }));
    await waitFor(() => expect(api.saveWorkflow).toHaveBeenCalled());
    const submitted = vi.mocked(api.saveWorkflow).mock.calls[0][0];
    fireEvent.change(name, { target: { value: "后续 B" } });
    finish({ ...fixture(), ...(submitted as WorkflowItem), version: 4, status: "draft" });
    expect(await screen.findByText(/之后的修改仍未保存/)).toBeTruthy();
    expect(name).toHaveValue("后续 B");
    expect(api.deployWorkflow).not.toHaveBeenCalled();
  });
  it("blocks invalid buffers, keeps them across history, and protects navigation", async () => {
    const router = renderEditor();
    fireEvent.click(await screen.findByRole("button", { name: "配置工作流" }));
    const input = await screen.findByLabelText("默认输入（整个对象）");
    fireEvent.change(input, { target: { value: "{" } });
    expect(screen.getByRole("button", { name: "运行" })).toBeDisabled();
    await act(async () => {
      await router.navigate("/workflows/wf-1/history");
    });
    await act(async () => {
      await router.navigate("/workflows/wf-1");
    });
    expect(screen.getByLabelText("默认输入（整个对象）")).toHaveValue("{");
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    await act(async () => {
      await router.navigate("/outside");
    });
    await waitFor(() => expect(confirm).toHaveBeenCalled());
    expect(router.state.location.pathname).toBe("/workflows/wf-1");
    confirm.mockRestore();
  });
  it("uses independent versions for layout saves and retains deployed status", async () => {
    const original = fixture();
    renderEditor(original);
    fireEvent.click(await screen.findByRole("button", { name: "配置工作流" }));
    await screen.findByLabelText("工作流名称");
    vi.mocked(api.saveWorkflowLayout).mockResolvedValue({
      ...original,
      layoutVersion: 2,
      editorLayout: { schemaVersion: 1, direction: "LR", positions: {} },
    } as never);
    fireEvent.click(screen.getByRole("button", { name: "更多" }));
    fireEvent.click(screen.getByRole("button", { name: "自动布局" }));
    fireEvent.click(screen.getByRole("button", { name: "更多" }));
    fireEvent.click(screen.getByRole("button", { name: "仅保存布局" }));
    await waitFor(() =>
      expect(api.saveWorkflowLayout).toHaveBeenCalledWith(
        "wf-1",
        expect.objectContaining({ expectedWorkflowVersion: 3, layoutVersion: 1 }),
      ),
    );
    expect(api.saveWorkflow).not.toHaveBeenCalled();
    expect(await screen.findByText(/部署状态和调度未改变/)).toBeTruthy();
  });
  it("keeps selection independent from configuration and preserves invalid JSON when closing", async () => {
    renderEditor();
    await screen.findByRole("button", { name: "配置工作流" });
    expect(screen.queryByLabelText("工作流名称")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "选择客户节点" }));
    expect(screen.queryByLabelText("节点名称")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "配置节点" }));
    expect(screen.getByLabelText("节点名称")).toBeTruthy();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByLabelText("节点名称")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "配置工作流" }));
    fireEvent.change(screen.getByLabelText("默认输入（整个对象）"), { target: { value: "{" } });
    fireEvent.click(screen.getByRole("button", { name: "关闭节点配置" }));
    fireEvent.click(screen.getByRole("button", { name: "配置节点" }));
    expect(screen.getByLabelText("默认输入（整个对象）")).toHaveValue("{");
  });
  it("cancels local additions without changing the graph and adds a selected successor", async () => {
    renderEditor();
    await screen.findByRole("button", { name: "配置工作流" });
    fireEvent.click(screen.getByRole("button", { name: "客户后添加" }));
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "客户后添加" }));
    fireEvent.click(screen.getByRole("button", { name: "API" }));
    expect(screen.getByLabelText("节点名称")).toHaveValue("API");
    fireEvent.click(screen.getByRole("button", { name: "更多" }));
    fireEvent.click(screen.getByRole("button", { name: "仅保存草稿" }));
    await waitFor(() => expect(api.saveWorkflow).toHaveBeenCalled());
    const graph = (vi.mocked(api.saveWorkflow).mock.calls[0][0] as any).graph;
    expect(graph.steps).toHaveLength(2);
    expect(graph.steps[1].dependsOn).toEqual(["customer"]);
  });
  it("locates missing workflow metadata before making an invalid save request", async () => {
    renderEditor({ ...fixture(), status: "draft", workflowKey: "" });
    fireEvent.click(await screen.findByRole("button", { name: "部署" }));
    expect(await screen.findByText(/请填写工作流标识/)).toBeTruthy();
    expect(screen.getByLabelText("工作流标识")).toBeTruthy();
    expect(api.saveWorkflow).not.toHaveBeenCalled();
  });
});
