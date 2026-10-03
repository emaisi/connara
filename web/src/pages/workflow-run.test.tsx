import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { api } from "../api";
import { PermissionContext } from "../permissions";
import { WorkflowRun } from "./workflow-run";
vi.mock("../api", () => ({
  api: {
    workflowRunView: vi.fn(() =>
      Promise.resolve({ schemaVersion: 2, steps: [], name: "冻结版本", workflowVersion: 7 }),
    ),
    operation: vi.fn(),
    workflowRunResult: vi.fn(() => Promise.resolve({ output: { total: 1900 } })),
  },
}));
vi.mock("./workflow-canvas", () => ({ WorkflowCanvas: () => <div>只读运行图</div> }));
vi.mock("./workflow-editor", () => ({ useWorkflowSession: () => null }));
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
  vi.restoreAllMocks();
});
const mount = () =>
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter initialEntries={["/runs/run-1"]}>
        <Routes>
          <Route path="/runs/:runId" element={<WorkflowRun />} />
        </Routes>
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
const settle = async () => {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
};
it("does not overlap requests, loads the frozen view once and stops on a terminal result", async () => {
  vi.useFakeTimers();
  let resolve!: (value: any) => void;
  vi.mocked(api.operation)
    .mockImplementationOnce(() => new Promise((done) => (resolve = done)))
    .mockResolvedValueOnce({ status: "success", events: [] });
  mount();
  await settle();
  await act(async () => vi.advanceTimersByTime(10000));
  expect(api.operation).toHaveBeenCalledTimes(1);
  await act(async () => resolve({ status: "queued", events: [] }));
  await act(async () => vi.advanceTimersByTime(2000));
  await settle();
  expect(api.operation).toHaveBeenCalledTimes(2);
  expect(api.workflowRunView).toHaveBeenCalledTimes(1);
  expect(api.workflowRunResult).toHaveBeenCalledTimes(1);
  expect(screen.getByText(/1900/)).toBeTruthy();
  await act(async () => vi.advanceTimersByTime(20000));
  expect(api.operation).toHaveBeenCalledTimes(2);
});
it("pauses when hidden, resumes when visible and stops after permission is revoked", async () => {
  vi.useFakeTimers();
  let hidden = false;
  vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
  vi.mocked(api.operation)
    .mockResolvedValueOnce({ status: "running", events: [] })
    .mockRejectedValueOnce({ status: 403 });
  mount();
  await settle();
  hidden = true;
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  await act(async () => vi.advanceTimersByTime(10000));
  expect(api.operation).toHaveBeenCalledTimes(1);
  hidden = false;
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  await settle();
  expect(api.operation).toHaveBeenCalledTimes(2);
  await act(async () => vi.advanceTimersByTime(20000));
  expect(api.operation).toHaveBeenCalledTimes(2);
});
