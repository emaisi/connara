import { useEffect, useState } from "react";
import { api } from "../api";

export function RequestResponseDetails({ runId, detail }: { runId?: string; detail?: any }) {
  const [loaded, setLoaded] = useState<any>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    setLoaded(null);
    setError("");
    if (!runId || detail) return;
    let cancelled = false;
    void api.operation(runId).then(
      (value) => {
        if (!cancelled) setLoaded(value);
      },
      () => {
        if (!cancelled) setError("请求详情加载失败，请查看运行记录或重试。");
      },
    );
    return () => {
      cancelled = true;
    };
  }, [runId, detail]);
  const value = detail ?? loaded;
  const trace = value?.events?.find((event: any) => event.message === "上游请求与响应")?.attributes;
  const preview = value?.events?.find((event: any) => event.message === "请求目标与定义快照")?.attributes?.request;
  const request = trace?.request ?? preview;
  if (error)
    return (
      <p role="alert" className="text-sm text-red-600">
        {error}
      </p>
    );
  if (!value) return runId ? <p className="text-sm">加载请求详情…</p> : null;
  return (
    <div className="grid min-w-0 gap-3 text-sm">
      <p className="text-[var(--muted-text)]">认证及敏感字段已脱敏；超出日志大小限制的内容显示 truncated。</p>
      {request && (
        <details open>
          <summary className="cursor-pointer font-semibold">
            {trace ? "最终请求（脱敏）" : "请求预览快照（历史记录，未包含最终认证头）"}
          </summary>
          <p className="my-2 break-all font-mono">
            {request.method} {request.url}
          </p>
          <p>请求头</p>
          <pre className="max-h-64 overflow-auto text-xs">{JSON.stringify(request.headers, null, 2)}</pre>
          <p className="mt-2">请求体</p>
          <pre className="max-h-64 overflow-auto text-xs">{JSON.stringify(request.body ?? null, null, 2)}</pre>
        </details>
      )}
      <details open>
        <summary className="cursor-pointer font-semibold">上游响应</summary>
        <p className="my-2">HTTP {trace?.response?.status || value.httpStatus || "—"}</p>
        {trace?.responseReceived === false ? (
          <p>未收到上游响应。</p>
        ) : (
          <>
            <p>响应头</p>
            <pre className="max-h-64 overflow-auto text-xs">
              {trace ? JSON.stringify(trace.response?.headers, null, 2) : "历史记录未保存响应头。"}
            </pre>
            <p className="mt-2">响应体</p>
            <pre className="max-h-80 overflow-auto text-xs">
              {JSON.stringify(trace ? (trace.response?.body ?? null) : (value.output ?? null), null, 2)}
            </pre>
          </>
        )}
      </details>
    </div>
  );
}
