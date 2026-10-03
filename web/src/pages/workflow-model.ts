export type ValueSource = unknown;
export type NodeKind = "api" | "transform" | "condition" | "code";
export interface Schema {
  type: string | string[];
  properties?: Record<string, Schema>;
  required?: string[];
  items?: Schema;
  description?: string;
}
export interface Scope {
  conditionId: string;
  branchId: string;
}
export interface Assignment {
  variable: string;
  value: ValueSource;
}
export interface Branch {
  id: string;
  title?: string;
  default?: boolean;
  condition?: Record<string, unknown>;
  assign?: Assignment[];
}
export interface Step {
  id: string;
  type?: NodeKind;
  title?: string;
  action?: string;
  integrationId?: string;
  connectionKey?: string;
  input?: Record<string, ValueSource>;
  dependsOn?: string[];
  scope?: Scope[];
  runIf?: Record<string, unknown>;
  onError?: string;
  source?: ValueSource;
  operations?: Record<string, unknown>[];
  branches?: Branch[];
  assign?: Assignment[];
  language?: string;
  runtimeProfile?: string;
  code?: string;
  inputSchema?: Schema;
  outputSchema?: Schema;
}
export interface Definition {
  schemaVersion?: number;
  inputSchema?: Schema;
  variables?: { name: string; type: string; description?: string; nullable?: boolean; initial: ValueSource }[];
  steps: Step[];
  output: Record<string, ValueSource>;
}
export interface Layout {
  schemaVersion: 1;
  direction: "LR";
  positions: Record<string, { x: number; y: number }>;
}
export interface WorkflowItem {
  id: string;
  workflowKey: string;
  name: string;
  description: string;
  status: string;
  version: number;
  layoutVersion: number;
  editorLayout?: Layout;
  graph: Definition;
  input: Record<string, unknown>;
  scheduleType: string;
  cronExpression?: string;
  scheduleTimezone: string;
  nextRunAt?: string;
  retryPolicy: { maxAttempts: number };
}
export interface Capabilities {
  v2Enabled: boolean;
  code: {
    enabled: boolean;
    backgroundCheck: string;
    local: {
      available: boolean;
      reasonCode: string;
      runtimeProfile: string;
      capacityPolicy: { totalConcurrency: number; interactiveConcurrency: number; formalPriority: boolean };
    };
  };
}
export const kindLabel: Record<string, string> = {
  api: "API",
  transform: "数据处理",
  condition: "条件判断",
  code: "代码",
  "@trigger": "开始",
  "@result": "结束",
};
export const emptySchema = (): Schema => ({ type: "object", properties: {}, required: [] });
export function blankWorkflow(): WorkflowItem {
  return {
    id: "",
    workflowKey: "",
    name: "新工作流",
    description: "",
    status: "draft",
    version: 0,
    layoutVersion: 1,
    graph: { steps: [], output: {} },
    input: {},
    scheduleType: "manual",
    scheduleTimezone: "UTC",
    retryPolicy: { maxAttempts: 1 },
  };
}
export function newStep(graph: Definition, kind: NodeKind, after?: Step, branchId?: string): Step {
  let number = 1;
  while (graph.steps.some((step) => step.id === `${kind}${number}`)) number++;
  const step: Step = {
    id: `${kind}${number}`,
    type: kind,
    title: kindLabel[kind],
    dependsOn: after ? [after.id] : [],
    scope: after ? [...(after.scope ?? []), ...(branchId ? [{ conditionId: after.id, branchId }] : [])] : [],
  };
  if (kind === "api")
    Object.assign(step, { action: "", integrationId: "", connectionKey: "", input: {}, onError: "fail" });
  if (kind === "code")
    Object.assign(step, {
      language: "javascript",
      runtimeProfile: "js-v1",
      input: {},
      inputSchema: emptySchema(),
      outputSchema: { type: "object", properties: { result: { type: "integer" } }, required: ["result"] },
      code: "function main(input) {\n  return { result: 0 };\n}",
    });
  if (kind === "transform") Object.assign(step, { source: [], operations: [{ op: "count" }] });
  if (kind === "condition")
    Object.assign(step, {
      branches: [
        { id: "if1", title: "IF", condition: { path: "trigger.value", op: "exists" } },
        { id: "otherwise", title: "ELSE", default: true },
      ],
    });
  return step;
}
// Insert a single dependency atomically; mappings continue to reference their original ancestors.
export function insertStep(graph: Definition, step: Step, source: string, target: string, exit?: string): Definition {
  const before = graph.steps.find((node) => node.id === source);
  const after = graph.steps.find((node) => node.id === target);
  if (source !== "@trigger" && !before) throw new Error("前置节点已不存在");
  if (target !== "@result" && !after) throw new Error("后续节点已不存在");
  if (after && (source === "@trigger" ? !!after.dependsOn?.length : !after.dependsOn?.includes(source)))
    throw new Error("连线已改变，请重新选择插入位置");
  const parent = step.scope ?? [];
  const targetScope = after?.scope ?? [];
  if (
    after &&
    (step.type === "condition"
      ? stableJSON(targetScope) !== stableJSON(parent)
      : !targetScope.every((entry, index) => stableJSON(entry) === stableJSON(parent[index])))
  )
    throw new Error("不能将后续路径隐式移入其他分支；条件请在分支内部或汇合节点之后插入");
  if (step.type === "condition" && after && !step.branches?.some((branch) => branch.id === exit))
    throw new Error("请选择后续节点接入新条件的哪个出口");
  const affected = after
    ? new Set([
        after.id,
        ...graph.steps.filter((node) => dependencies(graph, node.id).has(after.id)).map((node) => node.id),
      ])
    : new Set<string>();
  return {
    ...graph,
    steps: [
      ...graph.steps.map((node) => {
        let next = node;
        if (node.id === target)
          next = {
            ...next,
            dependsOn: [...new Set([...(node.dependsOn ?? []).filter((id) => id !== source), step.id])],
          };
        if (step.type === "condition" && affected.has(node.id)) {
          const scope = node.scope ?? [];
          if (!parent.every((entry, index) => stableJSON(entry) === stableJSON(scope[index])))
            throw new Error("后续路径跨越其他分支，请先整理汇合再插入条件");
          // Moving a shared continuation into a branch would hide unrelated dependencies.
          if (
            (node.dependsOn ?? []).some(
              (id) => id !== source && !affected.has(id) && !dependencies(graph, source).has(id),
            )
          )
            throw new Error("后续节点还有其他路径依赖，请选择独立分支插入条件");
          next = {
            ...next,
            scope: [...parent, { conditionId: step.id, branchId: exit! }, ...scope.slice(parent.length)],
          };
        }
        return next;
      }),
      step,
    ],
  };
}
export function stepReferences(graph: Definition, id: string): string[] {
  const refers = (value: unknown) =>
    stableJSON(value) !==
    stableJSON(
      rewriteValue(
        rewriteValue(
          rewriteValue(rewriteValue(value, id, "__removed__"), `status.${id}`, "__removed_status__"),
          `status["${id}"]`,
          "__removed_status__",
        ),
        `status['${id}']`,
        "__removed_status__",
      ),
    );
  const result: string[] = [];
  if (refers(graph.output)) result.push("结束返回映射");
  if (graph.variables?.some((variable) => refers(variable.initial))) result.push("变量初始值");
  for (const node of graph.steps) {
    if (node.id === id) continue;
    if (node.scope?.some((scope) => scope.conditionId === id)) result.push(`${node.title ?? node.id}（条件分支）`);
    if ([node.input, node.source, node.operations, node.runIf, node.assign, node.branches].some(refers))
      result.push(`${node.title ?? node.id}（参数或赋值）`);
  }
  return result;
}
export function orderedSteps(graph: Definition): Step[] {
  const result: Step[] = [],
    pending = [...graph.steps];
  while (pending.length) {
    const index = pending.findIndex((node) =>
      (node.dependsOn ?? []).every(
        (id) => result.some((row) => row.id === id) || !graph.steps.some((row) => row.id === id),
      ),
    );
    if (index < 0) return [...result, ...pending];
    result.push(pending.splice(index, 1)[0]);
  }
  return result;
}
export function placeStep(layout: Layout, step: Step, source: string, target?: string, graph?: Definition): Layout {
  const positions = structuredClone(layout.positions);
  delete positions[step.id];
  if (graph)
    for (const id of Object.keys(positions))
      if (id !== "@trigger" && id !== "@result" && !graph.steps.some((node) => node.id === id)) delete positions[id];
  const origin = positions[source] ?? { x: 0, y: 0 };
  const position = { x: origin.x + 330, y: origin.y };
  if (target && target !== "@result") {
    for (const [id, p] of Object.entries(positions))
      if ((id === target || (graph && dependencies(graph, id).has(target))) && p.x >= origin.x + 270)
        positions[id] = { ...p, x: p.x + 330 };
  }
  const height = (node: string) =>
    130 + ((node === step.id ? step : graph?.steps.find((row) => row.id === node))?.branches?.length ?? 0) * 35;
  // At most 20 nodes: a bounded scan keeps local additions clear without relaying the whole graph.
  while (
    Object.entries(positions).some(
      ([id, p]) => id !== "@result" && Math.abs(p.x - position.x) < 270 && Math.abs(p.y - position.y) < height(id),
    )
  )
    position.y += 210;
  positions[step.id] = position;
  if (positions["@result"])
    positions["@result"].x = Math.max(
      positions["@result"].x,
      ...Object.entries(positions)
        .filter(([id]) => id !== "@result")
        .map(([, p]) => p.x + 330),
    );
  return { ...layout, positions };
}
export function upgradeGraph(graph: Definition): Definition {
  if (graph.schemaVersion === 2) return graph;
  if (graph.steps.some((step) => step.id === "vars"))
    throw new Error("旧步骤 vars 与流程变量重名，请先改名并更新引用后升级。");
  return { ...graph, schemaVersion: 2, steps: graph.steps.map((step) => ({ ...step, type: step.type ?? "api" })) };
}
export function dependencies(graph: Definition, id: string): Set<string> {
  const found = new Set<string>();
  const visit = (key: string) => {
    for (const dep of graph.steps.find((step) => step.id === key)?.dependsOn ?? [])
      if (!found.has(dep)) {
        found.add(dep);
        visit(dep);
      }
  };
  visit(id);
  return found;
}
export function canDepend(graph: Definition, target: string, source: string): boolean {
  return source !== target && !dependencies(graph, source).has(target);
}
export function joinCondition(graph: Definition, targetId: string, conditionId: string): Definition {
  const target = graph.steps.find((step) => step.id === targetId);
  const condition = graph.steps.find((step) => step.id === conditionId && step.type === "condition");
  if (!target || !condition) throw new Error("选择一个存在的条件和汇合节点。");
  if (target.id === condition.id || dependencies(graph, condition.id).has(target.id))
    throw new Error("汇合不能选原条件自身或其祖先。");
  const parent = condition.scope ?? [];
  const previous = target.scope ?? [];
  const matches = (scope: Scope[], prefix: Scope[]) =>
    prefix.every(
      (entry, index) => scope[index]?.conditionId === entry.conditionId && scope[index]?.branchId === entry.branchId,
    );
  if (
    !matches(previous, parent) ||
    (previous.length > parent.length &&
      (previous.length !== parent.length + 1 || previous[parent.length]?.conditionId !== condition.id))
  )
    throw new Error("汇合只能收口当前条件到其父层级，请先完成内部汇合。");
  const descendants = new Set(
    graph.steps.filter((step) => dependencies(graph, step.id).has(target.id)).map((step) => step.id),
  );
  const paths = graph.steps.filter(
    (step) =>
      step.id !== target.id &&
      !descendants.has(step.id) &&
      step.scope?.some((scope) => scope.conditionId === condition.id),
  );
  const ends = paths.filter((step) => !paths.some((other) => other.dependsOn?.includes(step.id)));
  const dependsOn = [...new Set([...(target.dependsOn ?? []), condition.id, ...ends.map((step) => step.id)])];
  if (dependsOn.some((id) => !canDepend(graph, target.id, id)))
    throw new Error("这些末端会产生依赖环，请选择公共后续节点。");
  return {
    ...graph,
    steps: graph.steps.map((step) =>
      step.id === target.id
        ? { ...step, scope: parent, dependsOn }
        : descendants.has(step.id) && matches(step.scope ?? [], previous)
          ? { ...step, scope: [...parent, ...(step.scope ?? []).slice(previous.length)] }
          : step,
    ),
  };
}
export function fieldPaths(schema: Schema | undefined, prefix: string): { path: string; label: string }[] {
  const result = [{ path: prefix, label: `${prefix}（整个值）` }];
  for (const [name, child] of Object.entries(schema?.properties ?? {})) {
    const path = prefix + (/^[A-Za-z_][\w-]*$/.test(name) ? `.${name}` : `[${JSON.stringify(name)}]`);
    result.push({ path, label: `${path} · ${child.type}` });
    if (child.properties) result.push(...fieldPaths(child, path).slice(1));
  }
  return result;
}
export function samplePaths(value: unknown, prefix: string): { path: string; label: string }[] {
  const result: { path: string; label: string }[] = [];
  const walk = (item: unknown, path: string, depth: number) => {
    if (depth > 8 || result.length >= 256) return;
    const type = item === null ? "null" : Array.isArray(item) ? "array" : typeof item;
    result.push({ path, label: `${path} · ${type}（样本推断）` });
    if (Array.isArray(item)) item.slice(0, 3).forEach((row, index) => walk(row, `${path}[${index}]`, depth + 1));
    else if (item && typeof item === "object")
      Object.entries(item).forEach(([name, child]) =>
        walk(child, path + (/^[A-Za-z_][\w-]*$/.test(name) ? `.${name}` : `[${JSON.stringify(name)}]`), depth + 1),
      );
  };
  walk(value, prefix, 0);
  return result;
}
export function stableJSON(value: unknown): string {
  const sort = (item: any): any =>
    Array.isArray(item)
      ? item.map(sort)
      : item && typeof item === "object"
        ? Object.fromEntries(
            Object.keys(item)
              .sort()
              .map((key) => [key, sort(item[key])]),
          )
        : item;
  return JSON.stringify(sort(value));
}
export function definitionFingerprint(item: WorkflowItem): string {
  return stableJSON({
    workflowKey: item.workflowKey,
    name: item.name,
    description: item.description,
    graph: item.graph,
    input: item.input,
    scheduleType: item.scheduleType,
    cronExpression: item.cronExpression ?? "",
    scheduleTimezone: item.scheduleTimezone,
    retryPolicy: item.retryPolicy,
  });
}
export function executionBlock(item: WorkflowItem, cap: Capabilities | null): string {
  if (item.graph.schemaVersion !== 2) return "";
  if (!cap) return "正在检查平台能力";
  if (!cap.v2Enabled || (item.graph.steps.some((step) => step.type === "code") && !cap.code.enabled))
    return "平台暂未开放执行";
  if (item.graph.steps.some((step) => step.type === "code") && !cap.code.local.available) return "代码执行环境暂不可用";
  return "";
}

