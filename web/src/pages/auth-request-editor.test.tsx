import { afterEach, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { PermissionContext } from "../permissions";
import { AuthRequestEditor, ConditionEditor } from "./auth-request-editor";
import { authPreview, defaultAuthRequest, typedCredentials, type AuthCondition } from "../auth-request";
afterEach(cleanup);
const fields = [
  { name: "username", secret: false },
  { name: "password", secret: true },
];
function Editor() {
  const [request, setRequest] = useState(defaultAuthRequest());
  return (
    <>
      <AuthRequestEditor value={request} onChange={setRequest} fields={fields} />
      <output data-testid="config">{JSON.stringify(request)}</output>
    </>
  );
}
it("switches GET to query parameters without a body and keeps mapped credentials", () => {
  render(
    <PermissionContext.Provider value="owner">
      <Editor />
    </PermissionContext.Provider>,
  );
  fireEvent.click(screen.getByLabelText("自定义参数映射"));
  fireEvent.change(screen.getByLabelText("请求方法"), { target: { value: "GET" } });
  expect(screen.queryByLabelText("请求体格式")).not.toBeInTheDocument();
  const config = JSON.parse(screen.getByTestId("config").textContent!);
  expect(config.bodyType).toBe("none");
  expect(config.parameters.map((p: { target: string }) => p.target)).toEqual(["query", "query"]);
  expect(config.parameters.map((p: { value: { name: string } }) => p.value.name)).toEqual(["username", "password"]);
});
it("masks credential and runtime secrets in previews and retains literal JSON types", () => {
  const config = defaultAuthRequest();
  config.credentialMode = "mapped";
  config.parameters = [
    { name: "enabled", target: "body", value: { source: "literal", value: false } },
    { name: "password", target: "body", value: { source: "credential", name: "password" } },
    { name: "token", target: "body", value: { source: "runtime", name: "token" } },
  ];
  const preview = authPreview(config, fields);
  expect(preview.parameters?.map((p) => p.value)).toEqual([false, "***", "***"]);
  expect(
    typedCredentials(
      [
        { name: "enabled", type: "boolean" },
        { name: "number", type: "number" },
      ],
      { enabled: "false", number: "0" },
    ),
  ).toEqual({ enabled: false, number: 0 });
});
it("edits a numeric success condition without converting it to a string", () => {
  function Condition() {
    const [value, setValue] = useState<AuthCondition | undefined>({ path: "$.code", operator: "equals", value: 0 });
    return (
      <>
        <ConditionEditor value={value} onChange={setValue} />
        <output data-testid="condition">{JSON.stringify(value)}</output>
      </>
    );
  }
  render(<Condition />);
  fireEvent.change(screen.getByLabelText("固定参数值"), { target: { value: "200" } });
  expect(JSON.parse(screen.getByTestId("condition").textContent!).value).toBe(200);
});
