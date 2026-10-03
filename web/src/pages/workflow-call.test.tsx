import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { api } from "../api";
import { WorkflowCall } from "./workflow-call";
import { blankWorkflow } from "./workflow-model";

vi.mock("../api", () => ({ api: { workflow: vi.fn() } }));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("loads saved contracts and copies shell-safe calls without executing a workflow", async () => {
  vi.mocked(api.workflow).mockResolvedValue({
    ...blankWorkflow(),
    id: "wf-1",
    workflowKey: "orders",
    version: 3,
    status: "deployed",
    input: { name: "O'Brien", note: "$(touch /tmp/should-not-exist)" },
    graph: { inputSchema: { type: "object", properties: { name: { type: "string" } } }, steps: [], output: {} },
  } as never);
  const copy = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: copy } });
  render(
    <MemoryRouter>
      <WorkflowCall id="wf-1" active />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "调用说明" }));
  await screen.findByText("同步调用（最多 40 秒）");
  expect(api.workflow).toHaveBeenCalledWith("wf-1");
  fireEvent.click(screen.getAllByRole("button", { name: "复制" })[0]);
  await waitFor(() => expect(copy).toHaveBeenCalled());
  expect(copy.mock.calls[0][0]).toContain("/v1/workflows/orders'");
  expect(copy.mock.calls[0][0]).toContain("O'\"'\"'Brien");
  expect(copy.mock.calls[0][0]).toContain("${CONNARA_RUNTIME_TOKEN}");
  expect(copy.mock.calls[0][0]).toContain("$(touch /tmp/should-not-exist)");
  fireEvent.change(screen.getByLabelText("调用输入 JSON"), { target: { value: "[]" } });
  expect(screen.getByRole("alert").textContent).toContain("JSON 对象");
  expect(screen.queryByRole("button", { name: "复制" })).toBeNull();
});

it("explains when the saved workflow cannot be called", async () => {
  vi.mocked(api.workflow).mockResolvedValue({ ...blankWorkflow(), id: "wf-1", workflowKey: "draft" } as never);
  render(
    <MemoryRouter>
      <WorkflowCall id="wf-1" active />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole("button", { name: "调用说明" }));
  expect((await screen.findByRole("alert")).textContent).toContain("部署后才能调用");
});