// Rewrite structured references only. Source code, literal values and schema
// descriptions are opaque business text and must survive names changing.
function rewriteValue(value: unknown, from: string, to: string, path = false): any {
  if (typeof value === "string") {
    if (path && (value === from || value.startsWith(from + ".") || value.startsWith(from + "[")))
      return to + value.slice(from.length);
    return value.replace(/\{\{(\??)([^{}]+)\}\}/g, (whole, optional: string, raw: string) => {
      const text = raw.trim();
      if (text === from || text.startsWith(from + ".") || text.startsWith(from + "["))
        return `{{${optional}${to}${text.slice(from.length)}}}`;
      return whole;
    });
  }
  if (Array.isArray(value)) return value.map((item) => rewriteValue(item, from, to));
  if (value && typeof value === "object") {
    const marker = (value as any).$value;
    if (Object.keys(value).length === 1 && marker?.kind === "literal") return value;
    if (Object.keys(value).length === 1 && marker?.kind === "branch")
      return {
        $value: {
          ...marker,
          conditionId: marker.conditionId === from ? to : marker.conditionId,
          cases: rewriteValue(marker.cases, from, to),
        },
      };
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, rewriteValue(item, from, to, key === "path")]),
    );
  }
  return value;
}
export function renameVariable(graph: Definition, oldName: string, name: string): Definition {
  if (
    !/^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(name) ||
    graph.variables?.some((variable) => variable.name === name && name !== oldName)
  )
    throw new Error("变量名须为字母开头的 1–64 位字母、数字或下划线，且不能重复。");
  const rewrite = (value: unknown) =>
    rewriteValue(
      rewriteValue(rewriteValue(value, `vars.${oldName}`, `vars.${name}`), `vars["${oldName}"]`, `vars.${name}`),
      `vars['${oldName}']`,
      `vars.${name}`,
    );
  const assignments = (rows: Assignment[] | undefined) =>
    rows?.map((row) => ({ variable: row.variable === oldName ? name : row.variable, value: rewrite(row.value) }));
  return {
    ...graph,
    variables: graph.variables?.map((variable) => (variable.name === oldName ? { ...variable, name } : variable)),
    output: rewrite(graph.output),
    steps: graph.steps.map((step) => ({
      ...step,
      input: step.input ? rewrite(step.input) : undefined,
      source: step.source === undefined ? undefined : rewrite(step.source),
      operations: step.operations ? rewrite(step.operations) : undefined,
      runIf: step.runIf ? rewrite(step.runIf) : undefined,
      assign: assignments(step.assign),
      branches: step.branches?.map((branch) => ({
        ...branch,
        condition: branch.condition ? rewrite(branch.condition) : undefined,
        assign: assignments(branch.assign),
      })),
    })),
  };
}
export function rewriteSelfAssignments(step: Step, originalID: string): Step {
  return {
    ...step,
    assign: step.assign?.map((row) => ({ ...row, value: rewriteValue(row.value, originalID, step.id) })),
    branches: step.branches?.map((branch) => ({
      ...branch,
      assign: branch.assign?.map((row) => ({ ...row, value: rewriteValue(row.value, originalID, step.id) })),
    })),
  };
}

