import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { PermissionContext } from "./permissions";
import { api } from "./api";
import { DemoConnectionsPage } from "./pages/connections";
const state = vi.hoisted(() => ({
  integrations: [
    {
      id: "integration-1",
      name: "test",
      provider: "test",
      displayName: "Test",
      authInstanceId: "auth-1",
      status: "ready",
    },
  ],
  authInstances: [{ id: "auth-1", templateId: "no_auth", name: "No auth" }],
  authSchemes: [],
  connections: [],
  authenticated: true,
  reload: vi.fn(() => Promise.resolve()),
  notify: vi.fn(),
  startOAuth: vi.fn(),
  refetch: vi.fn(() => Promise.resolve()),
}));
vi.mock("./demo", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./demo")>()),
  useDemo: () => state,
}));
vi.mock("./pagination", () => ({
  useResourcePages: () => ({ items: [], refetch: state.refetch }),
  PageControls: () => null,
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
it("retries verification using the saved connection and persists its organization", async () => {
  const save = vi
    .spyOn(api, "saveConnection")
    .mockResolvedValue({ id: "saved-1" } as Awaited<ReturnType<typeof api.saveConnection>>);
  const verify = vi
    .spyOn(api, "verifyConnection")
    .mockRejectedValueOnce(new Error("verification failed"))
    .mockResolvedValueOnce({ connection: {} });
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter>
        <DemoConnectionsPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "创建连接账号" }));
  fireEvent.click(screen.getByRole("button", { name: "下一步" }));
  fireEvent.change(await screen.findByLabelText("最终用户 ID"), { target: { value: "account-1" } });
  fireEvent.change(screen.getByLabelText("组织显示名"), { target: { value: "Organization" } });
  await waitFor(() => expect(screen.getByRole("button", { name: "下一步" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "下一步" }));
  fireEvent.click(await screen.findByRole("button", { name: "保存并验证连接" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "重试验证并继续" })).toBeEnabled());
  expect(save).toHaveBeenCalledWith(expect.objectContaining({ endUserName: "Organization" }));
  fireEvent.click(screen.getByRole("button", { name: "重试验证并继续" }));
  await waitFor(() => expect(verify).toHaveBeenCalledTimes(2));
  expect(save).toHaveBeenCalledTimes(1);
  expect(verify.mock.calls).toEqual([["saved-1"], ["saved-1"]]);
});
