import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";
import { queryClient } from "./query";
import { ConnectionCredentialFields, Tabs } from "./pages/core-shared";
import { PermissionContext } from "./permissions";
import { MutationForm, Button } from "./ui";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
describe("UI workflow regression", () => {
  it("preserves cached resources when checking the session", async () => {
    queryClient.setQueryData(["/api/systems"], [{ id: "saved" }]);
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.resolve(new Response(JSON.stringify({ role: "owner" })))),
    );
    await api.session();
    expect(queryClient.getQueryData(["/api/systems"])).toEqual([{ id: "saved" }]);
  });
  it("preserves runtime error codes and operation links", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              success: false,
              errorCode: "provider_request_failed",
              message: "upstream unavailable",
              meta: { operationId: "run-1" },
            }),
            { status: 502, headers: { "X-Request-ID": "request-1" } },
          ),
        ),
      ),
    );
    await expect(api.testAction("action", {})).rejects.toMatchObject({
      code: "provider_request_failed",
      operationId: "run-1",
      requestId: "request-1",
      rawMessage: "upstream unavailable",
    });
  });
  it("keeps credentials controlled when returning to a step", () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <ConnectionCredentialFields
        auth="Basic"
        values={{ username: "saved", password: "secret" }}
        onChange={onChange}
      />,
    );
    expect(screen.getByLabelText("用户名 *")).toHaveValue("saved");
    rerender(
      <ConnectionCredentialFields
        auth="Basic"
        values={{ username: "saved", password: "secret" }}
        onChange={onChange}
      />,
    );
    expect(screen.getByLabelText("密码 *")).toHaveValue("secret");
  });
  it("lets keyboard users select tabs with arrow keys", () => {
    const onChange = vi.fn();
    render(<Tabs value="第一项" items={["第一项", "第二项"]} onChange={onChange} />);
    fireEvent.keyDown(screen.getByRole("tab", { name: "第一项" }), { key: "ArrowRight" });
    expect(onChange).toHaveBeenCalledWith("第二项");
    expect(screen.getByRole("tab", { name: "第二项" })).toHaveFocus();
  });
  it("disables viewer mutation forms and write buttons", () => {
    render(
      <PermissionContext.Provider value="viewer">
        <MutationForm>
          <input aria-label="凭据" />
          <Button write>保存</Button>
        </MutationForm>
      </PermissionContext.Provider>,
    );
    expect(screen.getByLabelText("凭据")).toBeDisabled();
    expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
  });
  it("prevents repeated submit while a mutation is pending", async () => {
    let finish!: () => void;
    const submit = vi.fn((event: React.FormEvent) => {
      event.preventDefault();
      return new Promise<void>((resolve) => {
        finish = resolve;
      });
    });
    render(
      <PermissionContext.Provider value="developer">
        <MutationForm onSubmit={submit}>
          <Button type="submit">保存</Button>
        </MutationForm>
      </PermissionContext.Provider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    expect(submit).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "保存" })).toBeDisabled();
    finish();
    await waitFor(() => expect(screen.getByRole("button", { name: "保存" })).not.toBeDisabled());
  });
});
