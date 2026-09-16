import { Component, type ReactNode } from "react";
export class PageErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null };
  static getDerivedStateFromError(error: unknown) {
    return { error: error instanceof Error ? error : new Error(String(error)) };
  }
  render() {
    if (this.state.error)
      return (
        <main role="alert" className="mx-auto grid max-w-xl gap-4 p-8">
          <h1 className="text-xl font-bold">页面暂时无法显示</h1>
          <p>{this.state.error.message || "页面加载发生错误，请重新加载。"}</p>
          <button className="rounded-lg border p-3" onClick={() => window.location.reload()}>
            重新加载页面
          </button>
        </main>
      );
    return this.props.children;
  }
}
