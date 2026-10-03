import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { PermissionContext } from "../permissions";
import { api } from "../api";
import { WorkflowsPage } from "./workflows";

const state = vi.hoisted(() => ({
  integrations: [
    {
      id: "int-1",
      name: "crm",
      provider: "crm",
      displayName: "CRM",
      authInstanceId: "auth-1",
      status: "ready" as const,
      baseUrl: "https://crm.example.test",
      updatedAt: "",
    },
  ],
  connections: [
    {
      id: "conn-1",
      name: "crm-service",
      integration: "crm",
      provider: "crm",
      endUser: "alice",
      authInstanceId: "auth-1",
      status: "active" as const,
      lastVerified: "",
      lastUsed: "",
      records: 0,
    },
  ],
  operations: [],
  authenticated: true,
  reload: vi.fn(() => Promise.resolve(true)),
  notify: vi.fn(),
}));
vi.mock("../demo", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../demo")>()),
  useDemo: () => state,
}));
vi.mock("../api", () => ({
  api: {
    workflows: vi.fn(() => Promise.resolve([])),
    actions: vi.fn(() => Promise.resolve([])),
    saveWorkflow: vi.fn((_body: unknown, _id?: string) => Promise.resolve({ id: "wf-1" })),
    deployWorkflow: vi.fn(() => Promise.resolve({})),
    pauseWorkflow: vi.fn(() => Promise.resolve({})),
    runWorkflow: vi.fn(() => Promise.resolve({ id: "run-1", status: "queued" })),
    workflowRunResult: vi.fn(() => Promise.resolve({ output: {} })),
    deleteWorkflow: vi.fn(() => Promise.resolve()),
    operation: vi.fn(() => Promise.resolve({ status: "success" })),
  },
  ApiError: class extends Error {},
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/workflows"]}>
      <PermissionContext.Provider value="owner">
        <WorkflowsPage />
      </PermissionContext.Provider>
    </MemoryRouter>,
  );
}

// jsdom does not run submit-button activation inside portal-hosted dialogs,
// so tests submit the form element directly.
function submitDialogForm(dialog: HTMLElement) {
  const form = dialog.querySelector("form");
  if (!form) throw new Error("dialog has no form");
  fireEvent.submit(form);
}

describe("workflows page", () => {
  it("renders the empty state and opens the editor", async () => {
    renderPage();
    expect(await screen.findByText("尚无工作流")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: /新建工作流/ })[0]);
    expect(await screen.findByRole("dialog")).toBeTruthy();
    expect(screen.getByLabelText(/工作流标识/)).toBeTruthy();
  });

  it("saves a draft with explicit step bindings", async () => {
    vi.mocked(api.actions).mockResolvedValue([
      { id: "act-1", actionKey: "crm.get", systemKey: "crm", status: "active", executable: true } as never,
    ]);
    renderPage();
    await screen.findByText("尚无工作流");
    fireEvent.click(screen.getAllByRole("button", { name: /新建工作流/ })[0]);
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("别名"), { target: { value: "get_customer" } });
    fireEvent.change(within(dialog).getByLabelText("API 操作"), { target: { value: "act-1" } });
    fireEvent.change(within(dialog).getByLabelText(/集成（部署时固定）/), { target: { value: "int-1" } });
    fireEvent.change(within(dialog).getByLabelText(/账号（部署时固定）/), { target: { value: "conn-1" } });
    fireEvent.change(within(dialog).getByLabelText(/输出映射 JSON 对象/), {
      target: { value: '{"name": "{{get_customer.data.name}}"}' },
    });
    submitDialogForm(dialog);
    await waitFor(() => expect(api.saveWorkflow).toHaveBeenCalled());
    const body = vi.mocked(api.saveWorkflow).mock.calls[0][0] as Record<string, unknown>;
    const graph = body.graph as { steps: Array<Record<string, unknown>>; output: Record<string, unknown> };
    expect(graph.steps[0]).toMatchObject({
      id: "get_customer",
      action: "crm.get",
      integrationId: "int-1",
      connectionKey: "conn-1",
    });
    expect(graph.output).toEqual({ name: "{{get_customer.data.name}}" });
    expect(body.retryPolicy).toEqual({ maxAttempts: 1 });
  });

  it("surfaces an error when a step misses explicit bindings", async () => {
    vi.mocked(api.actions).mockResolvedValue([
      { id: "act-1", actionKey: "crm.get", systemKey: "crm", status: "active", executable: true } as never,
    ]);
    renderPage();
    await screen.findByText("尚无工作流");
    fireEvent.click(screen.getAllByRole("button", { name: /新建工作流/ })[0]);
    const dialog = await screen.findByRole("dialog");
    submitDialogForm(dialog);
    expect(await within(dialog).findByText(/显式绑定/)).toBeTruthy();
    expect(api.saveWorkflow).not.toHaveBeenCalled();
  });
});
