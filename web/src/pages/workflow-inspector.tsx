import { createContext, useContext, lazy, Suspense, useEffect, useState, useRef } from "react";
import { api } from "../api";
import { Button, Field, Modal, fieldClass, textAreaClass } from "../ui";
import {
  dependencies,
  emptySchema,
  fieldPaths,
  samplePaths,
  kindLabel,
  stableJSON,
  renameVariable,
  codeTemplates,
  assertSchemaShape,
  assertDefinitionShape,
  type Assignment,
  type Definition,
  type Schema,
  type Step,
  type WorkflowItem,
} from "./workflow-model";
export const JsonBufferContext = createContext<{
  prefix: string;
  buffers: Record<string, { text: string; invalid: boolean; base: string }>;
  samples?: Record<string, any>;
  invalidate: (key: string, invalid: boolean) => void;
} | null>(null);
const CodeEditor = lazy(() => import("./workflow-code-editor"));
export interface ActionChoice {
  id: string;
  version?: number;
  actionKey: string;
  name: string;
  systemKey: string;
  inputSchema?: Schema;
  outputSchema?: Schema;
  executable: boolean;
  status: string;
}
export function JsonField({
  label,
  value,
  onChange,
  onInvalid,
  validate,
}: {
  validate?: (value: any) => void;
  label: string;
  value: unknown;
  onChange: (value: any) => void;
  onInvalid?: (invalid: boolean) => void;
}) {
  const buffer = useContext(JsonBufferContext);
  const key = `${buffer?.prefix}/${label}`;
  const base = JSON.stringify(value === undefined ? {} : value);
  const previous = buffer?.buffers[key];
  const [text, setText] = useState(() =>
    previous?.base === base ? previous.text : JSON.stringify(value === undefined ? {} : value, null, 2),
  );
  const [error, setError] = useState("");
  useEffect(() => {
    const previous = buffer?.buffers[key];
    const keep = previous?.base === base;
    setText(keep ? previous.text : JSON.stringify(value === undefined ? {} : value, null, 2));
    setError(keep && previous.invalid ? "JSON 尚未完整，保存前请修正" : "");
    onInvalid?.(!!(keep && previous.invalid));
    buffer?.invalidate(key, !!(keep && previous.invalid));
  }, [value]);
  return (
    <Field label={label}>
      <textarea
        aria-label={label}
        className={textAreaClass + " min-h-24 font-mono text-xs"}
        value={text}
        onChange={(event) => {
          const next = event.target.value;
          setText(next);
          try {
            const parsed = JSON.parse(next);
            const check = (v: any): void => {
              if (typeof v === "number" && Number.isInteger(v) && !Number.isSafeInteger(v))
                throw new Error("整数超出安全范围，请使用字符串");
              if (v && typeof v === "object") Object.values(v).forEach(check);
            };
            check(parsed);
            if (
              value !== null &&
              typeof value === "object" &&
              (parsed === null || typeof parsed !== "object" || Array.isArray(parsed) !== Array.isArray(value))
            )
              throw new Error(Array.isArray(value) ? "此字段必须是数组" : "此字段必须是对象");
            validate?.(parsed);
            const valueToApply = parsed;
            setError("");
            onInvalid?.(false);
            if (buffer) {
              buffer.buffers[key] = { text: next, invalid: false, base: JSON.stringify(valueToApply) };
              buffer.invalidate(key, false);
            }
            onChange(valueToApply);
          } catch (error) {
            setError(error instanceof SyntaxError ? "JSON 尚未完整，保存前请修正" : String(error));
            onInvalid?.(true);
            if (buffer) {
              buffer.buffers[key] = { text: next, invalid: true, base };
              buffer.invalidate(key, true);
            }
          }
        }}
      />
      {error && (
        <p role="alert" className="text-xs text-red-600">
          {error}
        </p>
      )}
    </Field>
  );
}
export function SchemaFields({
  schema,
  onChange,
  allRequired = false,
}: {
  schema: Schema | undefined;
  onChange: (value: Schema) => void;
  allRequired?: boolean;
}) {
  const value = schema ?? emptySchema();
  const entries = Object.entries(value.properties ?? {});
  const set = (name: string, newName: string, field: Schema) => {
    const properties = { ...value.properties };
    delete properties[name];
    properties[newName] = field;
    const required = (value.required ?? []).map((key) => (key === name ? newName : key));
    if (allRequired && !required.includes(newName)) required.push(newName);
    onChange({ ...value, type: "object", properties, required });
  };
  return (
    <div className="grid gap-2">
      {entries.map(([name, field], index) => (
        <div
          key={index}
          className="grid grid-cols-[minmax(0,1fr)_110px] gap-2 rounded-lg border border-[var(--border)] p-2"
        >
          <input
            className={fieldClass}
            aria-label="字段名称"
            value={name}
            onChange={(e) => {
              if (e.target.value && !value.properties?.[e.target.value]) set(name, e.target.value, field);
            }}
          />
          <select
            className={fieldClass}
            aria-label={`${name} 类型`}
            value={Array.isArray(field.type) ? field.type.find((type) => type !== "null") : field.type}
            onChange={(e) =>
              set(name, name, { ...field, type: Array.isArray(field.type) ? [e.target.value, "null"] : e.target.value })
            }
          >
            {["string", "number", "integer", "boolean", "object", "array"].map((type) => (
              <option key={type}>{type}</option>
            ))}
          </select>
          <label className="text-xs">
            <input
              type="checkbox"
              checked={allRequired || (value.required ?? []).includes(name)}
              disabled={allRequired}
              onChange={(e) =>
                onChange({
                  ...value,
                  required: e.target.checked
                    ? [...(value.required ?? []), name]
                    : (value.required ?? []).filter((key) => key !== name),
                })
              }
            />{" "}
            必填
          </label>
          <label className="text-xs">
            <input
              type="checkbox"
              checked={Array.isArray(field.type)}
              onChange={(e) =>
                set(name, name, {
                  ...field,
                  type: e.target.checked
                    ? [Array.isArray(field.type) ? field.type[0] : field.type, "null"]
                    : Array.isArray(field.type)
                      ? field.type[0]
                      : field.type,
                })
              }
            />{" "}
            允许 null
          </label>
          <input
            className={fieldClass + " col-span-2"}
            aria-label={`${name} 说明`}
            placeholder="字段说明"
            value={field.description ?? ""}
            onChange={(e) => set(name, name, { ...field, description: e.target.value })}
          />
          <Button
            variant="ghost"
            onClick={() => {
              const properties = { ...value.properties };
              delete properties[name];
              onChange({ ...value, properties, required: (value.required ?? []).filter((key) => key !== name) });
            }}
          >
            删除字段
          </Button>
        </div>
      ))}
      <Button
        variant="secondary"
        onClick={() => {
          let index = 1;
          while (value.properties?.[`field${index}`]) index++;
          const name = `field${index}`;
          onChange({
            ...value,
            properties: { ...value.properties, [name]: { type: "string" } },
            required: allRequired ? [...(value.required ?? []), name] : (value.required ?? []),
          });
        }}
      >
        添加字段
      </Button>
      <details>
        <summary className="cursor-pointer text-xs">嵌套 Schema / JSON</summary>
        <JsonField label="Schema JSON" value={value} validate={assertSchemaShape} onChange={onChange} />
      </details>
    </div>
  );
}
export function SourcePicker({
  value,
  onChange,
  graph,
  step,
  actions,
  includeCurrent = false,
  initial = false,
  label = "值来源",
  allowUnset = false,
  allowOptional = false,
  sourceScope,
}: {
  value: unknown;
  onChange: (value: unknown) => void;
  graph: Definition;
  step?: Step;
  actions: ActionChoice[];
  includeCurrent?: boolean;
  initial?: boolean;
  label?: string;
  allowUnset?: boolean;
  allowOptional?: boolean;
  sourceScope?: Step["scope"];
}) {
  const [selectedMode, setSelectedMode] = useState<string | null>(null);
  const sampleSessions = useContext(JsonBufferContext)?.samples;
  const sampleDifferences: string[] = [];
  const marker = value && typeof value === "object" && !Array.isArray(value) ? (value as any).$value : undefined;
  const optional = typeof value === "string" && value.startsWith("{{?");
  const ref = typeof value === "string" && /^\{\{.+\}\}$/.test(value) ? value.slice(optional ? 3 : 2, -2) : "";
  const writeRef = (path: string) => onChange(`{{${optional && allowOptional ? "?" : ""}${path}}}`);
  const inferred =
    marker?.kind === "literal"
      ? "literal"
      : marker?.kind === "run"
        ? "run"
        : marker?.kind === "branch"
          ? "branch"
          : ref.startsWith("trigger")
            ? "trigger"
            : ref.startsWith("status.")
              ? "status"
              : ref.startsWith("vars.")
                ? "vars"
                : ref
                  ? "upstream"
                  : typeof value === "string" && value.includes("{{")
                    ? "template"
                    : "fixed";
  const mode = selectedMode ?? (allowUnset && value === undefined ? "unset" : inferred);
  const scope = sourceScope ?? step?.scope ?? [];
  const [search, setSearch] = useState("");
  const allowed = step ? dependencies(graph, step.id) : new Set(graph.steps.map((step) => step.id));
  if (includeCurrent && step) allowed.add(step.id);
  const paths =
    mode === "status"
      ? graph.steps
          .filter((source) => allowed.has(source.id))
          .map((source) => ({
            path: `status.${source.id}`,
            label: `${source.title ?? source.id} · 执行状态`,
          }))
      : mode === "trigger"
        ? fieldPaths(graph.inputSchema, "trigger")
        : mode === "vars"
          ? (graph.variables ?? []).map((variable) => ({
              path: `vars.${variable.name}`,
              label: `${variable.name} · ${variable.type}`,
            }))
          : graph.steps
              .filter((source) => allowed.has(source.id))
              .flatMap((source) => {
                const available = (source.scope ?? []).every((sourceBranch) =>
                  scope.some(
                    (current) =>
                      current.conditionId === sourceBranch.conditionId && current.branchId === sourceBranch.branchId,
                  ),
                );
                if (!available && !allowOptional) return [];
                const schema =
                  source.type === "code"
                    ? source.outputSchema
                    : source.type === "transform"
                      ? {
                          type: "object",
                          properties: {
                            result: { type: source.operations?.at(-1)?.op === "count" ? "integer" : "array" },
                          },
                        }
                      : source.type === "condition"
                        ? { type: "object", properties: { branchId: { type: "string" } } }
                        : actions.find((action) => action.id === source.action || action.actionKey === source.action)
                            ?.outputSchema;
                const sample =
                  sampleSessions?.[step?.id ?? "@result"]?.samplesByMode?.workflow?.sampleOutputs?.[source.id] ??
                  sampleSessions?.[step?.id ?? "@result"]?.sample?.sampleOutputs?.[source.id] ??
                  sampleSessions?.[source.id]?.samplesByMode?.workflow?.sampleResult ??
                  sampleSessions?.[source.id]?.sample?.sampleResult;
                const declared = fieldPaths(schema, source.id);
                if (sample !== undefined) {
                  const inferred = samplePaths(sample, source.id);
                  if (!schema) return inferred;
                  if (inferred.some((field) => !declared.some((known) => field.path === known.path)))
                    sampleDifferences.push(source.title ?? source.id);
                }
                return declared;
              });
  return (
    <div className="grid gap-2 rounded-lg border border-[var(--border)] p-2">
      <label className="text-xs font-semibold">{label}</label>
      <select
        aria-label={`${label}来源类型`}
        className={fieldClass}
        value={mode}
        onChange={(e) => {
          setSelectedMode(e.target.value);
          if (e.target.value === "unset") onChange(undefined);
          if (e.target.value === "literal")
            onChange({
              $value: { kind: "literal", value: marker?.kind === "literal" ? marker.value : (value ?? null) },
            });
          if (mode === "literal" && e.target.value === "fixed") onChange(marker.value);
        }}
      >
        {allowUnset && <option value="unset">未设置（沿用 API 默认值）</option>}
        <option value="fixed">固定值</option>
        {graph.schemaVersion === 2 && <option value="literal">字面值（保留模板和 $value）</option>}
        <option value="trigger">开始输入</option>
        <option value="run">运行信息（本次固定值）</option>
        {!initial && (
          <>
            <option value="vars">流程变量</option>
            <option value="upstream">上游结果</option>
            <option value="status">上游状态</option>
            <option value="branch">按分支选择</option>
            <option value="template">组合文本 / 手工路径</option>
          </>
        )}
      </select>
      {mode === "literal" ? (
        <JsonField
          label={`${label}字面 JSON`}
          value={marker?.value ?? null}
          onChange={(value) => onChange({ $value: { kind: "literal", value } })}
        />
      ) : mode === "unset" ? (
        <span className="text-xs">未发送这个字段；显式 null 请选固定值。</span>
      ) : mode === "run" ? (
        <select
          className={fieldClass}
          aria-label={`${label}运行字段`}
          value={marker?.field ?? ""}
          onChange={(e) => onChange({ $value: { kind: "run", field: e.target.value } })}
        >
          <option value="">选择运行字段</option>
          {["triggeredAt", "scheduledFor", "timezone", "businessDate"].map((field) => (
            <option key={field} value={field}>
              {field}
              {field === "scheduledFor" ? "（仅定时运行有值）" : ""}
            </option>
          ))}
        </select>
      ) : mode === "trigger" || mode === "vars" || mode === "upstream" || mode === "status" ? (
        <>
          <input
            className={fieldClass}
            aria-label={`${label}搜索字段`}
            placeholder="搜索字段名或说明"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <select
            className={fieldClass}
            aria-label={`${label}字段`}
            value={ref}
            onChange={(e) => writeRef(e.target.value)}
          >
            <option value="">选择字段</option>
            {ref && !paths.some((path) => path.path === ref) && <option value={ref}>{ref}（手工路径）</option>}
            {paths
              .filter((path) => (path.label + path.path).toLowerCase().includes(search.toLowerCase()))
              .map((path) => (
                <option key={path.path} value={path.path}>
                  {path.label}
                </option>
              ))}
          </select>
          <input
            className={fieldClass}
            aria-label={`${label}手工路径`}
            placeholder="没有 Schema 时手工填写路径"
            value={ref}
            onChange={(e) => writeRef(e.target.value)}
          />
          {!!sampleDifferences.length && (
            <p className="text-xs">
              {sampleDifferences.join("、")} 的样本包含声明外字段；字段列表按 Schema 展示，可手工填写路径。
            </p>
          )}
          {allowOptional && graph.schemaVersion === 2 && mode === "upstream" && (
            <label className="text-xs">
              <input
                type="checkbox"
                checked={optional}
                onChange={(e) => onChange(`{{${e.target.checked ? "?" : ""}${ref}}}`)}
              />{" "}
              此结束字段可缺省（未命中分支时返回 null）
            </label>
          )}
        </>
      ) : mode === "template" ? (
        <input
          className={fieldClass}
          aria-label={`${label}模板`}
          value={typeof value === "string" ? value : ""}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : mode === "branch" ? (
        <>
          <select
            className={fieldClass}
            aria-label="来源条件节点"
            value={marker?.conditionId ?? ""}
            onChange={(e) => {
              const condition = graph.steps.find((step) => step.id === e.target.value);
              onChange({
                $value: {
                  kind: "branch",
                  conditionId: e.target.value,
                  cases: Object.fromEntries((condition?.branches ?? []).map((branch) => [branch.id, null])),
                },
              });
            }}
          >
            <option value="">选择条件</option>
            {graph.steps
              .filter((step) => step.type === "condition" && allowed.has(step.id))
              .map((step) => (
                <option key={step.id} value={step.id}>
                  {step.title ?? step.id}
                </option>
              ))}
          </select>
          {Object.entries(marker?.cases ?? {}).map(([key, item]) => (
            <SourcePicker
              key={key}
              label={`出口 ${key}`}
              value={item}
              onChange={(next) => onChange({ $value: { ...marker, cases: { ...marker.cases, [key]: next } } })}
              graph={graph}
              actions={actions}
              step={step}
              includeCurrent={includeCurrent}
              allowOptional={allowOptional}
              sourceScope={[
                ...(graph.steps.find((row) => row.id === marker.conditionId)?.scope ?? []),
                { conditionId: marker.conditionId, branchId: key },
                ...scope.filter(
                  (row) =>
                    row.conditionId !== marker.conditionId &&
                    !(graph.steps.find((node) => node.id === marker.conditionId)?.scope ?? []).some(
                      (parent) => parent.conditionId === row.conditionId,
                    ),
                ),
              ]}
            />
          ))}
        </>
      ) : (
        <>
          <select
            className={fieldClass}
            aria-label={`${label}固定类型`}
            value={value === null ? "null" : Array.isArray(value) ? "array" : typeof value}
            onChange={(e) =>
              onChange(
                ({ string: "", number: 0, boolean: false, object: {}, array: [], null: null } as any)[e.target.value],
              )
            }
          >
            {["string", "number", "boolean", "object", "array", "null"].map((type) => (
              <option key={type}>{type}</option>
            ))}
          </select>
          {typeof value === "string" ? (
            <input
              className={fieldClass}
              aria-label={`${label}固定值`}
              value={value}
              onChange={(e) => onChange(e.target.value)}
            />
          ) : typeof value === "boolean" ? (
            <select
              className={fieldClass}
              aria-label={`${label}布尔值`}
              value={String(value)}
              onChange={(e) => onChange(e.target.value === "true")}
            >
              <option>true</option>
              <option>false</option>
            </select>
          ) : typeof value === "number" ? (
            <input
              className={fieldClass}
              type="number"
              aria-label={`${label}数字`}
              value={value}
              onChange={(e) => {
                if (e.target.value !== "") onChange(Number(e.target.value));
              }}
            />
          ) : value !== null ? (
            <JsonField label="固定 JSON 值" value={value} onChange={onChange} />
          ) : (
            <span className="text-xs">显式 null</span>
          )}
        </>
      )}
    </div>
  );
}
function Assignments({
  value,
  onChange,
  graph,
  step,
  actions,
}: {
  value: Assignment[];
  onChange: (value: Assignment[]) => void;
  graph: Definition;
  step: Step;
  actions: ActionChoice[];
}) {
  return (
    <div className="grid gap-2">
      <p className="text-sm font-semibold">更新流程变量</p>
      {value.map((assignment, index) => (
        <div className="grid gap-2" key={index}>
          <select
            className={fieldClass}
            aria-label="赋值目标变量"
            value={assignment.variable}
            onChange={(e) =>
              onChange(value.map((row, i) => (i === index ? { ...row, variable: e.target.value } : row)))
            }
          >
            <option value="">选择变量</option>
            {graph.variables?.map((variable) => (
              <option key={variable.name}>{variable.name}</option>
            ))}
          </select>
          <SourcePicker
            value={assignment.value}
            onChange={(next) => onChange(value.map((row, i) => (i === index ? { ...row, value: next } : row)))}
            graph={graph}
            step={step}
            actions={actions}
            includeCurrent={step.type !== "condition"}
            label={`${assignment.variable || "变量"}新值`}
          />
          <Button variant="ghost" onClick={() => onChange(value.filter((_, i) => i !== index))}>
            删除赋值
          </Button>
        </div>
      ))}
      <Button
        variant="secondary"
        onClick={() => onChange([...value, { variable: graph.variables?.[0]?.name ?? "", value: null }])}
      >
        添加赋值
      </Button>
    </div>
  );
}
function ConditionFields({
  value,
  onChange,
  paths,
  item = false,
  graph,
  actions = [],
  step,
}: {
  value: Record<string, any>;
  onChange: (value: Record<string, any>) => void;
  paths: { path: string; label: string }[];
  item?: boolean;
  graph?: Definition;
  actions?: ActionChoice[];
  step?: Step;
}) {
  const group = value.all ? "all" : value.any ? "any" : null;
  if (group)
    return (
      <div className="grid gap-2 rounded border border-[var(--border)] p-2">
        <select
          className={fieldClass}
          aria-label="条件组规则"
          value={group}
          onChange={(e) => onChange({ [e.target.value]: value[group] })}
        >
          <option value="all">全部满足 all</option>
          <option value="any">任一满足 any</option>
        </select>
        {value[group].map((child: Record<string, any>, index: number) => (
          <div key={index} className="grid gap-2">
            <ConditionFields
              value={child}
              paths={paths}
              item={item}
              graph={graph}
              actions={actions}
              step={step}
              onChange={(next) =>
                onChange({ [group]: value[group].map((row: any, i: number) => (i === index ? next : row)) })
              }
            />
            <Button
              variant="ghost"
              disabled={value[group].length <= 1}
              onClick={() => onChange({ [group]: value[group].filter((_: any, i: number) => i !== index) })}
            >
              删除条件
            </Button>
          </div>
        ))}
        <Button
          variant="secondary"
          onClick={() =>
            onChange({
              [group]: [...value[group], { [item ? "itemPath" : "path"]: item ? "" : "trigger.value", op: "exists" }],
            })
          }
        >
          添加条件
        </Button>
      </div>
    );
  const field = item ? "itemPath" : "path";
  return (
    <div className="grid gap-2">
      <input
        className={fieldClass}
        list={item ? undefined : "workflow-condition-paths"}
        aria-label={item ? "元素字段路径" : "条件字段路径"}
        placeholder={item ? "相对元素路径；留空表示元素本身" : "选择或填写来源路径"}
        value={String(value[field] ?? "")}
        onChange={(e) => onChange({ ...value, [field]: e.target.value })}
      />
      {!item && (
        <datalist id="workflow-condition-paths">
          {paths.map((path) => (
            <option key={path.path} value={path.path}>
              {path.label}
            </option>
          ))}
        </datalist>
      )}
      <select
        className={fieldClass}
        aria-label="比较操作"
        value={value.op ?? "exists"}
        onChange={(e) => onChange({ ...value, op: e.target.value })}
      >
        {["eq", "ne", "gt", "gte", "lt", "lte", "exists", "not_exists"].map((op) => (
          <option key={op}>{op}</option>
        ))}
      </select>
      {value.op !== "exists" &&
        value.op !== "not_exists" &&
        (graph ? (
          <SourcePicker
            label="比较值"
            value={value.value ?? null}
            graph={graph}
            actions={actions}
            step={step}
            onChange={(next) => onChange({ ...value, value: next })}
          />
        ) : (
          <JsonField
            label="比较值"
            value={value.value ?? null}
            onChange={(next) => onChange({ ...value, value: next })}
          />
        ))}
      <Button
        variant="secondary"
        onClick={() => onChange({ all: [value, { [field]: item ? "" : "trigger.value", op: "exists" }] })}
      >
        组合 all / any
      </Button>
    </div>
  );
}
function OperationFields({
  value,
  onChange,
  graph,
  step,
  actions,
}: {
  value: Record<string, any>;
  onChange: (value: Record<string, any>) => void;
  graph: Definition;
  step: Step;
  actions: ActionChoice[];
}) {
  if (value.op === "count") return <p className="text-xs">返回当前列表的数量（空列表为 0）。</p>;
  if (value.op === "filter")
    return (
      <ConditionFields
        item
        value={value.condition ?? { itemPath: "", op: "exists" }}
        onChange={(condition) => onChange({ ...value, condition })}
        paths={[]}
        graph={graph}
        step={step}
        actions={actions}
      />
    );
  if (value.op === "slice")
    return (
      <div className="grid grid-cols-2 gap-2">
        {["offset", "limit"].map((field) => (
          <Field key={field} label={field === "offset" ? "起始位置" : "最多条数"}>
            <input
              className={fieldClass}
              type="number"
              min="0"
              step="1"
              value={value[field] ?? 0}
              onChange={(e) => {
                if (e.target.value !== "") onChange({ ...value, [field]: Number(e.target.value) });
              }}
            />
          </Field>
        ))}
      </div>
    );
  if (value.op === "sort")
    return (
      <div className="grid gap-2">
        <Field label="排序字段（留空表示元素本身）">
          <input
            className={fieldClass}
            value={value.path ?? ""}
            onChange={(e) => onChange({ ...value, path: e.target.value })}
          />
        </Field>
        <select
          className={fieldClass}
          aria-label="排序字段类型"
          value={value.valueType}
          onChange={(e) => onChange({ ...value, valueType: e.target.value })}
        >
          {["number", "string", "datetime"].map((type) => (
            <option key={type}>{type}</option>
          ))}
        </select>
        <select
          className={fieldClass}
          aria-label="排序方向"
          value={value.direction}
          onChange={(e) => onChange({ ...value, direction: e.target.value })}
        >
          <option value="asc">升序</option>
          <option value="desc">降序</option>
        </select>
        <p className="text-xs">缺失和 null 始终排在末尾；相同值保留原顺序。</p>
      </div>
    );
  if (value.op === "select")
    return (
      <div className="grid gap-2">
        {(value.fields ?? []).map((field: any, index: number) => (
          <div key={index} className="grid gap-2">
            <input
              className={fieldClass}
              aria-label="选择字段路径"
              value={field.path}
              onChange={(e) =>
                onChange({
                  ...value,
                  fields: value.fields.map((row: any, i: number) =>
                    i === index ? { ...row, path: e.target.value } : row,
                  ),
                })
              }
            />
            <input
              className={fieldClass}
              aria-label="输出字段名称"
              value={field.as}
              onChange={(e) =>
                onChange({
                  ...value,
                  fields: value.fields.map((row: any, i: number) =>
                    i === index ? { ...row, as: e.target.value } : row,
                  ),
                })
              }
            />
            <label className="text-xs">
              <input
                type="checkbox"
                checked={!!field.optional}
                onChange={(e) =>
                  onChange({
                    ...value,
                    fields: value.fields.map((row: any, i: number) =>
                      i === index ? { ...row, optional: e.target.checked } : row,
                    ),
                  })
                }
              />{" "}
              缺失时填 null
            </label>
            <Button
              variant="ghost"
              onClick={() => onChange({ ...value, fields: value.fields.filter((_: any, i: number) => i !== index) })}
            >
              删除投影字段
            </Button>
          </div>
        ))}
        <Button
          variant="secondary"
          onClick={() => onChange({ ...value, fields: [...(value.fields ?? []), { path: "", as: "" }] })}
        >
          添加投影字段
        </Button>
      </div>
    );
  return null;
}
export function ObjectInput({
  schema,
  value,
  onChange,
  label,
}: {
  schema?: Schema;
  value: Record<string, unknown>;
  onChange: (value: Record<string, unknown>) => void;
  label: string;
}) {
  const set = (name: string, next: unknown) => {
    const output = { ...value };
    if (next === undefined) delete output[name];
    else output[name] = next;
    onChange(output);
  };
  return (
    <div className="grid gap-2">
      {Object.entries(schema?.properties ?? {}).map(([name, field]) => {
        const type = Array.isArray(field.type) ? field.type.find((type) => type !== "null") : field.type;
        const data = value[name];
        return (
          <div key={name} className="grid gap-1">
            <label className="text-xs">
              {name} · {type}
              {schema?.required?.includes(name) ? " · 必填" : ""}
              {field.description ? ` · ${field.description}` : ""}
            </label>
            {Array.isArray(field.type) && (
              <label className="text-xs">
                <input
                  type="checkbox"
                  checked={data === null}
                  onChange={(e) => set(name, e.target.checked ? null : undefined)}
                />{" "}
                显式 null
              </label>
            )}
            {data !== null &&
              (type === "boolean" ? (
                <select
                  className={fieldClass}
                  aria-label={`${label}${name}`}
                  value={data === undefined ? "" : String(data)}
                  onChange={(e) => set(name, e.target.value === "" ? undefined : e.target.value === "true")}
                >
                  <option value="">未设置</option>
                  <option value="true">true</option>
                  <option value="false">false</option>
                </select>
              ) : type === "object" || type === "array" ? (
                <JsonField
                  label={`${label}${name}`}
                  value={data ?? (type === "object" ? {} : [])}
                  onChange={(next) => set(name, next)}
                />
              ) : (
                <input
                  className={fieldClass}
                  aria-label={`${label}${name}`}
                  type={type === "number" || type === "integer" ? "number" : "text"}
                  step={type === "integer" ? "1" : "any"}
                  value={data === undefined ? "" : String(data)}
                  onChange={(e) =>
                    set(
                      name,
                      e.target.value === "" ? undefined : type === "string" ? e.target.value : Number(e.target.value),
                    )
                  }
                />
              ))}
            {data !== undefined && (
              <Button variant="ghost" onClick={() => set(name, undefined)}>
                清除此字段
              </Button>
            )}
          </div>
        );
      })}
    </div>
  );
}
export function WorkflowInspector({
  item,
  selected,
  actions,
  onItem,
  onStep,
  onDelete,
  onCopy,
  onJoin,
  onInvalid,
  active = true,
  diagnostic,
  canPreview = true,
  codeAvailable = true,
}: {
  canPreview?: boolean;
  codeAvailable?: boolean;
  diagnostic?: { line?: number; column?: number; sequence: number; fieldPath?: string };
  active?: boolean;
  item: WorkflowItem;
  selected: string;
  actions: ActionChoice[];
  onItem: (value: WorkflowItem) => void;
  onStep: (value: Step) => void;
  onDelete: () => void;
  onCopy: () => void;
  onJoin: () => void;
  onInvalid: (key: string, invalid: boolean) => void;
}) {
  const graph = item.graph;
  const step = graph.steps.find((step) => step.id === selected);
  const jsonBuffer = useContext(JsonBufferContext);
  const hasInvalid = Object.values(jsonBuffer?.buffers ?? {}).some((buffer) => buffer.invalid);
  const [codeOpen, setCodeOpen] = useState(false);
  const [apiSearch, setApiSearch] = useState("");
  const [dependencySearch, setDependencySearch] = useState("");
  const codeTrigger = useRef<HTMLButtonElement>(null);
  const [insert, setInsert] = useState<{ text: string; sequence: number }>();
  const [codeLocation, setCodeLocation] = useState<{ line: number; column?: number; sequence: number }>();
  useEffect(() => {
    if (diagnostic?.line) setCodeLocation({ ...diagnostic, line: diagnostic.line });
    if (diagnostic?.fieldPath) {
      const timer = window.setTimeout(() => {
        const part = diagnostic.fieldPath?.split("/").at(-1)?.replace(/~1/g, "/").replace(/~0/g, "~");
        const field = part === "workflowKey" ? "工作流标识" : part === "name" ? "工作流名称" : part;
        const panel = document.querySelector("[data-workflow-inspector]");
        const controls = Array.from(
          panel?.querySelectorAll<HTMLElement>("input,textarea,select,[contenteditable=true]") ?? [],
        );
        const target =
          controls.find((control) => field && control.getAttribute("aria-label")?.startsWith(field)) ??
          controls.find((control) => control.getAttribute("aria-label") === "API 参数映射 JSON") ??
          controls[0];
        for (let parent = target?.parentElement; parent && parent !== panel; parent = parent.parentElement)
          if (parent instanceof HTMLDetailsElement) parent.open = true;
        target?.scrollIntoView?.({ block: "nearest" });
        target?.focus();
      }, 0);
      return () => window.clearTimeout(timer);
    }
  }, [diagnostic]);
  const [targets, setTargets] = useState<any[]>([]);
  const [sample, setSample] = useState<Record<string, unknown>>({});
  const [preview, setPreview] = useState<any>(null);
  const [previewMode, setPreviewMode] = useState("code_inputs");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [previewFingerprint, setPreviewFingerprint] = useState("");
  const scheduleFingerprint = JSON.stringify([item.scheduleType, item.cronExpression, item.scheduleTimezone]);
  const [scheduleResult, setScheduleResult] = useState<{ fingerprint: string; value: any } | null>(null);
  const [fingerprint] = [
    JSON.stringify({
      graph,
      sample,
      previewMode,
      selected,
      apiVersions: graph.steps.map((node) => [
        node.action,
        actions.find((action) => action.id === node.action)?.version,
      ]),
      targets,
    }),
  ];
  const actionVersion = actions.find((action) => action.id === step?.action)?.version;
  const codeDiagnostics = preview?.diagnostics
    ?.filter((diagnostic: any) => diagnostic.line)
    .map((diagnostic: any, index: number) => (
      <Button
        key={index}
        variant="secondary"
        disabled={previewFingerprint !== fingerprint}
        onClick={() => setCodeLocation({ ...diagnostic, sequence: Date.now() })}
      >
        定位代码 {diagnostic.line}:{diagnostic.column ?? 1}
      </Button>
    ));
  useEffect(() => {
    let cancelled = false;
    setTargets([]);
    if (step?.action)
      void api
        .actionExecutionOptions(step.action, true)
        .then((value) => {
          if (!cancelled) setTargets(value);
        })
        .catch((error) => {
          if (!cancelled) setMessage(String(error));
        });
    return () => {
      cancelled = true;
    };
  }, [step?.action, actionVersion]);
  const sampleKey = useRef(selected);
  if (sampleKey.current === selected && jsonBuffer?.samples)
    jsonBuffer.samples[selected] = {
      sample,
      preview,
      previewMode,
      previewFingerprint,
      samplesByMode: {
        ...jsonBuffer.samples[selected]?.samplesByMode,
        [step?.type === "code" ? previewMode : "workflow"]: sample,
      },
    };
  const changePreviewMode = (mode: string) => {
    if (mode !== previewMode) {
      setSample(jsonBuffer?.samples?.[selected]?.samplesByMode?.[mode] ?? {});
      setPreviewMode(mode);
    }
  };
  useEffect(() => {
    const stored = jsonBuffer?.samples?.[selected];
    sampleKey.current = selected;
    setSample(stored?.sample ?? {});
    setPreview(stored?.preview ?? null);
    setPreviewMode(stored?.previewMode ?? "code_inputs");
    setPreviewFingerprint(stored?.previewFingerprint ?? "");
    setMessage("");
    setCodeOpen(false);
    setApiSearch("");
    setDependencySearch("");
  }, [selected]);
  const target = targets.find((target) => target.integration.id === step?.integrationId);
  const action = actions.find((action) => action.id === step?.action || action.actionKey === step?.action);
  const conditionPaths = [
    ...fieldPaths(graph.inputSchema, "trigger"),
    ...(graph.variables ?? []).map((variable) => ({ path: `vars.${variable.name}`, label: variable.name })),
    ...graph.steps
      .filter((source) => step && dependencies(graph, step.id).has(source.id))
      .flatMap((source) =>
        fieldPaths(
          source.outputSchema ?? actions.find((action) => action.id === source.action)?.outputSchema,
          source.id,
        ),
      ),
  ];
  const updateGraph = (next: Definition) => onItem({ ...item, graph: next });
  const updateVariable = (index: number, patch: Partial<NonNullable<Definition["variables"]>[number]>) =>
    updateGraph({ ...graph, variables: graph.variables?.map((row, i) => (i === index ? { ...row, ...patch } : row)) });
  if (selected === "@trigger")
    return (
      <div className="grid gap-4">
        <Field label="工作流标识">
          <input
            className={fieldClass}
            required
            aria-label="工作流标识"
            maxLength={100}
            placeholder="例如 order_sync（字母、数字、-、_、.）"
            value={item.workflowKey}
            onChange={(e) => onItem({ ...item, workflowKey: e.target.value })}
          />
        </Field>
        <Field label="工作流名称">
          <input
            required
            aria-label="工作流名称"
            className={fieldClass}
            value={item.name}
            onChange={(e) => onItem({ ...item, name: e.target.value })}
          />
        </Field>
        <ObjectInput
          schema={graph.inputSchema}
          value={item.input}
          onChange={(input) => onItem({ ...item, input })}
          label="默认输入"
        />
        <details open={!Object.keys(graph.inputSchema?.properties ?? {}).length}>
          <summary className="cursor-pointer text-sm">默认输入 JSON</summary>
          <JsonField
            label="默认输入（整个对象）"
            value={item.input}
            onChange={(input) => onItem({ ...item, input })}
            onInvalid={(invalid) => onInvalid("default", invalid)}
          />
        </details>
        <details>
          <summary>开始输入字段</summary>
          <SchemaFields schema={graph.inputSchema} onChange={(inputSchema) => updateGraph({ ...graph, inputSchema })} />
        </details>
        <Field label="触发方式">
          <select
            className={fieldClass}
            value={item.scheduleType}
            onChange={(e) => onItem({ ...item, scheduleType: e.target.value })}
          >
            <option value="manual">手动</option>
            <option value="interval">固定间隔</option>
            <option value="cron">Cron</option>
          </select>
        </Field>
        {item.scheduleType !== "manual" && (
          <>
            <Field label="调度规则">
              {item.scheduleType === "interval" ? (
                <select
                  className={fieldClass}
                  value={item.cronExpression ?? "每 30 分钟"}
                  onChange={(e) => onItem({ ...item, cronExpression: e.target.value })}
                >
                  {["每 15 分钟", "每 30 分钟", "每小时", "每天"].map((interval) => (
                    <option key={interval}>{interval}</option>
                  ))}
                </select>
              ) : (
                <div className="grid gap-2">
                  <select
                    className={fieldClass}
                    aria-label="常用 Cron 计划"
                    value=""
                    onChange={(e) => onItem({ ...item, cronExpression: e.target.value })}
                  >
                    <option value="">选择常用计划（保留当前规则）</option>
                    <option value="0 * * * *">每小时整点</option>
                    <option value="0 9 * * *">每天 09:00</option>
                    <option value="0 9 * * 1">每周一 09:00</option>
                  </select>
                  <input
                    className={fieldClass}
                    aria-label="Cron 表达式"
                    value={item.cronExpression ?? ""}
                    onChange={(e) => onItem({ ...item, cronExpression: e.target.value })}
                  />
                </div>
              )}
            </Field>
            <Field label="IANA 时区">
              <input
                className={fieldClass}
                value={item.scheduleTimezone}
                onChange={(e) => onItem({ ...item, scheduleTimezone: e.target.value })}
              />
            </Field>
            <Button
              variant="secondary"
              onClick={() =>
                void api
                  .previewWorkflowSchedule({
                    scheduleType: item.scheduleType,
                    cronExpression: item.cronExpression,
                    scheduleTimezone: item.scheduleTimezone,
                  })
                  .then((value) => setScheduleResult({ fingerprint: scheduleFingerprint, value }))
                  .catch((error) => setMessage(String(error)))
              }
            >
              预览未来三次计划
            </Button>
            {scheduleResult?.fingerprint === scheduleFingerprint && (
              <pre className="overflow-auto text-xs">{JSON.stringify(scheduleResult.value, null, 2)}</pre>
            )}
          </>
        )}
        <details>
          <summary>流程变量</summary>
          <JsonField
            label="变量声明 JSON（name / type / nullable / initial）"
            value={graph.variables ?? []}
            onChange={(variables) => updateGraph({ ...graph, variables })}
          />
          <Button
            variant="secondary"
            onClick={() =>
              updateGraph({
                ...graph,
                variables: [
                  ...(graph.variables ?? []),
                  { name: `value${(graph.variables?.length ?? 0) + 1}`, type: "string", initial: "" },
                ],
              })
            }
          >
            添加变量
          </Button>
          {graph.variables?.map((variable, index) => (
            <div key={index} className="mt-2 grid gap-2 rounded-lg border border-[var(--border)] p-2">
              <p className="text-xs font-semibold">{variable.name}</p>
              <Button
                variant="secondary"
                onClick={() => {
                  const name = window.prompt(
                    "新变量名（会一起更新条件、映射和赋值引用，不修改脚本文本）",
                    variable.name,
                  );
                  if (!name || name === variable.name) return;
                  try {
                    updateGraph(renameVariable(graph, variable.name, name));
                  } catch (error) {
                    setMessage(String(error));
                  }
                }}
              >
                重命名变量
              </Button>
              <select
                className={fieldClass}
                aria-label={`${variable.name}变量类型`}
                value={variable.type}
                onChange={(e) => updateVariable(index, { type: e.target.value })}
              >
                {["string", "number", "integer", "boolean", "object", "array"].map((type) => (
                  <option key={type}>{type}</option>
                ))}
              </select>
              <label className="text-xs">
                <input
                  type="checkbox"
                  checked={!!variable.nullable}
                  onChange={(e) => updateVariable(index, { nullable: e.target.checked })}
                />{" "}
                允许 null
              </label>
              <input
                className={fieldClass}
                aria-label={`${variable.name}变量说明`}
                value={variable.description ?? ""}
                placeholder="变量说明"
                onChange={(e) => updateVariable(index, { description: e.target.value })}
              />
              <Button
                variant="ghost"
                onClick={() => {
                  if (window.confirm(`删除变量 ${variable.name}？依赖它的条件、映射和赋值需要修正，保存时会校验。`))
                    updateGraph({ ...graph, variables: graph.variables?.filter((_, i) => i !== index) });
                }}
              >
                删除变量
              </Button>
              <SourcePicker
                label={`${variable.name}初值`}
                value={variable.initial}
                onChange={(initial) =>
                  updateGraph({
                    ...graph,
                    variables: graph.variables?.map((row, i) => (i === index ? { ...row, initial } : row)),
                  })
                }
                graph={graph}
                actions={actions}
                initial
              />
            </div>
          ))}
        </details>
        {message && <p role="alert">{message}</p>}
      </div>
    );
  if (selected === "@result")
    return (
      <div className="grid gap-3">
        <p className="text-xs">结束等待全部有效步骤完成，再组织本次返回对象。</p>
        <Button variant="secondary" onClick={onJoin}>
          在结束汇合
        </Button>
        {Object.entries(graph.output).map(([key, value]) => (
          <div key={key}>
            <SourcePicker
              label={key}
              value={value}
              onChange={(next) => updateGraph({ ...graph, output: { ...graph.output, [key]: next } })}
              graph={graph}
              actions={actions}
              allowOptional
            />
            <Button
              variant="ghost"
              onClick={() => {
                const output = { ...graph.output };
                delete output[key];
                updateGraph({ ...graph, output });
              }}
            >
              删除返回字段
            </Button>
          </div>
        ))}
        <Button
          variant="secondary"
          onClick={() => {
            const key = window.prompt("返回字段名称");
            if (key) updateGraph({ ...graph, output: { ...graph.output, [key]: null } });
          }}
        >
          添加返回字段
        </Button>
        <JsonField
          label="完整返回映射 JSON"
          value={graph.output}
          onChange={(output) => updateGraph({ ...graph, output })}
          onInvalid={(invalid) => onInvalid("output", invalid)}
        />
      </div>
    );
  if (!step) return <p>选择一个节点。</p>;
  return (
    <div className="grid gap-4">
      <Field label="节点名称">
        <input
          className={fieldClass}
          value={step.title ?? ""}
          onChange={(e) => onStep({ ...step, title: e.target.value })}
        />
      </Field>
      {(step.type ?? "api") === "api" && (
        <>
          {action?.inputSchema?.properties &&
            Object.keys(step.input ?? {}).some((key) => !(key in action.inputSchema!.properties!)) && (
              <p role="status" className="text-xs">
                当前 API 声明不包含部分映射字段，原参数已保留。请在参数映射 JSON 中检查后重新预览。
              </p>
            )}
          <Field label="搜索 API">
            <input
              className={fieldClass}
              placeholder="按名称、系统或标识搜索"
              value={apiSearch}
              onChange={(e) => setApiSearch(e.target.value)}
            />
          </Field>
          <Field label="API">
            <select
              className={fieldClass}
              value={action?.id ?? step.action ?? ""}
              onChange={(e) => onStep({ ...step, action: e.target.value, integrationId: "", connectionKey: "" })}
            >
              <option value="">选择 API</option>
              {step.action && !action && <option value={step.action}>{step.action}（已失效）</option>}
              {[...new Set(actions.map((action) => action.systemKey))].map((system) => (
                <optgroup key={system} label={system}>
                  {actions
                    .filter(
                      (candidate) =>
                        candidate.systemKey === system &&
                        (candidate.id === action?.id ||
                          `${candidate.name} ${candidate.systemKey} ${candidate.actionKey}`
                            .toLowerCase()
                            .includes(apiSearch.toLowerCase())),
                    )
                    .map((candidate) => (
                      <option
                        key={candidate.id}
                        value={candidate.id}
                        disabled={!candidate.executable || candidate.status !== "active"}
                      >
                        {candidate.name} · {candidate.actionKey}
                        {candidate.status !== "active"
                          ? `（${candidate.status}）`
                          : !candidate.executable
                            ? "（不可执行）"
                            : ""}
                      </option>
                    ))}
                </optgroup>
              ))}
            </select>
          </Field>
          <a
            href={action ? `/actions?edit=${action.id}` : "/actions"}
            target="_blank"
            rel="noreferrer"
            className="text-sm text-blue-600"
          >
            查看 API 定义 ↗
          </a>
          <Field label="执行集成">
            <select
              className={fieldClass}
              value={step.integrationId ?? ""}
              onChange={(e) => onStep({ ...step, integrationId: e.target.value, connectionKey: "" })}
            >
              <option value="">选择集成</option>
              {step.integrationId && !target && (
                <option value={step.integrationId}>{step.integrationId}（不可用）</option>
              )}
              {targets.map((target) => (
                <option key={target.integration.id} value={target.integration.id}>
                  {target.integration.name}
                </option>
              ))}
            </select>
          </Field>
          {target?.requiresAccount && (
            <Field label="执行账号">
              <select
                className={fieldClass}
                value={step.connectionKey ?? ""}
                onChange={(e) => onStep({ ...step, connectionKey: e.target.value })}
              >
                <option value="">选择账号</option>
                {step.connectionKey && !target.accounts.some((account: any) => account.id === step.connectionKey) && (
                  <option value={step.connectionKey}>{step.connectionKey}（已失效）</option>
                )}
                {target.accounts.map((account: any) => (
                  <option key={account.id} value={account.id}>
                    {account.name} · {account.status}
                  </option>
                ))}
              </select>
              <a href="/auth?section=accounts" target="_blank" rel="noreferrer" className="text-xs text-blue-600">
                修复账号 ↗
              </a>
            </Field>
          )}
          {Object.entries(action?.inputSchema?.properties ?? step.input ?? {}).map(([name]) => (
            <SourcePicker
              key={name}
              label={name}
              value={step.input?.[name]}
              allowUnset
              onChange={(value) => {
                const input = { ...step.input };
                if (value === undefined) delete input[name];
                else input[name] = value;
                onStep({ ...step, input });
              }}
              graph={graph}
              step={step}
              actions={actions}
            />
          ))}
          <details open={!Object.keys(action?.inputSchema?.properties ?? {}).length}>
            <summary className="cursor-pointer text-sm">参数映射 JSON</summary>
            <JsonField
              label="API 参数映射 JSON"
              value={step.input ?? {}}
              onChange={(input) => onStep({ ...step, input })}
            />
          </details>
          <details>
            <summary className="cursor-pointer text-sm">失败处理</summary>{" "}
            <select
              className={fieldClass}
              aria-label="API 失败策略"
              value={step.onError ?? "fail"}
              onChange={(e) => onStep({ ...step, onError: e.target.value })}
            >
              <option value="fail">确认失败后停止</option>
              <option value="continue">
                {graph.schemaVersion === 2 ? "收到失败响应后继续" : "确认失败后继续（旧版）"}
              </option>
            </select>
          </details>
          <details>
            <summary>兼容执行条件 runIf</summary>
            <JsonField
              label="runIf（null 表示无条件）"
              value={step.runIf ?? null}
              onChange={(runIf) => onStep({ ...step, runIf: runIf ?? undefined })}
            />
          </details>
        </>
      )}
      {step.type === "code" && (
        <>
          <p className="text-xs">JavaScript · js-v1 · main(input) 返回命名字段对象。无网络、文件、系统时间或随机源。</p>
          <details>
            <summary className="cursor-pointer text-sm font-medium">命名输入声明</summary>
            <SchemaFields
              schema={step.inputSchema}
              allRequired
              onChange={(inputSchema) => {
                const old = Object.keys(step.inputSchema?.properties ?? {}),
                  names = Object.keys(inputSchema.properties ?? {});
                const removed = old.filter((key) => !names.includes(key)),
                  added = names.filter((key) => !old.includes(key));
                const input = { ...step.input };
                if (removed.length === 1 && added.length === 1) {
                  input[added[0]] = input[removed[0]] ?? null;
                }
                for (const key of removed) delete input[key];
                for (const key of names) if (!(key in input)) input[key] = null;
                onStep({ ...step, inputSchema, input });
              }}
            />
          </details>
          {Object.keys(step.inputSchema?.properties ?? {}).map((name) => (
            <SourcePicker
              key={name}
              label={name}
              value={step.input?.[name] === undefined ? null : step.input[name]}
              onChange={(value) => onStep({ ...step, input: { ...step.input, [name]: value } })}
              graph={graph}
              step={step}
              actions={actions}
            />
          ))}
          <div className="flex flex-wrap gap-2">
            <select
              className={fieldClass}
              aria-label="应用代码模板"
              value=""
              onChange={(event) => {
                const template = codeTemplates[Number(event.target.value)];
                if (!template) return;
                if (
                  window.confirm(
                    `模板“${template.title}”将替换源码、命名输入及返回声明；输入为 ${Object.keys(template.input).join("、")}，输出为 ${Object.keys(template.outputSchema.properties ?? {}).join("、")}。已有下游引用需检查。应用后可撤销。`,
                  )
                )
                  onStep({
                    ...step,
                    code: template.code,
                    inputSchema: template.inputSchema,
                    outputSchema: template.outputSchema,
                    input: template.input,
                  });
              }}
            >
              <option value="">选择模板</option>
              {codeTemplates.map((template, i) => (
                <option key={i} value={i}>
                  {template.title}
                </option>
              ))}
            </select>
            <Button
              ref={codeTrigger}
              variant="secondary"
              onClick={() => {
                changePreviewMode("code_inputs");
                setCodeOpen(true);
              }}
            >
              展开编辑
            </Button>
            {Object.keys(step.inputSchema?.properties ?? {}).map((name) => (
              <Button
                key={name}
                variant="ghost"
                onClick={() =>
                  setInsert({
                    text: /^[A-Za-z_$][\w$]*$/.test(name) ? `input.${name}` : `input[${JSON.stringify(name)}]`,
                    sequence: Date.now(),
                  })
                }
              >
                插入 input.{name}
              </Button>
            ))}
          </div>
          <Suspense fallback={<p role="status">正在加载代码编辑器…</p>}>
            <CodeEditor
              value={step.code ?? ""}
              onChange={(code) => onStep({ ...step, code })}
              inputNames={Object.keys(step.inputSchema?.properties ?? {})}
              insert={codeOpen ? undefined : insert}
              location={codeOpen ? undefined : codeLocation}
            />
          </Suspense>
          <Modal
            open={codeOpen && active}
            onOpenChange={() => setCodeOpen(false)}
            title="编辑 JavaScript"
            className="w-[min(96vw,70rem)]"
            onCloseAutoFocus={() => codeTrigger.current?.focus()}
            description="源码与命名输入设置"
            unsavedChanges={false}
          >
            <p className="text-xs">
              命名输入：{Object.keys(step.inputSchema?.properties ?? {}).join("、") || "无"}
              。修改会保留在当前编辑会话；Esc 关闭展开区，Tab 移向下一控件。
            </p>
            <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_320px]">
              <div className="min-w-0">
                <Suspense fallback={<p>正在加载…</p>}>
                  <CodeEditor
                    expanded
                    value={step.code ?? ""}
                    onChange={(code) => onStep({ ...step, code })}
                    inputNames={Object.keys(step.inputSchema?.properties ?? {})}
                    insert={insert}
                    location={codeLocation}
                  />
                </Suspense>
              </div>
              <div className="grid min-w-0 content-start gap-3">
                <ObjectInput schema={step.inputSchema} value={sample} onChange={setSample} label="命名输入样本" />
                <Button
                  variant="secondary"
                  disabled={busy || hasInvalid || !canPreview || !codeAvailable}
                  onClick={async () => {
                    setBusy(true);
                    setMessage("");
                    const capture = fingerprint;
                    try {
                      const result = await api.previewWorkflowStep({
                        definition: graph,
                        stepId: step.id,
                        previewMode: "code_inputs",
                        sampleInput: sample,
                      });
                      setPreview(result);
                      setPreviewFingerprint(capture);
                    } catch (error) {
                      setMessage(String(error));
                    } finally {
                      setBusy(false);
                    }
                  }}
                >
                  执行命名输入样本
                </Button>
                {codeDiagnostics}
                {message && (
                  <p role="alert" className="text-red-600">
                    {message}
                  </p>
                )}
                {preview && previewFingerprint === fingerprint && (
                  <pre className="max-h-52 overflow-auto whitespace-pre-wrap break-all text-xs">
                    {JSON.stringify(preview, null, 2)}
                  </pre>
                )}
              </div>
            </div>
          </Modal>
          <details>
            <summary className="cursor-pointer text-sm font-medium">返回字段声明</summary>
            <SchemaFields
              schema={step.outputSchema}
              allRequired
              onChange={(outputSchema) => onStep({ ...step, outputSchema })}
            />
          </details>
        </>
      )}
      {step.type === "transform" && (
        <>
          <SourcePicker
            label="数据来源"
            value={step.source}
            onChange={(source) => onStep({ ...step, source })}
            graph={graph}
            step={step}
            actions={actions}
          />
          {step.operations?.map((operation, index) => (
            <div key={index} className="grid gap-2 border border-[var(--border)] rounded-lg p-2">
              <p className="text-xs">
                {index + 1}. {String(operation.op)}
              </p>
              <OperationFields
                value={operation}
                graph={graph}
                step={step}
                actions={actions}
                onChange={(value) =>
                  onStep({ ...step, operations: step.operations?.map((row, i) => (i === index ? value : row)) })
                }
              />
              <details>
                <summary>操作 JSON</summary>
                <JsonField
                  label={`操作 ${index + 1}`}
                  value={operation}
                  onChange={(value) =>
                    onStep({ ...step, operations: step.operations?.map((row, i) => (i === index ? value : row)) })
                  }
                />
              </details>
              <div className="flex gap-2">
                {[-1, 1].map((delta) => (
                  <Button
                    key={delta}
                    variant="secondary"
                    disabled={index + delta < 0 || index + delta >= (step.operations?.length ?? 0)}
                    onClick={() => {
                      const operations = [...(step.operations ?? [])];
                      [operations[index], operations[index + delta]] = [operations[index + delta], operations[index]];
                      onStep({ ...step, operations });
                    }}
                  >
                    {delta === -1 ? "上移" : "下移"}
                  </Button>
                ))}
                <Button
                  variant="ghost"
                  onClick={() => onStep({ ...step, operations: step.operations?.filter((_, i) => i !== index) })}
                >
                  删除操作
                </Button>
              </div>
            </div>
          ))}
          <select
            className={fieldClass}
            aria-label="添加数据操作"
            value=""
            onChange={(e) => {
              const op = e.target.value;
              const config =
                op === "slice"
                  ? { op, offset: 0, limit: 10 }
                  : op === "sort"
                    ? { op, path: "", valueType: "number", direction: "asc" }
                    : op === "select"
                      ? { op, fields: [{ path: "id", as: "id" }] }
                      : op === "filter"
                        ? { op, condition: { itemPath: "status", op: "eq", value: "paid" } }
                        : { op };
              onStep({ ...step, operations: [...(step.operations ?? []), config] });
            }}
          >
            <option value="">添加操作</option>
            {["select", "filter", "sort", "slice", "count"].map((op) => (
              <option key={op}>{op}</option>
            ))}
          </select>
        </>
      )}
      {step.type === "condition" && (
        <>
          {step.branches?.map((branch, index) => (
            <div key={branch.id} className="grid gap-2 rounded-lg border border-[var(--border)] p-2">
              <Field label={branch.default ? "ELSE" : index === 0 ? "IF" : "ELSE IF"}>
                <input
                  className={fieldClass}
                  value={branch.title ?? ""}
                  onChange={(e) =>
                    onStep({
                      ...step,
                      branches: step.branches?.map((row, i) => (i === index ? { ...row, title: e.target.value } : row)),
                    })
                  }
                />
              </Field>
              <code className="text-xs">出口 {branch.id}</code>
              {!branch.default && (
                <ConditionFields
                  value={branch.condition ?? {}}
                  paths={conditionPaths}
                  graph={graph}
                  step={step}
                  actions={actions}
                  onChange={(condition) =>
                    onStep({
                      ...step,
                      branches: step.branches?.map((row, i) => (i === index ? { ...row, condition } : row)),
                    })
                  }
                />
              )}
              <Assignments
                value={branch.assign ?? []}
                onChange={(assign) =>
                  onStep({
                    ...step,
                    branches: step.branches?.map((row, i) => (i === index ? { ...row, assign } : row)),
                  })
                }
                graph={graph}
                step={step}
                actions={actions}
              />
              {!branch.assign?.length && <p className="text-xs text-[var(--muted-text)]">此出口保留变量原值。</p>}
              {!branch.default && (
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="secondary"
                    disabled={index === 0}
                    onClick={() => {
                      const branches = [...(step.branches ?? [])];
                      [branches[index], branches[index - 1]] = [branches[index - 1], branches[index]];
                      onStep({ ...step, branches });
                    }}
                  >
                    上移
                  </Button>
                  <Button
                    variant="secondary"
                    disabled={index >= (step.branches?.length ?? 0) - 2}
                    onClick={() => {
                      const branches = [...(step.branches ?? [])];
                      [branches[index], branches[index + 1]] = [branches[index + 1], branches[index]];
                      onStep({ ...step, branches });
                    }}
                  >
                    下移
                  </Button>
                  <Button
                    variant="ghost"
                    disabled={(step.branches?.length ?? 0) <= 2}
                    onClick={() => {
                      if (
                        window.confirm(
                          `删除出口 ${branch.id}。受影响节点：${
                            graph.steps
                              .filter((node) =>
                                node.scope?.some(
                                  (scope) => scope.conditionId === step.id && scope.branchId === branch.id,
                                ),
                              )
                              .map((node) => node.title ?? node.id)
                              .join("、") || "无"
                          }。按分支来源映射仍需修正。可撤销。`,
                        )
                      )
                        onStep({ ...step, branches: step.branches?.filter((row) => row.id !== branch.id) });
                    }}
                  >
                    删除出口
                  </Button>
                </div>
              )}
            </div>
          ))}
          <Button
            variant="secondary"
            disabled={(step.branches?.length ?? 0) >= 9}
            onClick={() => {
              let index = 1;
              while (step.branches?.some((branch) => branch.id === `if${index}`)) index++;
              const branches = [...(step.branches ?? [])];
              branches.splice(branches.length - 1, 0, {
                id: `if${index}`,
                title: "ELSE IF",
                condition: { path: "trigger.value", op: "exists" },
              });
              onStep({ ...step, branches });
            }}
          >
            添加 ELSE IF
          </Button>
        </>
      )}
      {step.type !== "condition" && (
        <Assignments
          value={step.assign ?? []}
          onChange={(assign) => onStep({ ...step, assign })}
          graph={graph}
          step={step}
          actions={actions}
        />
      )}
      <details>
        <summary className="cursor-pointer text-sm font-medium">依赖与节点操作</summary>
        <div className="mt-3 grid gap-3">
          <p className="text-xs text-[var(--muted-text)]">
            {kindLabel[step.type ?? "api"]} · {step.id}
          </p>
          <p className="text-xs text-[var(--muted-text)]">
            前置步骤控制执行顺序；参数需单独选择上游字段。不同分支请通过汇合连接。
          </p>
          <div className="flex flex-wrap gap-1">
            {(step.dependsOn ?? []).map((id) => (
              <span key={id} className="rounded border border-[var(--border)] px-2 py-1 text-xs">
                {graph.steps.find((source) => source.id === id)?.title ?? id} · {id}
              </span>
            ))}
          </div>
          <input
            className={fieldClass}
            aria-label="搜索前置步骤"
            placeholder="搜索前置步骤"
            value={dependencySearch}
            onChange={(e) => setDependencySearch(e.target.value)}
          />
          <div role="group" aria-label="前置步骤" className="grid max-h-44 gap-2 overflow-auto">
            {graph.steps
              .filter(
                (source) =>
                  source.id !== step.id &&
                  !dependencies(graph, source.id).has(step.id) &&
                  `${source.title ?? ""} ${source.id}`.toLowerCase().includes(dependencySearch.toLowerCase()),
              )
              .map((source) => (
                <label key={source.id} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={step.dependsOn?.includes(source.id) ?? false}
                    disabled={
                      !step.dependsOn?.includes(source.id) &&
                      !(source.scope ?? []).every(
                        (scope, index) => stableJSON(scope) === stableJSON(step.scope?.[index]),
                      )
                    }
                    title={
                      !(source.scope ?? []).every(
                        (scope, index) => stableJSON(scope) === stableJSON(step.scope?.[index]),
                      )
                        ? "此步骤位于其他分支，请使用汇合连接"
                        : ""
                    }
                    onChange={(e) =>
                      onStep({
                        ...step,
                        dependsOn: e.target.checked
                          ? [...(step.dependsOn ?? []), source.id]
                          : step.dependsOn?.filter((id) => id !== source.id),
                      })
                    }
                  />
                  {source.title ?? source.id}
                  <span className="text-xs text-[var(--muted-text)]">{source.id}</span>
                </label>
              ))}
          </div>
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" onClick={onJoin}>
              在此汇合
            </Button>
            <Button variant="secondary" disabled={graph.steps.length >= 20} onClick={onCopy}>
              复制节点
            </Button>
            <Button variant="danger" onClick={onDelete}>
              删除节点
            </Button>
          </div>
        </div>
      </details>
      <details>
        <summary>完整节点 JSON（保留高级配置）</summary>
        <JsonField
          label="节点 JSON"
          value={step}
          validate={(value) => {
            if (value.inputSchema) assertSchemaShape(value.inputSchema);
            if (value.outputSchema) assertSchemaShape(value.outputSchema);
            if (!value || value.id !== step.id) throw new Error("完整节点 JSON 必须保留当前 id；重命名需同步修改引用");
            assertDefinitionShape({ ...graph, steps: graph.steps.map((row) => (row.id === step.id ? value : row)) });
          }}
          onChange={onStep}
          onInvalid={(invalid) => onInvalid(step.id, invalid)}
        />
      </details>
      <details>
        <summary>样本预览</summary>
        {step.type === "code" && (
          <select
            className={fieldClass}
            aria-label="样本验证范围"
            value={previewMode}
            onChange={(e) => changePreviewMode(e.target.value)}
          >
            <option value="code_inputs">填写命名输入 · 仅验证代码计算</option>
            <option value="workflow">验证上游字段映射和赋值</option>
          </select>
        )}
        {step.type === "code" && previewMode === "code_inputs" && (
          <ObjectInput schema={step.inputSchema} value={sample} onChange={setSample} label="样本" />
        )}
        {(step.type !== "code" || previewMode === "workflow") && (
          <details>
            <summary>运行信息样本（提交后由服务端计算业务日期）</summary>
            {["triggeredAt", "scheduledFor", "timezone"].map((field) => (
              <Field key={field} label={field}>
                <input
                  className={fieldClass}
                  placeholder={
                    field === "timezone"
                      ? "Asia/Singapore"
                      : field === "scheduledFor"
                        ? "留空表示 null；定时时填 UTC RFC3339"
                        : "例如 2026-10-03T16:05:00Z"
                  }
                  value={String((sample.sampleRunMetadata as any)?.[field] ?? "")}
                  onChange={(e) =>
                    setSample({
                      ...sample,
                      sampleRunMetadata: {
                        triggeredAt: "",
                        scheduledFor: null,
                        timezone: "UTC",
                        ...(sample.sampleRunMetadata as object),
                        [field]: field === "scheduledFor" && !e.target.value ? null : e.target.value,
                      },
                    })
                  }
                />
              </Field>
            ))}
            <Button
              variant="ghost"
              onClick={() => {
                const next = { ...sample };
                delete next.sampleRunMetadata;
                setSample(next);
              }}
            >
              移除时间样本
            </Button>
          </details>
        )}
        <JsonField
          label={
            step.type === "code" && previewMode === "code_inputs"
              ? "命名输入样本 JSON"
              : "工作流样本（trigger / sampleOutputs / sampleStatuses / sampleVariables / sampleRunMetadata / sampleResult）"
          }
          value={sample}
          onChange={setSample}
        />
        <Button
          variant="secondary"
          disabled={busy || hasInvalid || !canPreview || (step.type === "code" && !codeAvailable)}
          onClick={async () => {
            setBusy(true);
            setMessage("");
            const snapshot = fingerprint;
            try {
              const result = await api.previewWorkflowStep({
                definition: graph,
                stepId: step.id,
                previewMode: step.type === "code" ? previewMode : "workflow",
                ...(step.type === "code" && previewMode === "code_inputs" ? { sampleInput: sample } : sample),
              });
              setPreview(result);
              setPreviewFingerprint(snapshot);
            } catch (error) {
              setMessage(String(error));
            } finally {
              setBusy(false);
            }
          }}
        >
          执行样本预览
        </Button>
        {codeDiagnostics}
        {previewFingerprint && previewFingerprint !== fingerprint && (
          <p role="status" className="text-xs">
            配置或样本已修改，旧预览已失效。
          </p>
        )}
        {preview && previewFingerprint === fingerprint && (
          <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-all text-xs">
            {JSON.stringify(preview, null, 2)}
          </pre>
        )}
      </details>
      {message && (
        <p role="alert" className="text-red-600">
          {message}
        </p>
      )}
    </div>
  );
}
