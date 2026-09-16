import { expect, it, vi, afterEach } from "vitest";
import { api } from "./api";
import { queryClient } from "./query";
afterEach(() => vi.unstubAllGlobals());
it("invalidates only affected resources after a mutation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve(new Response("{}", { status: 200 }))),
  );
  await api.meta();
  await api.settings();
  await api.runtimeTokens();
  const before = vi.mocked(fetch).mock.calls.length;
  await api.saveSettings({ platformName: "New" });
  await api.runtimeTokens();
  expect(vi.mocked(fetch).mock.calls.length).toBe(before + 1);
  await api.meta();
  expect(vi.mocked(fetch).mock.calls.length).toBe(before + 2);
  expect(queryClient.getQueryState(["/api/runtime-tokens"])?.isInvalidated).toBe(false);
});
