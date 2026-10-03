import { memo, useEffect, useMemo, useState, useRef, type ReactNode } from "react";
import { ZoomIn, ZoomOut, Scan, Map, LocateFixed, MousePointer2 } from "lucide-react";
import {
  Background,
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
  type EdgeProps,
  Handle,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  applyNodeChanges,
  useReactFlow,
  useNodesInitialized,
  useUpdateNodeInternals,
  type Node,
  type Edge,
  type NodeProps,
  type Connection,
} from "@xyflow/react";
import dagre from "@dagrejs/dagre";
import "@xyflow/react/dist/style.css";
import "./workflow-canvas.css";
import { dependencies, kindLabel, type Definition, type Layout, type Step } from "./workflow-model";
import { Button } from "../ui";
type FlowNode = Node<{
  step?: Step;
  label: string;
  status?: string;
  related?: boolean;
  onAdd?: (source: string, branch?: string, target?: string) => void;
  onConfigure?: (id: string) => void;
}>;
type FlowEdge = Edge<{ onAdd?: (source: string, branch?: string, target?: string) => void }>;
function InsertEdge(props: EdgeProps<FlowEdge>) {
  const [path, x, y] = getSmoothStepPath(props);
  return (
    <>
      <BaseEdge id={props.id} path={path} style={props.style} markerEnd={props.markerEnd} />
      <EdgeLabelRenderer>
        <div
          className="nodrag nopan absolute flex items-center gap-1 text-xs"
          style={{ transform: `translate(-50%, -50%) translate(${x}px,${y}px)`, pointerEvents: "all" }}
        >
          {props.label && <span className="rounded bg-[var(--surface)] px-1">{props.label}</span>}
          {props.data?.onAdd && (
            <button
              type="button"
              className="size-6 rounded-full border border-[var(--border)] bg-[var(--surface)] hover:border-blue-500"
              aria-label={`在 ${props.source} 到 ${props.target} 之间插入节点`}
              onClick={() => props.data?.onAdd?.(props.source, props.sourceHandleId ?? undefined, props.target)}
            >
              ＋
            </button>
          )}
        </div>
      </EdgeLabelRenderer>
    </>
  );
}
const edgeTypes = { insert: InsertEdge };
const WorkflowNode = memo(function WorkflowNode({ id, data, selected }: NodeProps<FlowNode>) {
  const branches = data.step?.branches ?? [];
  const updateInternals = useUpdateNodeInternals();
  useEffect(() => updateInternals(id), [id, data.step?.branches, updateInternals]);
  return (
    <div
      className={`w-[240px] rounded-xl border-2 bg-[var(--surface)] p-3 shadow-sm ${selected ? "border-blue-500" : data.related ? "border-blue-300" : "border-[var(--border)]"}`}
    >
      {id !== "@trigger" && <Handle type="target" position={Position.Left} />}
      <div className="workflow-drag-handle">
        <p className="text-xs text-[var(--muted-text)]">
          {kindLabel[data.step?.type ?? (data.step ? "api" : id)]}{" "}
          {data.status && <span role="status"> · {data.status}</span>}
        </p>
        <strong className="block break-words">{data.label}</strong>
      </div>
      {selected && data.onConfigure && (
        <div className="nodrag nopan mt-2 flex gap-2">
          <button
            type="button"
            className="text-xs text-blue-600"
            onClick={(e) => {
              e.stopPropagation();
              data.onConfigure?.(id);
            }}
          >
            配置
          </button>
          {id === "@result" && data.onAdd && (
            <button
              type="button"
              className="text-xs text-blue-600"
              onClick={(e) => {
                e.stopPropagation();
                data.onAdd?.(id);
              }}
            >
              在结束前添加
            </button>
          )}
        </div>
      )}
      {data.step?.type === "code" && (
        <p className="text-xs text-[var(--muted-text)]">
          JavaScript · {Object.keys(data.step.inputSchema?.properties ?? {}).length} 个输入 ·{" "}
          {Object.keys(data.step.outputSchema?.properties ?? {}).length} 个输出 · 更新 {data.step.assign?.length ?? 0}{" "}
          个变量
        </p>
      )}
      {data.step?.type === "transform" && (
        <p className="text-xs text-[var(--muted-text)]">
          {data.step.operations
            ?.map(
              (operation) =>
                ({ select: "投影", filter: "筛选", sort: "排序", slice: "截取", count: "计数" })[
                  String(operation.op) as "select"
                ],
            )
            .join(" → ")}
        </p>
      )}
      {!data.status &&
        data.step &&
        (data.step.type === undefined || data.step.type === "api") &&
        (!data.step.action || !data.step.integrationId) && <p className="text-xs text-amber-600">待配置执行目标</p>}
      {branches.map((branch, index) => (
        <div key={branch.id} className="relative mt-2 text-xs">
          {branch.title ?? branch.id}
          {data.onAdd && (
            <button
              type="button"
              className="nodrag nopan ml-2 rounded border border-[var(--border)] px-2 py-1 text-blue-600"
              aria-label={`在 ${data.label} 的 ${branch.title ?? branch.id} 出口添加节点`}
              onClick={(e) => {
                e.stopPropagation();
                data.onAdd?.(id, branch.id);
              }}
            >
              ＋
            </button>
          )}
          <Handle
            id={branch.id}
            type="source"
            position={Position.Right}
            style={{ top: "50%", right: -16 }}
            aria-label={`出口 ${index + 1}`}
          />
        </div>
      ))}
      {id !== "@result" && !branches.length && data.onAdd && (
        <button
          type="button"
          className="nodrag nopan absolute -right-8 top-1/2 size-7 -translate-y-1/2 rounded-full border border-[var(--border)] bg-[var(--surface)] text-blue-600"
          aria-label={`在 ${data.label} 后添加节点`}
          onClick={(e) => {
            e.stopPropagation();
            data.onAdd?.(id);
          }}
        >
          ＋
        </button>
      )}
      {id !== "@result" && (
        <Handle type="source" position={Position.Right} style={branches.length ? { top: 24 } : undefined} />
      )}
    </div>
  );
});
const nodeTypes = { workflow: WorkflowNode };
const emptyStatuses: Record<string, string> = {};
export function autoLayout(graph: Definition): Layout {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: "LR", ranksep: 90, nodesep: 45 });
  g.setDefaultEdgeLabel(() => ({}));
  for (const id of ["@trigger", ...graph.steps.map((step) => step.id), "@result"]) {
    const step = graph.steps.find((step) => step.id === id);
    g.setNode(id, { width: 240, height: 100 + (step?.branches?.length ?? 0) * 30 });
  }
  for (const step of graph.steps) {
    for (const dep of step.dependsOn?.length ? step.dependsOn : ["@trigger"]) g.setEdge(dep, step.id);
  }
  const ends = graph.steps.filter((step) => !graph.steps.some((other) => other.dependsOn?.includes(step.id)));
  for (const step of ends) g.setEdge(step.id, "@result");
  if (!graph.steps.length) g.setEdge("@trigger", "@result");
  dagre.layout(g);
  return {
    schemaVersion: 1,
    direction: "LR",
    positions: Object.fromEntries(
      g.nodes().map((id) => {
        const n = g.node(id);
        return [id, { x: n.x - 120, y: n.y - n.height / 2 }];
      }),
    ),
  };
}
function Canvas({
  graph,
  layout,
  selected,
  onSelect,
  onLayout,
  onConnect,
  statuses = emptyStatuses,
  readOnly = false,
  focusRequest,
  onAdd,
  onConfigure,
  onClear,
  highlightedCondition,
  tools,
}: {
  graph: Definition;
  layout?: Layout;
  selected: string;
  onSelect: (id: string) => void;
  onAdd?: (source: string, branch?: string, target?: string) => void;
  onConfigure?: (id: string) => void;
  onClear?: () => void;
  highlightedCondition?: string;
  tools?: ReactNode;
  onLayout?: (layout: Layout) => void;
  onConnect?: (connection: Connection) => void;
  statuses?: Record<string, string>;
  readOnly?: boolean;
  focusRequest?: { id: string; sequence: number };
}) {
  const flow = useReactFlow();
  const [mapOpen, setMapOpen] = useState(false);
  const [pointerVisible, setPointerVisible] = useState(true);
  const pointer = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const hide = () => {
      if (pointer.current) pointer.current.hidden = true;
    };
    window.addEventListener("blur", hide);
    return () => window.removeEventListener("blur", hide);
  }, []);
  const initialized = useNodesInitialized();
  const initialView = useRef(false);
  const appliedFocus = useRef(0);
  useEffect(() => {
    if (initialized && !initialView.current) {
      initialView.current = true;
      void flow.fitView(
        readOnly
          ? { padding: 0.15, minZoom: 0.02 }
          : {
              nodes: graph.steps.length > 3 ? [{ id: selected || "@trigger" }] : undefined,
              minZoom: 0.1,
              maxZoom: 0.85,
              padding: 0.25,
            },
      );
    }
  }, [initialized, flow, selected]);
  useEffect(() => {
    if (initialized && focusRequest && appliedFocus.current !== focusRequest.sequence) {
      // The inspector changes the canvas width; let ResizeObserver settle before positioning a new node.
      const timer = window.setTimeout(() => {
        appliedFocus.current = focusRequest.sequence;
        void flow.fitView({ nodes: [{ id: focusRequest.id }], minZoom: 0.1, maxZoom: 0.85, padding: 0.4 });
      }, 60);
      return () => window.clearTimeout(timer);
    }
  }, [focusRequest, initialized, flow]);
  const positions = useMemo(() => {
    const fallback = autoLayout(graph);
    return layout?.positions ? { ...layout, positions: { ...fallback.positions, ...layout.positions } } : fallback;
  }, [graph, layout]);
  const related = useMemo(() => {
    const condition =
      highlightedCondition || (graph.steps.find((step) => step.id === selected)?.type === "condition" ? selected : "");
    return new Set(
      condition
        ? [
            condition,
            ...graph.steps
              .filter((step) => step.scope?.some((scope) => scope.conditionId === condition))
              .map((step) => step.id),
          ]
        : dependencies(graph, selected),
    );
  }, [graph, selected, highlightedCondition]);
  const derived = useMemo(
    () =>
      [
        {
          id: "@trigger",
          type: "workflow",
          position: positions.positions["@trigger"] ?? { x: 0, y: 0 },
          dragHandle: ".workflow-drag-handle",
          data: { label: "开始", status: statuses["@trigger"], onAdd: readOnly ? undefined : onAdd, onConfigure },
          selected: selected === "@trigger",
        },
        ...graph.steps.map((step) => ({
          id: step.id,
          type: "workflow",
          position: positions.positions[step.id] ?? { x: 300, y: 0 },
          dragHandle: ".workflow-drag-handle",
          data: {
            step,
            label: step.title || step.id,
            status: statuses[step.id],
            related: related.has(step.id),
            onAdd: readOnly ? undefined : onAdd,
            onConfigure,
          },
          selected: step.id === selected,
        })),
        {
          id: "@result",
          type: "workflow",
          position: positions.positions["@result"] ?? { x: 600, y: 0 },
          dragHandle: ".workflow-drag-handle",
          data: { label: "结束", status: statuses["@result"], onAdd: readOnly ? undefined : onAdd, onConfigure },
          selected: selected === "@result",
        },
      ] as FlowNode[],
    [graph, positions, selected, statuses, onAdd, onConfigure, readOnly, related],
  );
  const [nodes, setNodes] = useState(derived);
  useEffect(() => setNodes(derived), [derived]);
  const edges = useMemo(() => {
    const result: Edge[] = [];
    for (const step of graph.steps)
      for (const source of step.dependsOn?.length ? step.dependsOn : ["@trigger"]) {
        const branch = step.scope?.find((scope) => scope.conditionId === source);
        const condition = graph.steps.find((node) => node.id === source && node.type === "condition");
        if (condition && !branch) {
          const empty = condition.branches?.filter(
            (branch) =>
              !graph.steps.some(
                (node) =>
                  node.dependsOn?.includes(source) &&
                  node.scope?.some((scope) => scope.conditionId === source && scope.branchId === branch.id),
              ),
          );
          for (const branch of empty ?? [])
            result.push({
              id: `${source}/${branch.id}/${step.id}`,
              source,
              target: step.id,
              sourceHandle: branch.id,
              label: branch.title ?? branch.id,
              type: "smoothstep",
            });
        }
        result.push({
          id: `${source}/${step.id}`,
          source,
          target: step.id,
          sourceHandle: branch?.branchId,
          label: condition?.branches?.find((exit) => exit.id === branch?.branchId)?.title ?? branch?.branchId,
          type: "smoothstep",
        });
      }
    for (const step of graph.steps.filter((step) => !graph.steps.some((other) => other.dependsOn?.includes(step.id)))) {
      if (step.type === "condition") {
        for (const branch of step.branches ?? [])
          result.push({
            id: `${step.id}/${branch.id}/@result`,
            source: step.id,
            target: "@result",
            sourceHandle: branch.id,
            label: branch.title ?? branch.id,
            type: "smoothstep",
          });
      } else result.push({ id: `${step.id}/@result`, source: step.id, target: "@result", type: "smoothstep" });
    }
    for (const condition of graph.steps.filter(
      (step) => step.type === "condition" && graph.steps.some((other) => other.dependsOn?.includes(step.id)),
    )) {
      for (const branch of condition.branches ?? []) {
        if (
          graph.steps.some((node) =>
            node.scope?.some((scope) => scope.conditionId === condition.id && scope.branchId === branch.id),
          )
        )
          continue;
        if (result.some((edge) => edge.source === condition.id && edge.sourceHandle === branch.id)) continue;
        result.push({
          id: `${condition.id}/${branch.id}/@result`,
          source: condition.id,
          target: "@result",
          sourceHandle: branch.id,
          label: branch.title ?? branch.id,
          type: "smoothstep",
        });
      }
    }
    if (!graph.steps.length) result.push({ id: "empty", source: "@trigger", target: "@result" });
    return result.map((edge) => ({ ...edge, type: "insert", data: { onAdd: readOnly ? undefined : onAdd } }));
  }, [graph, readOnly, onAdd, related, selected]);
  return (
    <div
      className="workflow-canvas relative h-full min-h-[320px] min-w-0"
      aria-label="工作流画布"
      onPointerMove={(event) => {
        if (!pointer.current) return;
        const show =
          pointerVisible &&
          event.pointerType === "mouse" &&
          !(event.target as Element).closest(".workflow-canvas-tools, .react-flow__minimap");
        pointer.current.hidden = !show;
        if (show) {
          const rect = event.currentTarget.getBoundingClientRect();
          pointer.current.style.transform = `translate(${event.clientX - rect.left}px, ${event.clientY - rect.top}px)`;
        }
      }}
      onPointerLeave={() => {
        if (pointer.current) pointer.current.hidden = true;
      }}
      onPointerCancel={() => {
        if (pointer.current) pointer.current.hidden = true;
      }}
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        nodesDraggable={!readOnly}
        nodesConnectable={!readOnly}
        edgesReconnectable={false}
        deleteKeyCode={null}
        minZoom={0.02}
        maxZoom={1.5}
        onNodeClick={(_, node) => onSelect(node.id)}
        onNodeDoubleClick={(_, node) => onConfigure?.(node.id)}
        onPaneClick={() => onClear?.()}
        onNodesChange={(changes) => setNodes((current) => applyNodeChanges(changes, current))}
        onNodeDragStop={(_, node) =>
          onLayout?.({ ...positions, positions: { ...positions.positions, [node.id]: node.position } })
        }
        onConnect={onConnect}
        proOptions={{ hideAttribution: false }}
      >
        <Background />
        {mapOpen && <MiniMap pannable zoomable />}
      </ReactFlow>
      {tools && (
        <div className="workflow-canvas-tools absolute left-3 top-3 z-10 rounded-xl border border-[var(--border)] bg-[var(--surface)] p-1 shadow-sm">
          {tools}
        </div>
      )}
      <div className="workflow-canvas-tools workflow-view-tools absolute bottom-8 left-3 z-10 flex gap-1 rounded-xl border border-[var(--border)] bg-[var(--surface)] p-1 shadow-sm">
        <Button variant="ghost" aria-label="放大" title="放大" onClick={() => void flow.zoomIn()}>
          <ZoomIn size={18} />
        </Button>
        <Button variant="ghost" aria-label="缩小" title="缩小" onClick={() => void flow.zoomOut()}>
          <ZoomOut size={18} />
        </Button>
        <Button
          variant="ghost"
          aria-label="适应画布"
          title="适应画布"
          onClick={() => void flow.fitView({ minZoom: 0.02, padding: 0.15 })}
        >
          <Scan size={18} />
        </Button>
        <Button
          variant="ghost"
          aria-label="小地图"
          title="小地图"
          aria-pressed={mapOpen}
          onClick={() => setMapOpen(!mapOpen)}
        >
          <Map size={18} />
        </Button>
        {selected && (
          <Button
            variant="ghost"
            aria-label="定位所选节点"
            title="定位所选节点"
            onClick={() => void flow.fitView({ nodes: [{ id: selected }], maxZoom: 1, padding: 0.4 })}
          >
            <LocateFixed size={18} />
          </Button>
        )}
        <Button
          variant="ghost"
          aria-label="光标指示"
          title="光标指示"
          aria-pressed={pointerVisible}
          onClick={() => {
            setPointerVisible(!pointerVisible);
            if (pointer.current) pointer.current.hidden = true;
          }}
        >
          <MousePointer2 size={18} />
        </Button>
      </div>
      <div ref={pointer} hidden aria-hidden="true" className="workflow-pointer" />
    </div>
  );
}
export function WorkflowCanvas(props: Parameters<typeof Canvas>[0]) {
  return (
    <ReactFlowProvider>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}