export function renameStep(graph: Definition, from: string, to: string): Definition {
  if (!/^[A-Za-z][A-Za-z0-9_]{0,63}$/.test(to) || graph.steps.some((step) => step.id === to))
    throw new Error("步骤名无效或已存在");
  const rewrite = (v: unknown) => rewriteValue(rewriteValue(v, from, to), `status.${from}`, `status.${to}`);
  return {
    ...graph,
    output: rewrite(graph.output),
    steps: graph.steps.map((step) => ({
      ...step,
      id: step.id === from ? to : step.id,
      dependsOn: step.dependsOn?.map((id) => (id === from ? to : id)),
      scope: step.scope?.map((scope) => ({
        ...scope,
        conditionId: scope.conditionId === from ? to : scope.conditionId,
      })),
      input: step.input ? rewrite(step.input) : undefined,
      source: step.source === undefined ? undefined : rewrite(step.source),
      operations: step.operations ? rewrite(step.operations) : undefined,
      runIf: step.runIf ? rewrite(step.runIf) : undefined,
      assign: step.assign?.map((row) => ({ ...row, value: rewrite(row.value) })),
      branches: step.branches?.map((branch) => ({
        ...branch,
        condition: branch.condition ? rewrite(branch.condition) : undefined,
        assign: branch.assign?.map((row) => ({ ...row, value: rewrite(row.value) })),
      })),
    })),
  };
}
export function assertDefinitionShape(value: any): asserts value is Definition {
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    !Array.isArray(value.steps) ||
    !value.output ||
    typeof value.output !== "object" ||
    Array.isArray(value.output)
  )
    throw new Error("定义须包含 steps 数组与 output 对象");
  const ids = new Set<string>();
  const assignments = (rows: any) => {
    if (
      rows !== undefined &&
      (!Array.isArray(rows) || rows.some((row: any) => !row || typeof row.variable !== "string"))
    )
      throw new Error("assign 必须为变量赋值数组");
  };
  const condition = (value: any, depth = 0): void => {
    if (value === undefined || value === null) return;
    if (depth > 16 || typeof value !== "object" || Array.isArray(value))
      throw new Error("条件须为对象且最多嵌套 16 层");
    for (const group of ["all", "any"])
      if (value[group] !== undefined) {
        if (!Array.isArray(value[group])) throw new Error("条件组须为数组");
        value[group].forEach((child: any) => condition(child, depth + 1));
      }
  };
  for (const step of value.steps) {
    if (!step || typeof step.id !== "string" || !step.id || ids.has(step.id)) throw new Error("步骤必须有不重复的 id");
    ids.add(step.id);
    if (step.type !== undefined && !["api", "transform", "condition", "code"].includes(step.type))
      throw new Error("未知节点类型");
    if (
      step.operations !== undefined &&
      (!Array.isArray(step.operations) || step.operations.some((row: any) => !row || typeof row.op !== "string"))
    )
      throw new Error("operations 须为操作数组");
    if (
      step.scope !== undefined &&
      (!Array.isArray(step.scope) ||
        step.scope.some(
          (scope: any) => !scope || typeof scope.conditionId !== "string" || typeof scope.branchId !== "string",
        ))
    )
      throw new Error("scope 必须包含条件和出口 id");
    if (step.branches !== undefined && !Array.isArray(step.branches)) throw new Error("branches 必须为数组");
    for (const branch of step.branches ?? []) {
      if (!branch || typeof branch.id !== "string") throw new Error("出口必须包含 id");
      assignments(branch.assign);
      condition(branch.condition);
    }
    assignments(step.assign);
    condition(step.runIf);
    if (step.inputSchema) assertSchemaShape(step.inputSchema);
    if (step.outputSchema) assertSchemaShape(step.outputSchema);

    if (
      step.dependsOn !== undefined &&
      (!Array.isArray(step.dependsOn) || step.dependsOn.some((id: any) => typeof id !== "string"))
    )
      throw new Error("dependsOn 必须为步骤 id 数组");
  }
  if (value.inputSchema) assertSchemaShape(value.inputSchema);
  if (value.variables !== undefined && !Array.isArray(value.variables)) throw new Error("variables 必须为数组");
  if (value.variables?.some((row: any) => !row || typeof row.name !== "string")) throw new Error("变量须包含名称");
}
export const codeTemplates: {
  title: string;
  code: string;
  inputSchema: Schema;
  outputSchema: Schema;
  input: Record<string, unknown>;
}[] = [
  {
    title: "多字段计算",
    inputSchema: {
      type: "object",
      properties: {
        unitPriceCents: { type: "integer" },
        quantity: { type: "integer" },
        discountCents: { type: "integer" },
      },
      required: ["unitPriceCents", "quantity", "discountCents"],
    },
    input: { unitPriceCents: 1000, quantity: 2, discountCents: 100 },
    outputSchema: { type: "object", properties: { totalCents: { type: "integer" } }, required: ["totalCents"] },
    code: 'function main(input) {\n  // 金额使用最小货币单位；显式检查业务范围。\n  if (input.quantity < 0 || input.unitPriceCents < 0) throw Error("invalid amount");\n  return { totalCents: input.unitPriceCents * input.quantity - input.discountCents };\n}',
  },
  {
    title: "列表按 ID 关联",
    inputSchema: {
      type: "object",
      properties: {
        left: { type: "array", items: { type: "object" } },
        right: { type: "array", items: { type: "object" } },
      },
      required: ["left", "right"],
    },
    input: { left: [], right: [] },
    outputSchema: {
      type: "object",
      properties: { rows: { type: "array", items: { type: "object" } } },
      required: ["rows"],
    },
    code: "function main(input) {\n  const byId = new Map(input.right.map(row => [row.id, row]));\n  return { rows: input.left.map(row => ({ ...row, related: byId.get(row.id) || null })) };\n}",
  },
  {
    title: "结构转换",
    inputSchema: {
      type: "object",
      properties: { rows: { type: "array", items: { type: "object" } } },
      required: ["rows"],
    },
    input: { rows: [] },
    outputSchema: {
      type: "object",
      properties: { rows: { type: "array", items: { type: "object" } } },
      required: ["rows"],
    },
    code: "function main(input) {\n  return { rows: input.rows.map(row => ({ id: row.id, name: row.name })) };\n}",
  },
];

export function assertSchemaShape(schema: any): void {
  if (!schema || typeof schema !== "object" || Array.isArray(schema)) throw new Error("Schema 必须是对象");
  if (schema.properties !== undefined) {
    if (!schema.properties || typeof schema.properties !== "object" || Array.isArray(schema.properties))
      throw new Error("properties 必须为对象");
    Object.values(schema.properties).forEach(assertSchemaShape);
  }
  if (schema.items !== undefined) assertSchemaShape(schema.items);
  if (
    schema.required !== undefined &&
    (!Array.isArray(schema.required) || schema.required.some((name: any) => typeof name !== "string"))
  )
    throw new Error("required 必须为字段名数组");
}
