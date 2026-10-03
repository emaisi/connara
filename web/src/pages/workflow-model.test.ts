import { describe, expect, it } from "vitest";
import {
  blankWorkflow,
  canDepend,
  joinCondition,
  renameStep,
  assertDefinitionShape,
  definitionFingerprint,
  renameVariable,
  rewriteSelfAssignments,
  upgradeGraph,
  insertStep,
  newStep,
  placeStep,
  stepReferences,
  orderedSteps,
  type Definition,
} from "./workflow-model";
describe("workflow editing invariants", () => {
  it("compares definitions independent of JSON object key order", () => {
    const first = blankWorkflow();
    const second = blankWorkflow();
    first.graph.output = { a: 1, b: 2 };
    second.graph.output = { b: 2, a: 1 };
    expect(definitionFingerprint(first)).toBe(definitionFingerprint(second));
  });
  it("rejects cycles and reserved v1 alias upgrades", () => {
    expect(canDepend({ steps: [{ id: "a" }, { id: "b", dependsOn: ["a"] }], output: {} }, "a", "b")).toBe(false);
    expect(() => upgradeGraph({ steps: [{ id: "vars" }], output: {} })).toThrow(/重名/);
  });
  it("renames variables atomically and preserves JavaScript and literal business data", () => {
    const graph: any = {
      schemaVersion: 2,
      variables: [{ name: "total", type: "integer", initial: 0 }],
      steps: [
        {
          id: "code1",
          type: "code",
          code: 'function main(){return {text:"{{vars.total}}"}}',
          input: { x: "{{vars.total}}", bracket: '{{vars["total"]}}' },
          assign: [{ variable: "total", value: "{{code1.total}}" }],
        },
      ],
      output: { x: "{{vars.total}}", literal: { $value: { kind: "literal", value: "{{vars.total}}" } } },
    };
    const next = renameVariable(graph, "total", "amount");
    expect(next.variables?.[0].name).toBe("amount");
    expect(next.steps[0].assign?.[0].variable).toBe("amount");
    expect(next.steps[0].input?.x).toBe("{{vars.amount}}");
    expect(next.steps[0].input?.bracket).toBe("{{vars.amount}}");
    expect(next.steps[0].code).toBe(graph.steps[0].code);
    expect(next.output.literal).toEqual(graph.output.literal);
  });
  it("previews an atomic reserved step rename without changing literals or source", () => {
    const next = renameStep(
      {
        steps: [
          { id: "vars" },
          { id: "next", dependsOn: ["vars"], input: { x: "{{vars.total}}", status: "{{status.vars}}" }, code: "vars" },
        ],
        output: { x: "{{vars.total}}" },
      },
      "vars",
      "legacy_vars",
    );
    expect(next.steps[1]).toMatchObject({
      dependsOn: ["legacy_vars"],
      input: { x: "{{legacy_vars.total}}", status: "{{status.legacy_vars}}" },
      code: "vars",
    });
    expect(upgradeGraph(next).schemaVersion).toBe(2);
  });
  it("keeps malformed advanced definitions out of the rendering model", () => {
    expect(() => assertDefinitionShape({})).toThrow(/steps/);
    expect(() => assertDefinitionShape({ steps: [{ id: "a" }, { id: "a" }], output: {} })).toThrow(/id/);
  });
  it("rewrites a copied node's own assignment references", () => {
    expect(
      rewriteSelfAssignments(
        { id: "copy", assign: [{ variable: "x", value: "{{original.result}}" }], code: "original" },
        "original",
      ),
    ).toMatchObject({ assign: [{ variable: "x", value: "{{copy.result}}" }], code: "original" });
  });
  it("joins an existing condition and moves its descendants out of the old branch together", () => {
    const a = { conditionId: "a", branchId: "yes" };
    const b = { conditionId: "b", branchId: "yes" };
    const graph: any = {
      steps: [
        { id: "a", type: "condition" },
        { id: "yes", dependsOn: ["a"], scope: [a] },
        { id: "no", dependsOn: ["a"], scope: [{ ...a, branchId: "no" }] },
        { id: "b", type: "condition", dependsOn: ["yes"], scope: [a] },
        { id: "child", dependsOn: ["b"], scope: [a, b] },
        { id: "tail", dependsOn: ["child"], scope: [a, b] },
        { id: "b_join", dependsOn: ["b", "tail"], scope: [a] },
        { id: "other_root" },
      ],
      output: { status: "{{status.b}}" },
    };
    const next = joinCondition(graph, "b", "a");
    expect(next.steps[3]).toMatchObject({ scope: [], dependsOn: ["yes", "a", "no"] });
    expect(next.steps[4].scope).toEqual([b]);
    expect(next.steps[5].scope).toEqual([b]);
    expect(next.steps[6].scope).toEqual([]);
    expect(next.steps[2]).toBe(graph.steps[2]);
    expect(next.steps[7]).toBe(graph.steps[7]);
    expect(graph.steps[4].scope).toEqual([a, b]);
    expect(next.output).toBe(graph.output);
    expect(() => joinCondition(graph, "tail", "a")).toThrow(/内部汇合/);
    expect(() => joinCondition(graph, "a", "b")).toThrow(/祖先/);
  });
  it("inserts one edge atomically without losing other dependencies or ancestor mappings", () => {
    const graph: Definition = {
      steps: [{ id: "a" }, { id: "other" }, { id: "b", dependsOn: ["a", "other"], input: { x: "{{a.value}}" } }],
      output: { x: "{{b}}" },
    };
    const next = insertStep(graph, newStep(graph, "api", graph.steps[0]), "a", "b");
    expect(next.steps[2].dependsOn).toEqual(["other", "api1"]);
    expect(next.steps[2].input).toEqual(graph.steps[2].input);
    expect(next.steps[3].dependsOn).toEqual(["a"]);
    expect(graph.steps[2].dependsOn).toEqual(["a", "other"]);
    expect(() => insertStep(graph, newStep(graph, "api"), "@trigger", "b")).toThrow(/连线/);
  });
  it("inserts inside a named branch and retains a public join without implicit scope movement", () => {
    const scope = [{ conditionId: "c", branchId: "yes" }];
    const graph: Definition = {
      schemaVersion: 2,
      steps: [
        { id: "c", type: "condition", branches: [{ id: "yes" }, { id: "no" }] },
        { id: "a", dependsOn: ["c"], scope },
        { id: "join", dependsOn: ["a"], scope: [] },
      ],
      output: {},
    };
    const step = newStep(graph, "transform", graph.steps[0], "yes");
    expect(insertStep(graph, step, "c", "a").steps[1]).toMatchObject({ dependsOn: [step.id], scope });
    const joined = insertStep(graph, newStep(graph, "api", graph.steps[1]), "a", "join");
    expect(joined.steps[2].scope).toEqual([]);
    expect(joined.steps[3].scope).toEqual(scope);
    expect(() => insertStep(graph, newStep(graph, "condition", graph.steps[1]), "a", "join", "if1")).toThrow(
      /其他分支/,
    );
  });
  it("requires an explicit exit for inserted conditions and carries descendant scope together", () => {
    const graph: Definition = {
      schemaVersion: 2,
      steps: [{ id: "a" }, { id: "b", dependsOn: ["a"] }, { id: "tail", dependsOn: ["b"] }],
      output: {},
    };
    const condition = newStep(graph, "condition", graph.steps[0]);
    expect(() => insertStep(graph, condition, "a", "b")).toThrow(/哪个出口/);
    const next = insertStep(graph, condition, "a", "b", "otherwise");
    expect(next.steps[1].scope).toEqual([{ conditionId: condition.id, branchId: "otherwise" }]);
    expect(next.steps[2].scope).toEqual(next.steps[1].scope);
  });
  it("keeps additions and copies clear of nodes and moves the ending forward", () => {
    const layout = {
      schemaVersion: 1 as const,
      direction: "LR" as const,
      positions: { "@trigger": { x: 0, y: 0 }, existing: { x: 330, y: 0 }, "@result": { x: 330, y: 0 } },
    };
    const placed = placeStep(layout, { id: "copy" }, "@trigger");
    expect(placed.positions.copy).toEqual({ x: 330, y: 210 });
    expect(placed.positions["@result"].x).toBe(660);
    expect(layout.positions["@result"].x).toBe(330);
    const reused = placeStep(
      { ...layout, positions: { "@trigger": { x: 0, y: 0 }, copy: { x: 330, y: 0 }, "@result": { x: 660, y: 0 } } },
      { id: "copy" },
      "@trigger",
    );
    expect(reused.positions.copy).toEqual({ x: 330, y: 0 });
  });
  it("protects structured and status references on deletion while ignoring literal business text", () => {
    const graph: Definition = {
      steps: [{ id: "a" }, { id: "b", source: { $value: { kind: "path", path: "a.items" } }, code: "{{a.value}}" }],
      output: { status: "{{status.a}}", literal: { $value: { kind: "literal", value: "{{a.x}}" } } },
    };
    expect(stepReferences(graph, "a")).toEqual(["结束返回映射", "b（参数或赋值）"]);
    expect(
      stepReferences(
        { ...graph, steps: [{ id: "a" }, { id: "b", code: "{{a}}" }], output: { literal: graph.output.literal } },
        "a",
      ),
    ).toEqual([]);
    expect(
      orderedSteps({ steps: [{ id: "b", dependsOn: ["a"] }, { id: "a" }], output: {} }).map((step) => step.id),
    ).toEqual(["a", "b"]);
  });
  it("makes horizontal room only on the inserted path and keeps the ending beyond its tail", () => {
    const graph: Definition = {
      steps: [{ id: "a" }, { id: "b", dependsOn: ["a"] }, { id: "tail", dependsOn: ["b"] }, { id: "other" }],
      output: {},
    };
    const layout = {
      schemaVersion: 1 as const,
      direction: "LR" as const,
      positions: {
        a: { x: 330, y: 0 },
        b: { x: 660, y: 0 },
        tail: { x: 990, y: 0 },
        other: { x: 660, y: 300 },
        "@result": { x: 1320, y: 0 },
      },
    };
    const next = placeStep(layout, { id: "new" }, "a", "b", graph);
    expect(next.positions.new).toEqual({ x: 660, y: 0 });
    expect(next.positions.b.x).toBe(990);
    expect(next.positions.tail.x).toBe(1320);
    expect(next.positions["@result"].x).toBe(1650);
    expect(next.positions.other).toEqual(layout.positions.other);
  });
  it("protects branch discriminator and bracketed status references on deletion", () => {
    expect(
      stepReferences(
        {
          steps: [{ id: "a", type: "condition" }],
          output: { branch: { $value: { kind: "branch", conditionId: "a", cases: {} } } },
        },
        "a",
      ),
    ).toEqual(["结束返回映射"]);
    expect(stepReferences({ steps: [{ id: "a" }], output: { status: '{{status["a"]}}' } }, "a")).toEqual([
      "结束返回映射",
    ]);
  });
});
