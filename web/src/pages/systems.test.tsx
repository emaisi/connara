import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { api } from "../api";
import { PermissionContext } from "../permissions";
import { DemoProvidersPage } from "./systems";

const state = vi.hoisted(() => ({
  authenticated: true,
  systemGroups: ["财务系统"],
  authInstances: [],
  notify: vi.fn(),
  reload: vi.fn(() => Promise.resolve(true)),
}));
vi.mock("../demo", async (original) => ({
  ...(await original<typeof import("../demo")>()),
  useDemo: () => state,
}));
vi.mock("../pagination", () => ({
  useResourcePages: () => ({ items: [] }),
  PageControls: () => null,
}));

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

it("renames a group inline, cancels with Escape, and preserves failed edits", async () => {
  let group = { id: "group-1", name: "财务系统", sortOrder: 20 };
  vi.spyOn(api, "systemGroups").mockImplementation(async () => [group]);
  vi.spyOn(api, "authTemplates").mockResolvedValue([]);
  const update = vi.spyOn(api, "updateSystemGroup").mockImplementation(async (_id, values) => {
    group = { ...group, ...(values as { name: string; sortOrder: number }) };
    return group;
  });
  const prompt = vi.spyOn(window, "prompt");
  render(
    <PermissionContext.Provider value="owner">
      <MemoryRouter>
        <DemoProvidersPage />
      </MemoryRouter>
    </PermissionContext.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "管理分组" }));
  fireEvent.click(await screen.findByRole("button", { name: "重命名" }));
  const input = screen.getByRole("textbox", { name: "分组名称" });
  expect(input).toHaveFocus();
  expect(screen.getAllByRole("dialog")).toHaveLength(1);
  expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
  fireEvent.change(input, { target: { value: "不保存" } });
  fireEvent.keyDown(input, { key: "Escape" });
  expect(screen.queryByRole("textbox", { name: "分组名称" })).not.toBeInTheDocument();
  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(update).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "重命名" }));
  fireEvent.change(screen.getByRole("textbox", { name: "分组名称" }), { target: { value: "  财务与采购  " } });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(screen.queryByRole("textbox", { name: "分组名称" })).not.toBeInTheDocument());
  expect(update).toHaveBeenCalledWith("group-1", { name: "财务与采购", sortOrder: 20 });
  expect(screen.getByText("财务与采购")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "重命名" }));
  update.mockRejectedValueOnce(new Error("保存失败"));
  fireEvent.change(screen.getByRole("textbox", { name: "分组名称" }), { target: { value: "新的名称" } });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(state.notify).toHaveBeenCalledWith(expect.objectContaining({ message: "保存失败" })));
  expect(screen.getByRole("textbox", { name: "分组名称" })).toHaveValue("新的名称");
  expect(prompt).not.toHaveBeenCalled();
});
