import { useRef, useState } from "react";
import { Link } from "react-router";
import { api } from "../api";
import { Button, CopyButton, Modal, textAreaClass } from "../ui";
import type { WorkflowItem } from "./workflow-model";

const shellQuote = (value: string) => `'${value.replaceAll("'", "'\"'\"'")}'`;

export function WorkflowCall({ id, active }: { id: string; active: boolean }) {
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [item, setItem] = useState<WorkflowItem>();
  const [error, setError] = useState("");
  const [input, setInput] = useState("{}");
  const [loading, setLoading] = useState(false);
  let body = "",
    inputError = "";
  try {
    const value = JSON.parse(input);
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error();
    body = JSON.stringify({ input: value });
  } catch {
    inputError = "请输入有效的 JSON 对象。";
  }
  const endpoint = `${window.location.origin}/v1/workflows/${encodeURIComponent(item?.workflowKey ?? "")}`;
  const command = (async: boolean) => `curl --fail-with-body --request POST \\
  ${shellQuote(endpoint + (async ? "?async=1" : ""))} \\
  --header "Authorization: Bearer \${CONNARA_RUNTIME_TOKEN}" \\
  --header 'Content-Type: application/json' \\
  --data ${shellQuote(body)}`;
  const snippets = [
    { title: "同步调用（最多 40 秒）", value: command(false) },
    { title: "异步调用（返回 202 和 data.operationId）", value: command(true) },
    {
      title: "查询异步结果",
      value: `curl --fail-with-body "${window.location.origin}/v1/workflow-runs/\${OPERATION_ID}" \\
  --header "Authorization: Bearer \${CONNARA_RUNTIME_TOKEN}"`,
    },
  ];
  return (
    <>
      <Button
        ref={trigger}
        variant="secondary"
        disabled={loading}
        onClick={async () => {
          setOpen(true);
          setLoading(true);
          setItem(undefined);
          setError("");
          try {
            const saved = await api.workflow(id);
            setItem(saved);
            setInput(JSON.stringify(saved.input, null, 2));
          } catch (error) {
            setError(String(error));
          } finally {
            setLoading(false);
          }
        }}
      >
        调用说明
      </Button>
      <Modal
        open={open && active}
        onOpenChange={setOpen}
        unsavedChanges={false}
        onCloseAutoFocus={() => trigger.current?.focus()}
        title="调用工作流"
        description="使用运行令牌从外部程序调用"
      >
        {loading && <p role="status">正在读取已保存版本…</p>}
        {error && <p role="alert">{error}</p>}
        {item && (
          <div className="grid min-w-0 gap-4 text-sm">
            <p>
              工作流标识：<code>{item.workflowKey}</code> · 版本 {item.version}
            </p>
            {item.status !== "deployed" && <p role="alert">当前工作流尚未部署或已暂停，部署后才能调用。</p>}
            <p>
              以下说明使用服务器已保存的配置。请先在
              <Link to="/access" className="text-blue-600 underline">
                访问控制
              </Link>
              创建运行令牌，并设置环境变量 CONNARA_RUNTIME_TOKEN。令牌需允许流程内所有 API 操作和绑定账号。
            </p>
            <p>省略 input 使用默认输入；显式传入 input 会整体替换默认输入。下面编辑示例不会保存配置或执行流程。</p>
            <label className="grid gap-2">
              调用输入 JSON
              <textarea
                className={textAreaClass}
                value={input}
                onChange={(event) => setInput(event.target.value)}
                spellCheck={false}
              />
            </label>
            {inputError && <p role="alert">{inputError}</p>}
            {item.graph.inputSchema && (
              <details>
                <summary>输入契约（JSON Schema）</summary>
                <pre className="max-w-full overflow-auto p-3 text-xs">
                  {JSON.stringify(item.graph.inputSchema, null, 2)}
                </pre>
              </details>
            )}
            {!inputError &&
              snippets.map(({ title, value }) => (
                <div key={title} className="min-w-0">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span>{title}</span>
                    <CopyButton value={value} />
                  </div>
                  <pre className="mt-2 max-w-full overflow-auto rounded bg-[var(--muted)] p-3 text-xs">{value}</pre>
                </div>
              ))}
            <p>
              将异步提交返回的 data.operationId 设置为环境变量 OPERATION_ID，再查询运行结果。data.status 为 queued /
              running 时继续查询，success 时读取 data.output；failed / unknown 时查看错误和运行历史。
            </p>
            <p>同步成功时 data 是最终输出。输出字段由结束节点决定。查询需使用发起运行的同一个令牌。</p>
            <p>
              可添加 Idempotency-Key 请求头：同一次请求重试时复用，新请求更换。结果为 unknown
              时先检查运行历史与上游状态。
            </p>
            <p>
              发现可调用的流程：GET /v1/workflows；查询输入契约：GET /v1/workflows/{item.workflowKey}。均使用同一个
              Bearer 令牌。
            </p>
          </div>
        )}
      </Modal>
    </>
  );
}
