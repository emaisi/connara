import { act, cleanup, createEvent, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WorkflowCanvas } from "./workflow-canvas";

const state = vi.hoisted(() => ({ initialized: true, flow: { fitView: vi.fn() } }));
vi.mock("@xyflow/react", () => ({
  ReactFlowProvider: ({ children }: any) => children,
  ReactFlow: () => <div>画布</div>,
  Background: () => null,
  Controls: () => null,
  MiniMap: () => null,
  useReactFlow: () => state.flow,
  useNodesInitialized: () => state.initialized,
  useUpdateNodeInternals: () => vi.fn(),
  applyNodeChanges: (_changes: any, nodes: any) => nodes,
  Position: { Left: "left", Right: "right" },
}));
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
  state.initialized = true;
});
describe("workflow viewport", () => {
  it("tracks a mouse without blocking the canvas, hides on exit and touch, and can disable the indicator", () => {
    const { container } = render(
      <WorkflowCanvas graph={{ steps: [], output: {} }} selected="" onSelect={vi.fn()} tools={<button>工具</button>} />,
    );
    const canvas = screen.getByLabelText("工作流画布");
    const pointer = container.querySelector<HTMLElement>(".workflow-pointer")!;
    const move = (target: Element, pointerType = "mouse") => {
      const event = createEvent.pointerMove(target);
      Object.defineProperties(event, {
        pointerType: { value: pointerType },
        clientX: { value: 200 },
        clientY: { value: 150 },
      });
      fireEvent(target, event);
    };
    expect(pointer.hidden).toBe(true);
    move(canvas);
    expect(pointer.hidden).toBe(false);
    expect(pointer.style.transform).toBe("translate(200px, 150px)");
    move(screen.getByRole("button", { name: "工具" }));
    expect(pointer.hidden).toBe(true);
    move(canvas, "touch");
    expect(pointer.hidden).toBe(true);
    move(canvas);
    fireEvent.pointerLeave(canvas);
    expect(pointer.hidden).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "光标指示" }));
    move(canvas);
    expect(pointer.hidden).toBe(true);
    expect(screen.getByRole("button", { name: "光标指示" })).toHaveAttribute("aria-pressed", "false");
  });

  it("positions an explicit focus once, after the panel resizes, without stealing a later manual viewport", () => {
    vi.useFakeTimers();
    const props = {
      graph: { steps: [{ id: "a" }], output: {} },
      selected: "a",
      onSelect: vi.fn(),
      focusRequest: { id: "a", sequence: 1 },
    };
    const view = render(<WorkflowCanvas {...props} />);
    state.flow.fitView.mockClear();
    act(() => {
      vi.advanceTimersByTime(60);
    });
    expect(state.flow.fitView).toHaveBeenCalledOnce();
    expect(state.flow.fitView).toHaveBeenCalledWith(expect.objectContaining({ nodes: [{ id: "a" }] }));
    state.flow.fitView.mockClear();
    // Node size initialization can repeat when selection or panel dimensions change.
    state.initialized = false;
    view.rerender(<WorkflowCanvas {...props} />);
    state.initialized = true;
    view.rerender(<WorkflowCanvas {...props} />);
    act(() => {
      vi.advanceTimersByTime(100);
    });
    expect(state.flow.fitView).not.toHaveBeenCalled();
    view.rerender(<WorkflowCanvas {...props} focusRequest={{ id: "a", sequence: 2 }} />);
    act(() => {
      vi.advanceTimersByTime(60);
    });
    expect(state.flow.fitView).toHaveBeenCalledOnce();
  });
});
