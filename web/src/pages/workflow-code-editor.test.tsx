import { act, cleanup, render } from "@testing-library/react";
import { EditorView } from "@codemirror/view";
import { afterEach, expect, it, vi } from "vitest";
import CodeEditor from "./workflow-code-editor";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

it("allows deleting a highlighted error line and synchronizes undo without emitting another user edit", () => {
  // Layout is checked in Chromium; this regression exercises real CM transactions.
  vi.spyOn(window, "requestAnimationFrame").mockImplementation(() => 0);
  vi.spyOn(window, "cancelAnimationFrame").mockImplementation(() => undefined);
  const value = "function main(input) {\nreturn {total:1900};\n}";
  const onChange = vi.fn();
  const component = render(<CodeEditor value={value} onChange={onChange} location={{ line: 2, sequence: 1 }} />);
  const editor = EditorView.findFromDOM(component.container.querySelector(".cm-editor")!)!;
  expect(component.container.querySelector(".workflow-code-error")).not.toBeNull();
  act(() => editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: "" } }));
  expect(onChange).toHaveBeenCalledExactlyOnceWith("");
  expect(component.container.querySelector(".workflow-code-error")).toBeNull();
  component.rerender(<CodeEditor value="" onChange={onChange} />);
  component.rerender(<CodeEditor value={value} onChange={onChange} />);
  expect(editor.state.doc.toString()).toBe(value);
  expect(onChange).toHaveBeenCalledTimes(1);
});
