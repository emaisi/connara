import { afterEach, expect, it, vi } from "vitest";
import { api } from "./api";
import { queryClient } from "./query";
afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});
it("fetches each operation poll and capability refresh despite the shared 30 second cache", async () => {
  const fetch = vi.fn(async () => new Response(JSON.stringify({ status: "queued" }), { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  await api.operation("run-1");
  await api.operation("run-1");
  expect(fetch).toHaveBeenCalledTimes(2);
  await api.workflowCapabilities();
  await api.workflowCapabilities();
  expect(fetch).toHaveBeenCalledTimes(4);
  await api.workflow("wf-1");
  await api.workflow("wf-1");
  expect(fetch).toHaveBeenCalledTimes(6);
});
