import { Plus, Settings2, Undo2, Redo2, Search, Maximize2, Minimize2, Ellipsis } from "lucide-react";
import { createContext, useContext, useEffect, useRef, useState } from "react";
import { Link, Outlet, useNavigate, useParams, useLocation } from "react-router";
import { type Connection } from "@xyflow/react";
import { api } from "../api";
import { useDemo } from "../demo";
import { useCanWrite } from "../permissions";
import { WorkflowNavigationGuard } from "./workflow-navigation";
import { WorkflowCall } from "./workflow-call";
import { Button, Card, Modal, fieldClass } from "../ui";
import { WorkflowCanvas, autoLayout } from "./workflow-canvas";
import { JsonField, JsonBufferContext, ObjectInput, WorkflowInspector, type ActionChoice } from "./workflow-inspector";
import {
  blankWorkflow,
  canDepend,
  dependencies,
  joinCondition,
  definitionFingerprint,
  executionBlock,
  kindLabel,
  newStep,
  insertStep,
  placeStep,
  orderedSteps,
  stepReferences,
  upgradeGraph,
  stableJSON,
  rewriteSelfAssignments,
  renameStep,
  assertDefinitionShape,
  type Capabilities,
  type Layout,
  type NodeKind,
  type Step,
  type WorkflowItem,
} from "./workflow-model";
interface Session {
  item: WorkflowItem;
  setItem: (item: WorkflowItem) => void;
  cap: Capabilities | null;
  actions: ActionChoice[];
  refreshCapabilities: () => Promise<void>;
  selected: string;
  setSelected: (id: string) => void;
  definitionDirty: boolean;
  layoutDirty: boolean;
  baseline: (item: WorkflowItem) => void;
}
const SessionContext = createContext<Session | null>(null);
export function useWorkflowSession() {
  return useContext(SessionContext);
}
export function WorkflowSession() {
  const { id } = useParams();
  const location = useLocation();
  const demo = useDemo();
  const [item, setItem] = useState(blankWorkflow);
  const [cap, setCap] = useState<Capabilities | null>(null);
  const [actions, setActions] = useState<ActionChoice[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState("");
  const [definitionBase, setDefinitionBase] = useState("");
  const [layoutBase, setLayoutBase] = useState("{}");
  const loaded = useRef("");
  useEffect(() => {
    let stopped = false;
    if (!demo.authenticated) return;
    setLoading(true);
    void Promise.all([
      id === "new"
        ? Promise.resolve(blankWorkflow())
        : id === loaded.current
          ? Promise.resolve(item)
          : api.workflow(id!),
      api.workflowCapabilities(),
      api.actions(),
    ])
      .then(([workflow, capabilities, available]) => {
        if (stopped) return;
        if (id !== loaded.current) {
          workflow = { ...workflow, retryPolicy: { maxAttempts: 1 } };
          setItem(workflow);
          setDefinitionBase(definitionFingerprint(workflow));
          setLayoutBase(stableJSON(workflow.editorLayout ?? {}));
          setSelected("");
          loaded.current = id ?? "";
        }
        setCap(capabilities);
        setActions(available);
        setLoading(false);
      })
      .catch((error) => {
        if (!stopped) {
          setError(String(error));
          setLoading(false);
        }
      });
    return () => {
      stopped = true;
    };
  }, [id, demo.authenticated]);
  useEffect(() => {
    if (!demo.authenticated) {
      setItem(blankWorkflow());
      loaded.current = "";
    }
  }, [demo.authenticated]);
  const baseline = (saved: WorkflowItem) => {
    setDefinitionBase(definitionFingerprint(saved));
    setLayoutBase(stableJSON(saved.editorLayout ?? {}));
    loaded.current = saved.id || "new";
  };
  if (loading) return <p role="status">正在加载工作流…</p>;
  if (error) return <p role="alert">{error}</p>;
  return (
    <SessionContext.Provider
      value={{
        item,
        setItem,
        cap,
        actions,
        refreshCapabilities: async () => {
          const [capabilities, available] = await Promise.all([api.workflowCapabilities(), api.actions(true)]);
          setCap(capabilities);
          setActions(available);
        },
        selected,
        setSelected,
        definitionDirty: definitionFingerprint(item) !== definitionBase,
        layoutDirty: stableJSON(item.editorLayout ?? {}) !== layoutBase,
        baseline,
      }}
    >
      <div hidden={location.pathname !== `/workflows/${id}`}>
        <WorkflowEditor key={id} active={location.pathname === `/workflows/${id}`} />
      </div>
      <Outlet />
    </SessionContext.Provider>
  );
}
export function WorkflowEditor({ active = true }: { active?: boolean }) {
  const session = useWorkflowSession();
  const navigate = useNavigate();
  const demo = useDemo();
  const canWrite = useCanWrite();
  const [message, setMessage] = useState("");
  const [diagnostics, setDiagnostics] = useState<any[]>([]);
  const [focusedDiagnostic, setFocusedDiagnostic] = useState<any>();
  const [pending, setPending] = useState(false);
  const inspectorTrigger = useRef<HTMLButtonElement>(null),
    graphTrigger = useRef<HTMLButtonElement>(null),
    runTrigger = useRef<HTMLButtonElement>(null);
  const [inspectorOpen, setInspectorOpen] = useState(false);
  const [invalid, setInvalid] = useState<Record<string, boolean>>({});
  const buffers = useRef<Record<string, { text: string; invalid: boolean; base: string }>>({});
  const invalidate = (key: string, invalid: boolean) =>
    setInvalid((current) => (current[key] === invalid ? current : { ...current, [key]: invalid }));
  const [past, setPast] = useState<WorkflowItem[]>([]);
  const [future, setFuture] = useState<WorkflowItem[]>([]);
  const [runOpen, setRunOpen] = useState(false);
  const [runInvalid, setRunInvalid] = useState(false);
  const [runInput, setRunInput] = useState<Record<string, unknown>>({});
  const [focusRequest, setFocusRequest] = useState<{ id: string; sequence: number }>();
  const [mobileGraph, setMobileGraph] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [serverVersion, setServerVersion] = useState<WorkflowItem | null>(null);
  const samples = useRef<Record<string, any>>({});
  const [graphOpen, setGraphOpen] = useState(false);
  const [focused, setFocused] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [stepSearch, setStepSearch] = useState("");
  const [moreOpen, setMoreOpen] = useState(false);
  const [addContext, setAddContext] = useState<{ source: string; branch?: string; target?: string } | null>(null);
  const [insertExit, setInsertExit] = useState("if1");
  const [addKind, setAddKind] = useState<NodeKind | null>(null);
  const [joinOpen, setJoinOpen] = useState(false);
  const [joinId, setJoinId] = useState("");
  const lastEdit = useRef<{ element: Element | null; time: number } | null>(null);
  const [mobile, setMobile] = useState(() => window.innerWidth < 1024);
  useEffect(() => {
    const resize = () => setMobile(window.innerWidth < 1024);
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, []);
  const { item, setItem, cap, actions, selected, setSelected, definitionDirty, layoutDirty, baseline } = session!;
  const invalidBuffer = Object.values(invalid).some(Boolean);
  const current = useRef(item);
  current.current = item;

  const mutate = (next: WorkflowItem) => {
    if (!canWrite) return;
    const element = document.activeElement;
    const typing = element?.matches("input,textarea,[contenteditable=true]") || element?.closest(".cm-editor");
    if (!typing || lastEdit.current?.element !== element || Date.now() - lastEdit.current.time > 800)
      setPast((history) => [...history.slice(-49), item]);
    lastEdit.current = typing ? { element, time: Date.now() } : null;
    setFuture([]);
    setItem(next);
  };
  const upgrade = (graph: WorkflowItem["graph"]) => {
    if (graph.schemaVersion !== 2 && graph.steps.some((step) => step.id === "vars")) {
      let name = "legacy_vars",
        i = 1;
      while (graph.steps.some((step) => step.id === name)) name = `legacy_vars${i++}`;
      if (
        !window.confirm(
          `升级到 v2 需将旧步骤 vars 改名为 ${name}，同步更新依赖、条件路径、输入与返回引用。源码和固定业务字面量保持原文。确认后整体应用，可撤销。`,
        )
      )
        throw new Error("已保留原定义，未升级");
      graph = renameStep(graph, "vars", name);
    }
    return upgradeGraph(graph);
  };
  const updateBusiness = (next: WorkflowItem) => {
    try {
      const needsV2 =
        next.graph.schemaVersion === 2 ||
        !!next.graph.inputSchema ||
        !!next.graph.variables?.length ||
        next.graph.steps.some(
          (step) => !!step.assign?.length || !!step.scope?.length || (!!step.type && step.type !== "api"),
        ) ||
        JSON.stringify({ ...next.graph, steps: next.graph.steps.map(({ code: _code, ...step }) => step) }).includes(
          '"$value"',
        );
      if (needsV2 && !cap?.v2Enabled) throw new Error("工作流 v2 当前关闭，暂时不能添加新节点或映射能力。");
      let layout = next.editorLayout;
      if (layout?.positions && next.graph.steps.some((step) => !layout!.positions[step.id])) {
        for (const step of orderedSteps(next.graph))
          if (!layout.positions[step.id])
            layout = placeStep(layout, step, step.dependsOn?.[0] ?? "@trigger", undefined, next.graph);
      }
      mutate({ ...next, editorLayout: layout, graph: needsV2 ? upgrade(next.graph) : next.graph });
    } catch (error) {
      setMessage(String(error));
    }
  };
  const select = (id: string, configure = false, locate = false) => {
    if (id !== selected && invalidBuffer && !window.confirm("当前 JSON 尚未应用，切换节点会丢弃缓冲，是否继续？"))
      return;
    if (id !== selected) {
      setInvalid({});
      buffers.current = {};
    }
    setSelected(id);
    setMoreOpen(false);
    if (locate) setFocusRequest({ id, sequence: Date.now() });
    if (configure) setInspectorOpen(true);
    lastEdit.current = null;
  };
  const clearSelection = () => {
    setSearchOpen(false);
    setMoreOpen(false);
    setMoreOpen(false);
    setStepSearch("");
    if (invalidBuffer) {
      setInspectorOpen(false);
      return;
    }
    select("");
    setInspectorOpen(false);
  };
  const requestAdd = (source = selected, branch?: string, target?: string) => {
    if (!canWrite || item.graph.steps.length >= 20) return;
    if (mobile) setInspectorOpen(false);
    if (!source || source === "@result") {
      const tails = item.graph.steps.filter(
        (step) => !item.graph.steps.some((other) => other.dependsOn?.includes(step.id)),
      );
      if (tails.length === 1 || !item.graph.steps.length) {
        target = source === "@result" ? "@result" : target;
        source = tails[0]?.id ?? "@trigger";
      }
    }
    setInsertExit("if1");
    setAddKind(null);
    setMessage("");
    setAddContext({ source, branch, target });
  };
  const add = (kind: NodeKind) => {
    if (!addContext) return;
    try {
      if (invalidBuffer) throw new Error("请先修正当前未应用的 JSON，再添加节点。");
      if (kind !== "api" && !cap?.v2Enabled) throw new Error("工作流 v2 当前关闭");
      if (kind === "code" && !cap?.code.enabled) throw new Error("代码节点开关当前关闭");
      let graph = item.graph;
      if (kind !== "api" || graph.schemaVersion === 2) graph = upgrade(graph);
      if (graph.steps.length >= 20) throw new Error("最多 20 个执行步骤");
      const after = graph.steps.find((step) => step.id === addContext.source);
      if (!after && addContext.source !== "@trigger") throw new Error("请选择添加位置");
      if (after?.type === "condition" && !addContext.branch) throw new Error("请选择条件出口");
      const step = newStep(graph, kind, after, addContext.branch);
      if (graph.schemaVersion !== 2) {
        delete step.type;
        delete step.scope;
      }
      const nextGraph = addContext.target
        ? insertStep(graph, step, addContext.source, addContext.target, insertExit)
        : { ...graph, steps: [...graph.steps, step] };
      const layout = placeStep(
        item.editorLayout?.positions ? item.editorLayout : autoLayout(graph),
        step,
        addContext.source,
        addContext.target,
        graph,
      );
      mutate({ ...item, editorLayout: layout, graph: nextGraph });
      setAddContext(null);
      select(step.id, true, true);
      if (kind === "condition" && addContext.target && addContext.target !== "@result")
        setMessage("后续路径已接到所选条件出口；请检查结束返回映射和其他路径中的引用，必要时改为按分支取值。");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error));
    }
  };
  const stepUpdate = (step: Step) =>
    updateBusiness({
      ...item,
      graph: { ...item.graph, steps: item.graph.steps.map((row) => (row.id === selected ? step : row)) },
    });
  const remove = () => {
    const step = item.graph.steps.find((step) => step.id === selected);
    if (!step) return;
    const references = stepReferences(item.graph, step.id);
    if (references.length) {
      setMessage(`请先修正这些引用，再删除“${step.title ?? step.id}”：${references.join("、")}。`);
      return;
    }
    const affected = item.graph.steps.filter((row) => row.dependsOn?.includes(step.id));
    if (
      !window.confirm(
        `删除 ${step.title ?? step.id}。受影响：${affected.map((step) => step.title ?? step.id).join("、") || "无"}。后续节点将接到它的前置步骤，保留祖先引用；操作可撤销。`,
      )
    )
      return;
    mutate({
      ...item,
      graph: {
        ...item.graph,
        steps: item.graph.steps
          .filter((row) => row.id !== step.id)
          .map((row) =>
            row.dependsOn?.includes(step.id)
              ? {
                  ...row,
                  dependsOn: [...new Set([...row.dependsOn.filter((id) => id !== step.id), ...(step.dependsOn ?? [])])],
                }
              : row,
          ),
      },
    });
    clearSelection();
  };
  const copy = () => {
    const source = item.graph.steps.find((step) => step.id === selected);
    if (!source || item.graph.steps.length >= 20 || invalidBuffer) return;
    const next = newStep(item.graph, source.type ?? "api");
    const clone = structuredClone(source);
    clone.id = next.id;
    clone.title = `${source.title ?? source.id} 副本`;
    mutate({
      ...item,
      graph: { ...item.graph, steps: [...item.graph.steps, rewriteSelfAssignments(clone, source.id)] },
      editorLayout: placeStep(item.editorLayout ?? autoLayout(item.graph), clone, source.id, undefined, item.graph),
    });
    select(clone.id, true, true);
  };
  const join = (conditionId: string) => {
    const node = item.graph.steps.find((step) => step.id === selected);
    const ending = selected === "@result";
    if (!node && !ending) return;
    const condition = item.graph.steps.find((step) => step.id === conditionId && step.type === "condition");
    if (!condition) {
      setMessage("选择一个存在的条件节点。");
      return;
    }
    if (ending) {
      const related = item.graph.steps.filter(
        (step) => step.id === condition.id || step.scope?.some((scope) => scope.conditionId === condition.id),
      );
      const continuations = item.graph.steps.filter(
        (step) =>
          step.id !== condition.id &&
          dependencies(item.graph, step.id).has(condition.id) &&
          !step.scope?.some((scope) => scope.conditionId === condition.id),
      );
      if (continuations.length) {
        setMessage(
          `这些路径仍有公共后续：${continuations.map((step) => step.title ?? step.id).join("、")}。请选择后续节点汇合。`,
        );
        return;
      }
      const nested = related
        .filter((step) => step.type === "condition")
        .sort((a, b) => (b.scope?.length ?? 0) - (a.scope?.length ?? 0));
      if (
        window.confirm(
          `在结束汇合：按 ${nested.map((step) => step.title ?? step.id).join(" → ")} 从内到外收口，保留 ${related.map((step) => step.title ?? step.id).join("、")} 及其他根路径。最终映射：${JSON.stringify(item.graph.output)}。仅整理显示布局，结束仍等待全部有效步骤；可一次撤销。`,
        )
      )
        mutate({ ...item, editorLayout: autoLayout(item.graph) });
      return;
    }
    if (!node) return;
    try {
      const graph = joinCondition(item.graph, node.id, condition.id);
      const joined = graph.steps.find((step) => step.id === node.id)!;
      const moved = graph.steps.filter(
        (step, index) => stableJSON(step.scope ?? []) !== stableJSON(item.graph.steps[index].scope ?? []),
      );
      if (
        window.confirm(
          `汇合 ${condition.title ?? condition.id} → ${node.title ?? node.id}。等待 ${joined.dependsOn!.map((id) => item.graph.steps.find((step) => step.id === id)?.title ?? id).join("、")}。同步调整路径：${moved.map((step) => step.title ?? step.id).join("、") || "无需调整"}。空分支直接等待原条件，检查参数的按分支来源。应用可一次撤销。`,
        )
      )
        updateBusiness({ ...item, graph });
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error));
    }
  };
  const connect = (connection: Connection) => {
    if (connection.target === "@result") {
      if (connection.source) setMessage("结束等待所有路径；如需结构化汇合，请选择公共节点并使用“在此汇合”。");
      return;
    }
    const target = item.graph.steps.find((step) => step.id === connection.target);
    const source = item.graph.steps.find((step) => step.id === connection.source);
    if (!target || !source) return;
    const expectedScope =
      source.type === "condition" && connection.sourceHandle
        ? [...(source.scope ?? []), { conditionId: source.id, branchId: connection.sourceHandle }]
        : (source.scope ?? []);
    if (stableJSON(target.scope ?? []) !== stableJSON(expectedScope)) {
      setMessage("不能直接连接不同分支；请选择公共后续节点并使用“在此汇合”。");
      return;
    }
    if (!canDepend(item.graph, target.id, source.id)) {
      setMessage("此依赖会产生环");
      return;
    }
    const scope =
      source.type === "condition" && connection.sourceHandle
        ? [...(source.scope ?? []), { conditionId: source.id, branchId: connection.sourceHandle }]
        : target.scope;
    updateBusiness({
      ...item,
      graph: {
        ...item.graph,
        steps: item.graph.steps.map((step) =>
          step.id === target.id
            ? { ...step, scope, dependsOn: [...new Set([...(step.dependsOn ?? []), source.id])] }
            : step,
        ),
      },
    });
  };
  const block = executionBlock(item, cap);
  async function save(layoutOnly = false, deploy = false, run = false) {
    if (!canWrite || pending || invalidBuffer) return;
    if (layoutOnly && !item.id) layoutOnly = false;
    if (!layoutOnly && (!/^[A-Za-z0-9_.-]{1,100}$/.test(item.workflowKey.trim()) || !item.name.trim())) {
      const fieldPath = !/^[A-Za-z0-9_.-]{1,100}$/.test(item.workflowKey.trim()) ? "/workflowKey" : "/name";
      select("@trigger", true, true);
      setFocusedDiagnostic({ fieldPath, sequence: Date.now() });
      setMessage(
        fieldPath === "/workflowKey"
          ? "请填写工作流标识：1–100 位字母、数字、短横线、下划线或点。"
          : "请填写工作流名称。",
      );
      return;
    }
    setPending(true);
    setMessage("");
    const captured = structuredClone(item);
    try {
      let saved: WorkflowItem;
      if (layoutOnly) {
        saved = await api.saveWorkflowLayout(captured.id, {
          expectedWorkflowVersion: captured.version,
          layoutVersion: captured.layoutVersion,
          editorLayout: captured.editorLayout,
        });
        baseline({ ...captured, ...saved });
        setItem({ ...current.current, layoutVersion: saved.layoutVersion });
        setMessage("布局已保存，部署状态和调度未改变。");
        return;
      }
      saved = await api.saveWorkflow(
        {
          workflowKey: captured.workflowKey,
          name: captured.name,
          description: captured.description,
          graph: captured.graph,
          input: captured.input,
          retryPolicy: captured.retryPolicy,
          scheduleType: captured.scheduleType,
          cronExpression: captured.cronExpression,
          scheduleTimezone: captured.scheduleTimezone,
          editorLayout: captured.editorLayout,
          layoutVersion: captured.layoutVersion,
        },
        captured.id,
        captured.version,
      );
      baseline(saved);
      setItem({
        ...current.current,
        id: saved.id,
        version: saved.version,
        layoutVersion: saved.layoutVersion,
        status: saved.status,
        nextRunAt: saved.nextRunAt,
      });
      if (!captured.id) navigate(`/workflows/${saved.id}`, { replace: true });
      if (definitionFingerprint(current.current) !== definitionFingerprint(captured)) {
        setMessage("已保存提交时的版本；之后的修改仍未保存。");
        return;
      }
      if (deploy) {
        try {
          saved = await api.deployWorkflow(saved.id, saved.version);
          baseline(saved);
          if (definitionFingerprint(current.current) !== definitionFingerprint(captured)) {
            setMessage("已部署提交时的版本；之后的修改仍未保存。");
          }
          setItem({ ...current.current, version: saved.version, status: saved.status, nextRunAt: saved.nextRunAt });
        } catch (error) {
          setMessage(`已保存，但部署失败；新的触发已停止。${String(error)}`);
          return;
        }
      }
      if (run) {
        const operation = await api.runWorkflow(saved.id, runInput, saved.version);
        navigate(`/workflows/${saved.id}/runs/${operation.id}`);
      } else if (definitionFingerprint(current.current) === definitionFingerprint(captured))
        setMessage(deploy ? "已保存并部署" : "草稿已保存，需要部署后恢复新触发");
      void demo.reload();
    } catch (error) {
      setMessage(String(error));
      setDiagnostics((error as any)?.details?.diagnostics ?? []);
      if ((error as any)?.code === "conflict") setConflict(true);
    } finally {
      setPending(false);
    }
  }
  const undo = (redo = false) => {
    if (!canWrite || invalidBuffer) return;
    const next = redo ? future[0] : past.at(-1);
    if (!next) return;
    if (redo) {
      setPast([...past, item]);
      setFuture(future.slice(1));
    } else {
      setFuture([item, ...future]);
      setPast(past.slice(0, -1));
    }
    setItem({
      ...next,
      id: item.id,
      version: item.version,
      layoutVersion: item.layoutVersion,
      status: item.status,
      nextRunAt: item.nextRunAt,
    });
    lastEdit.current = null;
    if (
      selected &&
      selected !== "@trigger" &&
      selected !== "@result" &&
      !next.graph.steps.some((step) => step.id === selected)
    ) {
      const anchor =
        item.graph.steps
          .find((step) => step.id === selected)
          ?.dependsOn?.find((id) => next.graph.steps.some((step) => step.id === id)) ?? "@trigger";
      setSelected(anchor);
      setFocusRequest({ id: anchor, sequence: Date.now() });
    }
  };
  useEffect(() => {
    if (!active) return;
    const key = (event: KeyboardEvent) => {
      if (event.defaultPrevented || (event.target as HTMLElement)?.closest?.("[role=dialog]")) return;
      if (event.key === "Escape" && !graphOpen && !runOpen) {
        if (searchOpen) {
          setSearchOpen(false);
          setStepSearch("");
        } else if (stepSearch) setStepSearch("");
        else if (addContext) setAddContext(null);
        else if (joinOpen) setJoinOpen(false);
        else if (moreOpen) setMoreOpen(false);
        else if (inspectorOpen) {
          setInspectorOpen(false);
          inspectorTrigger.current?.focus();
        } else if (focused) setFocused(false);
        return;
      }
      if (
        (event.ctrlKey || event.metaKey) &&
        event.key.toLowerCase() === "s" &&
        !graphOpen &&
        !runOpen &&
        !addContext
      ) {
        event.preventDefault();
        if (layoutDirty && !definitionDirty) void save(true);
        else void save();
        return;
      }
      if (
        (event.target as HTMLElement)?.closest?.(
          "input,textarea,select,[contenteditable=true],.cm-editor,[role=dialog]",
        )
      )
        return;
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") {
        event.preventDefault();
        undo(event.shiftKey);
      }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "y") {
        event.preventDefault();
        undo(true);
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  });
  const mainAction =
    item.status === "deployed"
      ? definitionDirty
        ? "保存并部署"
        : "运行"
      : !item.id || definitionDirty
        ? "保存草稿"
        : "部署";
  const inspector = (
    <fieldset data-workflow-inspector disabled={!canWrite} className="grid min-w-0 gap-4">
      <WorkflowInspector
        active={active}
        item={item}
        selected={selected}
        diagnostic={focusedDiagnostic}
        canPreview={item.graph.schemaVersion !== 2 || !!cap?.v2Enabled}
        codeAvailable={!!cap?.code.enabled && !!cap?.code.local.available}
        actions={actions}
        onItem={updateBusiness}
        onStep={stepUpdate}
        onDelete={remove}
        onCopy={copy}
        onJoin={() => {
          setJoinId(item.graph.steps.find((step) => step.type === "condition")?.id ?? "");
          setJoinOpen(true);
        }}
        onInvalid={(key, invalid) =>
          setInvalid((current) => (current[key] === invalid ? current : { ...current, [key]: invalid }))
        }
      />
    </fieldset>
  );
  const tools = (
    <div className={mobile ? "flex flex-wrap items-center gap-2" : "workflow-editor-tools flex flex-col gap-1"}>
      <Button
        write
        disabled={!canWrite || item.graph.steps.length >= 20}
        variant="secondary"
        onClick={() => requestAdd()}
        aria-label="＋ 添加节点"
        title="添加节点"
      >
        {mobile ? "＋ 添加节点" : <Plus size={18} aria-hidden="true" />}
      </Button>
      <Button
        ref={inspectorTrigger}
        aria-label={inspectorOpen ? "收起配置" : "配置节点"}
        title={inspectorOpen ? "收起配置" : "配置节点"}
        variant="ghost"
        disabled={!selected}
        aria-expanded={inspectorOpen}
        onClick={() => setInspectorOpen(!inspectorOpen)}
      >
        {mobile ? inspectorOpen ? "收起配置" : "配置节点" : <Settings2 size={18} aria-hidden="true" />}
      </Button>
      <Button
        variant="ghost"
        disabled={!past.length || invalidBuffer}
        onClick={() => undo()}
        aria-label="撤销"
        title="撤销 Ctrl/⌘ Z"
      >
        {mobile ? "撤销" : <Undo2 size={18} aria-hidden="true" />}
      </Button>
      <Button
        variant="ghost"
        disabled={!future.length || invalidBuffer}
        onClick={() => undo(true)}
        aria-label="重做"
        title="重做 Ctrl/⌘ Shift Z"
      >
        {mobile ? "重做" : <Redo2 size={18} aria-hidden="true" />}
      </Button>
      <div className="relative">
        {!mobile && (
          <Button
            variant="ghost"
            aria-label="搜索节点"
            title="搜索节点"
            aria-expanded={searchOpen}
            onClick={() => setSearchOpen(!searchOpen)}
          >
            <Search size={18} aria-hidden="true" />
          </Button>
        )}
        {(mobile || searchOpen) && (
          <div
            className={
              mobile
                ? "w-48"
                : "absolute left-full top-0 ml-2 w-60 rounded-lg border border-[var(--border)] bg-[var(--surface)] p-2 shadow-lg"
            }
          >
            <input
              autoFocus={!mobile}
              className={fieldClass + " !py-1.5"}
              aria-label="搜索流程节点"
              placeholder="搜索 / 定位节点"
              value={stepSearch}
              onChange={(e) => setStepSearch(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && stepSearch) {
                  const step = [
                    { id: "@trigger", title: "开始" },
                    ...orderedSteps(item.graph),
                    { id: "@result", title: "结束" },
                  ].find((step) => `${step.title ?? ""} ${step.id}`.toLowerCase().includes(stepSearch.toLowerCase()));
                  if (step) {
                    select(step.id, false, true);
                    setStepSearch("");
                    setSearchOpen(false);
                  }
                }
              }}
            />
            {stepSearch && (
              <div className="absolute left-0 right-0 top-full z-20 max-h-64 overflow-auto rounded-lg border border-[var(--border)] bg-[var(--surface)] p-1 shadow-lg">
                {[{ id: "@trigger", title: "开始" }, ...orderedSteps(item.graph), { id: "@result", title: "结束" }]
                  .filter((step) => `${step.title ?? ""} ${step.id}`.toLowerCase().includes(stepSearch.toLowerCase()))
                  .map((step) => (
                    <button
                      key={step.id}
                      className="block w-full rounded p-2 text-left text-sm hover:bg-[var(--background)]"
                      onClick={() => {
                        select(step.id, false, true);
                        setStepSearch("");
                        setSearchOpen(false);
                      }}
                    >
                      {step.title ?? step.id}{" "}
                      <span className="text-xs text-[var(--muted-text)]">{step.id.startsWith("@") ? "" : step.id}</span>
                    </button>
                  ))}
              </div>
            )}
          </div>
        )}
      </div>
      <Button
        variant="ghost"
        aria-label={focused ? "退出专注" : "专注模式"}
        title={focused ? "退出专注" : "专注模式"}
        aria-pressed={focused}
        onClick={() => setFocused(!focused)}
      >
        {mobile ? (
          focused ? (
            "退出专注"
          ) : (
            "专注模式"
          )
        ) : focused ? (
          <Minimize2 size={18} aria-hidden="true" />
        ) : (
          <Maximize2 size={18} aria-hidden="true" />
        )}
      </Button>
      <div className="relative">
        <Button
          variant="ghost"
          aria-expanded={moreOpen}
          aria-label="更多"
          title="更多"
          onClick={() => setMoreOpen(!moreOpen)}
        >
          {mobile ? "更多" : <Ellipsis size={18} aria-hidden="true" />}
        </Button>
        {moreOpen && (
          <div
            className={`absolute z-20 grid min-w-44 gap-1 rounded-lg border border-[var(--border)] bg-[var(--surface)] p-2 shadow-lg ${mobile ? "right-0 top-full" : "bottom-0 left-full ml-2"}`}
          >
            <Button
              write
              variant="ghost"
              onClick={() => {
                mutate({ ...item, editorLayout: autoLayout(item.graph) });
                setMoreOpen(false);
              }}
            >
              自动布局
            </Button>
            <Button
              write
              variant="ghost"
              disabled={!layoutDirty || definitionDirty || !item.id || pending}
              onClick={() => {
                void save(true);
                setMoreOpen(false);
              }}
            >
              仅保存布局
            </Button>
            <Button
              write
              variant="ghost"
              disabled={pending || invalidBuffer}
              onClick={() => {
                void save();
                setMoreOpen(false);
              }}
            >
              仅保存草稿
            </Button>
            {item.status === "deployed" && definitionDirty && (
              <p className="max-w-60 text-xs text-[var(--muted-text)]">
                仅保存草稿会停止新的触发；重新部署后恢复，已接受的运行继续。
              </p>
            )}
            <Button
              ref={graphTrigger}
              variant="ghost"
              onClick={() => {
                setGraphOpen(true);
                setMoreOpen(false);
              }}
            >
              高级 JSON
            </Button>
            <Button
              variant="ghost"
              onClick={async () => {
                setMoreOpen(false);
                try {
                  await session!.refreshCapabilities();
                  setMessage("平台能力已刷新，当前编辑内容已保留。");
                } catch (error) {
                  setMessage(String(error));
                }
              }}
            >
              刷新平台能力
            </Button>
          </div>
        )}
      </div>
    </div>
  );
  return (
    <JsonBufferContext.Provider
      value={{ prefix: selected, buffers: buffers.current, samples: samples.current, invalidate }}
    >
      <WorkflowNavigationGuard
        dirty={definitionDirty || layoutDirty || invalidBuffer}
        prefix={`/workflows/${item.id || "new"}`}
      />
      <div
        className={
          focused
            ? "fixed inset-0 z-30 flex min-w-0 flex-col gap-2 bg-[var(--background)] p-3"
            : "flex h-[calc(100dvh-88px)] min-h-[540px] min-w-0 flex-col gap-2"
        }
      >
        <header className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <Link to="/workflows" className="shrink-0 text-sm text-[var(--muted-text)]" aria-label="返回工作流列表">
              ←
            </Link>
            <h1 className="max-w-[240px] truncate text-base font-semibold">
              <button
                type="button"
                title="配置工作流名称、输入和触发方式"
                aria-label="配置工作流"
                onClick={() => select("@trigger", true)}
              >
                {item.name}
              </button>
            </h1>
            <span role="status" className="text-xs text-[var(--muted-text)]">
              {invalidBuffer
                ? "JSON 未应用"
                : definitionDirty
                  ? "业务未保存"
                  : layoutDirty
                    ? "布局未保存"
                    : item.status === "deployed"
                      ? "已部署"
                      : item.id
                        ? "待部署"
                        : "新草稿"}
              {item.version ? ` · v${item.version}` : ""}
              {block ? ` · ${block}` : ""}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {item.id && <WorkflowCall id={item.id} active={active} />}
            {item.id && (
              <Link to="history" className="text-sm text-blue-600">
                运行历史
              </Link>
            )}
            <Button
              write
              ref={runTrigger}
              disabled={pending || invalidBuffer || (mainAction !== "保存草稿" && !!block)}
              onClick={() => {
                if (mainAction === "运行") {
                  setRunInput(item.input);
                  setRunOpen(true);
                } else void save(false, mainAction !== "保存草稿");
              }}
            >
              {pending ? "处理中…" : mainAction}
            </Button>
          </div>
        </header>
        {conflict && (
          <div role="alert" className="flex flex-wrap gap-2 border border-[var(--border)] rounded p-3 text-sm">
            <span>版本冲突，本地修改已保留。</span>
            <Button
              variant="secondary"
              onClick={async () => {
                try {
                  setServerVersion(await api.workflow(item.id));
                } catch (error) {
                  setMessage(String(error));
                }
              }}
            >
              查看服务器版本
            </Button>
            <Button
              variant="secondary"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(JSON.stringify(item, null, 2));
                  setMessage("本地配置已复制");
                } catch {
                  setGraphOpen(true);
                }
              }}
            >
              复制本地配置
            </Button>
          </div>
        )}
        <Modal
          open={!!serverVersion}
          onOpenChange={() => setServerVersion(null)}
          title="服务器版本"
          description="独立只读配置比较"
          unsavedChanges={false}
        >
          <p>
            服务器版本 {serverVersion?.version}；本地基于版本 {item.version}。本地修改尚未覆盖。
          </p>
          <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all text-xs">
            {JSON.stringify(serverVersion, null, 2)}
          </pre>
          <Button
            write
            variant="danger"
            onClick={() => {
              if (serverVersion && window.confirm("放弃本地定义、布局、样本和未应用缓冲，加载服务器版本？")) {
                setItem(serverVersion);
                baseline(serverVersion);
                setPast([]);
                setFuture([]);
                setInvalid({});
                buffers.current = {};
                samples.current = {};
                setConflict(false);
                setServerVersion(null);
              }
            }}
          >
            放弃本地修改并加载
          </Button>
        </Modal>
        {mobile && tools}
        <div className="relative flex min-h-0 min-w-0 flex-1 flex-col gap-2 lg:flex-row">
          <Card className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
            {mobile && (
              <div className="p-2">
                <Button variant="secondary" onClick={() => setMobileGraph(!mobileGraph)}>
                  {mobileGraph ? "切换步骤列表" : "查看图"}
                </Button>
              </div>
            )}
            {mobile && !mobileGraph ? (
              <div className="grid gap-2 overflow-auto p-3">
                {["@trigger", ...orderedSteps(item.graph).map((step) => step.id), "@result"].map((id, index) => (
                  <div key={id} className="flex min-w-0 items-center gap-2">
                    <Button
                      className="min-w-0 flex-1 justify-start overflow-hidden"
                      variant={selected === id ? "secondary" : "ghost"}
                      onClick={() => select(id, true)}
                    >
                      <span className="truncate">
                        {index + 1}. {item.graph.steps.find((step) => step.id === id)?.title ?? kindLabel[id] ?? id}
                      </span>
                    </Button>
                    <Button
                      write
                      disabled={item.graph.steps.length >= 20}
                      variant="ghost"
                      aria-label={`在 ${kindLabel[id] ?? id} 后添加节点`}
                      onClick={() => requestAdd(id)}
                    >
                      {id === "@result" ? "结束前＋" : "＋"}
                    </Button>
                  </div>
                ))}
              </div>
            ) : (
              <div className="min-h-[320px] flex-1">
                {!item.graph.steps.length && (
                  <div className="pointer-events-none absolute left-4 right-4 top-16 z-10 text-center text-sm text-[var(--muted-text)]">
                    点击开始节点旁的 ＋ 添加第一个节点；点击流程名称配置输入与触发方式。
                  </div>
                )}
                <WorkflowCanvas
                  tools={mobile ? undefined : tools}
                  graph={item.graph}
                  layout={item.editorLayout}
                  selected={selected}
                  focusRequest={focusRequest}
                  onSelect={(id) => select(id)}
                  onConfigure={(id) => select(id, true)}
                  onClear={clearSelection}
                  onAdd={requestAdd}
                  highlightedCondition={joinOpen ? joinId : undefined}
                  onLayout={(layout: Layout) => mutate({ ...item, editorLayout: layout })}
                  onConnect={connect}
                  readOnly={!canWrite || mobile}
                />
              </div>
            )}
          </Card>
          {!mobile && inspectorOpen && (
            <Card className="flex w-[min(360px,40%)] shrink-0 flex-col overflow-hidden">
              <div className="flex items-center justify-between border-b border-[var(--border)] px-3 py-2">
                <h2 className="truncate text-sm font-semibold">
                  {item.graph.steps.find((step) => step.id === selected)?.title ?? kindLabel[selected] ?? "节点配置"}
                </h2>
                <Button
                  variant="ghost"
                  aria-label="关闭节点配置"
                  onClick={() => {
                    setInspectorOpen(false);
                    inspectorTrigger.current?.focus();
                  }}
                >
                  ×
                </Button>
              </div>
              <div className="min-h-0 overflow-auto p-3">{inspector}</div>
            </Card>
          )}
        </div>
        {((message && !addContext) || diagnostics.length > 0) && (
          <div
            className="fixed bottom-4 right-4 z-40 max-h-[35vh] max-w-[min(440px,calc(100vw-32px))] overflow-auto rounded-lg border border-[var(--border)] bg-[var(--surface)] p-3 text-sm shadow-lg"
            role={message ? "alert" : "status"}
          >
            <Button
              variant="ghost"
              aria-label="关闭提示"
              className="float-right"
              onClick={() => {
                setMessage("");
                setDiagnostics([]);
              }}
            >
              ×
            </Button>
            {message}
            {diagnostics.map((diagnostic, index) => (
              <Button
                key={index}
                variant="ghost"
                onClick={() => {
                  select(diagnostic.stepId ?? (diagnostic.phase === "output" ? "@result" : "@trigger"), true, true);
                  setFocusedDiagnostic({ ...diagnostic, sequence: Date.now() });
                }}
              >
                {diagnostic.message ?? diagnostic.fieldPath ?? "定位错误"}
              </Button>
            ))}
          </div>
        )}
        <Modal
          onCloseAutoFocus={() => inspectorTrigger.current?.focus()}
          open={!!addContext && active}
          onOpenChange={() => setAddContext(null)}
          title={addContext?.target ? "插入节点" : "添加后续节点"}
          description="选择位置和节点类型；取消不会改变流程。"
          unsavedChanges={false}
        >
          {addContext && (
            <>
              {(!addContext.source || addContext.source === "@result") && (
                <label className="grid gap-2 text-sm">
                  {addContext.source === "@result" ? "在结束前接到哪个末端" : "添加位置"}
                  <select
                    aria-label="添加位置"
                    className={fieldClass}
                    value=""
                    onChange={(e) =>
                      setAddContext({
                        ...addContext,
                        source: e.target.value,
                        target: addContext.source === "@result" ? "@result" : undefined,
                      })
                    }
                  >
                    <option value="">请选择</option>
                    <option value="@trigger">开始后（新增根路径）</option>
                    {orderedSteps(item.graph)
                      .filter(
                        (step) =>
                          addContext.source !== "@result" ||
                          !item.graph.steps.some((other) => other.dependsOn?.includes(step.id)),
                      )
                      .map((step) => (
                        <option key={step.id} value={step.id}>
                          {step.title ?? step.id} · {step.id}
                        </option>
                      ))}
                  </select>
                </label>
              )}
              <p className="text-sm text-[var(--muted-text)]">
                {kindLabel[addContext.source] ??
                  item.graph.steps.find((step) => step.id === addContext.source)?.title ??
                  "请选择位置"}
                {addContext.target
                  ? ` → 新节点 → ${kindLabel[addContext.target] ?? item.graph.steps.find((step) => step.id === addContext.target)?.title}`
                  : " → 新节点"}
              </p>
              {item.graph.steps.find((step) => step.id === addContext.source)?.type === "condition" && (
                <label className="grid gap-2 text-sm">
                  条件出口
                  <select
                    aria-label="添加到条件出口"
                    className={fieldClass}
                    value={addContext.branch ?? ""}
                    onChange={(e) => setAddContext({ ...addContext, branch: e.target.value })}
                  >
                    <option value="">请选择出口</option>
                    {item.graph.steps
                      .find((step) => step.id === addContext.source)
                      ?.branches?.map((branch) => (
                        <option key={branch.id} value={branch.id}>
                          {branch.title ?? branch.id}
                        </option>
                      ))}
                  </select>
                </label>
              )}
              {addKind === "condition" && addContext.target && addContext.target !== "@result" && (
                <label className="grid gap-2 text-sm">
                  插入条件时，原后续节点接入
                  <select
                    aria-label="插入条件的后续出口"
                    className={fieldClass}
                    value={insertExit}
                    onChange={(e) => setInsertExit(e.target.value)}
                  >
                    <option value="if1">IF</option>
                    <option value="otherwise">ELSE</option>
                  </select>
                </label>
              )}
              <div className="grid grid-cols-2 gap-2">
                {(["api", "transform", "condition", "code"] as NodeKind[]).map((kind) => {
                  const reason =
                    kind !== "api" && !cap?.v2Enabled
                      ? "工作流 v2 开关关闭"
                      : kind === "code" && !cap?.code.enabled
                        ? "代码节点开关关闭"
                        : "";
                  return (
                    <Button
                      key={kind}
                      write
                      variant="secondary"
                      disabled={
                        !!reason ||
                        !addContext.source ||
                        addContext.source === "@result" ||
                        (item.graph.steps.find((step) => step.id === addContext.source)?.type === "condition" &&
                          !addContext.branch)
                      }
                      onClick={() => {
                        if (kind === "condition" && addContext.target && addContext.target !== "@result")
                          setAddKind(kind);
                        else add(kind);
                      }}
                      className="h-auto min-h-16 flex-col"
                    >
                      <span>{kindLabel[kind]}</span>
                      {reason && <span className="text-xs font-normal">{reason}</span>}
                    </Button>
                  );
                })}
              </div>
              {addKind === "condition" && (
                <Button write onClick={() => add("condition")}>
                  确认插入条件
                </Button>
              )}
              {message && (
                <p role="alert" className="text-sm text-red-600">
                  {message}
                </p>
              )}
              <p className="text-xs text-[var(--muted-text)]">
                {item.graph.steps.length}/20 个执行节点。
                {!cap?.v2Enabled ? "开关关闭时仍可添加 API；其他类型保留展示。" : ""}
              </p>
            </>
          )}
        </Modal>
        <Modal
          onCloseAutoFocus={() => inspectorTrigger.current?.focus()}
          open={joinOpen && active}
          onOpenChange={() => setJoinOpen(false)}
          title="选择要汇合的条件"
          description="汇合等待有效分支，并保留其他路径。"
          unsavedChanges={false}
        >
          <select
            aria-label="汇合条件"
            className={fieldClass}
            value={joinId}
            onChange={(e) => {
              setJoinId(e.target.value);
              setFocusRequest({ id: e.target.value, sequence: Date.now() });
            }}
          >
            <option value="">请选择条件</option>
            {item.graph.steps
              .filter((step) => step.type === "condition")
              .map((step) => (
                <option key={step.id} value={step.id}>
                  {step.title ?? step.id} · {step.id}
                </option>
              ))}
          </select>
          <p className="text-sm">
            等待分支：
            {item.graph.steps
              .find((step) => step.id === joinId)
              ?.branches?.map((branch) => branch.title ?? branch.id)
              .join("、") || "无可用条件"}
          </p>
          <Button
            write
            disabled={!joinId}
            onClick={() => {
              join(joinId);
              setJoinOpen(false);
            }}
          >
            检查并应用汇合
          </Button>
        </Modal>
        {mobile && (
          <Modal
            open={inspectorOpen && active}
            onOpenChange={() => setInspectorOpen(false)}
            title={`配置 ${selected}`}
            onCloseAutoFocus={() => inspectorTrigger.current?.focus()}
            description="配置当前工作流节点"
            unsavedChanges={false}
          >
            {inspector}
          </Modal>
        )}
        <Modal
          open={graphOpen && active}
          onOpenChange={() => {
            if (!invalidBuffer || window.confirm("丢弃未应用的 JSON 缓冲？")) {
              setGraphOpen(false);
              setInvalid({});
            }
          }}
          title="工作流定义 JSON"
          onCloseAutoFocus={() => graphTrigger.current?.focus()}
          description="配置当前工作流节点"
          unsavedChanges={false}
        >
          <JsonField
            label="完整定义（直接应用当前编辑会话）"
            value={item.graph}
            validate={assertDefinitionShape}
            onChange={(graph) => updateBusiness({ ...item, graph })}
            onInvalid={(invalid) => setInvalid((current) => ({ ...current, graph: invalid }))}
          />
        </Modal>
        <Modal
          open={runOpen && active}
          onOpenChange={() => setRunOpen(false)}
          title="运行当前已部署版本"
          onCloseAutoFocus={() => runTrigger.current?.focus()}
          description="输入整体替换默认值"
        >
          <p className="text-sm">本次 input 整体替换默认输入。不会自动重发运行请求。</p>
          <ObjectInput schema={item.graph.inputSchema} value={runInput} onChange={setRunInput} label="本次输入" />
          <JsonField label="本次输入 JSON" value={runInput} onChange={setRunInput} onInvalid={setRunInvalid} />
          <Button
            write
            disabled={pending || runInvalid}
            onClick={async () => {
              setPending(true);
              try {
                const operation = await api.runWorkflow(item.id, runInput, item.version);
                setRunOpen(false);
                navigate(`/workflows/${item.id}/runs/${operation.id}`);
              } catch (error) {
                setMessage(`请求结果未确认，请查看运行历史。${String(error)}`);
              } finally {
                setPending(false);
              }
            }}
          >
            运行版本 {item.version}
          </Button>
        </Modal>
      </div>
    </JsonBufferContext.Provider>
  );
}
