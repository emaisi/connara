import { useRef, useState } from "react";
import { Button, Card, Field, MutationForm, Modal, fieldClass, textAreaClass } from "../ui";
import { api, type ApiError } from "../api";
import { useUnsavedChanges } from "../unsaved";
import { ConditionEditor } from "./auth-request-editor";
import { useDemo } from "../demo";

export interface ApiParameter {
  name: string;
  in: string;
  type: string;
  required: boolean;
  default?: unknown;
  example?: unknown;
}
export interface ApiDefinition {
  id: string;
  actionKey: string;
  name: string;
  systemId: string;
  systemKey: string;
  description: string;
  httpMethod: string;
  relativePath: string;
  status: string;
  source: string;
  version: number;
  requestConfig?: {
    schemaVersion: number;
    bodyFormat: string;
    parameters: ApiParameter[];
    headers: { name: string; value: string }[];
  };
  responseConfig?: { statusCodes?: number[]; successCondition?: { path: string; operator: string; value: unknown } };
  executionConfig?: { timeoutMs?: number; retryMode?: string };
  inputSchema?: Record<string, unknown>;
  outputSchema?: Record<string, unknown>;
  exampleInput?: Record<string, unknown>;
  requiredScopes?: string[];
}
type ParameterDraft = ApiParameter & { rowId: number; exampleText: string; defaultText: string };
let nextParameterId = 0;
function valueText(value: unknown) {
  return value === undefined ? "" : typeof value === "string" ? value : JSON.stringify(value);
}
function parameterDraft(p: ApiParameter): ParameterDraft {
  return { ...p, rowId: ++nextParameterId, exampleText: valueText(p.example), defaultText: valueText(p.default) };
}
function parseValue(type: string, text: string): unknown {
  if (text === "") return undefined;
  const value = type === "string" ? text : JSON.parse(text);
  const valid =
    type === "string"
      ? typeof value === "string"
      : type === "boolean"
        ? typeof value === "boolean"
        : type === "array"
          ? Array.isArray(value)
          : type === "object"
            ? value !== null && typeof value === "object" && !Array.isArray(value)
            : typeof value === "number" && Number.isFinite(value) && (type !== "integer" || Number.isInteger(value));
  if (!valid) throw new Error("value does not match type");
  return value;
}
function parsedParameter(p: ParameterDraft): ApiParameter {
  return {
    name: p.name,
    in: p.in,
    type: p.type,
    required: p.required,
    example: parseValue(p.type, p.exampleText),
    default: parseValue(p.type, p.defaultText),
  };
}
function schemaText(value: unknown) {
  return JSON.stringify(value, (_key, item) =>
    item && typeof item === "object" && !Array.isArray(item)
      ? Object.fromEntries(Object.entries(item).sort(([a], [b]) => a.localeCompare(b)))
      : item,
  );
}
export function parameterSchema(parameters: ApiParameter[]) {
  return {
    type: "object",
    properties: Object.fromEntries(
      parameters.map((p) => [
        p.name,
        {
          type: p.type,
          ...(p.type === "array" && p.in === "query" ? { items: { type: ["string", "number", "boolean"] } } : {}),
        },
      ]),
    ),
    required: parameters.filter((p) => p.required && p.default === undefined).map((p) => p.name),
    additionalProperties: false,
  };
}
export function ApiDefinitionEditor({
  initial,
  systemKey,
  onSaved,
  onCancel,
}: {
  initial?: ApiDefinition;
  systemKey: string;
  onSaved: (row: ApiDefinition) => void;
  onCancel: () => void;
}) {
  const demo = useDemo();
  const [system, setSystem] = useState(initial?.systemKey ?? systemKey);
  const [name, setName] = useState(initial?.name ?? "");
  const [key, setKey] = useState(initial?.actionKey ?? "");
  const [method, setMethod] = useState(initial?.httpMethod ?? "GET");
  const [path, setPath] = useState(initial?.relativePath ?? "/");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [status, setStatus] = useState(initial?.status ?? "active");
  const [format, setFormat] = useState(
    initial?.requestConfig?.bodyFormat ??
      (initial?.httpMethod && !["GET", "HEAD"].includes(initial.httpMethod) ? "json" : "none"),
  );
  const [parameters, setParameters] = useState<ParameterDraft[]>(() =>
    (initial?.requestConfig?.parameters ?? []).map(parameterDraft),
  );
  const [headers, setHeaders] = useState(initial?.requestConfig?.headers ?? []);
  const [response, setResponse] = useState(JSON.stringify(initial?.responseConfig ?? {}, null, 2));
  const [output, setOutput] = useState(JSON.stringify(initial?.outputSchema ?? {}, null, 2));
  const [schemaMode, setSchemaMode] = useState(
    Boolean(
      initial &&
      (!initial.requestConfig?.schemaVersion ||
        schemaText(initial.inputSchema ?? {}) !== schemaText(parameterSchema(initial.requestConfig?.parameters ?? []))),
    ),
  );
  const [schema, setSchema] = useState(JSON.stringify(initial?.inputSchema ?? {}, null, 2));
  const [timeout, setTimeout] = useState(initial?.executionConfig?.timeoutMs ?? 30000);
  const [error, setError] = useState("");
  const formRef = useRef<HTMLDivElement>(null);
  const [fieldErrors, setFieldErrors] = useState<{ field: string; path?: string; constraint?: string }[]>([]);
  const valueErrors = new Map<string, string>();
  for (const p of parameters)
    for (const field of ["example", "default"] as const) {
      try {
        parseValue(p.type, p[field === "example" ? "exampleText" : "defaultText"]);
      } catch {
        valueErrors.set(
          `${p.rowId}:${field}`,
          `参数 ${p.name || "未命名"} 的${field === "example" ? "示例" : "默认值"}必须符合所选类型`,
        );
      }
    }
  const mappedParameters = parameters.map((p) => {
    try {
      return parsedParameter(p);
    } catch {
      return { name: p.name, in: p.in, type: p.type, required: p.required };
    }
  });
  const [confirmedPath, setConfirmedPath] = useState(path);
  const [removedPath, setRemovedPath] = useState<string[]>([]);
  const pathParametersBeforeEdit = useRef(parameters);
  function restorePath() {
    setPath(confirmedPath);
    setParameters(pathParametersBeforeEdit.current);
    setRemovedPath([]);
  }

  function focusField(field: string, path?: string) {
    const nodes = Array.from(formRef.current?.querySelectorAll<HTMLElement>("[data-field]") ?? []);
    const target =
      nodes.find((n) => n.dataset.field === field && (!path || n.dataset.path === path)) ??
      nodes.find((n) => n.dataset.field === field);
    const details = target?.closest("details");
    if (details) details.open = true;
    window.setTimeout(() => target?.focus(), 0);
  }
  function readJSON(field: string, text: string) {
    try {
      return JSON.parse(text);
    } catch {
      setFieldErrors([{ field, constraint: "JSON" }]);
      focusField(field);
      throw new Error(`${field} 必须是有效 JSON`);
    }
  }
  const snapshot = JSON.stringify({
    system,
    name,
    key,
    method,
    path,
    description,
    status,
    format,
    parameters,
    headers,
    response,
    output,
    schemaMode,
    schema,
    timeout,
  });
  const [initialSnapshot] = useState(snapshot);
  const dirty = snapshot !== initialSnapshot;

  useUnsavedChanges(dirty);
  const [discard, setDiscard] = useState(false);
  function updateParameter(index: number, patch: Partial<ParameterDraft>) {
    setError("");
    setFieldErrors([]);
    setParameters((rows) => rows.map((p, i) => (i === index ? { ...p, ...patch } : p)));
  }
  function changePath(value: string) {
    setPath(value);
    const placeholders = [...value.matchAll(/\{([A-Za-z0-9_]+)\}/g)].map((m) => m[1]);
    setParameters((rows) => [
      ...rows,
      ...placeholders
        .filter((n) => !rows.some((p) => p.name === n))
        .map((n) => parameterDraft({ name: n, in: "path", type: "string", required: true })),
    ]);
  }
  async function save() {
    setError("");
    setFieldErrors([]);
    try {
      if (removedPath.length) throw new Error("请先确认移除路径参数");
      if (valueErrors.size) {
        const invalid = formRef.current?.querySelector<HTMLElement>("[aria-invalid='true']");
        window.setTimeout(() => invalid?.focus(), 0);
        return;
      }
      const cleanParameters = parameters.map(parsedParameter);
      const selected = demo.customSystems.find((s) => s.service === system);
      if (!selected?.id) throw new Error("请选择所属系统");
      const example = Object.fromEntries(
        cleanParameters.filter((p) => p.example !== undefined).map((p) => [p.name, p.example]),
      );
      const row = await api.saveAction(
        {
          systemId: selected.id,
          actionKey: key.trim(),
          name: name.trim(),
          description,
          status,
          httpMethod: method,
          relativePath: path,
          requiredScopes: initial?.requiredScopes ?? [],
          requestConfig: { schemaVersion: 1, bodyFormat: format, parameters: cleanParameters, headers },
          responseConfig: readJSON("responseConfig", response),
          executionConfig: { timeoutMs: timeout, retryMode: "none" },
          inputSchema: schemaMode ? readJSON("inputSchema", schema) : parameterSchema(cleanParameters),
          outputSchema: readJSON("outputSchema", output),
          exampleInput: example,
          version: initial?.version ?? 0,
        },
        initial?.id,
      );
      onSaved(row);
      await demo.reload();
    } catch (e) {
      setError((e as Error).message);
      const issues = (
        (e as ApiError).details as { fieldErrors?: { field: string; path?: string; constraint?: string }[] } | undefined
      )?.fieldErrors;
      if (issues?.length) {
        setFieldErrors(issues);
        focusField(issues[0].field, issues[0].path);
      }
    }
  }
  return (
    <div ref={formRef} className="grid min-w-0 gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-xl font-bold">{initial ? "编辑 API" : "添加 API"}</h1>
        <Button variant="secondary" onClick={() => (dirty ? setDiscard(true) : onCancel())}>
          返回
        </Button>
      </div>
      <Card className="min-w-0 p-5">
        <MutationForm
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            return save();
          }}
        >
          <Field label="所属系统">
            <select
              className={fieldClass}
              required
              disabled={Boolean(initial)}
              value={system}
              onChange={(e) => setSystem(e.target.value)}
            >
              <option value="">请选择系统</option>
              {demo.customSystems.map((s) => (
                <option key={s.service} value={s.service}>
                  {s.name}
                </option>
              ))}
            </select>
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="API 名称">
              <input className={fieldClass} required value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label="API 标识" hint="在当前系统内唯一，例如 list_resources。">
              <input
                className={fieldClass}
                required
                pattern="[A-Za-z0-9_.-]+"
                value={key}
                onChange={(e) => setKey(e.target.value)}
              />
            </Field>
          </div>
          <Field label="用途说明">
            <input className={fieldClass} value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-[8rem_1fr]">
            <Field label="请求方法">
              <select
                className={fieldClass}
                value={method}
                onChange={(e) => {
                  const next = e.target.value;
                  if (["GET", "HEAD"].includes(next) && parameters.some((p) => p.in === "body")) {
                    setError("请先将请求体参数改为查询参数，再切换到 GET/HEAD。");
                    return;
                  }
                  setMethod(next);
                  if (["GET", "HEAD"].includes(next)) setFormat("none");
                  else if (format === "none") setFormat("json");
                }}
              >
                {["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"].map((m) => (
                  <option key={m}>{m}</option>
                ))}
              </select>
            </Field>
            <Field label="接口路径" hint="追加到集成基础地址后面，可使用 {变量}。">
              <input
                className={fieldClass}
                required
                pattern="/.*"
                value={path}
                data-field="relativePath"
                onFocus={() => {
                  pathParametersBeforeEdit.current = parameters;
                }}
                onChange={(e) => changePath(e.target.value)}
                onBlur={() => {
                  const holders = new Set([...path.matchAll(/\{([A-Za-z0-9_]+)\}/g)].map((m) => m[1]));
                  const removed = parameters.filter((p) => p.in === "path" && !holders.has(p.name)).map((p) => p.name);
                  if (removed.length) setRemovedPath(removed);
                  else {
                    setConfirmedPath(path);
                    pathParametersBeforeEdit.current = parameters;
                  }
                }}
              />
            </Field>
          </div>
          {!["GET", "HEAD"].includes(method) && (
            <Field label="请求体类型">
              <select className={fieldClass} value={format} onChange={(e) => setFormat(e.target.value)}>
                <option value="none">无</option>
                <option value="json">JSON</option>
                <option value="form">表单</option>
              </select>
            </Field>
          )}
          <section className="grid gap-3">
            <div className="flex items-center justify-between">
              <h2 className="font-semibold">请求参数</h2>
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  setParameters((rows) => [
                    ...rows,
                    parameterDraft({
                      name: "",
                      in: format === "none" ? "query" : "body",
                      type: "string",
                      required: false,
                    }),
                  ]);
                }}
              >
                添加参数
              </Button>
            </div>
            {!parameters.length && <p className="text-sm text-[var(--muted-text)]">无需参数时可以直接保存。</p>}
            {parameters.map((p, index) => (
              <div
                key={p.rowId}
                className="grid gap-3 rounded-lg border border-[var(--border)] p-3 sm:grid-cols-2 xl:grid-cols-4"
              >
                <Field label="参数名">
                  <input
                    className={fieldClass}
                    required
                    value={p.name}
                    data-field="requestConfig"
                    data-path={p.name}
                    onChange={(e) => updateParameter(index, { name: e.target.value })}
                  />
                </Field>
                <Field label="位置">
                  <select
                    className={fieldClass}
                    value={p.in}
                    onChange={(e) =>
                      updateParameter(index, { in: e.target.value, required: e.target.value === "path" || p.required })
                    }
                  >
                    {["path", "query", "header", ...(format !== "none" ? ["body"] : [])].map((v) => (
                      <option key={v}>{v}</option>
                    ))}
                  </select>
                </Field>
                <Field label="类型">
                  <select
                    className={fieldClass}
                    value={p.type}
                    onChange={(e) => updateParameter(index, { type: e.target.value })}
                  >
                    {[
                      "string",
                      "integer",
                      "number",
                      "boolean",
                      ...(p.in === "body" && format === "json"
                        ? ["object", "array"]
                        : p.in === "query"
                          ? ["array"]
                          : []),
                    ].map((v) => (
                      <option key={v}>{v}</option>
                    ))}
                  </select>
                </Field>
                <Field label="示例">
                  <input
                    className={fieldClass}
                    value={p.exampleText}
                    aria-invalid={valueErrors.has(`${p.rowId}:example`)}
                    data-field="requestConfig"
                    data-path={p.name}
                    onChange={(e) => updateParameter(index, { exampleText: e.target.value })}
                  />
                </Field>
                <Field label="默认值">
                  <input
                    className={fieldClass}
                    value={p.defaultText}
                    aria-invalid={valueErrors.has(`${p.rowId}:default`)}
                    data-field="requestConfig"
                    data-path={p.name}
                    onChange={(e) => updateParameter(index, { defaultText: e.target.value })}
                  />
                </Field>
                {["example", "default"].map(
                  (field) =>
                    valueErrors.has(`${p.rowId}:${field}`) && (
                      <p key={field} role="alert" className="text-sm text-red-600">
                        {valueErrors.get(`${p.rowId}:${field}`)}
                      </p>
                    ),
                )}
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={p.required}
                    disabled={p.in === "path"}
                    onChange={(e) => updateParameter(index, { required: e.target.checked })}
                  />
                  必填
                </label>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => {
                    setParameters((rows) => rows.filter((_, i) => i !== index));
                  }}
                >
                  删除参数
                </Button>
              </div>
            ))}
          </section>
          <details className="min-w-0 rounded-lg border p-4">
            <summary className="cursor-pointer font-semibold">高级选项</summary>
            <div className="mt-4 grid gap-4">
              <section className="grid gap-2">
                <h3>固定请求头</h3>
                {headers.map((h, index) => (
                  <div key={index} className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
                    <input
                      aria-label="请求头名称"
                      className={fieldClass}
                      data-field="requestConfig"
                      value={h.name}
                      onChange={(e) =>
                        setHeaders((rows) => rows.map((r, i) => (i === index ? { ...r, name: e.target.value } : r)))
                      }
                    />
                    <input
                      aria-label="请求头值"
                      className={fieldClass}
                      value={h.value}
                      onChange={(e) =>
                        setHeaders((rows) => rows.map((r, i) => (i === index ? { ...r, value: e.target.value } : r)))
                      }
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      onClick={() => {
                        setHeaders((rows) => rows.filter((_, i) => i !== index));
                      }}
                    >
                      删除
                    </Button>
                  </div>
                ))}
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => {
                    setHeaders((rows) => [...rows, { name: "", value: "" }]);
                  }}
                >
                  添加请求头
                </Button>
                <p className="text-sm text-[var(--muted-text)]">认证信息由执行账号注入，请勿填写 Token 或密码。</p>
              </section>
              <Field label="成功 HTTP 状态" hint="留空允许全部 2xx；多个状态用逗号分隔。">
                <input
                  className={fieldClass}
                  data-field="responseConfig"
                  value={(JSON.parse(response).statusCodes ?? []).join(",")}
                  onChange={(e) =>
                    setResponse(
                      JSON.stringify({
                        ...JSON.parse(response),
                        statusCodes: e.target.value.trim()
                          ? e.target.value.split(",").map((v) => Number(v.trim()))
                          : [],
                      }),
                    )
                  }
                />
              </Field>
              <ConditionEditor
                value={JSON.parse(response).successCondition}
                onChange={(value) => {
                  setResponse(JSON.stringify({ ...JSON.parse(response), successCondition: value }));
                }}
              />
              <Field label="输出结构 Schema JSON" hint="留空对象表示不检查响应结构。">
                <textarea
                  className={textAreaClass}
                  data-field="outputSchema"
                  value={output}
                  onChange={(e) => setOutput(e.target.value)}
                />
              </Field>
              <label className="flex gap-2 text-sm">
                <input type="checkbox" checked={schemaMode} onChange={(e) => setSchemaMode(e.target.checked)} />
                使用自定义输入 Schema
              </label>
              <Field label="输入约束 Schema JSON">
                <textarea
                  className={textAreaClass}
                  data-field="inputSchema"
                  readOnly={!schemaMode}
                  value={schemaMode ? schema : JSON.stringify(parameterSchema(mappedParameters), null, 2)}
                  onChange={(e) => setSchema(e.target.value)}
                />
              </Field>
              <Field label="超时（毫秒）">
                <input
                  type="number"
                  className={fieldClass}
                  min={100}
                  max={40000}
                  data-field="executionConfig"
                  value={timeout}
                  onChange={(e) => setTimeout(Number(e.target.value))}
                />
              </Field>
            </div>
          </details>
          <Field label="定义状态">
            <select className={fieldClass} value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="active">可调用</option>
              <option value="draft">草稿</option>
              <option value="disabled">停用</option>
            </select>
          </Field>
          {fieldErrors.map((issue, i) => (
            <button
              key={i}
              type="button"
              className="text-left text-sm text-red-600 underline"
              onClick={() => focusField(issue.field, issue.path)}
            >
              {issue.field}
              {issue.path ? ` · ${issue.path}` : ""}: {issue.constraint}
            </button>
          ))}
          {error && (
            <p role="alert" className="break-words text-sm text-red-600">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => (dirty ? setDiscard(true) : onCancel())}>
              取消
            </Button>
            <Button write type="submit">
              保存
            </Button>
          </div>
        </MutationForm>
      </Card>
      <Modal
        open={removedPath.length > 0}
        onOpenChange={(open) => {
          if (!open) {
            restorePath();
          }
        }}
        title="移除路径参数？"
        description={`路径中已没有 ${removedPath.join("、")}。移除这些参数会清除其示例和默认值。`}
      >
        <div className="flex justify-end gap-2">
          <Button
            variant="secondary"
            onClick={() => {
              restorePath();
            }}
          >
            保留原路径
          </Button>
          <Button
            onClick={() => {
              setParameters((rows) => rows.filter((p) => !(p.in === "path" && removedPath.includes(p.name))));
              setConfirmedPath(path);
              setRemovedPath([]);
            }}
          >
            确认移除参数
          </Button>
        </div>
      </Modal>
      <Modal
        open={discard}
        onOpenChange={setDiscard}
        title="放弃未保存的修改？"
        description="尚未保存的 API 输入将被清除。"
      >
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setDiscard(false)}>
            继续编辑
          </Button>
          <Button onClick={onCancel}>放弃修改</Button>
        </div>
      </Modal>
    </div>
  );
}
