import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { LanguageProvider, useLanguage } from "./i18n";

const values = new Map<string, string>();

beforeEach(() => {
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
      clear: () => values.clear(),
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
    },
  });
});

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

function Example() {
  const { setLanguage } = useLanguage();
  return (
    <div>
      <button onClick={() => setLanguage("en")}>switch</button>
      <span>系统目录</span>
      <input aria-label="邮箱账号" placeholder="请输入登录密码" />
    </div>
  );
}

function DynamicExample({ text }: { text: string }) {
  const { language, setLanguage } = useLanguage();
  return (
    <div>
      <button onClick={() => setLanguage(language === "en" ? "zh-CN" : "en")}>toggle</button>
      <span data-testid="status" title={text}>
        {text}
      </span>
      <input data-testid="field" placeholder={text} aria-label={text} />
    </div>
  );
}

describe("language switching", () => {
  it("translates visible text and accessible labels and persists the language", () => {
    render(
      <LanguageProvider>
        <Example />
      </LanguageProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "switch" }));
    expect(screen.getByText("System Catalog")).toBeInTheDocument();
    expect(screen.getByLabelText("Email Account")).toHaveAttribute("placeholder", "Enter your password");
    expect(window.localStorage.getItem("apihub.language")).toBe("en");
    expect(document.documentElement.lang).toBe("en");
  });

  it("keeps asynchronous loading and pagination updates in Chinese", async () => {
    const view = (text: string) => (
      <LanguageProvider>
        <DynamicExample text={text} />
      </LanguageProvider>
    );
    const { rerender } = render(view("正在加载…"));
    await act(async () => rerender(view("已加载 100 条")));
    expect(screen.getByTestId("status")).toHaveTextContent("已加载 100 条");
    await act(async () => rerender(view("已加载 105 条")));
    expect(screen.getByTestId("status")).toHaveTextContent("已加载 105 条");
  });

  it.each(["zh-CN", "en"])("preserves updated text and attributes when switching from %s", async (language) => {
    window.localStorage.setItem("apihub.language", language);
    const view = (text: string) => (
      <LanguageProvider>
        <DynamicExample text={text} />
      </LanguageProvider>
    );
    const { rerender } = render(view("系统目录"));
    await act(async () => rerender(view("概览")));
    const expected = language === "en" ? "Overview" : "概览";
    expect(screen.getByTestId("status")).toHaveTextContent(expected);
    expect(screen.getByTestId("status")).toHaveAttribute("title", expected);
    expect(screen.getByTestId("field")).toHaveAttribute("placeholder", expected);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "toggle" })));
    const switched = language === "en" ? "概览" : "Overview";
    expect(screen.getByTestId("status")).toHaveTextContent(switched);
    expect(screen.getByTestId("field")).toHaveAttribute("aria-label", switched);
    await act(async () => rerender(view("ready")));
    expect(screen.getByTestId("status")).toHaveTextContent("ready");
    expect(screen.getByTestId("field")).toHaveAttribute("placeholder", "ready");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "toggle" })));
    expect(screen.getByTestId("status")).toHaveTextContent("ready");
    expect(screen.getByTestId("field")).toHaveAttribute("aria-label", "ready");
  });
});
