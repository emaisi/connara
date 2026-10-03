import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { RequestResponseDetails } from "./request-response-details";

afterEach(cleanup);
it("shows final request and response sections", () => {
  render(
    <RequestResponseDetails
      detail={{
        events: [
          {
            message: "上游请求与响应",
            attributes: {
              request: {
                method: "POST",
                url: "https://example.com/items",
                headers: { Authorization: "[REDACTED]" },
                body: { page: 1 },
              },
              responseReceived: true,
              response: { status: 200, headers: { "X-Request-Id": "upstream-1" }, body: { ok: true } },
            },
          },
        ],
      }}
    />,
  );
  expect(screen.getByText("最终请求（脱敏）")).toBeTruthy();
  expect(screen.getByText("响应头")).toBeTruthy();
  expect(screen.getByText(/upstream-1/)).toBeTruthy();
  expect(screen.getByText(/REDACTED/)).toBeTruthy();
});
it("distinguishes historical previews from final authenticated requests", () => {
  render(
    <RequestResponseDetails
      detail={{
        httpStatus: 200,
        output: { ok: true },
        events: [
          {
            message: "请求目标与定义快照",
            attributes: { request: { method: "GET", url: "https://example.com/items", headers: {} } },
          },
        ],
      }}
    />,
  );
  expect(screen.getByText("历史记录未保存响应头。")).toBeTruthy();
  expect(screen.getByText("请求预览快照（历史记录，未包含最终认证头）")).toBeTruthy();
});
